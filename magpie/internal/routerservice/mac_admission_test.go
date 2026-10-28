package routerservice

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/routerconfig"
	"github.com/yetone/magpie/internal/routerdevice"
	"github.com/yetone/magpie/internal/routermanifest"
	"github.com/yetone/magpie/internal/routerruntime"
)

func TestMacDownloadIsSeparateFromInferenceAdmission(t *testing.T) {
	for _, tc := range []struct {
		name, id string
		memory   uint64
		enable   int
	}{
		{"Q4 at 4.7 GiB", "spark-x2.6-4B", 47 * routerdevice.GiB / 10, 200},
		{"Q4 below trial floor", "spark-x2.6-4B", 4*routerdevice.GiB - 1, 409},
		{"Q8 is download only", "spark-x2.6-4B-Q8_0", 47 * routerdevice.GiB / 10, 409},
	} {
		t.Run(tc.name, func(t *testing.T) {
			layout, address := fixture(t)
			store := routerconfig.New(layout)
			installer, factory := &managedFixtureInstaller{}, &managedFixtureFactory{}
			catalog := routermanifest.Fixed()
			var local *LocalModels
			manager, err := routerruntime.New(context.Background(), routerruntime.Options{
				Catalog: catalog, RuntimeID: "windows-amd64-cpu", Store: routerruntime.ConfigIntentStore{Store: store}, Installer: installer, Factory: factory,
				BeforeStart: func(ctx context.Context, id, backend string) error {
					return local.admitModel(ctx, id, "load", backend == "cpu")
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = manager.Close(context.Background()) })
			snapshot := routerdevice.Snapshot{
				System:      routerdevice.Known(routerdevice.System{OS: "darwin", Architecture: routerdevice.Known("arm64"), Version: routerdevice.Known("14.0"), LogicalCPUs: routerdevice.Known(8)}),
				MemoryBytes: routerdevice.Known(16 * routerdevice.GiB), AvailableMemoryBytes: routerdevice.Known(tc.memory),
				DiskFreeBytes: routerdevice.Known(100 * routerdevice.GiB), DiskVolume: routerdevice.Known("fixture"),
				Backends: routerdevice.Known([]routerdevice.Backend{{Kind: routerdevice.CPU, State: routerdevice.Available}, {Kind: routerdevice.Metal, State: routerdevice.UnknownAvailability}}),
			}
			local = &LocalModels{Manager: manager, Layout: layout, Catalog: catalog, Detector: routerdevice.New(routerdevice.ProviderFunc(func(context.Context, string) (routerdevice.Snapshot, error) { return snapshot, nil }))}
			h := handlerWithModelOptions(Info{Service: "api-router", Address: address}, "mac-admission-fixture", store, ModelOptions{LocalRuntime: local})
			request := func(action, body string, expected int) {
				t.Helper()
				r := httptest.NewRequest("POST", "http://"+address+"/api/local-model/"+action, strings.NewReader(body))
				r.RemoteAddr = "127.0.0.1:2345"
				r.Header.Set("Authorization", "Bearer mac-admission-fixture")
				w := httptest.NewRecorder()
				h.ServeHTTP(w, r)
				if w.Code != expected {
					t.Fatalf("%s: got %d, want %d: %s", action, w.Code, expected, w.Body)
				}
			}
			request("select", `{"model":"`+tc.id+`"}`, 200)
			request("install", `{}`, 200)
			if manager.Status().Intent.Enabled || factory.calls.Load() != 0 {
				t.Fatal("downloading enabled or started inference")
			}
			if err := manager.CancelInstall(context.Background()); err != nil {
				t.Fatal(err)
			}
			installer.installed.Store(true)
			if err := manager.RefreshInstallation(context.Background()); err != nil {
				t.Fatal(err)
			}
			request("enable", `{}`, tc.enable)
			if tc.enable == 200 {
				request("load", `{}`, 200)
				if !manager.Status().Healthy || factory.calls.Load() != 1 {
					t.Fatal("4.7 GiB Q4 trial did not reach runtime health verification")
				}
				request("disable", `{}`, 200)
			} else if err := local.admitModel(context.Background(), tc.id, "load", true); err == nil {
				t.Fatal("download permission bypassed inference admission")
			}
			// Force a fresh installation budget, independent of verified files.
			installer.installed.Store(false)
			snapshot.DiskFreeBytes = routerdevice.Known(uint64(0))
			request("install", `{"model":"`+tc.id+`"}`, 409)
		})
	}
}
