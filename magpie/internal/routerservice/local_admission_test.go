package routerservice

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/routerconfig"
	"github.com/yetone/magpie/internal/routerdevice"
	"github.com/yetone/magpie/internal/routermanifest"
	"github.com/yetone/magpie/internal/routerruntime"
)

func TestUnknownDeviceDetectionAllowsExplicitModelLifecycle(t *testing.T) {
	for _, id := range []string{"spark-x2.6-4B", "spark-x2.6-4B-Q8_0"} {
		t.Run(id, func(t *testing.T) {
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
			var snapshot routerdevice.Snapshot
			var probeError error = errors.New("device probe failed")
			local = &LocalModels{Manager: manager, Layout: layout, Catalog: catalog, Detector: routerdevice.New(routerdevice.ProviderFunc(func(context.Context, string) (routerdevice.Snapshot, error) { return snapshot, probeError }))}
			h := handlerWithModelOptions(Info{Service: "api-router", Address: address}, "admission-fixture", store, ModelOptions{LocalRuntime: local})
			request := func(action, body string, expected int) {
				t.Helper()
				r := httptest.NewRequest("POST", "http://"+address+"/api/local-model/"+action, strings.NewReader(body))
				r.RemoteAddr = "127.0.0.1:2345"
				r.Header.Set("Authorization", "Bearer admission-fixture")
				w := httptest.NewRecorder()
				h.ServeHTTP(w, r)
				if w.Code != expected {
					t.Fatalf("%s: got %d, want %d: %s", action, w.Code, expected, w.Body)
				}
			}
			request("select", `{"model":"`+id+`"}`, 200)
			request("enable", `{}`, 200)
			request("install", `{}`, 200)
			if err := manager.CancelInstall(context.Background()); err != nil {
				t.Fatal(err)
			}
			// Verified fixtures stand in for a completed download; no real model is started.
			installer.installed.Store(true)
			if err := manager.RefreshInstallation(context.Background()); err != nil {
				t.Fatal(err)
			}
			request("load", `{}`, 200)
			if factory.calls.Load() != 1 || !manager.Status().Healthy {
				t.Fatal("unknown device information blocked loading")
			}
			request("disable", `{}`, 200)

			// A failed system probe must not hide a confirmed memory shortage.
			probeError = nil
			snapshot.AvailableMemoryBytes = routerdevice.Known(uint64(1) * routerdevice.GiB)
			request("enable", `{}`, 409)
			request("install", `{}`, 409)
			if err := local.admitModel(context.Background(), id, "load", true); err == nil {
				t.Fatal("known memory shortage admitted a load")
			}
			if factory.calls.Load() != 1 {
				t.Fatal("known shortage started another model")
			}
		})
	}
}
