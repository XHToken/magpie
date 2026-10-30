package routerservice

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/routercloud"
	"github.com/yetone/magpie/internal/routerconfig"
	"github.com/yetone/magpie/internal/routerruntime"
)

type currentPromptInvoker struct {
	t     *testing.T
	texts [][]string
}

type unrelatedPromptInvoker struct {
	currentPromptInvoker
	started chan struct{}
	release chan struct{}
	held    atomic.Int32
}

func (v *unrelatedPromptInvoker) Invoke(ctx context.Context, purpose routerruntime.Purpose, body []byte, consume func(*http.Response) error) error {
	if bytes.Contains(body, []byte("UNRELATED_PROMPT")) {
		v.held.Add(1)
		close(v.started)
		select {
		case <-v.release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return v.currentPromptInvoker.Invoke(ctx, purpose, body, consume)
}

func TestHostCachedPromptDoesNotWaitForUnrelatedScan(t *testing.T) {
	v := &unrelatedPromptInvoker{currentPromptInvoker: currentPromptInvoker{t: t}, started: make(chan struct{}), release: make(chan struct{})}
	opts := redactionTestOptions(t, ModelOptions{Routing: &RoutingOptions{LocalInvoker: v}})
	prepare := func(prompt string) error {
		body, _ := json.Marshal(map[string]any{"model": "fixture", "messages": []any{map[string]string{"role": "user", "content": prompt}}})
		p, err := opts.Redaction.Prepare(context.Background(), RedactionRequest{RequestID: prompt, AppID: "loomy", SourceProtocol: "chat", TargetProtocol: "chat", Operation: "generation", Body: body})
		if err == nil {
			closePreparedCloud(p)
		}
		return err
	}
	const prompt = "请整理联系人：张小测，电话13800001234。"
	if err := prepare(prompt); err != nil {
		t.Fatal(err)
	}
	unrelatedDone := make(chan error, 1)
	go func() { unrelatedDone <- prepare("UNRELATED_PROMPT") }()
	t.Cleanup(func() {
		close(v.release)
		if err := <-unrelatedDone; err != nil {
			t.Error(err)
		}
	})
	select {
	case <-v.started:
	case <-time.After(5 * time.Second):
		t.Fatal("unrelated scan did not start")
	}
	cachedDone := make(chan error, 1)
	go func() { cachedDone <- prepare(prompt) }()
	select {
	case err := <-cachedDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Error("cached prompt waited behind unrelated inference")
		return
	}
	if v.held.Load() != 1 {
		t.Fatal("unrelated scan was not still held")
	}
}

func TestPromptScanWaiterCancellationAndCleanup(t *testing.T) {
	r := &RedactionService{}
	key := [32]byte{1}
	release, err := r.acquirePromptScan(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := r.acquirePromptScan(ctx, key); !errors.Is(err, context.Canceled) {
		t.Fatal("duplicate waiter ignored cancellation", err)
	}
	release()
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.promptGates) != 0 {
		t.Fatal("finished/canceled prompts retained admission gates")
	}
}

func TestLoomyTitleReusesOnlyCurrentHumanPrompt(t *testing.T) {
	const prompt = "请整理联系人：张小测，电话13800001234。"
	v := &currentPromptInvoker{t: t}
	opts := redactionTestOptions(t, ModelOptions{Routing: &RoutingOptions{LocalInvoker: v}})
	layout, _ := fixture(t)
	store := routerconfig.New(layout)
	var calls int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		body, _ := io.ReadAll(r.Body)
		if bytes.Contains(body, []byte("OLD_USER")) || bytes.Contains(body, []byte("SECRET_MEMORY")) || bytes.Contains(body, []byte("13800001234")) || bytes.Contains(body, []byte("user_messages")) {
			t.Error("title scanned context or leaked a user value")
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"整理联系人"}}]}`)
	}))
	defer upstream.Close()
	h := handlerWithModelOptions(Info{Address: "127.0.0.1:13145"}, cloudHTTPManagement, store, opts)
	cloudHTTPBind(t, h, upstream.URL, "chat", "loomy", "fixture")
	apps, err := store.Apps()
	if err != nil {
		t.Fatal(err)
	}
	for _, app := range apps {
		if app.ID == "loomy" {
			if _, err := store.SaveAppWithRedactionCapabilityContext(context.Background(), routerconfig.AppEdit{ID: app.ID, Revision: app.Revision, Name: app.Name, CredentialMode: app.CredentialMode, Cloud: app.Cloud, RedactionEnabled: true}, routerconfig.RedactionAvailable); err != nil {
				t.Fatal(err)
			}
		}
	}
	// Prime the same service with the human request, then replay the actual
	// managed title envelope containing history, memory and a repeated prompt.
	body, _ := json.Marshal(map[string]any{"model": "fixture", "messages": []any{map[string]string{"role": "user", "content": prompt}}})
	p, err := opts.Redaction.Prepare(context.Background(), RedactionRequest{RequestID: "human-prompt", AppID: "loomy", SourceProtocol: "chat", TargetProtocol: "chat", Operation: "generation", Body: body})
	if err != nil {
		t.Fatal(err)
	}
	closePreparedCloud(p)
	envelope, _ := json.Marshal(map[string]any{"api_router_title": 1, "user_messages": []string{"OLD_USER", strings.Replace(titleMemoryFixture, "SECRET_MEMORY_BOOK_RESTORATION", "SECRET_MEMORY", 1) + "\n\n" + prompt}})
	body, _ = json.Marshal(map[string]any{"model": "profile-pinned", "messages": []any{map[string]string{"role": "user", "content": string(envelope)}}})
	r := httptest.NewRequest("POST", "http://127.0.0.1:13145/app/loomy/v1/chat/completions", bytes.NewReader(body))
	r.RemoteAddr = "127.0.0.1:1234"
	r.Header.Set("Authorization", "Bearer host-session")
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Api-Router-Purpose", "chat.title")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 || calls != 1 || !strings.Contains(w.Body.String(), "整理联系人") {
		t.Fatal("title failed", w.Code, w.Body.String(), calls)
	}
	if !reflect.DeepEqual(v.texts, [][]string{{prompt}}) {
		t.Fatal("title rescanned history or JSON wrapper", v.texts)
	}
}

func (v *currentPromptInvoker) Invoke(_ context.Context, _ routerruntime.Purpose, body []byte, consume func(*http.Response) error) error {
	if bytes.Contains(body, []byte(redactionTaskInstruction)) {
		v.t.Fatal("execution instruction entered local privacy recognition")
	}
	var request struct {
		Messages []struct {
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &request); err != nil || len(request.Messages) != 2 {
		v.t.Fatal("invalid recognition request", err)
	}
	var input struct {
		Fields []struct {
			Text string `json:"text"`
		} `json:"fields"`
	}
	if err := json.Unmarshal([]byte(request.Messages[1].Content), &input); err != nil {
		v.t.Fatal(err)
	}
	texts := []string{}
	for _, field := range input.Fields {
		texts = append(texts, field.Text)
	}
	v.texts = append(v.texts, texts)
	return consume(&http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(strictRedactionFixture(body, map[string]string{"张小测": "private_person", "13800001234": "private_phone"}))))})
}

func TestHostOnlyCurrentPromptReachesModelAndRestores(t *testing.T) {
	const prompt = "请整理联系人：张小测，电话13800001234。"
	for _, app := range []string{"workbuddy", "loomy"} {
		for _, targetProtocol := range []string{"chat", "responses", "anthropic"} {
			t.Run(app+"/"+targetProtocol, func(t *testing.T) {
				current := "<system-reminder>NON_USER_CONTEXT</system-reminder>\n\n" + prompt
				if app == "workbuddy" {
					current = "<system-reminder data-role=\"user-context\">NON_USER_CONTEXT</system-reminder>\n<user_query>" + prompt + "</user_query>"
				}
				body, _ := json.Marshal(map[string]any{"model": "fixture", "messages": []any{
					map[string]any{"role": "system", "content": strings.Repeat("NON_USER_SYSTEM ", 1000)},
					map[string]any{"role": "user", "content": strings.Repeat("NON_USER_HISTORY ", 500)},
					map[string]any{"role": "assistant", "content": "NON_USER_ASSISTANT"},
					map[string]any{"role": "user", "content": current},
				}})
				v := &currentPromptInvoker{t: t}
				opts := redactionTestOptions(t, ModelOptions{Routing: &RoutingOptions{LocalInvoker: v}})
				prepareFixtureHistory(t, opts.Redaction, app, "chat", body, 0)
				v.texts = nil
				wire, err := routercloud.PrepareRequestBody(routercloud.Request{Protocol: "chat", Body: body, Target: routercloud.Target{Model: "fixture", Provider: routerconfig.ResolvedProvider{Config: routerconfig.CloudProvider{Protocol: targetProtocol, BaseURL: "http://127.0.0.1/v1"}}}}, false)
				if err != nil {
					t.Fatal(err)
				}
				target, err := wire.UserInputTarget(app)
				if err != nil {
					t.Fatal(err)
				}
				prepared, err := opts.Redaction.Prepare(context.Background(), RedactionRequest{RequestID: "prompt-only", AppID: app, SourceProtocol: "chat", TargetProtocol: targetProtocol, Operation: "generation", SourceBody: body, Body: wire.Body(), UserTarget: target})
				if err != nil {
					t.Fatal(err)
				}
				defer closePreparedCloud(prepared)
				if !bytes.Contains(prepared.Body, []byte(redactionTaskInstruction)) {
					t.Fatal("execution model was not told to use supplied opaque values")
				}
				if !reflect.DeepEqual(v.texts, [][]string{{prompt}}) || prepared.ObservationMeta.NewUserCharacters != 25 || prepared.ObservationMeta.LocalInferenceCalls != 1 {
					t.Fatal("host context or history entered recognition", v.texts, prepared.ObservationMeta)
				}
				if strings.Contains(string(prepared.Body), "张小测") || strings.Contains(string(prepared.Body), "13800001234") || !strings.Contains(string(prepared.Body), "NON_USER_CONTEXT") || !strings.Contains(string(prepared.Body), "NON_USER_HISTORY") {
					t.Fatal("incorrect source replacement or lost cloud context")
				}
				response := []byte(fixtureEchoTokens(prepared.Body, `{"choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"<NAME:1> <PHONE:1>"}}]}`))
				restored, err := prepared.Restore(context.Background(), "chat", "application/json", response)
				if err != nil || !strings.Contains(string(restored), "张小测") || !strings.Contains(string(restored), "13800001234") {
					t.Fatal("prompt response failed to restore", err)
				}
			})
		}
	}
}

