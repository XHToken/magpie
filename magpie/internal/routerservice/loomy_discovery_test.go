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
	"runtime"
	"strings"
	"testing"

	"github.com/tidwall/jsonc"
	"github.com/yetone/magpie/internal/product"
	"github.com/yetone/magpie/internal/routerconfig"
	"github.com/yetone/magpie/internal/testenv"
)

func writeLoomyDiscoveryConfig(t *testing.T, root string, raw []byte) {
	t.Helper()
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "opencode.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestDiscoverLoomyConfigRootByPlatform(t *testing.T) {
	for _, goos := range []string{"darwin", "linux", "windows"} {
		t.Run(goos, func(t *testing.T) {
			home := filepath.Join(t.TempDir(), "用户 home")
			testenv.SetHome(t, home)
			public := filepath.Join(t.TempDir(), "Public")
			t.Setenv("PUBLIC", public)
			unixRoot := filepath.Join(home, ".config", "loomy-opencode")
			windowsRoot := filepath.Join(public, "Loomy", "identity", "opencode")
			for _, root := range []string{unixRoot, windowsRoot} {
				writeLoomyDiscoveryConfig(t, root, []byte(`{"model":"imodel/default"}`))
			}
			// Loomy's getOpenCodeDir uses ~/.config even with XDG_CONFIG_HOME set.
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "xdg"))
			writeLoomyDiscoveryConfig(t, filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "loomy-opencode"), []byte(`{}`))
			want := unixRoot
			if goos == "windows" {
				want = windowsRoot
			}
			got, err := discoverLoomyConfigRootForOS(goos)
			if err != nil || got != want {
				t.Fatalf("%s Loomy directory = %q, %v; want %q", goos, got, err, want)
			}
		})
	}
}

