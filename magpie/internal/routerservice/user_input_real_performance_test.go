//go:build realmodel

package routerservice

import (
	"context"
	"encoding/json"
	"github.com/yetone/magpie/internal/cloudcodec"
	"github.com/yetone/magpie/internal/routerconfig"
	"github.com/yetone/magpie/internal/routermanifest"
	"github.com/yetone/magpie/internal/routerredact"
	"github.com/yetone/magpie/internal/routerruntime"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// Installed artifacts are read-only. All lifecycle calls below belong to this
// test's own Manager, isolated store and owned model process.
func TestRealUserInputPerformanceComparison(t *testing.T) {
	data := os.Getenv("API_ROUTER_REAL_DATA")
	if !filepath.IsAbs(data) {
		t.Fatal("API_ROUTER_REAL_DATA must identify existing installed artifacts")
	}
	layout, _ := fixture(t)
	artifactLayout := layout
	artifactLayout.Models = filepath.Join(data, "models")
	artifactLayout.Runtime = filepath.Join(data, "runtime")
	store := routerconfig.New(layout)
	catalog := routermanifest.Fixed()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	manager, err := routerruntime.New(ctx, routerruntime.Options{Catalog: catalog, RuntimeID: "windows-amd64-vulkan", Store: routerruntime.ConfigIntentStore{Store: store}, Installer: &routerruntime.FileInstaller{Layout: artifactLayout}, Factory: realRedactionFactory{}, ExperimentalRedaction: true})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close(context.Background())
	if err := manager.Select(ctx, "spark-x2.6-4B"); err != nil {
		t.Fatal(err)
	}
	settings := routerconfig.StandardRuntimeSettings()
	settings.ContextTokens = 8192
	if err := manager.UpdateSettings(ctx, settings); err != nil {
		t.Fatal(err)
	}
	if err := manager.Enable(ctx); err != nil {
		t.Fatal(err)
	}

	output := os.Getenv("API_ROUTER_PERF_OUTPUT")
	if !filepath.IsAbs(output) {
		t.Fatal("absolute report path required")
	}
	var mu sync.Mutex
	events := []routerruntime.LocalCallObservation{}
	manager.SetLocalObserver(routerruntime.LocalCallObserverFunc(func(v routerruntime.LocalCallObservation) { mu.Lock(); events = append(events, v); mu.Unlock() }))
	type sample struct {
		Scenario, Mode, State, Error, Disposition string
		Index, UserCharacters, UserChunks, Calls  int
		PreparationMS                             float64
		Events                                    []routerruntime.LocalCallObservation
	}
	rows := []sample{}
	save := func() {
		raw, _ := json.MarshalIndent(map[string]any{"scope": "user-input-v1", "model": "spark-x2.6-4B", "runtime": "windows-amd64-vulkan", "context": 8192, "budgetSeconds": 30, "hotSamplesPerScenario": 20, "coldSamplesPerScenario": 1, "queue": "no synthetic queue hold; ordinary service remains running and may contend for hardware", "rows": rows}, "", "  ")
		if e := os.WriteFile(output, raw, 0600); e != nil {
			t.Fatal(e)
		}
	}
	source := func(scenario string, index int) []byte {
		messages := []any{map[string]any{"role": "user", "content": "邮箱：synthetic@example.test"}}
		root := map[string]any{"model": "fixture", "messages": messages}
		switch scenario {
		case "short-user-large-system":
			root["messages"] = append([]any{map[string]any{"role": "system", "content": strings.Repeat("普通固定系统规则。", 350)}}, messages...)
		case "short-user-large-tools":
			tools := []any{}
			for i := 0; i < 100; i++ {
				tools = append(tools, map[string]any{"type": "function", "function": map[string]any{"name": "tool" + strconv.Itoa(i), "description": "普通工具说明", "parameters": map[string]any{"type": "object", "properties": map[string]any{"value": map[string]any{"type": "string", "description": "普通参数说明"}}}}})
			}
			root["tools"] = tools
		case "history-single-new-user":
			messages = []any{}
			for i := 0; i < 4; i++ {
				messages = append(messages, map[string]any{"role": "user", "content": "第" + strconv.Itoa(i) + "轮普通历史。"}, map[string]any{"role": "assistant", "content": "普通回复。"})
			}
			messages = append(messages, map[string]any{"role": "user", "content": "邮箱：synthetic@example.test"})
			for next := 1; next <= index; next++ {
				messages = append(messages, map[string]any{"role": "user", "content": "继续" + strconv.Itoa(next)})
			}
			root["messages"] = messages
		case "tool-only-callback":
			root["messages"] = []any{map[string]any{"role": "tool", "tool_call_id": "fixture", "content": strings.Repeat("普通工具返回。", 400) + " synthetic@example.test"}}
		case "long-user":
			root["messages"] = []any{map[string]any{"role": "user", "content": strings.Repeat("普通用户粘贴的完整资料。", 350) + "邮箱：synthetic@example.test"}}
		}
		body, _ := json.Marshal(root)
		return body
	}
	for _, scenario := range []string{"short-user-large-system", "short-user-large-tools", "history-single-new-user", "tool-only-callback", "long-user"} {
		for _, mode := range []string{"full-request-baseline", "user-input"} {
			if e := manager.Disable(ctx); e != nil {
				t.Fatal(e)
			}
			if e := manager.Enable(ctx); e != nil {
				t.Fatal(e)
			}
			identity, settings := manager.SnapshotBinding(ctx)
			sp, e := routerredact.NewEngineSpecification(routerredact.ModelBinding{ModelID: identity.ModelID, RuntimeID: identity.RuntimeID, ContextTokens: settings.Effective().ContextTokens, ExecutionPlan: identity.ExecutionPlan}, true)
			if e != nil {
				t.Fatal(e)
			}
			inv, e := manager.NewExperimentalRedactionInvoker(sp.RuntimeFingerprint(), sp.CapabilitySuite())
			if e != nil {
				t.Fatal(e)
			}
			pipeline := routerredact.New(routerredact.Options{Extractor: routerredact.NewHybridExtractor(inv, sp), ReversibleAmounts: true})
			for i := 0; i <= 20; i++ {
				if scenario == "tool-only-callback" && mode == "user-input" && i == 1 {
					seed := routerredact.Input{RequestID: "seed", AppID: "codex", Protocol: routerredact.Chat, Operation: "generation", SessionID: "perf", SessionVerified: true, Versions: sp.Versions(), Body: source("short-user-large-system", 0)}
					seeded, e := pipeline.ScanUserInput(ctx, seed)
					if e == nil {
						seeded.Close()
					} else {
						t.Log("callback seed failed", e)
					}
				}
				mu.Lock()
				events = nil
				mu.Unlock()
				body := source(scenario, i)
				in := routerredact.Input{RequestID: "perf", AppID: "codex", Protocol: routerredact.Chat, Operation: "generation", SessionID: "perf", SessionVerified: scenario == "history-single-new-user" || scenario == "tool-only-callback", Versions: sp.Versions(), Body: body}
				callctx := routerruntime.WithLocalCallContext(ctx, routerruntime.LocalCallContext{Check: true, Mode: routerruntime.LocalCallDiagnostic, Material: "user-input-performance", PromptVersion: sp.Versions().Prompt})
				started := time.Now()
				row := sample{Scenario: scenario, Mode: mode, Index: i, State: "hot"}
				if i == 0 {
					row.State = "cold"
				}
				var err error
				if mode == "full-request-baseline" {
					prepared, e := pipeline.Prepare(callctx, in)
					err = e
					if prepared != nil {
						prepared.Close()
					}
				} else {
					user, e := pipeline.ScanUserInput(callctx, in)
					err = e
					if e == nil {
						view, _ := cloudcodec.NewUserTextView("chat", body)
						target, e := view.BindTarget(body, func(b []byte) ([]byte, error) { return b, nil })
						err = e
						if e == nil {
							prepared, e := user.PrepareTarget(routerredact.Chat, body, target)
							err = e
							if prepared != nil {
								meta, _ := prepared.ObservationMeta()
								row.Disposition = meta.ScanDisposition
								row.UserCharacters = meta.NewUserCharacters
								row.UserChunks = meta.NewUserChunks
								prepared.Close()
							}
						}
						user.Close()
					}
				}
				row.PreparationMS = float64(time.Since(started)) / float64(time.Millisecond)
				if err != nil {
					row.Error = err.Error()
				}
				mu.Lock()
				row.Events = append([]routerruntime.LocalCallObservation(nil), events...)
				mu.Unlock()
				for _, event := range row.Events {
					if event.Attempted {
						row.Calls++
					}
				}
				rows = append(rows, row)
				save()
				t.Logf("scenario=%s mode=%s state=%s sample=%d elapsed=%.1fms calls=%d error=%s", scenario, mode, row.State, i, row.PreparationMS, row.Calls, row.Error)
			}
			pipeline.Close()
		}
	}
	save()
}

func TestRealUserInputQueuedCallback(t *testing.T) {
	data := os.Getenv("API_ROUTER_REAL_DATA")
	if !filepath.IsAbs(data) {
		t.Fatal("API_ROUTER_REAL_DATA must identify existing installed artifacts")
	}
	layout, _ := fixture(t)
	artifactLayout := layout
	artifactLayout.Models = filepath.Join(data, "models")
	artifactLayout.Runtime = filepath.Join(data, "runtime")
	store := routerconfig.New(layout)
	catalog := routermanifest.Fixed()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
	defer cancel()
	manager, err := routerruntime.New(ctx, routerruntime.Options{Catalog: catalog, RuntimeID: "windows-amd64-vulkan", Store: routerruntime.ConfigIntentStore{Store: store}, Installer: &routerruntime.FileInstaller{Layout: artifactLayout}, Factory: realRedactionFactory{}, ExperimentalRedaction: true})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close(context.Background())
	if err := manager.Select(ctx, "spark-x2.6-4B"); err != nil {
		t.Fatal(err)
	}
	settings := routerconfig.StandardRuntimeSettings()
	settings.ContextTokens = 8192
	if err := manager.UpdateSettings(ctx, settings); err != nil {
		t.Fatal(err)
	}
	if err := manager.Enable(ctx); err != nil {
		t.Fatal(err)
	}

	identity, settings := manager.SnapshotBinding(ctx)
	sp, e := routerredact.NewEngineSpecification(routerredact.ModelBinding{ModelID: identity.ModelID, RuntimeID: identity.RuntimeID, ContextTokens: settings.Effective().ContextTokens, ExecutionPlan: identity.ExecutionPlan}, true)
	if e != nil {
		t.Fatal(e)
	}
	inv, e := manager.NewExperimentalRedactionInvoker(sp.RuntimeFingerprint(), sp.CapabilitySuite())
	if e != nil {
		t.Fatal(e)
	}
	pipeline := routerredact.New(routerredact.Options{Extractor: routerredact.NewHybridExtractor(inv, sp), ReversibleAmounts: true})
	defer pipeline.Close()
	in := routerredact.Input{RequestID: "queue-seed", AppID: "codex", Protocol: routerredact.Chat, Operation: "generation", SessionID: "queue-fixture", SessionVerified: true, Versions: sp.Versions(), Body: []byte(`{"model":"m","messages":[{"role":"user","content":"邮箱：synthetic@example.test"}]}`)}
	seeded, e := pipeline.ScanUserInput(ctx, in)
	if e != nil {
		t.Fatal(e)
	}
	seeded.Close()
	held, release, finished := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		finished <- manager.Use(ctx, routerruntime.Redaction, func(c context.Context, _ string) error {
			close(held)
			select {
			case <-release:
				return nil
			case <-c.Done():
				return c.Err()
			}
		})
	}()
	defer func() {
		close(release)
		if e := <-finished; e != nil {
			t.Error(e)
		}
	}()
	select {
	case <-held:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	durations := []float64{}
	in.Body = []byte(`{"model":"m","messages":[{"role":"tool","tool_call_id":"fixture","content":"工具回显 synthetic@example.test"}]}`)
	for i := 0; i < 20; i++ {
		started := time.Now()
		user, e := pipeline.ScanUserInput(ctx, in)
		if e != nil {
			t.Fatal(e)
		}
		view, _ := cloudcodec.NewUserTextView("chat", in.Body)
		target, e := view.BindTarget(in.Body, func(b []byte) ([]byte, error) { return b, nil })
		if e != nil {
			t.Fatal(e)
		}
		p, e := user.PrepareTargetContext(ctx, routerredact.Chat, in.Body, target)
		if e != nil {
			t.Fatal(e)
		}
		meta, _ := p.ObservationMeta()
		if meta.ScanDisposition != "known_values_only" || meta.LocalInferenceCalls != 0 {
			t.Fatal(meta.ScanDisposition, meta.LocalInferenceCalls)
		}
		body, _ := p.Body()
		if strings.Contains(string(body), "synthetic@example.test") {
			t.Fatal("known original escaped")
		}
		p.Close()
		user.Close()
		durations = append(durations, float64(time.Since(started))/float64(time.Millisecond))
	}
	in.Body = []byte(`{"model":"m","messages":[{"role":"user","content":"邮箱：synthetic@example.test"},{"role":"user","content":"new text must wait"}]}`)
	shorter, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, queueErr := pipeline.ScanUserInput(shorter, in)
	if queueErr == nil || queueErr.Error() != "redaction_timeout" {
		t.Fatal("new text bypassed busy scheduler", queueErr)
	}
	report := map[string]any{"heldSlotDuringCallbacks": true, "callbackSamples": 20, "callbackMs": durations, "callbackInferenceCalls": 0, "newUserQueueError": queueErr.Error(), "newUserQueueMs": float64(time.Since(started)) / float64(time.Millisecond), "queueDeadlineMs": 500}
	raw, _ := json.MarshalIndent(report, "", "  ")
	output := os.Getenv("API_ROUTER_QUEUE_OUTPUT")
	if !filepath.IsAbs(output) {
		t.Fatal("absolute queue report required")
	}
	if e := os.WriteFile(output, raw, 0600); e != nil {
		t.Fatal(e)
	}
	t.Log(string(raw))
}