func TestHostCurrentPromptCacheIgnoresInjectedContext(t *testing.T) {
	const prompt = "请整理联系人：张小测，电话13800001234。"
	for _, app := range []string{"workbuddy", "loomy"} {
		t.Run(app, func(t *testing.T) {
			v := &currentPromptInvoker{t: t}
			opts := redactionTestOptions(t, ModelOptions{Routing: &RoutingOptions{LocalInvoker: v}})
			for _, contextText := range []string{"FIRST_CONTEXT", "CHANGED_CONTEXT"} {
				current := "<system-reminder>" + contextText + "</system-reminder>\n\n" + prompt
				if app == "workbuddy" {
					current = "<system-reminder data-role=\"user-context\">" + contextText + "</system-reminder><user_query>" + prompt + "</user_query>"
				}
				messages := []any{map[string]any{"role": "user", "content": current}}

				body, _ := json.Marshal(map[string]any{"model": "fixture", "messages": messages})
				p, err := opts.Redaction.Prepare(context.Background(), RedactionRequest{RequestID: contextText, AppID: app, SourceProtocol: "chat", TargetProtocol: "chat", Operation: "generation", Body: body})
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(string(p.Body), contextText) || strings.Contains(string(p.Body), "13800001234") {
					t.Fatal("cache changed context or leaked prompt")
				}
				if contextText == "CHANGED_CONTEXT" && (p.ObservationMeta.NewUserCharacters != 0 || p.ObservationMeta.LocalInferenceCalls != 0 || p.ObservationMeta.ScanDisposition != "persistent_reuse") {
					t.Fatal("identical prompt was scanned again", p.ObservationMeta)
				}
				closePreparedCloud(p)
			}
			if len(v.texts) != 1 {
				t.Fatal("host context invalidated prompt cache", len(v.texts))
			}
		})
	}
}
