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

	"github.com/yetone/magpie/internal/routerconfig"
	"github.com/yetone/magpie/internal/routermanifest"
	"github.com/yetone/magpie/internal/routerobserve"
	"github.com/yetone/magpie/internal/routerruntime"
)

// Use the real Manager's captured identity through HTTP, protocol conversion,
// SQLite and management reads. A model switch must not rename an older record.
func TestX26LocalModelRecordsAcrossProtocolsAndSwitch(t *testing.T) {
	for _, protocol := range []string{"chat", "responses", "anthropic"} {
		t.Run(protocol, func(t *testing.T) {
			local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var input struct{ Stream bool }
				if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
					t.Error(err)
				}
				if input.Stream {
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = io.WriteString(w, "data: {\"id\":\"local-reply\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":7,\"completion_tokens\":2,\"total_tokens\":9}}\n\ndata: [DONE]\n\n")
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"id":"local-reply","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":7,"completion_tokens":2,"total_tokens":9}}`)
			}))
			defer local.Close()
			cloud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				t.Error("local request reached cloud")
				w.WriteHeader(http.StatusBadGateway)
			}))
			defer cloud.Close()
			h, ledger, store, _ := ledgerHTTPFixture(t, ModelOptions{})
			appID := map[string]string{"chat": "workbuddy", "responses": "codex", "anthropic": "claude-code"}[protocol]
			cloudHTTPBind(t, h, cloud.URL, "chat", appID, "fixture-key")
			catalog := routermanifest.Fixed()
			manager, err := routerruntime.New(context.Background(), routerruntime.Options{
				Catalog: catalog, RuntimeID: routerruntime.PlatformRuntime(catalog, runtime.GOOS, runtime.GOARCH),
				Store:     &observationIntentStore{intent: routerruntime.Intent{Enabled: true, Selected: "spark-x2.6-4B"}},
				Installer: integrationInstaller{}, Factory: integrationFactory{endpoint: local.URL},
			})
			if err != nil {
				t.Fatal(err)
			}
			defer manager.Close(context.Background())
			calls := routerobserve.NewCalls(ledger, 32)
			defer calls.Close()
			h = handlerWithModelOptions(Info{Address: "127.0.0.1:13145"}, cloudHTTPManagement, store, ModelOptions{
				Ledger: ledger, LocalCalls: calls, LocalObserver: calls, LocalRuntime: &LocalModels{Manager: manager},
			})
			requestsPerModel := 1
			if protocol == "chat" {
				requestsPerModel = 2
			}
			for index, modelID := range []string{"spark-x2.6-4B", "spark-x2.6-4B-Q8_0"} {
				if index > 0 {
					if err := manager.Disable(context.Background()); err != nil {
						t.Fatal(err)
					}
					if err := manager.Select(context.Background(), modelID); err != nil {
						t.Fatal(err)
					}
					if err := manager.Enable(context.Background()); err != nil {
						t.Fatal(err)
					}
				}
				if err := manager.CheckCapability(context.Background(), routerruntime.Check{Purpose: routerruntime.Execution, Suite: "x26-observation-v1", Run: func(context.Context, string) (string, error) { return "fixture-evidence", nil }}); err != nil {
					t.Fatal(err)
				}
				profiles, err := store.Apps()
				if err != nil {
					t.Fatal(err)
				}
				selector := "local/" + strings.ToLower(modelID)
				for _, profile := range profiles {
					if profile.ID == appID {
						models := []string{selector}
						if _, err := store.SaveApp(routerconfig.AppEdit{ID: appID, Revision: profile.Revision, Name: profile.Name, CredentialMode: profile.CredentialMode, Cloud: profile.Cloud, Models: &models}); err != nil {
							t.Fatal(err)
						}
					}
				}
				for _, stream := range []bool{false, true} {
					if stream && protocol != "chat" {
						continue // Cross-protocol local streaming is explicitly unsupported.
					}
					body := cloudHTTPInput(protocol, stream, false)
					body["model"] = selector
					w := cloudHTTPCall(t, h, context.Background(), appID, protocol, body, "Bearer host-session", "")
					if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "hello") {
						t.Fatalf("%s stream=%t failed: %d %s", modelID, stream, w.Code, w.Body.String())
					}
				}
				if err := calls.Flush(context.Background()); err != nil {
					t.Fatal(err)
				}
				rows := requestRecords(t, h, "")
				if len(rows) != requestsPerModel*(index+1) {
					t.Fatalf("business count = %d", len(rows))
				}
				for i, row := range rows {
					wantModel := modelID
					if i >= requestsPerModel {
						wantModel = "spark-x2.6-4B"
					}
					if row.Status != "succeeded" || row.Destination != "local" || row.LocalModel != wantModel || row.LocalCallID == "" || row.Attempts != 0 {
						t.Fatalf("actual model changed or lost: %+v; want %s", row, wantModel)
					}
					storedCalls, err := ledger.Calls(row.RequestID)
					if err != nil || len(storedCalls) != 1 || storedCalls[0].ModelID != wantModel || storedCalls[0].CallID != row.LocalCallID {
						t.Fatalf("execution identity mismatch: %+v %v", storedCalls, err)
					}
				}
			}
			if status := ledger.Status(); status.ObservationFailures != 0 {
				t.Fatalf("local observations rejected: %+v", status)
			}
		})
	}
}
