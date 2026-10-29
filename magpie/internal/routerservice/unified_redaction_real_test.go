//go:build realmodel

package routerservice

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/routerconfig"
	"github.com/yetone/magpie/internal/routermanifest"
	"github.com/yetone/magpie/internal/routerprocess"
	"github.com/yetone/magpie/internal/routerruntime"
)

// This opt-in test reads installed pinned artifacts. Only config/state and the
// process it creates are owned by the fixture; it never installs or edits them.
type realRedactionFactory struct{}

func (realRedactionFactory) Start(ctx context.Context, spec routerprocess.Spec) (routerprocess.Process, error) {
	spec.GPU = &routerprocess.GPUOptions{Device: "Vulkan0", Layers: -1, ReserveMiB: 2048}
	return (routerprocess.NativeFactory{}).Start(ctx, spec)
}

func TestRealUnifiedRedactionWithInstalledArtifacts(t *testing.T) {
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
	var calls atomic.Int32
	cloud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), "synthetic@example.test") {
			t.Error("synthetic original reached mock upstream")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"<EMAIL:1>"}}]}`)
	}))
	defer cloud.Close()
	models := &LocalModels{Manager: manager, Layout: artifactLayout, Catalog: catalog}
	service := newRedactionService(models, true)
	defer service.Close()
	h, appStore := newGroupRoutingEnv(t, ModelOptions{Redaction: service, LocalRuntime: models, ExperimentalRedaction: true, Routing: &RoutingOptions{LocalInvoker: manager}})
	provider := saveTestCloudProvider(t, appStore, "RealUnifiedFixture", cloud.URL+"/v1", "fixture")
	_, err = appStore.SaveAppWithRedactionCapabilityContext(ctx, routerconfig.AppEdit{ID: "codex", Name: "Codex", Cloud: routerconfig.AppCloud{ProviderID: provider.ID, Model: "fixture"}, CredentialMode: routerconfig.StoredCredential, RedactionEnabled: true}, routerconfig.RedactionExperimental)
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	response := doGroupRequest(t, h, "", http.MethodPost, "/app/codex/v1/chat/completions", map[string]any{"model": provider.ID + "/fixture", "messages": []map[string]string{{"role": "user", "content": "邮箱：synthetic@example.test"}}})
	elapsed := time.Since(started)
	t.Logf("real model HTTP=%d mock-cloud-calls=%d elapsed=%s preparation=%+v", response.Code, calls.Load(), elapsed, service.Stats())
	if response.Code != 200 || calls.Load() != 1 || !strings.Contains(response.Body.String(), "synthetic@example.test") {
		t.Fatal("real extraction/restoration did not complete", response.Code, response.Body.String())
	}
}