func TestDiscoverLoomyUnixRequiresExistingConfig(t *testing.T) {
	for _, kind := range []string{"missing-root", "missing-file", "directory-instead-of-file"} {
		t.Run(kind, func(t *testing.T) {
			home := t.TempDir()
			testenv.SetHome(t, home)
			t.Setenv("PUBLIC", "")
			root := filepath.Join(home, ".config", "loomy-opencode")
			if kind != "missing-root" {
				if err := os.MkdirAll(root, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "directory-instead-of-file" {
				if err := os.Mkdir(filepath.Join(root, "opencode.json"), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			// A standalone OpenCode installation must not be adopted as Loomy.
			writeLoomyDiscoveryConfig(t, filepath.Join(home, ".config", "opencode"), []byte(`{}`))
			for _, goos := range []string{"darwin", "linux"} {
				if got, err := discoverLoomyConfigRootForOS(goos); err == nil || got != "" {
					t.Fatalf("%s accepted %s: %q, %v", goos, kind, got, err)
				}
			}
			if kind == "missing-root" {
				if _, err := os.Stat(root); !os.IsNotExist(err) {
					t.Fatal("discovery created a Loomy directory", err)
				}
			}
		})
	}
}

func TestDiscoverLoomyUnixWithoutPublic(t *testing.T) {
	home := t.TempDir()
	testenv.SetHome(t, home)
	t.Setenv("PUBLIC", "")
	root := filepath.Join(home, ".config", "loomy-opencode")
	writeLoomyDiscoveryConfig(t, root, []byte(`{}`))
	for _, goos := range []string{"darwin", "linux"} {
		if got, err := discoverLoomyConfigRootForOS(goos); err != nil || got != root {
			t.Fatalf("%s without PUBLIC = %q, %v; want %q", goos, got, err, root)
		}
	}
}

func TestDiscoverLoomyWindowsFallbackAndAmbiguity(t *testing.T) {
	base := t.TempDir()
	testenv.SetHome(t, filepath.Join(base, "Alice"))
	t.Setenv("PUBLIC", "")
	root := filepath.Join(base, "Public", "Loomy", "first", "opencode")
	writeLoomyDiscoveryConfig(t, root, []byte(`{}`))
	if got, err := discoverLoomyConfigRootForOS("windows"); err != nil || got != root {
		t.Fatalf("Windows Public fallback = %q, %v; want %q", got, err, root)
	}
	writeLoomyDiscoveryConfig(t, filepath.Join(base, "Public", "Loomy", "second", "opencode"), []byte(`{}`))
	if got, err := discoverLoomyConfigRootForOS("windows"); err == nil || got != "" || !strings.Contains(err.Error(), "multiple") {
		t.Fatalf("ambiguous Windows identities accepted: %q, %v", got, err)
	}
}

func TestLoomyExplicitRootOverridesDiscovery(t *testing.T) {
	testenv.SetHome(t, t.TempDir())
	t.Setenv("PUBLIC", "")
	first, second := t.TempDir(), t.TempDir()
	writeLoomyDiscoveryConfig(t, first, []byte(`{}`))
	writeLoomyDiscoveryConfig(t, second, []byte(`{}`))
	t.Setenv("LOOMY_CONFIG_DIR", first)
	t.Setenv("LOOMY_OPENCODE_DIR", second)
	if got, err := autoHostConfigRoot("loomy"); err != nil || got != first {
		t.Fatalf("explicit Loomy root = %q, %v; want %q", got, err, first)
	}
	t.Setenv("LOOMY_CONFIG_DIR", "")
	if got, err := autoHostConfigRoot("loomy"); err != nil || got != second {
		t.Fatalf("legacy explicit Loomy root = %q, %v; want %q", got, err, second)
	}
	t.Setenv("LOOMY_CONFIG_DIR", filepath.Join(t.TempDir(), "missing"))
	if got, err := autoHostConfigRoot("loomy"); err == nil || got != "" {
		t.Fatalf("invalid explicit root silently fell back: %q, %v", got, err)
	}
}

// Run this on macOS as well as Windows: no explicit directory is supplied to
// either the service environment or the same switch payload used by Agents.
func TestLoomyAutoHostDefaultRootLifecycle(t *testing.T) {
	layout, _ := fixture(t)
	store := routerconfig.New(layout)
	t.Setenv("LOOMY_CONFIG_DIR", "")
	t.Setenv("LOOMY_OPENCODE_DIR", "")
	home := filepath.Join(t.TempDir(), "用户 home")
	testenv.SetHome(t, home)
	public := filepath.Join(t.TempDir(), "Public")
	t.Setenv("PUBLIC", public)
	root := filepath.Join(home, ".config", "loomy-opencode")
	if runtime.GOOS == "windows" {
		root = filepath.Join(public, "Loomy", "identity", "opencode")
	}
	original := []byte("{\n // retain user configuration\n \"model\":\"imodel/original\",\"theme\":\"system\"\n}\n")
	writeLoomyDiscoveryConfig(t, root, original)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			t.Errorf("connecting Loomy unexpectedly called %s", r.URL.Path)
		}
		io.WriteString(w, `{"service":"api-router","status":"ok"}`)
	}))
	defer server.Close()
	provider, err := store.SaveCloudProvider(providerInputFor("http://127.0.0.1:1/v1"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveGroup(routerconfig.Group{ID: "mac-loomy", Name: "Mac 编程", Members: []string{provider.ID + "/deepseek-chat"}}); err != nil {
		t.Fatal(err)
	}
	state := t.TempDir()
	const token = "loomy-discovery-management"
	info := Info{Service: product.ID, Address: server.Listener.Addr().String()}
	productHandler := handlerWithModelOptions(info, token, store, ModelOptions{HostStateDir: state})
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Host, r.URL.Host = info.Address, info.Address
		productHandler.ServeHTTP(w, r)
	})
	connect := map[string]any{"host": "loomy", "enabled": true, "models": []string{"group/mac-loomy", provider.ID + "/deepseek-chat"}}
	for attempt := 0; attempt < 2; attempt++ {
		w := providerRequest(t, h, "POST", "/api/hosts/auto", token, connect)
		if w.Code != http.StatusOK {
			t.Fatalf("default-directory connect/reconnect: %d %s", w.Code, w.Body.String())
		}
	}
	path := filepath.Join(root, "opencode.json")
	before, err := os.ReadFile(path)
	var config struct {
		Model    string `json:"model"`
		Provider map[string]struct {
			Options struct {
				BaseURL string `json:"baseURL"`
			} `json:"options"`
			Models map[string]any `json:"models"`
		} `json:"provider"`
	}
	if err != nil || json.Unmarshal(jsonc.ToJSON(before), &config) != nil || config.Model != product.ID+"/group/mac-loomy" || config.Provider[product.ID].Options.BaseURL != server.URL+"/app/loomy/v1" || len(config.Provider[product.ID].Models) != 2 {
		t.Fatal("discovered Loomy configuration lacks the selected models or endpoint", err)
	}
	w := providerRequest(t, h, "GET", "/api/hosts/auto", token, nil)
	var status struct {
		Items []struct {
			Host, ConfigRoot string
			Connected        bool
		}
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &status) != nil {
		t.Fatalf("cannot read host status: %s", w.Body.String())
	}
	found := false
	for _, item := range status.Items {
		if item.Host == "loomy" {
			found = item.Connected && item.ConfigRoot == root
		}
	}
	if !found {
		t.Fatal("status did not discover the connected Loomy directory")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("status rewrote Loomy's configuration", err)
	}
	targets, err := startupPortTargets(context.Background(), store, state)
	if err != nil || len(targets) == 0 {
		t.Fatalf("startup lost the discovered Loomy connection: %+v, %v", targets, err)
	}
	for _, target := range targets {
		if target.Host != "loomy" || target.ConfigRoot != root || target.Model != "group/mac-loomy" {
			t.Fatalf("startup discovered an unrelated target: %+v", target)
		}
	}
	w = providerRequest(t, h, "POST", "/api/hosts/auto", token, map[string]any{"host": "loomy", "enabled": false, "action": "disconnect"})
	if w.Code != http.StatusOK {
		t.Fatalf("default-directory disconnect: %d %s", w.Code, w.Body.String())
	}
	restored, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(original, restored) {
		t.Fatal("disconnect failed to restore the original Loomy configuration", err)
	}
	if _, err := os.Stat(filepath.Join(root, "api-router-title.mjs")); !os.IsNotExist(err) {
		t.Fatal("disconnect left the managed Loomy extension", err)
	}
}
