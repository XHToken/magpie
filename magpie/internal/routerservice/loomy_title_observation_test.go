package routerservice

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/routerconfig"
	"github.com/yetone/magpie/internal/routerledger"
	"github.com/yetone/magpie/internal/routermanifest"
	"github.com/yetone/magpie/internal/routerobserve"
	"github.com/yetone/magpie/internal/routerruntime"
)

// Exercise the real Manager producer and request-bound observer, not synthetic
// completion events: title classification must not discard the executed model.
func TestLoomyLocalTitlesRetainActualModel(t *testing.T) {
	for _, modelID := range []string{"spark-x2.6-4B", "spark-x2.6-4B-Q8_0"} {
		for _, route := range []string{"routed", "group", "local"} {
			t.Run(modelID+"/"+route, func(t *testing.T) {
				local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					var body struct {
						PromptVersion string `json:"prompt_version"`
						Stream        bool   `json:"stream"`
					}
					if json.NewDecoder(r.Body).Decode(&body) != nil {
						t.Error("invalid local request")
					}
					content := "整理会议纪要"
					if body.PromptVersion != "" {
						content = `{"decision":"local"}`
					}
					if body.Stream {
						w.Header().Set("Content-Type", "text/event-stream")
						_, _ = io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"整理会议纪要\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":137,\"completion_tokens\":2,\"total_tokens\":139}}\n\ndata: [DONE]\n\n")
						return
					}
					_ = json.NewEncoder(w).Encode(map[string]any{
						"choices": []any{map[string]any{"index": 0, "message": map[string]string{"role": "assistant", "content": content}, "finish_reason": "stop"}},
						"usage":   map[string]int{"prompt_tokens": 137, "completion_tokens": 2, "total_tokens": 139},
					})
				}))
				defer local.Close()
				cloud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					t.Error("local request unexpectedly reached cloud")
					w.WriteHeader(http.StatusBadGateway)
				}))
				defer cloud.Close()
				h, ledger, store, _ := ledgerHTTPFixture(t, ModelOptions{})
				cloudHTTPBind(t, h, cloud.URL, "chat", "loomy", "fixture-key")
				catalog := routermanifest.Fixed()
				manager, err := routerruntime.New(context.Background(), routerruntime.Options{
					Catalog: catalog, RuntimeID: routerruntime.PlatformRuntime(catalog, runtime.GOOS, runtime.GOARCH),
					Store:     &observationIntentStore{intent: routerruntime.Intent{Enabled: true, Selected: modelID}},
					Installer: integrationInstaller{}, Factory: integrationFactory{endpoint: local.URL},
				})
				if err != nil {
					t.Fatal(err)
				}
				defer manager.Close(context.Background())
				for _, purpose := range []routerruntime.Purpose{routerruntime.Judgment, routerruntime.Execution} {
					if err := manager.CheckCapability(context.Background(), routerruntime.Check{Purpose: purpose, Suite: "title-observation-v1", Run: func(context.Context, string) (string, error) { return "fixture-evidence", nil }}); err != nil {
						t.Fatal(err)
					}
				}
				apps, err := store.Apps()
				if err != nil {
					t.Fatal(err)
				}
				var app routerconfig.AppProfile
				for _, p := range apps {
					if p.ID == "loomy" {
						app = p
					}
				}
				model := "profile-pinned"
				if route != "routed" {
					model = "local/" + modelID
					if route == "group" {
						if _, err := store.SaveGroups([]routerconfig.Group{{ID: "title-hybrid", Name: "Title hybrid", Routing: "smart_split", Members: []string{model, app.Cloud.ProviderID + "/profile-pinned"}}}); err != nil {
							t.Fatal(err)
						}
						model = "group/title-hybrid"
					}
					models := []string{model}
					app, err = store.SaveApp(routerconfig.AppEdit{ID: app.ID, Revision: app.Revision, Name: app.Name, CredentialMode: app.CredentialMode, Cloud: app.Cloud, Models: &models})
					if err != nil {
						t.Fatal(err)
					}
				}
				calls := routerobserve.NewCalls(ledger, 32)
				defer calls.Close()
				h = handlerWithModelOptions(Info{Address: "127.0.0.1:13145"}, cloudHTTPManagement, store, ModelOptions{
					Ledger: ledger, LocalCalls: calls, LocalObserver: calls,
					LocalRuntime: &LocalModels{Manager: manager},
					Routing:      &RoutingOptions{Judge: RuntimeJudge{Manager: manager}, LocalInvoker: manager},
				})
				for _, purpose := range []string{"generation", "chat.title", "native-title"} {
					t.Run(purpose, func(t *testing.T) {
						messages := []map[string]string{{"role": "user", "content": "请整理会议纪要"}}
						if purpose == "native-title" {
							messages = append([]map[string]string{{"role": "system", "content": nativeTitleFixture}}, messages...)
						}
						body, _ := json.Marshal(map[string]any{"model": model, "stream": purpose == "native-title", "messages": messages})
						r := httptest.NewRequest("POST", "http://127.0.0.1:13145/app/loomy/v1/chat/completions", strings.NewReader(string(body)))
						r.RemoteAddr = "127.0.0.1:2345"
						r.Header.Set("Authorization", "Bearer host-session")
						r.Header.Set("Content-Type", "application/json")
						if purpose == "chat.title" {
							r.Header.Set("X-Api-Router-Purpose", purpose)
						}
						w := httptest.NewRecorder()
						h.ServeHTTP(w, r)
						if w.Code != 200 || !strings.Contains(w.Body.String(), "整理会议纪要") {
							t.Fatalf("generation failed: %d %s", w.Code, w.Body.String())
						}
						if err := calls.Flush(context.Background()); err != nil {
							t.Fatal(err)
						}
						detail := providerRequest(t, h, "GET", "/api/requests/"+w.Header().Get("X-Request-ID"), cloudHTTPManagement, nil)
						var got struct {
							Request routerledger.Record `json:"request"`
							Calls   []routerledger.Call `json:"calls"`
						}
						if detail.Code != 200 || json.Unmarshal(detail.Body.Bytes(), &got) != nil {
							t.Fatal("request detail unavailable", detail.Code)
						}
						if got.Request.Destination != "local" || got.Request.Status != "succeeded" || got.Request.LocalModel != modelID || got.Request.LocalCallID == "" || got.Request.Attempts != 0 {
							t.Fatalf("actual local model missing: %+v", got.Request)
						}
						wantCalls := 2
						if route == "local" {
							wantCalls = 1
						}
						if len(got.Calls) != wantCalls {
							t.Fatalf("local calls: got %d want %d", len(got.Calls), wantCalls)
						}
						for _, call := range got.Calls {
							if call.Scope != "business" || call.RequestID != got.Request.RequestID || call.ModelID != modelID || !call.InferenceStarted {
								t.Fatalf("local call attribution lost: %+v", call)
							}
						}
						if got.Request.Usage == nil || got.Request.Usage.TotalTokens == nil || *got.Request.Usage.TotalTokens != 139 {
							t.Fatal("actual execution usage lost")
						}
					})
				}
				// An explicit management check remains diagnostic even with local execution.
				check := providerRequest(t, h, "POST", "/api/apps/check", cloudHTTPManagement, routerconfig.AppEdit{ID: app.ID, Revision: app.Revision, Name: app.Name, CredentialMode: routerconfig.StoredCredential, Cloud: app.Cloud, Models: &app.Models})
				if check.Code != 200 {
					t.Fatalf("connection check failed: %d %s", check.Code, check.Body.String())
				}
				if err := calls.Flush(context.Background()); err != nil {
					t.Fatal(err)
				}
				if records := requestRecords(t, h, ""); len(records) != 3 {
					t.Fatalf("connection check changed business count: %d", len(records))
				}
				summary, err := ledger.Summary(routerledger.Query{})
				if err != nil {
					t.Fatal(err)
				}
				var checkExecutions int
				for _, call := range summary.CheckCalls {
					if call.Purpose == "execution" {
						checkExecutions += call.Total
					}
				}
				if checkExecutions != 1 {
					t.Fatalf("connection check execution was not recorded separately: %d", checkExecutions)
				}
				if status := ledger.Status(); status.ObservationFailures != 0 {
					t.Fatalf("local events were rejected: %+v", status)
				}
			})
		}
	}
}

