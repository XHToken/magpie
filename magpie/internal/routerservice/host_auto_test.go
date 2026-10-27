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
	"sync/atomic"
	"testing"

	"github.com/yetone/magpie/internal/routerconfig"
	"github.com/yetone/magpie/internal/routerhost"
)

func TestAutoHostConfigRootUsesExplicitDirectoriesAndRejectsAmbiguousLoomy(t *testing.T) {
	workbuddy := filepath.Join(t.TempDir(), "workbuddy")
	t.Setenv("WORKBUDDY_CONFIG_DIR", workbuddy)
	if got, err := autoHostConfigRoot("workbuddy"); err != nil || got != workbuddy {
		t.Fatalf("workbuddy root = %q, %v", got, err)
	}
	loomy := filepath.Join(t.TempDir(), "loomy")
	if err := os.MkdirAll(loomy, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(loomy, "opencode.json"), []byte(`{"provider":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LOOMY_CONFIG_DIR", loomy)
	if got, err := autoHostConfigRoot("loomy"); err != nil || got != loomy {
		t.Fatalf("loomy root = %q, %v", got, err)
	}
	t.Setenv("LOOMY_CONFIG_DIR", filepath.Join(t.TempDir(), "missing"))
	if _, err := autoHostConfigRoot("loomy"); err == nil {
		t.Fatal("missing explicit Loomy root was accepted")
	}
}

func TestHostAutoAPIUpdatesPreviouslyConnectedModel(t *testing.T) {
	for _, host := range []string{"workbuddy", "loomy"} {
		t.Run(host, func(t *testing.T) {
			const token = "management-fixture"
			root := filepath.Join(t.TempDir(), "host")
			state := filepath.Join(t.TempDir(), "recovery")
			if err := os.MkdirAll(root, 0o700); err != nil {
				t.Fatal(err)
			}
			var adapter routerhost.Adapter = routerhost.WorkBuddyAdapter{}
			if host == "loomy" {
				adapter = routerhost.LoomyAdapter{}
				t.Setenv("LOOMY_CONFIG_DIR", root)
				if err := os.WriteFile(filepath.Join(root, "opencode.json"), []byte(`{"model":"original/model"}`), 0o600); err != nil {
					t.Fatal(err)
				}
			} else {
				t.Setenv("WORKBUDDY_CONFIG_DIR", root)
			}
			var probes atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/healthz":
					_, _ = io.WriteString(w, `{"service":"wefto","status":"ok"}`)
				case "/app/" + host + "/v1/chat/completions":
					probes.Add(1)
					http.Error(w, "unexpected model probe", http.StatusInternalServerError)
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			address := server.Listener.Addr().String()
			h := handlerWithModelOptions(Info{Service: "wefto", Address: address}, token, nil, ModelOptions{HostStateDir: state})
			for _, selected := range []string{"deepseek-flash", "spark-x2.5"} {
				raw, _ := json.Marshal(map[string]string{"host": host, "model": selected})
				req := httptest.NewRequest(http.MethodPost, server.URL+"/api/hosts/auto", bytes.NewReader(raw))
				req.Host, req.RemoteAddr = address, "127.0.0.1:1234"
				req.Header.Set("Authorization", "Bearer "+token)
				req.Header.Set("Content-Type", "application/json")
				w := httptest.NewRecorder()
				h.ServeHTTP(w, req)
				if w.Code != http.StatusOK {
					t.Fatalf("saved %s Profile did not update the host model to %s: status %d: %s", host, selected, w.Code, w.Body.String())
				}
				e := routerhost.Engine{StateDir: state, Adapter: adapter}
				p, err := e.Preview(routerhost.Options{Host: host, ConfigRoot: root, RouterURL: server.URL, Model: selected})
				if err != nil {
					t.Fatal("host does not match the saved Profile:", err)
				}
				if p == nil {
					t.Fatal("missing host plan")
				}
			}
			if probes.Load() != 0 {
				t.Fatalf("expected 0 model inference probes during fast auto connect, got %d", probes.Load())
			}
		})
	}
}

func TestHostAutoAPIAppliesWorkBuddyConfigurationAfterRealChecks(t *testing.T) {
	const token = "management-fixture"
	const model = "fixture-model"
	root := filepath.Join(t.TempDir(), "workbuddy")
	state := filepath.Join(t.TempDir(), "recovery")
	t.Setenv("WORKBUDDY_CONFIG_DIR", root)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			_, _ = io.WriteString(w, `{"service":"wefto","status":"ok"}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	address := server.Listener.Addr().String()
	info := Info{Service: "wefto", Address: address}
	h := handlerWithModelOptions(info, token, nil, ModelOptions{HostStateDir: state})
	raw, _ := json.Marshal(map[string]any{"host": "workbuddy", "model": model})
	req := httptest.NewRequest(http.MethodPost, "http://"+address+"/api/hosts/auto", bytes.NewReader(raw))
	req.Host, req.RemoteAddr = address, "127.0.0.1:1234"
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("auto apply status %d: %s", w.Code, w.Body.String())
	}
	var result struct {
		Connected bool `json:"connected"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || !result.Connected {
		t.Fatalf("unexpected auto apply response: %s", w.Body.String())
	}
	if models, err := os.ReadFile(filepath.Join(root, "models.json")); err != nil || len(models) == 0 {
		t.Fatalf("models.json was not written: %v", err)
	}
	if settings, err := os.ReadFile(filepath.Join(root, "settings.json")); err != nil || len(settings) == 0 {
		t.Fatalf("settings.json was not written: %v", err)
	}
}

func TestHostAutoAPIMultiModelConnectInspectAndDisconnectRestore(t *testing.T) {
	layout, _ := fixture(t)
	store := routerconfig.New(layout)
	const token = "management-fixture"
	loomyRoot := filepath.Join(t.TempDir(), "loomy")
	wbRoot := filepath.Join(t.TempDir(), "workbuddy")
	state := filepath.Join(t.TempDir(), "recovery")
	if err := os.MkdirAll(loomyRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(wbRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	origLoomy := []byte("{\n  \"model\": \"imodel/default\",\n  \"small_model\": \"imodel/small\"\n}\n")
	if err := os.WriteFile(filepath.Join(loomyRoot, "opencode.json"), origLoomy, 0o600); err != nil {
		t.Fatal(err)
	}
	origWBSettings := []byte("{\n  \"model\": \"builtin:default\"\n}\n")
	if err := os.WriteFile(filepath.Join(wbRoot, "settings.json"), origWBSettings, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LOOMY_CONFIG_DIR", loomyRoot)
	t.Setenv("WORKBUDDY_CONFIG_DIR", wbRoot)

	var probes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			_, _ = io.WriteString(w, `{"service":"wefto","status":"ok"}`)
			return
		}
		probes.Add(1)
		http.Error(w, "unexpected probe call", http.StatusInternalServerError)
	}))
	defer server.Close()
	address := server.Listener.Addr().String()
	h := handlerWithModelOptions(Info{Service: "wefto", Address: address}, token, store, ModelOptions{HostStateDir: state})

	callJSON := func(method, path string, body any) *httptest.ResponseRecorder {
		t.Helper()
		var rdr io.Reader
		if body != nil {
			b, _ := json.Marshal(body)
			rdr = bytes.NewReader(b)
		}
		req := httptest.NewRequest(method, "http://"+address+path, rdr)
		req.Host, req.RemoteAddr = address, "127.0.0.1:1234"
		req.Header.Set("Authorization", "Bearer "+token)
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		return w
	}

	prov, err := store.SaveCloudProvider(providerInputFor("http://127.0.0.1:1/v1"))
	if err != nil {
		t.Fatal(err)
	}
	models := []string{"group/coding-hybrid", prov.ID + "/gpt-4o", "local/qwen2.5-coder-7b"}
	for _, id := range []string{"coding-hybrid", "111"} {
		if _, err := store.SaveGroup(routerconfig.Group{ID: id, Name: id, Members: []string{prov.ID + "/gpt-4o"}}); err != nil {
			t.Fatal(err)
		}
	}
	if w := callJSON(http.MethodPost, "/api/apps", map[string]any{
		"id": "loomy", "enabled": true, "models": models,
	}); w.Code != http.StatusOK {
		t.Fatalf("save app models without referencePrice failed: %d %s", w.Code, w.Body.String())
	}

	for _, host := range []string{"loomy", "workbuddy"} {
		w := callJSON(http.MethodPost, "/api/hosts/auto", map[string]any{
			"host": host, "enabled": true, "models": models,
		})
		if w.Code != http.StatusOK {
			t.Fatalf("connect %s failed: %d %s", host, w.Code, w.Body.String())
		}
		var resp struct {
			Connected bool     `json:"connected"`
			Enabled   bool     `json:"enabled"`
			Model     string   `json:"model"`
			Models    []string `json:"models"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil || !resp.Connected || !resp.Enabled || resp.Model != models[0] || !reflect.DeepEqual(resp.Models, models) {
			t.Fatalf("unexpected connect %s response: %s", host, w.Body.String())
		}
	}
	if probes.Load() != 0 {
		t.Fatalf("expected 0 probe requests, got %d", probes.Load())
	}

	gw := callJSON(http.MethodGet, "/api/hosts/auto", nil)
	if gw.Code != http.StatusOK {
		t.Fatalf("GET /api/hosts/auto status %d: %s", gw.Code, gw.Body.String())
	}
	var statusResp struct {
		Items []struct {
			Host      string   `json:"host"`
			Enabled   bool     `json:"enabled"`
			Connected bool     `json:"connected"`
			Model     string   `json:"model"`
			Models    []string `json:"models"`
		} `json:"items"`
	}
	if err := json.Unmarshal(gw.Body.Bytes(), &statusResp); err != nil || len(statusResp.Items) != 2 {
		t.Fatalf("unexpected GET /api/hosts/auto response: %s", gw.Body.String())
	}
	for _, item := range statusResp.Items {
		if !item.Enabled || !item.Connected || item.Model != models[0] || len(item.Models) != 3 {
			t.Fatalf("unexpected host status item: %+v", item)
		}
	}

	for _, host := range []string{"loomy", "workbuddy"} {
		dw := callJSON(http.MethodPost, "/api/hosts/auto", map[string]any{
			"host": host, "enabled": false,
		})
		if dw.Code != http.StatusOK {
			t.Fatalf("disconnect %s failed: %d %s", host, dw.Code, dw.Body.String())
		}
	}
	gotLoomy, _ := os.ReadFile(filepath.Join(loomyRoot, "opencode.json"))
	if !bytes.Equal(gotLoomy, origLoomy) {
		t.Fatalf("loomy opencode.json not restored:\ngot: %s\nwant: %s", gotLoomy, origLoomy)
	}
	gotWB, _ := os.ReadFile(filepath.Join(wbRoot, "settings.json"))
	if !bytes.Equal(gotWB, origWBSettings) {
		t.Fatalf("workbuddy settings.json not restored:\ngot: %s\nwant: %s", gotWB, origWBSettings)
	}

	// Reconnect Loomy, simulate Loomy runtime clearing provider.api_router.models to {} and changing model,
	// then verify reconnecting and disconnecting via /api/hosts/auto both succeed.
	if w := callJSON(http.MethodPost, "/api/hosts/auto", map[string]any{
		"host": "loomy", "enabled": true, "models": []string{"group/111"},
	}); w.Code != http.StatusOK {
		t.Fatalf("reconnect loomy failed: %d %s", w.Code, w.Body.String())
	}
	loomyRuntimeRaw := []byte(`{"model":"wefto/` + prov.ID + `/deepseek-flash","small_model":"imodel/qwen3.8-flash","provider":{"wefto":{"models":{},"name":"端云协同","npm":"@ai-sdk/openai-compatible","options":{"apiKey":"wefto-local-placeholder","baseURL":"http://` + address + `/app/loomy/v1","useSessionAuth":false}}}}`)
	if err := os.WriteFile(filepath.Join(loomyRoot, "opencode.json"), loomyRuntimeRaw, 0o600); err != nil {
		t.Fatal(err)
	}
	if w := callJSON(http.MethodPost, "/api/hosts/auto", map[string]any{
		"host": "loomy", "enabled": true, "models": []string{prov.ID + "/deepseek-flash"},
	}); w.Code != http.StatusOK {
		t.Fatalf("connect loomy after runtime cleared models failed: %d %s", w.Code, w.Body.String())
	}
	// Verify a genuine restore conflict returns 409 on disconnect instead of silently reporting disconnected.
	conflictRaw := bytes.Replace(loomyRuntimeRaw, []byte(`"useSessionAuth":false`), []byte(`"useSessionAuth":true`), 1)
	if err := os.WriteFile(filepath.Join(loomyRoot, "opencode.json"), conflictRaw, 0o600); err != nil {
		t.Fatal(err)
	}
	if dw := callJSON(http.MethodPost, "/api/hosts/auto", map[string]any{
		"host": "loomy", "enabled": false,
	}); dw.Code != http.StatusConflict {
		t.Fatalf("expected 409 on conflicting disconnect, got %d %s", dw.Code, dw.Body.String())
	}
	// Restore non-conflicting runtime state and verify disconnect restores original opencode.json.
	if err := os.WriteFile(filepath.Join(loomyRoot, "opencode.json"), loomyRuntimeRaw, 0o600); err != nil {
		t.Fatal(err)
	}
	if dw := callJSON(http.MethodPost, "/api/hosts/auto", map[string]any{
		"host": "loomy", "enabled": false,
	}); dw.Code != http.StatusOK {
		t.Fatalf("disconnect loomy after runtime cleared models failed: %d %s", dw.Code, dw.Body.String())
	}

	// Verify switching WorkBuddy between cloud models updates Cloud binding and reports store save errors.
	prov2, err := store.SaveCloudProvider(providerInputFor("http://127.0.0.1:1/v1"))
	if err != nil {
		t.Fatal(err)
	}
	if w := callJSON(http.MethodPost, "/api/hosts/auto", map[string]any{
		"host": "workbuddy", "enabled": true, "models": []string{prov.ID + "/model-a"},
	}); w.Code != http.StatusOK {
		t.Fatalf("connect workbuddy model-a failed: %d %s", w.Code, w.Body.String())
	}
	if w := callJSON(http.MethodPost, "/api/hosts/auto", map[string]any{
		"host": "workbuddy", "enabled": true, "models": []string{prov2.ID + "/model-b"},
	}); w.Code != http.StatusOK {
		t.Fatalf("connect workbuddy model-b failed: %d %s", w.Code, w.Body.String())
	}
	savedWB := findSavedApp(context.Background(), store, "workbuddy")
	if savedWB == nil || savedWB.Cloud.ProviderID != prov2.ID || savedWB.Cloud.Model != "model-b" {
		t.Fatalf("expected workbuddy Cloud to update to %s/model-b, got %+v", prov2.ID, savedWB)
	}
	backupPath := layout.ConfigFile() + ".bak"
	_ = os.RemoveAll(backupPath)
	if err := os.MkdirAll(backupPath, 0o700); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(backupPath) }()
	if dw := callJSON(http.MethodPost, "/api/hosts/auto", map[string]any{
		"host": "workbuddy", "enabled": false, "action": "disconnect",
	}); dw.Code < 400 {
		t.Fatalf("expected profile sync failure on disconnect to return >=400, got %d %s", dw.Code, dw.Body.String())
	}
}
