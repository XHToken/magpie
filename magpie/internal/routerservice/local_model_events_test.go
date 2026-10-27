package routerservice

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/routerconfig"
	"github.com/yetone/magpie/internal/routermanifest"
	"github.com/yetone/magpie/internal/routerprocess"
	"github.com/yetone/magpie/internal/routerruntime"
)

type eventFixtureInstaller struct {
	steps     chan routerruntime.Progress
	finish    chan struct{}
	installed atomic.Bool
}

func (i *eventFixtureInstaller) Verify(context.Context, routermanifest.Model, routermanifest.Runtime) (routerprocess.Spec, error) {
	if !i.installed.Load() {
		return routerprocess.Spec{}, routerruntime.ErrNotInstalled
	}
	return routerprocess.Spec{}, nil
}

func (i *eventFixtureInstaller) Install(ctx context.Context, _ routermanifest.Model, _ routermanifest.Runtime, progress func(routerruntime.Progress)) (routerprocess.Spec, error) {
	for {
		select {
		case p := <-i.steps:
			progress(p)
		case <-i.finish:
			i.installed.Store(true)
			return routerprocess.Spec{}, nil
		case <-ctx.Done():
			return routerprocess.Spec{}, ctx.Err()
		}
	}
}

func TestLocalModelEventsAuthorizationProgressAndDisconnect(t *testing.T) {
	layout, _ := fixture(t)
	store := routerconfig.New(layout)
	if err := store.Update(func(d routerconfig.Document) error {
		d["runtime"] = json.RawMessage(`{"selected":"spark-x2.6-4B","enabled":true}`)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	installer := &eventFixtureInstaller{steps: make(chan routerruntime.Progress), finish: make(chan struct{})}
	catalog := routermanifest.Fixed()
	manager, err := routerruntime.New(context.Background(), routerruntime.Options{Catalog: catalog, RuntimeID: "windows-amd64-cpu", Store: routerruntime.ConfigIntentStore{Store: store}, Installer: installer, Factory: &managedFixtureFactory{}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Close(context.Background()) })
	server := httptest.NewUnstartedServer(nil)
	h := handlerWithModelOptions(Info{Address: server.Listener.Addr().String()}, "events-fixture", store, ModelOptions{LocalRuntime: &LocalModels{Manager: manager, Catalog: catalog}})
	disconnected := make(chan struct{}, 4)
	server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.ServeHTTP(w, r)
		if r.URL.Path == "/api/local-model/events" && r.Context().Err() != nil {
			disconnected <- struct{}{}
		}
	})
	server.Start()
	t.Cleanup(server.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	request := func(method, path, auth, origin string) *http.Response {
		t.Helper()
		r, err := http.NewRequestWithContext(ctx, method, server.URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		r.Header.Set("Authorization", auth)
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		response, err := server.Client().Do(r)
		if err != nil {
			t.Fatal(err)
		}
		return response
	}
	for _, check := range []struct {
		method, path, auth, origin string
		code                       int
	}{
		{"GET", "/api/local-model/events", "", "", 401},
		{"GET", "/api/local-model/events?token=events-fixture", "Bearer events-fixture", "", 400},
		{"GET", "/api/local-model/%65vents", "Bearer events-fixture", "", 404},
		{"POST", "/api/local-model/events", "Bearer events-fixture", "", 405},
		{"GET", "/api/local-model/events", "Bearer events-fixture", "http://foreign.example", 403},
		{"HEAD", "/api/local-model/events", "Bearer events-fixture", "", 200},
	} {
		response := request(check.method, check.path, check.auth, check.origin)
		response.Body.Close()
		if response.StatusCode != check.code {
			t.Fatalf("%s %s: got %d want %d", check.method, check.path, response.StatusCode, check.code)
		}
	}
	response := request("GET", "/api/local-model/events", "Bearer events-fixture", "")
	defer response.Body.Close()
	if response.StatusCode != 200 || response.Header.Get("Content-Type") != "text/event-stream" || response.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("unexpected stream headers: %d %v", response.StatusCode, response.Header)
	}
	scanner := bufio.NewScanner(response.Body)
	read := func() routerruntime.Status {
		t.Helper()
		for scanner.Scan() {
			if !strings.HasPrefix(scanner.Text(), "data: ") {
				continue
			}
			var event struct {
				SchemaVersion int
				Status        routerruntime.Status
			}
			if err := json.Unmarshal([]byte(strings.TrimPrefix(scanner.Text(), "data: ")), &event); err != nil {
				t.Fatal(err)
			}
			if event.SchemaVersion != 1 {
				t.Fatal("invalid event schema")
			}
			return event.Status
		}
		t.Fatalf("stream ended before status: %v", scanner.Err())
		return routerruntime.Status{}
	}
	if status := read(); status.Installation != "not_installed" {
		t.Fatal(status)
	}
	if err := manager.Install(); err != nil {
		t.Fatal(err)
	}
	installer.steps <- routerruntime.Progress{Downloaded: 25, Total: 100}
	if status := read(); status.Installation != "downloading" || status.Progress.Downloaded != 25 {
		t.Fatal(status)
	}
	installer.steps <- routerruntime.Progress{Downloaded: 75, Total: 100}
	if status := read(); status.Progress.Downloaded != 75 {
		t.Fatal(status)
	}
	// Closing the observer must release the handler without canceling the job.
	response.Body.Close()
	select {
	case <-disconnected:
	case <-ctx.Done():
		t.Fatal("stream handler did not exit on disconnect")
	}
	if status := manager.Status(); status.Installation != "downloading" {
		t.Fatalf("observer canceled download: %+v", status)
	}
	response = request("GET", "/api/local-model/events", "Bearer events-fixture", "")
	defer response.Body.Close()
	scanner = bufio.NewScanner(response.Body)
	if status := read(); status.Progress.Downloaded != 75 {
		t.Fatalf("reconnect lost progress: %+v", status)
	}
	close(installer.finish)
	if status := read(); status.Installation != "installed" {
		t.Fatalf("completion was not pushed: %+v", status)
	}
}
