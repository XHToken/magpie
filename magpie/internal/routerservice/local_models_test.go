package routerservice

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/yetone/magpie/internal/routerconfig"
	"github.com/yetone/magpie/internal/routerdevice"
	"github.com/yetone/magpie/internal/routermanifest"
	"github.com/yetone/magpie/internal/routerprocess"
	"github.com/yetone/magpie/internal/routerruntime"
)

type managedFixtureInstaller struct {
	installed atomic.Bool
	calls     atomic.Int32
}

func (i *managedFixtureInstaller) Verify(context.Context, routermanifest.Model, routermanifest.Runtime) (routerprocess.Spec, error) {
	if !i.installed.Load() {
		return routerprocess.Spec{}, errors.New("missing fixture")
	}
	return routerprocess.Spec{}, nil
}
func (i *managedFixtureInstaller) Install(ctx context.Context, _ routermanifest.Model, _ routermanifest.Runtime, progress func(routerruntime.Progress)) (routerprocess.Spec, error) {
	i.calls.Add(1)
	progress(routerruntime.Progress{Downloaded: 1, Total: 2})
	<-ctx.Done()
	return routerprocess.Spec{}, ctx.Err()
}

type managedFixtureProcess struct {
	done chan struct{}
	once sync.Once
}

func (p *managedFixtureProcess) Endpoint() string             { return "http://127.0.0.1:1" }
func (p *managedFixtureProcess) PID() int                     { return 123 }
func (p *managedFixtureProcess) Done() <-chan struct{}        { return p.done }
func (p *managedFixtureProcess) Err() error                   { return nil }
func (p *managedFixtureProcess) Health(context.Context) error { return nil }
func (p *managedFixtureProcess) Stop(context.Context) error {
	p.once.Do(func() { close(p.done) })
	return nil
}

type managedFixtureFactory struct{ calls atomic.Int32 }

func (f *managedFixtureFactory) Start(context.Context, routerprocess.Spec) (routerprocess.Process, error) {
	f.calls.Add(1)
	return &managedFixtureProcess{done: make(chan struct{})}, nil
}

