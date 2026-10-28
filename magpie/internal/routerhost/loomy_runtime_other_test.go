//go:build !windows

package routerhost

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/testenv"
)

func TestRefreshLoomyRuntimeInvalidatesNativeCatalog(t *testing.T) {
	bin := t.TempDir()
	log := filepath.Join(t.TempDir(), "pkill-args")
	t.Setenv("LOOMY_RUNTIME_TEST_LOG", log)
	// Record process invalidation requests; never terminate an actual runtime.
	testenv.Program(t, filepath.Join(bin, "pkill"), "#!/bin/sh\nprintf '%s\\n' \"$@\" >> \"$LOOMY_RUNTIME_TEST_LOG\"\n")
	t.Setenv("PATH", bin)
	home := filepath.Join(t.TempDir(), "用户 home.with.dots")
	testenv.SetHome(t, home)
	arch := runtime.GOARCH
	if arch == "amd64" {
		arch = "x64"
	}
	cached := filepath.Join(home, ".local", "state", "loomy", "bin", runtime.GOOS+"-"+arch, "opencode")
	put(t, cached, []byte("stand-in runtime"))
	root := filepath.Join("/", "Users", "router-runtime-test", ".config", "loomy-opencode")
	before := []byte(`{"model":"api_router/provider/old"}`)
	after := []byte(`{"model":"api_router/group/new"}`)
	refreshLoomyRuntime(root, []snapshot{{FilePlan: FilePlan{Name: "opencode.json", Existed: true, Before: before, After: after}}})
	raw, err := os.ReadFile(log)
	if err != nil || !strings.Contains(string(raw), "/Loomy.app/Contents/Resources/opencode/") {
		t.Fatalf("changed native model catalog did not invalidate the bundled runtime: %s, %v", raw, err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	pattern := lines[len(lines)-1]
	match, err := regexp.Compile(pattern)
	if err != nil || !match.MatchString(cached+" serve --port 1234") || match.MatchString(cached+"-unrelated serve") {
		t.Fatalf("cached runtime invalidation must match only Loomy's executable: %q, %v", pattern, err)
	}
	// Reconnecting without file changes and editing an isolated test root
	// must not invalidate a running bundled server.
	if err := os.Remove(log); err != nil {
		t.Fatal(err)
	}
	refreshLoomyRuntime(root, []snapshot{{FilePlan: FilePlan{Name: "opencode.json", Existed: true, Before: after, After: after}}})
	refreshLoomyRuntime(filepath.Join(t.TempDir(), ".config", "loomy-opencode"), []snapshot{{FilePlan: FilePlan{Name: "opencode.json", Existed: true, Before: before, After: after}}})
	if raw, err := os.ReadFile(log); !os.IsNotExist(err) {
		t.Fatalf("unchanged/isolated configuration invalidated a runtime: %s, %v", raw, err)
	}
}
