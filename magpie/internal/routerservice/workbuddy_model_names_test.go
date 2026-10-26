package routerservice

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/yetone/magpie/internal/routerconfig"
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
			models := []string{"group/coding", provider.ID + "/deepseek-chat", "local/spark-x2.5-1.7b"}
			address := server.Listener.Addr().String()
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
				want := map[string]string{"group/coding": groupName, provider.ID + "/deepseek-chat": "deepseek-chat · DeepSeek", "local/spark-x2.5-1.7b": "Spark-X2.5-1.7B"}
				entries := readModels()
				if len(entries) != len(models) {
					t.Fatalf("stale generated entries retained: %v", entries)
				}
				for _, entry := range entries {
					id, _ := entry["id"].(string)
					if entry["name"] != want[id] {
						t.Errorf("WorkBuddy label for %s = %v; want %q", id, entry["name"], want[id])
					}
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