func TestLocalModelStartupUsesStandardSettingsAndPreservesIntent(t *testing.T) {
	for _, legacy := range []routerconfig.RuntimeSettings{{}, {
		ContextTokens: 65536, JudgmentQueueSeconds: 60, JudgmentInferenceSeconds: 60,
		ExecutionQueueSeconds: 300, ExecutionSeconds: 1800, LoadSeconds: 180,
		RequestSeconds: 3600, CloudReserveSeconds: 10,
	}} {
		t.Run(legacy.Fingerprint(), func(t *testing.T) {
			layout, _ := fixture(t)
			store := routerconfig.New(layout)
			if err := store.Update(func(d routerconfig.Document) error {
				settings, _ := json.Marshal(legacy)
				settingsField := ""
				if !legacy.IsZero() {
					settingsField = `,"settings":` + string(settings[:len(settings)-1]) + `,"futureSetting":19}`
				}
				d["runtime"] = json.RawMessage(`{"selected":"spark-x2.5-4B","enabled":false,"userDisabled":true,"trial":{"judgment":true,"execution":true},"futureRuntime":17` + settingsField + `}`)
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(layout.ConfigFile())
			if err != nil {
				t.Fatal(err)
			}
			local, err := newLocalModels(context.Background(), layout, store)
			if err != nil {
				t.Fatal(err)
			}
			defer local.Manager.Close(context.Background())
			status := local.Manager.Status()
			if status.Settings != routerconfig.LongContextRuntimeSettings() || status.EffectiveSettings != status.Settings {
				t.Fatalf("startup retained manual settings instead of the standard 64K policy: %+v", status.Settings)
			}
			if status.Intent.Enabled || !status.Intent.UserDisabled || status.Intent.Selected != "spark-x2.5-4B" || !status.Intent.Trial.Judgment || !status.Intent.Trial.Execution || status.PID != 0 {
				t.Fatalf("standard settings changed user intent or loaded a process: %+v", status)
			}
			afterStartup, err := os.ReadFile(layout.ConfigFile())
			if err != nil || string(afterStartup) != string(before) {
				t.Fatalf("resolving the policy rewrote configuration at startup: %v", err)
			}
			// An explicit intent save carries the standard policy through the
			// existing coordinator, preserving unknown runtime/settings fields.
			if err := local.Manager.Disable(context.Background()); err != nil {
				t.Fatal(err)
			}
			doc, err := store.Load()
			if err != nil {
				t.Fatal(err)
			}
			var saved struct {
				Settings struct {
					routerconfig.RuntimeSettings
					Future int `json:"futureSetting"`
				} `json:"settings"`
				Future int `json:"futureRuntime"`
			}
			if err := json.Unmarshal(doc["runtime"], &saved); err != nil || saved.Settings.RuntimeSettings != status.Settings || saved.Future != 17 {
				t.Fatalf("migration did not persist just the runtime policy: %+v %v", saved, err)
			}
			if !legacy.IsZero() && saved.Settings.Future != 19 {
				t.Fatal("migration discarded an unknown settings field")
			}
			backup, err := os.ReadFile(layout.ConfigFile() + ".bak")
			if err != nil {
				t.Fatal(err)
			}
			if err := local.Manager.Close(context.Background()); err != nil {
				t.Fatal(err)
			}
			restarted, err := newLocalModels(context.Background(), layout, store)
			if err != nil {
				t.Fatal(err)
			}
			defer restarted.Manager.Close(context.Background())
			after, err := os.ReadFile(layout.ConfigFile() + ".bak")
			if err != nil || string(after) != string(backup) {
				t.Fatalf("repeated startup replaced the migration backup: %v", err)
			}
		})
	}
}

func TestLocalRuntimeSettingsAPIIsReadOnly(t *testing.T) {
	layout, address := fixture(t)
	store := routerconfig.New(layout)
	local, err := newLocalModels(context.Background(), layout, store)
	if err != nil {
		t.Fatal(err)
	}
	defer local.Manager.Close(context.Background())
	h := handlerWithModelOptions(Info{Service: "wefto", Address: address}, "settings-fixture", store, ModelOptions{LocalRuntime: local})
	request := func(method, auth string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://"+address+"/api/local-model/settings", strings.NewReader("{}"))
		r.RemoteAddr = "127.0.0.1:2345"
		r.Header.Set("Authorization", auth)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	for _, method := range []string{"GET", "HEAD"} {
		if w := request(method, ""); w.Code != 401 {
			t.Fatalf("unauthenticated %s: %d", method, w.Code)
		}
		if w := request(method, "Bearer settings-fixture"); w.Code != 200 || method == "HEAD" && w.Body.Len() != 0 {
			t.Fatalf("read settings: %d %s", w.Code, w.Body)
		}
	}
	for _, method := range []string{"POST", "PUT", "DELETE"} {
		if w := request(method, "Bearer settings-fixture"); w.Code != 405 || w.Header().Get("Allow") != "GET, HEAD" {
			t.Fatalf("manual settings mutation accepted: %s %d %s", method, w.Code, w.Body)
		}
	}
	if got := local.Manager.Status(); got.Settings != routerconfig.StandardRuntimeSettings() || got.Intent.Enabled || got.PID != 0 {
		t.Fatalf("reading settings changed standard policy or started the model: %+v", got)
	}
}

func TestLocalModelManagementConsentBoundariesAndCapabilitySeparation(t *testing.T) {
	for _, modelID := range []string{"spark-x2.5-1.7B", "spark-x2.5-4B"} {
		t.Run(modelID, func(t *testing.T) {
			testLocalModelManagement(t, modelID)
		})
	}
}

func testLocalModelManagement(t *testing.T, modelID string) {
	l, address := fixture(t)
	store := routerconfig.New(l)
	if err := store.Update(func(d routerconfig.Document) error { d["redaction"] = json.RawMessage(`{"enabled":true}`); return nil }); err != nil {
		t.Fatal(err)
	}
	installer := &managedFixtureInstaller{}
	factory := &managedFixtureFactory{}
	catalog := routermanifest.Fixed()
	manager, err := routerruntime.New(context.Background(), routerruntime.Options{Catalog: catalog, RuntimeID: "windows-amd64-cpu", Store: routerruntime.ConfigIntentStore{Store: store}, Installer: installer, Factory: factory})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close(context.Background())
	device := routerdevice.New(routerdevice.ProviderFunc(func(context.Context, string) (routerdevice.Snapshot, error) {
		return routerdevice.Snapshot{System: routerdevice.Known(routerdevice.System{OS: "windows", Version: routerdevice.Known("10.0.26200"), Architecture: routerdevice.Known("amd64")}), MemoryBytes: routerdevice.Known(uint64(32) * routerdevice.GiB), DiskFreeBytes: routerdevice.Known(uint64(1) << 40), Backends: routerdevice.Known([]routerdevice.Backend{{Kind: routerdevice.CPU, State: routerdevice.Available}})}, nil
	}))
	h := handlerWithModelOptions(Info{Service: "wefto", Address: address}, "management-fixture", store, ModelOptions{LocalRuntime: &LocalModels{Manager: manager, Detector: device, Layout: l, Catalog: catalog}})
	request := func(method, path, body, auth, origin string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://"+address+path, strings.NewReader(body))
		r.RemoteAddr = "127.0.0.1:2345"
		if auth != "" {
			r.Header.Set("Authorization", auth)
		}
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	auth := "Bearer management-fixture"
	for _, path := range []string{"/api/local-model", "/api/local-model/detect", "/api/local-model/enable", "/api/local-model/install", "/api/local-model/cancel", "/api/local-model/load", "/api/local-model/disable", "/api/local-model/check"} {
		method := "POST"
		if path == "/api/local-model" {
			method = "GET"
		}
		if w := request(method, path, "{}", "", ""); w.Code != 401 {
			t.Fatalf("unauthorized %s: %d", path, w.Code)
		}
	}
	if w := request("POST", "/api/local-model/enable", "{}", auth, "https://foreign.example"); w.Code != 403 {
		t.Fatal(w.Code)
	}
	if w := request("GET", "/api/local-model/enable", "", auth, ""); w.Code != 405 {
		t.Fatal(w.Code)
	}
	if w := request("GET", "/%61pi/local-model", "", auth, ""); w.Code != 404 {
		t.Fatal(w.Code)
	}
	if w := request("POST", "/api/local-model/select", `{"model":"spark-x2.5-1.7B","url":"https://example.invalid"}`, auth, ""); w.Code != 400 {
		t.Fatal(w.Code)
	}
	for _, op := range []struct {
		path, body string
		status     int
	}{
		{"/api/local-model", "", 200}, {"/api/local-model/detect", "{}", 200}, {"/api/local-model/install", "{}", 409}, {"/api/local-model/load", "{}", 409},
		{"/api/local-model/select", `{"model":"` + modelID + `"}`, 200},
	} {
		method := "POST"
		if op.path == "/api/local-model" {
			method = "GET"
		}
		w := request(method, op.path, op.body, auth, "")
		if w.Code != op.status {
			t.Fatalf("%s %d %s", op.path, w.Code, w.Body)
		}
	}
	if installer.calls.Load() != 0 || factory.calls.Load() != 0 || manager.Status().Intent.Enabled {
		t.Fatal("management reads/detection/selection enabled resources")
	}
	for _, path := range []string{"/api/local-model/enable", "/api/local-model/install", "/api/local-model/cancel"} {
		w := request("POST", path, "{}", auth, "")
		if w.Code != 200 {
			t.Fatalf("%s %d %s", path, w.Code, w.Body)
		}
	}
	if manager.Status().Installation != "canceled" {
		t.Fatal(manager.Status())
	}
	installer.installed.Store(true)
	if err := manager.RefreshInstallation(context.Background()); err != nil {
		t.Fatal(err)
	}
	if w := request("POST", "/api/local-model/load", "{}", auth, ""); w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	if w := request("POST", "/api/local-model/health", "{}", auth, ""); w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	if !manager.Status().Healthy || factory.calls.Load() != 1 {
		t.Fatal(manager.Status())
	}
	for _, c := range manager.Status().Capabilities {
		if c.State != "unknown" {
			t.Fatal("HTTP health passed capability")
		}
	}
	if w := request("POST", "/api/local-model/check", "{}", auth, ""); w.Code != 409 {
		t.Fatal(w.Code)
	}
	if w := request("POST", "/api/local-model/disable", "{}", auth, ""); w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	if w := request("POST", "/api/local-model/load", "{}", auth, ""); w.Code != 409 {
		t.Fatal(w.Code)
	}
	d, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	var redaction struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.Unmarshal(d["redaction"], &redaction); err != nil || !redaction.Enabled {
		t.Fatal("runtime disabled application redaction", string(d["redaction"]))
	}
	if manager.Status().Intent.Enabled || factory.calls.Load() != 1 {
		t.Fatal("disabled runtime restarted")
	}
	if w := request("HEAD", "/api/local-model", "", auth, ""); w.Code != 200 || w.Body.Len() != 0 {
		t.Fatal(w.Code, w.Body)
	}
}

func TestLocalModelTierEnablementBoundaries(t *testing.T) {
	l, address := fixture(t)
	store := routerconfig.New(l)
	installer := &managedFixtureInstaller{}
	factory := &managedFixtureFactory{}
	catalog := routermanifest.Fixed()
	manager, err := routerruntime.New(context.Background(), routerruntime.Options{Catalog: catalog, RuntimeID: "windows-amd64-cpu", Store: routerruntime.ConfigIntentStore{Store: store}, Installer: installer, Factory: factory})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close(context.Background())

	var snap routerdevice.Snapshot
	device := routerdevice.New(routerdevice.ProviderFunc(func(context.Context, string) (routerdevice.Snapshot, error) {
		return snap, nil
	}))
	h := handlerWithModelOptions(Info{Service: "wefto", Address: address}, "tier-fixture", store, ModelOptions{LocalRuntime: &LocalModels{Manager: manager, Detector: device, Layout: l, Catalog: catalog}})
	auth := "Bearer tier-fixture"
	request := func(path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "http://"+address+path, strings.NewReader(body))
		r.RemoteAddr = "127.0.0.1:2345"
		r.Header.Set("Authorization", auth)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}

	// 16 GiB RAM with sufficient available memory and 8 CPU threads recommends 1.7B (MaxTier: 4B), allowing both 1.7B and 4B.
	snap = routerdevice.Snapshot{
		System:               routerdevice.Known(routerdevice.System{OS: "windows", Version: routerdevice.Known("10.0.26200"), Architecture: routerdevice.Known("amd64"), LogicalCPUs: routerdevice.Known(8)}),
		MemoryBytes:          routerdevice.Known(uint64(16) * routerdevice.GiB),
		AvailableMemoryBytes: routerdevice.Known(uint64(6) * routerdevice.GiB),
		DiskFreeBytes:        routerdevice.Known(uint64(100) * routerdevice.GiB),
		Backends:             routerdevice.Known([]routerdevice.Backend{{Kind: routerdevice.CPU, State: routerdevice.Available}}),
	}
	if w := request("/api/local-model/select", `{"model":"spark-x2.5-4B"}`); w.Code != 200 {
		t.Fatalf("select 4B: %d %s", w.Code, w.Body)
	}
	if w := request("/api/local-model/enable", `{}`); w.Code != 200 {
		t.Fatalf("16 GiB (MaxTier=4B) rejected 4B enable: %d %s", w.Code, w.Body)
	}
	if w := request("/api/local-model/disable", `{}`); w.Code != 200 {
		t.Fatalf("disable 4B: %d %s", w.Code, w.Body)
	}

	// 16 GiB RAM with low available memory (3 GiB < 4.5 GiB) caps MaxTier at 1.7B: 4B enable is rejected, 1.7B enable succeeds.
	snap.AvailableMemoryBytes = routerdevice.Known(uint64(3) * routerdevice.GiB)
	if w := request("/api/local-model/enable", `{}`); w.Code != 409 || !strings.Contains(w.Body.String(), "device_check_not_passed") {
		t.Fatalf("low available memory accepted 4B enable: %d %s", w.Code, w.Body)
	}
	if w := request("/api/local-model/select", `{"model":"spark-x2.5-1.7B"}`); w.Code != 200 {
		t.Fatalf("select 1.7B: %d %s", w.Code, w.Body)
	}
	if w := request("/api/local-model/enable", `{}`); w.Code != 200 {
		t.Fatalf("low available memory (3 GiB >= 2.5 GiB) rejected 1.7B enable: %d %s", w.Code, w.Body)
	}
	if w := request("/api/local-model/disable", `{}`); w.Code != 200 {
		t.Fatalf("disable 1.7B: %d %s", w.Code, w.Body)
	}
}

func TestLocalModelInstallOtherModelWhileRunning(t *testing.T) {
	l, address := fixture(t)
	store := routerconfig.New(l)
	installer := &managedFixtureInstaller{}
	installer.installed.Store(true)
	factory := &managedFixtureFactory{}
	catalog := routermanifest.Fixed()
	manager, err := routerruntime.New(context.Background(), routerruntime.Options{Catalog: catalog, RuntimeID: "windows-amd64-cpu", Store: routerruntime.ConfigIntentStore{Store: store}, Installer: installer, Factory: factory})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close(context.Background())

	device := routerdevice.New(routerdevice.ProviderFunc(func(context.Context, string) (routerdevice.Snapshot, error) {
		return routerdevice.Snapshot{
			System:               routerdevice.Known(routerdevice.System{OS: "windows", Version: routerdevice.Known("10.0.26200"), Architecture: routerdevice.Known("amd64"), LogicalCPUs: routerdevice.Known(8)}),
			MemoryBytes:          routerdevice.Known(uint64(32) * routerdevice.GiB),
			AvailableMemoryBytes: routerdevice.Known(uint64(16) * routerdevice.GiB),
			DiskFreeBytes:        routerdevice.Known(uint64(100) * routerdevice.GiB),
			Backends:             routerdevice.Known([]routerdevice.Backend{{Kind: routerdevice.CPU, State: routerdevice.Available}}),
		}, nil
	}))
	h := handlerWithModelOptions(Info{Service: "wefto", Address: address}, "install-other-fixture", store, ModelOptions{LocalRuntime: &LocalModels{Manager: manager, Detector: device, Layout: l, Catalog: catalog}})
	auth := "Bearer install-other-fixture"
	request := func(path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "http://"+address+path, strings.NewReader(body))
		r.RemoteAddr = "127.0.0.1:2345"
		r.Header.Set("Authorization", auth)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}

	if w := request("/api/local-model/select", `{"model":"spark-x2.5-4B"}`); w.Code != 200 {
		t.Fatalf("select 4B: %d %s", w.Code, w.Body)
	}
	if w := request("/api/local-model/enable", `{}`); w.Code != 200 {
		t.Fatalf("enable 4B: %d %s", w.Code, w.Body)
	}
	if w := request("/api/local-model/load", `{}`); w.Code != 200 {
		t.Fatalf("load 4B: %d %s", w.Code, w.Body)
	}
	if st := manager.Status(); !st.Intent.Enabled || st.Intent.Selected != "spark-x2.5-4B" || st.Runtime != "idle" || !st.Healthy || st.PID != 123 {
		t.Fatalf("expected 4B running: %+v", st)
	}

	// Installing the currently running model must return 409 Conflict (busy).
	if w := request("/api/local-model/install", `{"model":"spark-x2.5-4B"}`); w.Code != 409 {
		t.Fatalf("installing running 4B should return 409: %d %s", w.Code, w.Body)
	}

	// Installing the other catalog model (1.7B) while 4B is running must succeed without disabling or stopping 4B.
	if w := request("/api/local-model/install", `{"model":"spark-x2.5-1.7B"}`); w.Code != 200 {
		t.Fatalf("install 1.7B while 4B running: %d %s", w.Code, w.Body)
	}
	st := manager.Status()
	if !st.Intent.Enabled || st.Intent.Selected != "spark-x2.5-4B" || st.Runtime != "idle" || !st.Healthy || st.PID != 123 {
		t.Fatalf("installing 1.7B disturbed running 4B: %+v", st)
	}
	if st.Installation != "downloading" || st.InstallingModel != "spark-x2.5-1.7B" {
		t.Fatalf("expected 1.7B downloading status: %+v", st)
	}
	if w := request("/api/local-model/cancel", `{}`); w.Code != 200 {
		t.Fatalf("cancel 1.7B install: %d %s", w.Code, w.Body)
	}
	if st := manager.Status(); !st.Intent.Enabled || st.Intent.Selected != "spark-x2.5-4B" || st.Runtime != "idle" || !st.Healthy || st.PID != 123 {
		t.Fatalf("canceling 1.7B install disturbed running 4B: %+v", st)
	}
}

func TestStartupAutoLoadCannotUndoUserDisableDuringDeviceCheck(t *testing.T) {
	layout, _ := fixture(t)
	store := routerconfig.New(layout)
	installer := &managedFixtureInstaller{}
	installer.installed.Store(true)
	factory := &managedFixtureFactory{}
	catalog := routermanifest.Fixed()
	manager, err := routerruntime.New(context.Background(), routerruntime.Options{Catalog: catalog, RuntimeID: "windows-amd64-cpu", Store: routerruntime.ConfigIntentStore{Store: store}, Installer: installer, Factory: factory})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close(context.Background())
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	device := routerdevice.New(routerdevice.ProviderFunc(func(ctx context.Context, _ string) (routerdevice.Snapshot, error) {
		once.Do(func() { close(entered) })
		select {
		case <-release:
		case <-ctx.Done():
			return routerdevice.Snapshot{}, ctx.Err()
		}
		return routerdevice.Snapshot{
			System:      routerdevice.Known(routerdevice.System{OS: "windows", Version: routerdevice.Known("10.0.26200"), Architecture: routerdevice.Known("amd64"), LogicalCPUs: routerdevice.Known(8)}),
			MemoryBytes: routerdevice.Known(uint64(16) * routerdevice.GiB), AvailableMemoryBytes: routerdevice.Known(uint64(8) * routerdevice.GiB),
			DiskFreeBytes: routerdevice.Known(uint64(100) * routerdevice.GiB), Backends: routerdevice.Known([]routerdevice.Backend{{Kind: routerdevice.CPU, State: routerdevice.Available}}),
		}, nil
	}))
	local := &LocalModels{Manager: manager, Detector: device, Layout: layout, Catalog: catalog}
	go func() { defer close(done); local.autoLoadInstalled(ctx) }()
	defer func() { cancel(); <-done }()
	<-entered
	if err := manager.Disable(context.Background()); err != nil {
		t.Fatal(err)
	}
	close(release)
	<-done
	status := manager.Status()
	if status.Intent.Enabled || !status.Intent.UserDisabled || status.PID != 0 || factory.calls.Load() != 0 {
		t.Fatalf("startup overrode explicit user disable: %+v starts=%d", status, factory.calls.Load())
	}
}

func TestLocalModelStartupAutoLoadAndUserDisablePersistence(t *testing.T) {
	l, address := fixture(t)
	store := routerconfig.New(l)
	installer := &managedFixtureInstaller{}
	installer.installed.Store(true)
	factory := &managedFixtureFactory{}
	catalog := routermanifest.Fixed()
	device := routerdevice.New(routerdevice.ProviderFunc(func(context.Context, string) (routerdevice.Snapshot, error) {
		return routerdevice.Snapshot{
			System:               routerdevice.Known(routerdevice.System{OS: "windows", Version: routerdevice.Known("10.0.26200"), Architecture: routerdevice.Known("amd64"), LogicalCPUs: routerdevice.Known(8)}),
			MemoryBytes:          routerdevice.Known(uint64(16) * routerdevice.GiB),
			AvailableMemoryBytes: routerdevice.Known(uint64(8) * routerdevice.GiB),
			DiskFreeBytes:        routerdevice.Known(uint64(100) * routerdevice.GiB),
			Backends:             routerdevice.Known([]routerdevice.Backend{{Kind: routerdevice.CPU, State: routerdevice.Available}}),
		}, nil
	}))
	trialChecks := []routerruntime.Check{
		{
			Purpose: routerruntime.Judgment,
			Suite:   "trial-judgment-v1",
			Trial:   true,
			Run:     func(context.Context, string) (string, error) { return "ok", nil },
		},
		{
			Purpose: routerruntime.Execution,
			Suite:   "trial-execution-v1",
			Trial:   true,
			Run:     func(context.Context, string) (string, error) { return "ok", nil },
		},
	}

	// 1. First startup with installed models and no prior userDisabled state: auto-loads recommended 1.7B and enables trial.
	manager1, err := routerruntime.New(context.Background(), routerruntime.Options{Catalog: catalog, RuntimeID: "windows-amd64-cpu", Store: routerruntime.ConfigIntentStore{Store: store}, Installer: installer, Factory: factory})
	if err != nil {
		t.Fatal(err)
	}
	defer manager1.Close(context.Background())
	local1 := &LocalModels{Manager: manager1, Detector: device, Layout: l, Catalog: catalog, AutoLoad: true, Checks: trialChecks}
	local1.autoLoadInstalled(context.Background())

	st1 := manager1.Status()
	if !st1.Intent.Enabled || st1.Intent.UserDisabled || st1.Intent.Selected != "spark-x2.5-1.7B" {
		t.Fatalf("expected auto-enabled 1.7B intent, got %+v", st1.Intent)
	}
	if st1.Runtime != routerruntime.Idle || !st1.Healthy || factory.calls.Load() != 1 {
		t.Fatalf("expected loaded healthy runtime on startup, got state=%s healthy=%v calls=%d", st1.Runtime, st1.Healthy, factory.calls.Load())
	}
	if !st1.Capabilities[routerruntime.Judgment].TrialReady || !st1.Capabilities[routerruntime.Execution].TrialReady {
		t.Fatalf("expected trial capabilities ready on startup, got %+v", st1.Capabilities)
	}

	// 2. User explicitly disables local model via HTTP API.
	h1 := handlerWithModelOptions(Info{Service: "wefto", Address: address}, "autoload-fixture", store, ModelOptions{LocalRuntime: local1})
	req := httptest.NewRequest("POST", "http://"+address+"/api/local-model/disable", strings.NewReader("{}"))
	req.RemoteAddr = "127.0.0.1:2345"
	req.Header.Set("Authorization", "Bearer autoload-fixture")
	w := httptest.NewRecorder()
	h1.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("disable local model: %d %s", w.Code, w.Body)
	}
	if st := manager1.Status(); st.Intent.Enabled || !st.Intent.UserDisabled || st.Runtime != routerruntime.Unloaded {
		t.Fatalf("expected userDisabled unloaded state after explicit disable, got %+v", st)
	}

	// 3. Second startup after explicit user disable: must respect userDisabled and stay unloaded.
	manager2, err := routerruntime.New(context.Background(), routerruntime.Options{Catalog: catalog, RuntimeID: "windows-amd64-cpu", Store: routerruntime.ConfigIntentStore{Store: store}, Installer: installer, Factory: factory})
	if err != nil {
		t.Fatal(err)
	}
	defer manager2.Close(context.Background())
	local2 := &LocalModels{Manager: manager2, Detector: device, Layout: l, Catalog: catalog, AutoLoad: true, Checks: trialChecks}
	local2.autoLoadInstalled(context.Background())

	st2 := manager2.Status()
	if st2.Intent.Enabled || !st2.Intent.UserDisabled || st2.Runtime != routerruntime.Unloaded || factory.calls.Load() != 1 {
		t.Fatalf("expected startup to respect userDisabled and keep runtime unloaded, got %+v calls=%d", st2, factory.calls.Load())
	}

	// 4. User manually re-enables local model via HTTP API; subsequent startup auto-loads again.
	h2 := handlerWithModelOptions(Info{Service: "wefto", Address: address}, "autoload-fixture", store, ModelOptions{LocalRuntime: local2})
	reqEnable := httptest.NewRequest("POST", "http://"+address+"/api/local-model/enable", strings.NewReader("{}"))
	reqEnable.RemoteAddr = "127.0.0.1:2345"
	reqEnable.Header.Set("Authorization", "Bearer autoload-fixture")
	wEnable := httptest.NewRecorder()
	h2.ServeHTTP(wEnable, reqEnable)
	if wEnable.Code != 200 {
		t.Fatalf("re-enable local model: %d %s", wEnable.Code, wEnable.Body)
	}
	if st := manager2.Status(); !st.Intent.Enabled || st.Intent.UserDisabled {
		t.Fatalf("expected userDisabled cleared after manual enable, got %+v", st.Intent)
	}

	manager3, err := routerruntime.New(context.Background(), routerruntime.Options{Catalog: catalog, RuntimeID: "windows-amd64-cpu", Store: routerruntime.ConfigIntentStore{Store: store}, Installer: installer, Factory: factory})
	if err != nil {
		t.Fatal(err)
	}
	defer manager3.Close(context.Background())
	local3 := &LocalModels{Manager: manager3, Detector: device, Layout: l, Catalog: catalog, AutoLoad: true, Checks: trialChecks}
	local3.autoLoadInstalled(context.Background())

	st3 := manager3.Status()
	if !st3.Intent.Enabled || st3.Intent.UserDisabled || st3.Runtime != routerruntime.Idle || !st3.Healthy || factory.calls.Load() != 2 {
		t.Fatalf("expected third startup after re-enable to auto-load, got %+v calls=%d", st3, factory.calls.Load())
	}
	if err := manager3.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	manager4, err := routerruntime.New(context.Background(), routerruntime.Options{Catalog: catalog, RuntimeID: "windows-amd64-cpu", Store: routerruntime.ConfigIntentStore{Store: store}, Installer: installer, Factory: factory})
	if err != nil {
		t.Fatal(err)
	}
	defer manager4.Close(context.Background())
	local4 := &LocalModels{Manager: manager4, Detector: device, Layout: l, Catalog: catalog, AutoLoad: true, Checks: trialChecks}
	local4.autoLoadInstalled(context.Background())
	if st := manager4.Status(); !st.Intent.Enabled || st.Intent.UserDisabled || st.Runtime != routerruntime.Idle || !st.Healthy || factory.calls.Load() != 3 {
		t.Fatalf("project shutdown lost the saved enabled choice: %+v starts=%d", st, factory.calls.Load())
	}
}