func TestLoomyTitleRetainsBusinessBudgetAndCloudReserve(t *testing.T) {
	deadline := time.Now().Add(4 * time.Minute)
	parent, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	ctx := routerruntime.WithRuntimeSettings(parent, routerconfig.LongContextRuntimeSettings())
	ctx, options := snapshotRuntimeSettings(ctx, ModelOptions{purpose: "chat.title"})
	if options.Budget != 600*time.Second {
		t.Fatalf("title total budget = %s", options.Budget)
	}
	local, stop := localWorkContext(ctx, options)
	defer stop()
	if actual, _ := local.Deadline(); !actual.Equal(deadline.Add(-60 * time.Second)) {
		t.Fatal("title lost reserved cloud time", actual)
	}
	_, shorter := snapshotRuntimeSettings(ctx, ModelOptions{purpose: "chat.title", Budget: time.Second})
	if shorter.Budget != time.Second {
		t.Fatal("title extended an explicit shorter budget")
	}
	_, check := snapshotRuntimeSettings(ctx, ModelOptions{purpose: "connection_check", Budget: time.Second})
	localCheck, stopCheck := localWorkContext(ctx, check)
	defer stopCheck()
	if actual, _ := localCheck.Deadline(); !actual.Equal(deadline) || check.Budget != time.Second {
		t.Fatal("connection check inherited business budgets")
	}
}
