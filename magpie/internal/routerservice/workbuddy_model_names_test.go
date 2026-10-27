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

func TestWorkBuddyModelNamesConnectRenameRetireAndRestore(t *testing.T) {
	for _, arrayRoot := range []bool{false, true} {
		t.Run(map[bool]string{false: "object", true: "array"}[arrayRoot], func(t *testing.T) {
			layout, _ := fixture(t)
			store := routerconfig.New(layout)
			root, state := t.TempDir(), t.TempDir()
			t.Setenv("WORKBUDDY_CONFIG_DIR", root)
			path := filepath.Join(root, "models.json")
			original := []byte(`{"models":[],"keep":true}`)
			if arrayRoot {
				original = []byte(`[]`)
			}
			if err := os.WriteFile(path, original, 0o600); err != nil {
				t.Fatal(err)
			}
			settings := []byte("{\n // user preferences\n \"model\":\"builtin-original\",\"keep\":true\n}\n")
			if err := os.WriteFile(filepath.Join(root, "settings.json"), settings, 0o600); err != nil {
				t.Fatal(err)
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/healthz" {
					t.Errorf("unexpected inference during connection: %s", r.URL.Path)
				}
				io.WriteString(w, `{"service":"api-router","status":"ok"}`)
			}))
			defer server.Close()
			input := providerInputFor("http://127.0.0.1:1/v1")
			input.Name = "DeepSeek"
			provider, err := store.SaveCloudProvider(input)
			if err != nil {
				t.Fatal(err)
			}
			group := routerconfig.Group{ID: "coding", Name: "编程组", Members: []string{provider.ID + "/deepseek-chat"}}
			if _, err := store.SaveGroup(group); err != nil {
				t.Fatal(err)
			}
			models := []string{"group/coding", provider.ID + "/deepseek-chat", "local/spark-x2.6-4b"}
			address := server.Listener.Addr().String()
			if !arrayRoot {
				// Upgrade a previously connected raw-ID catalog in the same journal.
				e := routerhost.Engine{StateDir: state, Adapter: routerhost.WorkBuddyAdapter{}, Checker: routerhost.FastChecker("fixture-management")}
				if _, err := e.ConnectAndCommit(context.Background(), routerhost.Options{Host: "workbuddy", ConfigRoot: root, RouterURL: server.URL, Models: models}, nil); err != nil {
					t.Fatal(err)
				}
			}
			product := handlerWithModelOptions(Info{Service: "api-router", Address: address}, "fixture-management", store, ModelOptions{HostStateDir: state, LocalRuntime: &LocalModels{Catalog: routermanifest.Fixed()}})
			h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				r.Host, r.URL.Host = address, address
				product.ServeHTTP(w, r)
			})
			connect := func() {
				t.Helper()
				w := providerRequest(t, h, "POST", "/api/hosts/auto", "fixture-management", map[string]any{"host": "workbuddy", "enabled": true, "models": models})
				if w.Code != 200 {
					t.Fatalf("connect: %d %s", w.Code, w.Body.String())
				}
			}
			readModels := func() []map[string]any {
				t.Helper()
				raw, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				var entries []map[string]any
				if json.Unmarshal(raw, &entries) != nil {
					var doc struct {
						Models []map[string]any `json:"models"`
					}
					if err := json.Unmarshal(raw, &doc); err != nil {
						t.Fatal(err)
					}
					entries = doc.Models
				}
				return entries
			}
			check := func(groupName string) {
				t.Helper()
				want := map[string]string{"group/coding": groupName, provider.ID + "/deepseek-chat": "deepseek-chat · DeepSeek", "local/spark-x2.6-4b": "Spark-X2.6-4B-Q4_K_M"}
				entries := readModels()
				saved := findSavedApp(context.Background(), store, "workbuddy")
				if len(entries) != len(models) {
					t.Fatalf("stale generated entries retained: %v", entries)
				}
				for _, entry := range entries {
					id, _ := entry["id"].(string)
					target := saved.ModelAliases[id]
					if target == "" || entry["name"] != want[target] || id != entry["name"] {
						t.Errorf("WorkBuddy label must render without an ID suffix: id=%q name=%v target=%q", id, entry["name"], target)
					}
				}
				targets, err := startupPortTargets(context.Background(), store, state)
				if err != nil || len(targets) == 0 {
					t.Fatalf("startup did not retain the readable host selection: %v %v", targets, err)
				}
				for _, target := range targets {
					if target.Host != "workbuddy" || target.Model != entries[0]["id"] {
						t.Fatalf("startup changed the readable host selection: %v", target)
					}
				}
				// A user can select another bound model in WorkBuddy itself. Port
				// synchronization must use that readable ID, not the Profile default.
				settingsPath := filepath.Join(root, "settings.json")
				current, err := os.ReadFile(settingsPath)
				if err != nil {
					t.Fatal(err)
				}
				alternate := entries[len(entries)-1]["id"].(string)
				selected, err := edit.PatchJSON(current, []edit.KV{{Path: "model", Value: "custom-local:" + alternate}}, nil)
				if err != nil || os.WriteFile(settingsPath, selected, 0o600) != nil {
					t.Fatal("cannot select an alternate host model", err)
				}
				targets, err = startupPortTargets(context.Background(), store, state)
				if err != nil || len(targets) == 0 {
					t.Fatal("alternate host selection was not found", err)
				}
				for _, target := range targets {
					if target.Model != alternate {
						t.Fatalf("startup lost the user-selected alias: %q; want %q", target.Model, alternate)
					}
				}
				if err := os.WriteFile(settingsPath, current, 0o600); err != nil {
					t.Fatal(err)
				}
				w := providerRequest(t, h, "GET", "/api/hosts/auto", "fixture-management", nil)
				var status struct {
					Items []struct {
						Host      string `json:"host"`
						Connected bool   `json:"connected"`
					} `json:"items"`
				}
				if json.Unmarshal(w.Body.Bytes(), &status) != nil {
					t.Fatal("invalid status")
				}
				for _, item := range status.Items {
					if item.Host == "workbuddy" && !item.Connected {
						t.Fatal("display labels disconnected the saved model set")
					}
				}
			}
			connect()
			check("编程组")
			group.Name = "日常编程"
			if _, err := store.SaveGroup(group); err != nil {
				t.Fatal(err)
			}
			connect()
			check("日常编程")
			// Switch primary and remove a previously named model.
			models = []string{models[1], models[0]}
			connect()
			check("日常编程")
			w := providerRequest(t, h, "POST", "/api/hosts/auto", "fixture-management", map[string]any{"host": "workbuddy", "enabled": false})
			if w.Code != 200 {
				t.Fatalf("restore: %s", w.Body.String())
			}
			restored, _ := os.ReadFile(path)
			if !bytes.Equal(restored, original) {
				t.Fatal("display labels lost the original models backup")
			}
			restored, _ = os.ReadFile(filepath.Join(root, "settings.json"))
			if !bytes.Equal(restored, settings) {
				t.Fatal("display labels changed original settings")
			}
		})
	}
}
