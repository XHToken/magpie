package routerredact

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/cloudcodec"
	"github.com/yetone/magpie/internal/dirs"
	"github.com/yetone/magpie/internal/testkeyvault"
)

func historyStore(t *testing.T, l dirs.Layout) *ResultStore {
	t.Helper()
	s, err := OpenResultStoreWithKeyProtector(l, testkeyvault.Protector{})
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func historyInput(t *testing.T, texts ...string) Input {
	t.Helper()
	messages := []any{}
	for _, text := range texts {
		messages = append(messages, map[string]any{"role": "user", "content": text})
	}
	raw, _ := json.Marshal(map[string]any{"model": "fixture", "messages": messages})
	return Input{RequestID: "history", AppID: "workbuddy", AuthenticatedScope: "workbuddy/1", RecognitionMaterial: "fixture-material", Versions: Versions{"model", "prompt", "config"}, Protocol: Chat, Operation: "generation", Body: raw}
}
func historyTarget(t *testing.T, s *UserSnapshot, in Input) *Prepared {
	t.Helper()
	v, err := cloudcodec.NewUserHistoryViewForApp(string(in.Protocol), in.Body, in.AppID)
	if err != nil {
		t.Fatal(err)
	}
	target, err := v.BindTarget(in.Body, func(b []byte) ([]byte, error) { return b, nil })
	if err != nil {
		t.Fatal(err)
	}
	out, err := s.PrepareTargetContext(context.Background(), in.Protocol, in.Body, target)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(out.Close)
	return out
}

func TestHistoryRestartEvictionAndIncrementalWork(t *testing.T) {
	l := dirs.Layout{State: t.TempDir()}
	store := historyStore(t, l)
	var seen [][]string
	detector := ExtractorFunc(func(ctx context.Context, in Extraction) (Result, error) {
		seen = append(seen, append([]string(nil), in.Texts...))
		return synthetic("13800001234").Extract(ctx, in)
	})
	p := New(Options{Results: store, Extractor: detector})
	in := historyInput(t, "电话13800001234")
	s, err := p.ScanUserInput(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	first := historyTarget(t, s, in)
	firstMapping, _ := first.mappingCopy()
	s.Close()
	p.Clear()
	p.Sweep()
	p.Close()
	store.Close()
	store = historyStore(t, l)
	defer store.Close()
	p = New(Options{Results: store, Extractor: detector, CacheEntries: 1, CacheBytes: 1})
	defer p.Close()
	in = historyInput(t, "电话13800001234", "继续")
	s, err = p.ScanUserInput(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	out := historyTarget(t, s, in)
	raw, _ := out.Body()
	if bytes.Contains(raw, []byte("13800001234")) || !reflect.DeepEqual(seen, [][]string{{"电话13800001234"}, {"继续"}}) {
		t.Fatalf("history leaked or rescanned: %v", seen)
	}
	if s.meta.HistoryReused != 1 {
		t.Fatal(s.meta)
	}
	for old := range firstMapping {
		restored, e := out.RestoreText(old)
		if e != nil || restored != old {
			t.Fatal("cross-request restoration authority")
		}
	}
	// Cache operations have no authority to delete reliable evidence.
	p.Clear()
	replay, err := p.ScanUserInput(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	defer replay.Close()
	if len(seen) != 2 || replay.meta.ReusedUserFragments != 2 {
		t.Fatal("eviction rescanned history", seen, replay.meta)
	}
}

func TestUnknownHistoryInventoryPrecedesEveryInference(t *testing.T) {
	store := historyStore(t, dirs.Layout{State: t.TempDir()})
	defer store.Close()
	calls := 0
	p := New(Options{Results: store, Extractor: ExtractorFunc(func(context.Context, Extraction) (Result, error) {
		calls++
		return Result{}, errors.New("must not scan")
	})})
	defer p.Close()
	in := historyInput(t, "unknown old 1", "unknown old 2", "new")
	_, err := p.ScanUserInput(context.Background(), in)
	var missing *HistoryRequired
	if !errors.As(err, &missing) || missing.Messages != 2 || calls != 0 {
		t.Fatal("history not inventoried before inference", err, calls)
	}
}

func TestHistoryExplicitBoundedPreparationAndEdit(t *testing.T) {
	store := historyStore(t, dirs.Layout{State: t.TempDir()})
	defer store.Close()
	var seen [][]string
	p := New(Options{Results: store, Extractor: ExtractorFunc(func(ctx context.Context, in Extraction) (Result, error) {
		runes := 0
		for _, text := range in.Texts {
			runes += len([]rune(text))
		}
		if len(in.Texts) > 16 || runes > 2048 {
			t.Error("unbounded batch")
		}
		seen = append(seen, in.Texts)
		return synthetic("13800001234").Extract(ctx, in)
	})})
	defer p.Close()
	in := historyInput(t, strings.Repeat("文本 ", 1500)+"13800001234", "current")
	task, err := p.NewHistoryTask(context.Background(), in, []int{0})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = task.Step(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	if task.Progress().Done {
		t.Fatal("long message prematurely complete")
	}
	// An interrupted task cannot authorize a partially covered message.
	if _, err = p.ScanUserInput(context.Background(), in); err == nil {
		t.Fatal("partial result authorized egress")
	}
	for !task.Progress().Done {
		if _, err = task.Step(context.Background(), p); err != nil {
			t.Fatal(err)
		}
	}
	before := len(seen)
	s, err := p.ScanUserInput(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	if len(seen) != before+1 || seen[len(seen)-1][0] != "current" {
		t.Fatal("prepared history rescanned")
	}
	resumed, err := p.NewHistoryTask(context.Background(), in, []int{0})
	if err != nil || !resumed.Progress().Done {
		t.Fatal("completed work not resumed", err)
	}
	// Unknown edits require explicit processing of the entire changed message.
	edited := historyInput(t, "edited complete message", "current")
	if _, err = p.ScanUserInput(context.Background(), edited); err == nil {
		t.Fatal("unverified edit silently scanned")
	}
	editTask, err := p.NewHistoryTask(context.Background(), edited, []int{0})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = editTask.Step(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	if got := seen[len(seen)-1]; !reflect.DeepEqual(got, []string{"edited complete message"}) {
		t.Fatal(got)
	}
}

func TestHistoryKnownValuesCoverFinalBodyAndControlFields(t *testing.T) {
	store := historyStore(t, dirs.Layout{State: t.TempDir()})
	defer store.Close()
	calls := 0
	p := New(Options{Results: store, Extractor: ExtractorFunc(func(ctx context.Context, in Extraction) (Result, error) {
		calls++
		return synthetic("13800001234").Extract(ctx, in)
	})})
	defer p.Close()
	in := historyInput(t, "电话13800001234")
	s, err := p.ScanUserInput(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	for _, suffix := range []string{
		`,{"role":"assistant","content":"<PHONE:13800001234>"}`,
		`,{"role":"tool","tool_call_id":"call","content":"13800001234 new-secret"}`,
		`,{"role":"assistant","tool_calls":[{"id":"call","type":"function","function":{"name":"f","arguments":"{\"value\":\"\\u0031\\u0033\\u0038\\u0030\\u0030\\u0030\\u0030\\u0031\\u0032\\u0033\\u0034\"}"}}]}`,
	} {
		in.Body = []byte(`{"model":"fixture","messages":[{"role":"system","content":"13800001234"},{"role":"user","content":"电话13800001234"}` + suffix + `]}`)
		s, err = p.ScanUserInput(context.Background(), in)
		if err != nil {
			t.Fatal(err)
		}
		out := historyTarget(t, s, in)
		s.Close()
		body, _ := out.Body()
		if bytes.Contains(body, []byte("13800001234")) || calls != 1 {
			t.Fatal("echo leak or new model call", calls)
		}
		if err = out.VerifyFinalBody(context.Background(), body); err != nil {
			t.Fatal(err)
		}
	}
	for _, extra := range []string{`,"user":"13800001234"`, `,"seed":13800001234`, `,"13800001234":"control"`, `,"tools":[{"type":"function","function":{"name":"13800001234","parameters":{"type":"object"}}}]`} {
		in.Body = []byte(`{"model":"fixture","messages":[{"role":"user","content":"电话13800001234"}]` + extra + `}`)
		s, err = p.ScanUserInput(context.Background(), in)
		if err != nil {
			continue // Invalid protocol/control shapes are already blocked.
		}
		v, _ := cloudcodec.NewUserHistoryViewForApp("chat", in.Body, in.AppID)
		target, e := v.BindTarget(in.Body, func(b []byte) ([]byte, error) { return b, nil })
		if e != nil {
			t.Fatal(e)
		}
		out, e := s.PrepareTarget(Chat, in.Body, target)
		s.Close()
		if e == nil {
			out.Close()
			t.Fatal("control value escaped final check")
		}
	}
}

func TestResultStoreRestartTamperingAndNoPlaintext(t *testing.T) {
	l := dirs.Layout{State: t.TempDir()}
	store := historyStore(t, l)
	ctx := context.Background()
	texts := []string{"unique complete synthetic source 13800001234"}
	result := Result{Complete: true, Entities: []Entity{{0, 33, 44, "13800001234", "PHONE"}}}
	// Use exact coordinates, including a zero-hit record's complete coverage.
	result.Entities[0].Start = strings.Index(texts[0], "13800001234")
	result.Entities[0].End = len(texts[0])
	if err := store.Save(ctx, "app/1", "material", texts, result); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(ctx, "app/1", "material", []string{"zero hit full source"}, Result{Complete: true}); err != nil {
		t.Fatal(err)
	}
	store.Close()
	store = historyStore(t, l)
	defer store.Close()
	loaded, err := store.Load(ctx, "app/1", "material", [][]string{texts, {"zero hit full source"}})
	if err != nil || loaded[0] == nil || loaded[1] == nil || !loaded[1].Complete {
		t.Fatal("restart lost evidence", err)
	}
	for _, scope := range []string{"other/1", "app/2"} {
		got, e := store.Load(ctx, scope, "material", [][]string{texts})
		if e != nil || got[0] != nil {
			t.Fatal("authorization scope crossed")
		}
	}
	for _, file := range []string{resultKeyFile(), "results.sqlite"} {
		raw, e := os.ReadFile(filepath.Join(l.RedactionResults(), file))
		if e != nil {
			t.Fatal(e)
		}
		if bytes.Contains(raw, []byte("13800001234")) || bytes.Contains(raw, []byte(texts[0])) {
			t.Fatal("plaintext on disk")
		}
	}
	key, _ := store.record("app/1", "material", texts)
	var payload []byte
	store.db.QueryRow("SELECT payload FROM results WHERE result_key=?", key).Scan(&payload)
	payload[len(payload)-1] ^= 1
	if _, err = store.db.Exec("UPDATE results SET payload=? WHERE result_key=?", payload, key); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Load(ctx, "app/1", "material", [][]string{texts}); err == nil {
		t.Fatal("unauthenticated record accepted")
	}
}

func TestRecognitionSingleFlightAcrossRequests(t *testing.T) {
	store := historyStore(t, dirs.Layout{State: t.TempDir()})
	defer store.Close()
	var calls atomic.Int32
	p := New(Options{Results: store, Extractor: ExtractorFunc(func(ctx context.Context, in Extraction) (Result, error) {
		calls.Add(1)
		return synthetic("13800001234").Extract(ctx, in)
	})})
	defer p.Close()
	in := historyInput(t, "电话13800001234")
	var wg sync.WaitGroup
	for range 12 {
		wg.Go(func() {
			s, e := p.ScanUserInput(context.Background(), in)
			if e != nil {
				t.Error(e)
			} else {
				s.Close()
			}
		})
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatal("duplicate recognition", calls.Load())
	}
}

func TestHistoryCancellationKeepsPartialResultsUnavailable(t *testing.T) {
	store := historyStore(t, dirs.Layout{State: t.TempDir()})
	defer store.Close()
	started := make(chan struct{})
	p := New(Options{Results: store, Extractor: ExtractorFunc(func(ctx context.Context, _ Extraction) (Result, error) {
		close(started)
		<-ctx.Done()
		return Result{}, ctx.Err()
	})})
	defer p.Close()
	in := historyInput(t, "old", "new")
	task, err := p.NewHistoryTask(context.Background(), in, []int{0})
	if err != nil {
		t.Fatal(err)
	}
	finished := make(chan error, 1)
	go func() { _, err := task.Step(context.Background(), p); finished <- err }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("recognition never started")
	}
	// Progress must remain readable while inference is held.
	if progress := task.Progress(); progress.CompletedMessages != 0 {
		t.Fatal("partial work published")
	}
	task.Cancel()
	select {
	case err := <-finished:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancel did not interrupt inference")
	}
	loaded, err := store.Load(context.Background(), in.AuthenticatedScope, in.RecognitionMaterial, [][]string{{"old"}})
	if err != nil || loaded[0] != nil {
		t.Fatal("partial result survived cancellation", err)
	}
}

func TestHistoryUsesOneTokenPerExactOriginalAcrossMessageCategories(t *testing.T) {
	store := historyStore(t, dirs.Layout{State: t.TempDir()})
	defer store.Close()
	in := historyInput(t, "phone 13800001234", "number 13800001234", "continue")
	for i, text := range []string{"phone 13800001234", "number 13800001234"} {
		category := []string{"PHONE", "GOVERNMENTID"}[i]
		start := strings.Index(text, "13800001234")
		if err := store.Save(context.Background(), in.AuthenticatedScope, in.RecognitionMaterial, []string{text}, Result{Complete: true, Entities: []Entity{{0, start, len(text), "13800001234", category}}}); err != nil {
			t.Fatal(err)
		}
	}
	p := New(Options{Results: store, Extractor: ExtractorFunc(func(context.Context, Extraction) (Result, error) { return Result{Complete: true}, nil })})
	defer p.Close()
	s, err := p.ScanUserInput(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	out := historyTarget(t, s, in)
	mapping, err := out.mappingCopy()
	if err != nil || len(mapping) != 1 {
		t.Fatal("same original acquired conflicting request tokens", err)
	}
}

func TestHistoryEmptyCurrentMessageNeedsNoExtractor(t *testing.T) {
	store := historyStore(t, dirs.Layout{State: t.TempDir()})
	defer store.Close()
	p := New(Options{Results: store})
	defer p.Close()
	in := historyInput(t, "")
	s, err := p.ScanUserInput(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.meta.LocalInferenceCalls != 0 || s.meta.ScanDisposition != "no_user_input" {
		t.Fatal("empty input entered inference")
	}
	stored, err := store.Load(context.Background(), in.AuthenticatedScope, in.RecognitionMaterial, [][]string{{""}})
	if err != nil || stored[0] == nil || !stored[0].Complete {
		t.Fatal("empty result not reliably committed", err)
	}
}

func TestHistoryUsesCommittedWinnerAfterConcurrentPreparation(t *testing.T) {
	store := historyStore(t, dirs.Layout{State: t.TempDir()})
	defer store.Close()
	in := historyInput(t, "Alfa Beta")
	p := New(Options{Results: store, Extractor: ExtractorFunc(func(ctx context.Context, ex Extraction) (Result, error) {
		winner := Result{Complete: true, Entities: []Entity{{0, 5, 9, "Beta", "NAME"}}}
		if err := store.Save(ctx, in.AuthenticatedScope, in.RecognitionMaterial, ex.Texts, winner); err != nil {
			return Result{}, err
		}
		return Result{Complete: true, Entities: []Entity{{0, 0, 4, "Alfa", "NAME"}}}, nil
	})})
	defer p.Close()
	s, err := p.ScanUserInput(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	out := historyTarget(t, s, in)
	mapping, err := out.mappingCopy()
	if err != nil || len(mapping) != 1 {
		t.Fatal(err)
	}
	for _, original := range mapping {
		if original != "Beta" {
			t.Fatal("uncommitted preparation replaced committed evidence")
		}
	}
}

func TestTwentyHistoricalMessagesAndLongToolOutputDoNotIncreaseInference(t *testing.T) {
	store := historyStore(t, dirs.Layout{State: t.TempDir()})
	defer store.Close()
	in := historyInput(t, "new input")
	messages := []any{}
	for i := 0; i < 20; i++ {
		text := fmt.Sprintf("historical input %d", i)
		if err := store.Save(context.Background(), in.AuthenticatedScope, in.RecognitionMaterial, []string{text}, Result{Complete: true}); err != nil {
			t.Fatal(err)
		}
		messages = append(messages, map[string]string{"role": "user", "content": text}, map[string]string{"role": "assistant", "content": strings.Repeat("LONG_ASSISTANT ", 200)})
	}
	messages = append(messages, map[string]string{"role": "user", "content": "new input"})
	in.Body, _ = json.Marshal(map[string]any{"model": "fixture", "messages": messages})
	calls := 0
	p := New(Options{Results: store, Extractor: ExtractorFunc(func(_ context.Context, ex Extraction) (Result, error) {
		calls++
		if !reflect.DeepEqual(ex.Texts, []string{"new input"}) {
			t.Error("historical or assistant text entered inference")
		}
		return Result{Complete: true, LocalInferenceCalls: 1, Chunks: 1}, nil
	})})
	defer p.Close()
	s, err := p.ScanUserInput(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if s.meta.HistoryUserMessages != 20 || s.meta.HistoryReused != 20 || s.meta.NewUserCharacters != 9 || calls != 1 {
		t.Fatal("wrong incremental workload")
	}
	s.Close()
	messages = append(messages, map[string]string{"role": "tool", "tool_call_id": "call", "content": strings.Repeat("new tool secret not recognized ", 4000)})
	in.Body, _ = json.Marshal(map[string]any{"model": "fixture", "messages": messages})
	s, err = p.ScanUserInput(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if calls != 1 || s.meta.LocalInferenceCalls != 0 || s.meta.SecondaryRecognition != "known_values_only" {
		t.Fatal("tool callback entered recognition or misreported scope")
	}
	_ = historyTarget(t, s, in)
}

func TestHistoryRejectsSourceTokensMovedIntoControlPositions(t *testing.T) {
	store := historyStore(t, dirs.Layout{State: t.TempDir()})
	defer store.Close()
	p := New(Options{Results: store, Extractor: synthetic("13800001234")})
	defer p.Close()
	in := historyInput(t, "phone 13800001234")
	s, err := p.ScanUserInput(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	view, err := cloudcodec.NewUserHistoryViewForApp("chat", in.Body, in.AppID)
	if err != nil {
		t.Fatal(err)
	}
	convert := func(body []byte) ([]byte, error) {
		var root map[string]any
		if err := json.Unmarshal(body, &root); err != nil {
			return nil, err
		}
		root["user"] = root["messages"].([]any)[0].(map[string]any)["content"]
		return json.Marshal(root)
	}
	final, err := convert(in.Body)
	if err != nil {
		t.Fatal(err)
	}
	target, err := view.BindTarget(final, convert)
	if err != nil {
		t.Fatal(err)
	}
	out, err := s.PrepareTarget(Chat, final, target)
	if err == nil {
		out.Close()
		t.Fatal("source token in control field authorized outbound request")
	}
}
