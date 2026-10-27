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
	"reflect"
	"testing"

	"github.com/tidwall/jsonc"
	"github.com/yetone/magpie/internal/edit"
	"github.com/yetone/magpie/internal/routerconfig"
	"github.com/yetone/magpie/internal/routerhost"
	"github.com/yetone/magpie/internal/routermanifest"
)

func TestLoomyModelNamesThroughConnectUpgradeAndRestore(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(map[bool]string{false: "new", true: "legacy"}[legacy], func(t *testing.T) {
			layout, _ := fixture(t)
			store := routerconfig.New(layout)
			root, state := t.TempDir(), t.TempDir()
			t.Setenv("LOOMY_CONFIG_DIR", root)
			path := filepath.Join(root, "opencode.json")
			original := []byte("{\n // keep user configuration\n \"model\":\"imodel/original\",\"theme\":\"system\"\n}\n")
			if err := os.WriteFile(path, original, 0o600); err != nil {
				t.Fatal(err)
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/healthz" {
					t.Errorf("connecting models invoked inference: %s", r.URL.Path)
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
			group := routerconfig.Group{ID: "2222", Name: "日常编程", Members: []string{provider.ID + "/deepseek-chat"}}
			if _, err := store.SaveGroup(group); err != nil {
				t.Fatal(err)
			}
			mirrorInput := providerInputFor("http://127.0.0.1:2/v1")
			mirrorInput.Name = "DeepSeek mirror"
			mirror, err := store.SaveCloudProvider(mirrorInput)
			if err != nil {
				t.Fatal(err)
			}
			models := []string{"group/2222", provider.ID + "/deepseek-chat", provider.ID + "/deepseek-reasoner", "local/spark-x2.6-4b", mirror.ID + "/deepseek-chat"}
			local := &LocalModels{Catalog: routermanifest.Fixed()}
			address := server.Listener.Addr().String()
			product := handlerWithModelOptions(Info{Service: "api-router", Address: address}, "management-fixture", store, ModelOptions{HostStateDir: state, LocalRuntime: local})
			h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				r.Host, r.URL.Host = address, address
				product.ServeHTTP(w, r)
			})
			if legacy {
				e := routerhost.Engine{StateDir: state, Adapter: routerhost.LoomyAdapter{}, Checker: routerhost.FastChecker("management-fixture")}
				if _, err := e.ConnectAndCommit(context.Background(), routerhost.Options{Host: "loomy", ConfigRoot: root, RouterURL: server.URL, Models: models}, nil); err != nil {
					t.Fatal(err)
				}
			}
			connect := func() {
				t.Helper()
				w := providerRequest(t, h, "POST", "/api/hosts/auto", "management-fixture", map[string]any{"host": "loomy", "enabled": true, "models": models})
				if w.Code != http.StatusOK {
					t.Fatalf("connect failed: %d %s", w.Code, w.Body.String())
				}
			}
			check := func(groupName, chatName string) {
				t.Helper()
				raw, err := os.ReadFile(path)
				var doc struct {
					Model    string `json:"model"`
					Provider map[string]struct {
						Models map[string]struct {
							Name     string `json:"name"`
							ToolCall bool   `json:"tool_call"`
						} `json:"models"`
					} `json:"provider"`
				}
				if err != nil || json.Unmarshal(jsonc.ToJSON(raw), &doc) != nil {
					t.Fatal("invalid host configuration", err)
				}
				if doc.Model != "wefto/"+models[0] || len(doc.Provider["wefto"].Models) != len(models) {
					t.Fatal("display names changed the request IDs or selection")
				}
				want := map[string]string{
					"group/2222":                       groupName,
					provider.ID + "/deepseek-chat":     chatName,
					provider.ID + "/deepseek-reasoner": "deepseek-reasoner · " + provider.Name,
					provider.ID + "/vendor/code.v1":    "vendor/code.v1 · " + provider.Name,
					"local/spark-x2.6-4b":              "Spark-X2.6-4B-Q4_K_M",
					mirror.ID + "/deepseek-chat":       "deepseek-chat · DeepSeek mirror",
				}
				for _, id := range models {
					entry := doc.Provider["wefto"].Models[id]
					if entry.Name != want[id] || !entry.ToolCall {
						t.Errorf("Loomy label for %s = %q; want %q with tool calls", id, entry.Name, want[id])
					}
				}
			}
			connect()
			check("日常编程", "deepseek-chat · DeepSeek")
			group.Name = "编程与排障"
			if _, err := store.SaveGroup(group); err != nil {
				t.Fatal(err)
			}
			connect()
			check("编程与排障", "deepseek-chat · DeepSeek")
			input.ID, input.Revision, input.Name = provider.ID, provider.Revision, "DeepSeek primary"
			provider, err = store.SaveCloudProvider(input)
			if err != nil {
				t.Fatal(err)
			}
			connect()
			check("编程与排障", "deepseek-chat · DeepSeek primary")
			// A user alias is preserved while generated labels still refresh.
			raw, _ := os.ReadFile(path)
			raw, err = edit.PatchJSON(raw, []edit.KV{{Path: "provider.wefto.models." + models[1] + ".name", Value: "我的聊天模型"}}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, raw, 0o600); err != nil {
				t.Fatal(err)
			}
			connect()
			check("编程与排障", "我的聊天模型")
			// Rebuilding the connection for a different primary must keep aliases.
			models = append(models[1:], models[0])
			connect()
			check("编程与排障", "我的聊天模型")
			models = append(models, provider.ID+"/vendor/code.v1")
			connect()
			check("编程与排障", "我的聊天模型")
			before, _ := os.ReadFile(path)
			status := providerRequest(t, h, "GET", "/api/hosts/auto", "management-fixture", nil)
			if status.Code != 200 || !bytes.Contains(status.Body.Bytes(), []byte(`"connected":true`)) {
				t.Fatalf("labels invalidated the connection: %s", status.Body.String())
			}
			after, _ := os.ReadFile(path)
			if !bytes.Equal(before, after) {
				t.Fatal("status inspection rewrote model labels")
			}
			w := providerRequest(t, h, "POST", "/api/hosts/auto", "management-fixture", map[string]any{"host": "loomy", "enabled": false})
			if w.Code != 200 {
				t.Fatalf("restore failed: %s", w.Body.String())
			}
			restored, _ := os.ReadFile(path)
			var originalDoc, restoredDoc map[string]any
			if json.Unmarshal(jsonc.ToJSON(original), &originalDoc) != nil || json.Unmarshal(jsonc.ToJSON(restored), &restoredDoc) != nil || !reflect.DeepEqual(originalDoc, restoredDoc) || !bytes.Contains(restored, []byte("keep user configuration")) {
				t.Fatal("label synchronization lost the original recovery baseline")
			}
		})
	}
}
