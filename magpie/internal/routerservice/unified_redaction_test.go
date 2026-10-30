package routerservice

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/routerconfig"
	"github.com/yetone/magpie/internal/routerledger"
	"github.com/yetone/magpie/internal/routermanifest"
	"github.com/yetone/magpie/internal/routerruntime"
)

type heldRedactionInvoker struct{ started chan struct{} }

func (h heldRedactionInvoker) Invoke(ctx context.Context, _ routerruntime.Purpose, _ []byte, _ func(*http.Response) error) error {
	close(h.started)
	<-ctx.Done()
	return ctx.Err()
}

func TestUnifiedRedactionCloseCancelsPreparationAndStopsPipeline(t *testing.T) {
	started := make(chan struct{})
	opts := redactionTestOptions(t, ModelOptions{Routing: &RoutingOptions{LocalInvoker: heldRedactionInvoker{started}}})
	service := opts.Redaction
	finished := make(chan error, 1)
	go func() {
		prepared, err := service.Prepare(context.Background(), RedactionRequest{RequestID: "close-fixture", AppID: "codex", SourceProtocol: "chat", TargetProtocol: "chat", Operation: "generation", Body: []byte(`{"model":"fixture","messages":[{"role":"user","content":"ordinary fixture"}]}`)})
		if prepared.Close != nil {
			_ = prepared.Close()
		}
		finished <- err
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("preparation did not start")
	}
	service.mu.Lock()
	pipeline := service.active
	service.mu.Unlock()
	_ = service.Close()
	select {
	case err := <-finished:
		if err == nil {
			t.Fatal("closed service completed preparation")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("close did not cancel in-flight inference")
	}
	waitPipelineDone(t, pipeline, "closed unified engine")
	_ = service.Close()
}

func TestUnifiedRedactionConcurrentRequestsShareOneBoundPipeline(t *testing.T) {
	opts := redactionTestOptions(t, ModelOptions{Routing: &RoutingOptions{LocalInvoker: &dynamicPayloadInvoker{}}})
	service := opts.Redaction
	const workers = 24
	var wg sync.WaitGroup
	pipelines := make([]any, workers)
	for i := range pipelines {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			p, _, err := service.current()
			if err != nil {
				t.Error(err)
			}
			pipelines[i] = p
		}(i)
	}
	wg.Wait()
	for _, p := range pipelines {
		if p == nil || p != pipelines[0] {
			t.Fatal("service created duplicate bound pipelines")
		}
	}
}

func TestUnifiedRedactionApplicationGroupAndCombinedUseSameEngine(t *testing.T) {
	for _, model := range []string{"spark-x2.6-4B", "spark-x2.6-4B-Q8_0"} {
		t.Run(model, func(t *testing.T) {
			layout, _ := fixture(t)
			var mu sync.Mutex
			var sent []string
			local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write(strictRedactionFixture(body, map[string]string{"Elodie": "private_person", "synthetic@example.test": "private_email"}))
			}))
			defer local.Close()
			cloud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				mu.Lock()
				sent = append(sent, string(body))
				mu.Unlock()
				content, _ := extractLatestUserInput("chat", body)
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"index": 0, "finish_reason": "stop", "message": map[string]any{"role": "assistant", "content": content}}}})
			}))
			defer cloud.Close()
			catalog := routermanifest.Fixed()
			manager, err := routerruntime.New(context.Background(), routerruntime.Options{Catalog: catalog, RuntimeID: routerruntime.PlatformRuntime(catalog, runtime.GOOS, runtime.GOARCH), Store: routerruntime.ConfigIntentStore{Store: routerconfig.New(layout)}, Installer: integrationInstaller{}, Factory: integrationFactory{endpoint: local.URL}, ExperimentalRedaction: true})
			if err != nil {
				t.Fatal(err)
			}
			defer manager.Close(context.Background())
			if err := manager.Select(context.Background(), model); err != nil {
				t.Fatal(err)
			}
			if err := manager.Enable(context.Background()); err != nil {
				t.Fatal(err)
			}
			const original = "Contact Elodie at synthetic@example.test，合同金额88.6万元。"
			for _, scope := range []struct {
				name       string
				app, group bool
			}{{"application", true, false}, {"group", false, true}, {"combined", true, true}} {
				t.Run(scope.name, func(t *testing.T) {
					ledger, err := routerledger.Open(filepath.Join(t.TempDir(), "requests.sqlite"), routerledger.Options{})
					if err != nil {
						t.Fatal(err)
					}
					defer ledger.Close()
					models := &LocalModels{Manager: manager, Layout: layout, Catalog: catalog}
					service := newFixtureRedactionService(models, true)
					defer service.Close()
					h, store := newGroupRoutingEnv(t, ModelOptions{Ledger: ledger, Redaction: service, LocalRuntime: models, ExperimentalRedaction: true, Routing: &RoutingOptions{LocalInvoker: manager}})
					provider := saveTestCloudProvider(t, store, "UnifiedFixture", cloud.URL+"/v1", "fixture")
					if _, err := store.SaveGroup(routerconfig.Group{ID: "unified", Name: "Unified", Routing: "order", LocalRedaction: scope.group, Members: []string{provider.ID + "/fixture", "local/" + strings.ToLower(model)}}); err != nil {
						t.Fatal(err)
					}
					saveGroupBoundApp(t, store, []string{"group/unified"})
					if scope.app {
						apps, err := store.Apps()
						if err != nil {
							t.Fatal(err)
						}
						for _, app := range apps {
							if app.ID == "codex" {
								_, err = store.SetRedactionIntentsContext(context.Background(), []routerconfig.RedactionIntentEdit{{ID: app.ID, Revision: app.Revision, Enabled: true}}, routerconfig.RedactionExperimental)
								if err != nil {
									t.Fatal(err)
								}
							}
						}
					}
					response := doGroupRequest(t, h, "", http.MethodPost, "/app/codex/v1/chat/completions", map[string]any{"model": "group/unified", "messages": []map[string]string{{"role": "user", "content": original}}})
					if response.Code != 200 || !strings.Contains(response.Body.String(), original) {
						t.Fatal("source failed preparation or exact restoration", response.Code, response.Body.String())
					}
					if stats := service.Stats(); stats.Processed != 1 || stats.Blocked != 0 {
						t.Fatal("source duplicated or bypassed shared preparation", stats)
					}
					projection := doGroupRequest(t, h, "mgmt-secret-token", http.MethodGet, "/api/redaction", nil)
					if projection.Code != 200 || !strings.Contains(projection.Body.String(), `"processed":1`) {
						t.Fatal("management omitted shared statistics", projection.Body.String())
					}
					records, err := ledger.List(routerledger.Query{Limit: 10})
					if err != nil || len(records.Records) != 1 {
						t.Fatal("request policy was not persisted", err)
					}
					policy := records.Records[0].Redaction
					if policy == nil || policy.ProfileRequired != scope.app || policy.ActualModelID != model || policy.EngineVersion != "privacy-user-input-v1" || policy.State != "prepared" {
						t.Fatalf("incorrect persisted binding: %+v", policy)
					}
				})
			}
			mu.Lock()
			defer mu.Unlock()
			if len(sent) != 3 {
				t.Fatal("unexpected cloud calls", len(sent))
			}
			for _, body := range sent {
				for _, value := range []string{"Elodie", "synthetic@example.test", "88.6万元"} {
					if strings.Contains(body, value) {
						t.Fatal("private value reached upstream", value)
					}
				}
				if fixtureLegacyTokens(body) != fixtureLegacyTokens(sent[0]) {
					t.Fatal("policy source changed final engine output")
				}
			}
		})
	}
}
