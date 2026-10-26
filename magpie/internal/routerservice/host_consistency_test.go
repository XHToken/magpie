package routerservice

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/yetone/magpie/internal/edit"
	"github.com/yetone/magpie/internal/routerconfig"
	"github.com/yetone/magpie/internal/routerhost"
	"github.com/yetone/magpie/internal/routermanifest"
)

func TestAutoHostAcceptsLocalCatalogModelAliases(t *testing.T) {
	layout, _ := fixture(t)
	store := routerconfig.New(layout)
	local := &LocalModels{Catalog: routermanifest.Fixed()}
	if err := validateAutoHostModels(context.Background(), store, []string{"local/spark-x2.5-1.7b"}, local); err != nil {
		t.Fatal("model IDs published by the picker must be accepted:", err)
	}
}

func TestLoomyStatusAllowsIndependentBoundSmallModel(t *testing.T) {
	root := t.TempDir()
	o := routerhost.Options{Host: "loomy", ConfigRoot: root, RouterURL: "http://127.0.0.1:13145", Models: []string{"group/first", "provider/second"}}
	files, err := (routerhost.LoomyAdapter{}).Build(o, []routerhost.FilePlan{{Name: "opencode.json"}, {Name: "api-router-title.mjs"}})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := edit.PatchJSON(files[0].After, []edit.KV{{Path: "model", Value: "wefto/provider/second"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "opencode.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, files[1].Name), files[1].After, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := hostConnectionMatches("loomy", root, o.RouterURL, []string{"provider/second", "group/first"}); err != nil {
		t.Fatal("switching to a bound model incorrectly disconnected Loomy:", err)
	}
	got, _ := os.ReadFile(filepath.Join(root, "opencode.json"))
	if !bytes.Equal(got, raw) {
		t.Fatal("status changed Loomy's small-model selection")
	}
}

func TestAutoHostDoesNotReportOrphanedLoomyAsConnected(t *testing.T) {
	layout, _ := fixture(t)
	store := routerconfig.New(layout)
	root := t.TempDir()
	t.Setenv("LOOMY_CONFIG_DIR", root)
	raw := []byte(`{"model":"api_router/group/666","provider":{"api_router":{"npm":"@ai-sdk/openai-compatible","name":"API Router","options":{"baseURL":"http://127.0.0.1:13145/app/loomy/v1","apiKey":"api-router-local-placeholder","useSessionAuth":false},"models":{"group/666":{"name":"group/666","tool_call":true}}}}}`)
	if err := os.WriteFile(filepath.Join(root, "opencode.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, item := range inspectAutoHosts(context.Background(), store) {
		if item["host"] == "loomy" && (item["connected"] == true || item["enabled"] == true) {
			t.Fatalf("an unconfigured Router Profile was advertised as connected: %+v", item)
		}
	}
	got, _ := os.ReadFile(filepath.Join(root, "opencode.json"))
	if !bytes.Equal(got, raw) {
		t.Fatal("status inspection changed Loomy's configuration")
	}
}

func TestAutoHostRejectsMissingGroupAndRollsBackProfileFailure(t *testing.T) {
	for _, failure := range []string{"missing-group", "profile-write"} {
		t.Run(failure, func(t *testing.T) {
			layout, _ := fixture(t)
			store := routerconfig.New(layout)
			root, state := t.TempDir(), filepath.Join(t.TempDir(), "recovery")
			original := []byte(`{"model":"imodel/default","keep":"user setting"}`)
			file := filepath.Join(root, "opencode.json")
			if err := os.WriteFile(file, original, 0o600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("LOOMY_CONFIG_DIR", root)
			provider, err := store.SaveCloudProvider(providerInputFor("http://127.0.0.1:1/v1"))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := store.SaveGroup(routerconfig.Group{ID: "coding", Name: "Coding", Members: []string{provider.ID + "/model"}}); err != nil {
				t.Fatal(err)
			}
			model := "group/666"
			if failure == "profile-write" {
				model = "group/coding"
				backup := layout.ConfigFile() + ".bak"
				if err := os.Remove(backup); err != nil && !os.IsNotExist(err) {
					t.Fatal(err)
				}
				if err := os.Mkdir(backup, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/healthz" {
					t.Errorf("connection attempted upstream inference: %s", r.URL.Path)
					http.NotFound(w, r)
					return
				}
				_, _ = io.WriteString(w, `{"service":"api-router","status":"ok"}`)
			}))
			defer server.Close()
			raw, _ := json.Marshal(map[string]any{"host": "loomy", "enabled": true, "models": []string{model}})
			req := httptest.NewRequest(http.MethodPost, "/api/hosts/auto", bytes.NewReader(raw))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			hostAutoAPI(w, req, Info{Address: server.Listener.Addr().String()}, "management-fixture", store, state)
			if w.Code < 400 {
				t.Fatalf("invalid connection reported success: %d %s", w.Code, w.Body.String())
			}
			got, _ := os.ReadFile(file)
			if !bytes.Equal(got, original) {
				t.Fatalf("failed connection left Loomy using an unusable Router Profile: %s", got)
			}
			app := findSavedApp(context.Background(), store, "loomy")
			if app == nil || app.Configured || app.Enabled {
				t.Fatalf("failed connection changed the Profile: %+v", app)
			}
		})
	}
}
