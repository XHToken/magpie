package routerservice

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/yetone/magpie/internal/routerconfig"
	"github.com/yetone/magpie/internal/routermanifest"
	"github.com/yetone/magpie/internal/routerruntime"
)

// The model and upstream transports are isolated HTTP fixtures; no weights,
// device probes, downloads or real inference are used by this regression.
func TestX26VariantsUseExistingGroupRedactionPath(t *testing.T) {
	for _, modelID := range []string{"spark-x2.6-4B", "spark-x2.6-4B-Q8_0"} {
		t.Run(modelID, func(t *testing.T) {
			layout, _ := fixture(t)
			store := routerconfig.New(layout)
			var invalid atomic.Bool
			var localCalls, cloudCalls atomic.Int32
			var upstreamBody atomic.Value
			local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				localCalls.Add(1)
				body, _ := io.ReadAll(r.Body)
				var request struct {
					Model string `json:"model"`
				}
				if json.Unmarshal(body, &request) != nil || request.Model != modelID {
					t.Error("redaction did not use the selected X2.6 variant")
				}
				if invalid.Load() {
					_, _ = io.WriteString(w, `{"choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"invalid"}}]}`)
				} else {
					_, _ = w.Write(strictRedactionFixture(body, map[string]string{"Alice": "private_person", "first@example.test": "private_email"}))
				}
			}))
			defer local.Close()
			cloud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				cloudCalls.Add(1)
				raw, _ := io.ReadAll(r.Body)
				upstreamBody.Store(string(raw))
				var request struct {
					Messages []struct{ Content string } `json:"messages"`
				}
				_ = json.Unmarshal(raw, &request)
				content := ""
				if len(request.Messages) > 0 {
					content = request.Messages[len(request.Messages)-1].Content
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"index": 0, "finish_reason": "stop", "message": map[string]string{"role": "assistant", "content": content}}}})
			}))
			defer cloud.Close()
			catalog := routermanifest.Fixed()
			manager, err := routerruntime.New(context.Background(), routerruntime.Options{
				Catalog: catalog, RuntimeID: routerruntime.PlatformRuntime(catalog, runtime.GOOS, runtime.GOARCH),
				Store: routerruntime.ConfigIntentStore{Store: store}, Installer: integrationInstaller{},
				Factory: integrationFactory{endpoint: local.URL}, ExperimentalRedaction: true,
			})
			if err != nil {
				t.Fatal(err)
			}
			defer manager.Close(context.Background())
			if err := manager.Select(context.Background(), modelID); err != nil {
				t.Fatal(err)
			}
			if err := manager.Enable(context.Background()); err != nil {
				t.Fatal(err)
			}
			localModels := &LocalModels{Manager: manager, Layout: layout, Catalog: catalog}
			redactor := newFixtureRedactionService(localModels, true)
			defer redactor.close()
			h := handlerWithModelOptions(Info{Service: "api-router", Address: "127.0.0.1:13145"}, "mgmt-secret-token", store, ModelOptions{
				LocalRuntime: localModels, Redaction: redactor, ExperimentalRedaction: true, Routing: &RoutingOptions{LocalInvoker: manager},
			})
			provider := saveTestCloudProvider(t, store, "X26RedactionFixture", cloud.URL+"/v1", "fixture")
			if _, err := store.SaveGroup(routerconfig.Group{ID: "x26-redaction", Name: "X26 Redaction", Routing: "order", LocalRedaction: true, Members: []string{"local/" + strings.ToLower(modelID), provider.ID + "/fixture"}}); err != nil {
				t.Fatal(err)
			}
			saveGroupBoundApp(t, store, []string{"group/x26-redaction"})
			input := "Contact Alice at first@example.test."
			call := func(text string) *httptest.ResponseRecorder {
				return doGroupRequest(t, h, "", http.MethodPost, "/app/codex/v1/chat/completions", map[string]any{
					"model": "group/x26-redaction", "messages": []map[string]string{{"role": "user", "content": text}},
				})
			}
			response := call(input)
			if response.Code != http.StatusOK || cloudCalls.Load() != 1 || localCalls.Load() == 0 {
				t.Fatalf("X2.6 group redaction failed: status=%d local=%d upstream=%d body=%s", response.Code, localCalls.Load(), cloudCalls.Load(), response.Body.String())
			}
			outbound, _ := upstreamBody.Load().(string)
			outbound = fixtureLegacyTokens(outbound)
			if strings.Contains(outbound, "Alice") || strings.Contains(outbound, "first@example.test") || !strings.Contains(outbound, "<NAME:1>") || !strings.Contains(outbound, "<EMAIL:1>") {
				t.Fatalf("upstream did not receive redacted values: %s", outbound)
			}
			if !strings.Contains(response.Body.String(), input) {
				t.Fatal("response placeholders were not restored", response.Body.String())
			}
			invalid.Store(true)
			response = call("Contact Alice at second@example.test.")
			if response.Code < 400 || cloudCalls.Load() != 1 {
				t.Fatal("invalid extraction reached upstream", response.Code, cloudCalls.Load())
			}
			if manager.Require(routerruntime.Redaction) == nil || manager.Status().Capabilities[routerruntime.Redaction].State != "unknown" {
				t.Fatal("trial support fabricated formal recognition acceptance")
			}
		})
	}
}
