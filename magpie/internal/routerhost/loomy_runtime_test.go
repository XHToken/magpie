package routerhost

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsRealLoomyConfigRoot(t *testing.T) {
	tempRoot := filepath.Join(t.TempDir(), "Loomy", "ebaa0a0a90b7", "opencode")
	if isRealLoomyConfigRoot(tempRoot) {
		t.Fatalf("expected temp Loomy root %q to be treated as test directory", tempRoot)
	}
	nonTemp := filepath.Join(string(filepath.Separator), "Users", "Public", "Loomy", "ebaa0a0a90b7", "opencode")
	if !isRealLoomyConfigRoot(nonTemp) {
		t.Fatalf("expected %q to be recognized as a real Loomy config root", nonTemp)
	}
	// Loomy's native macOS/Linux directory is not nested under Loomy/.
	// It must trigger bundled-runtime invalidation after a model/group edit.
	native := filepath.Join(string(filepath.Separator), "Users", "tester", ".config", "loomy-opencode")
	if !isRealLoomyConfigRoot(native) {
		t.Fatalf("native Loomy configuration %q would leave the embedded model catalog stale", native)
	}
	for _, root := range []string{
		filepath.Join(t.TempDir(), ".config", "loomy-opencode"),
		native + "-backup",
		filepath.Join(native, "unrelated"),
		filepath.Join(string(filepath.Separator), "Users", "tester", ".config", "opencode"),
	} {
		if isRealLoomyConfigRoot(root) {
			t.Fatalf("unrelated or temporary root %q must not invalidate a bundled runtime", root)
		}
	}
	other := filepath.Join(string(filepath.Separator), "Users", "Public", "WorkBuddy", "opencode")
	if isRealLoomyConfigRoot(other) {
		t.Fatalf("expected non-Loomy root %q to return false", other)
	}
}

func TestSyncLoomySidecarEnablesCustomModelsWhenConnected(t *testing.T) {
	root := t.TempDir()
	sidecar := filepath.Join(root, loomySidecarFile)
	if err := os.WriteFile(sidecar, []byte(`{"customModelsEnabled":false,"modelUserSelected":false}`), 0o600); err != nil {
		t.Fatal(err)
	}
	refreshLoomyRuntime(root, []snapshot{{
		FilePlan: FilePlan{
			Name:    "opencode.json",
			Existed: true,
			Before:  []byte(`{"model":"imodel/default"}`),
			After:   []byte(`{"model":"api_router/group/666"}`),
		},
	}})
	raw, err := os.ReadFile(sidecar)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := readJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	if doc["customModelsEnabled"] != true || doc["modelUserSelected"] != true || doc["customModelBackup"] != "api_router/group/666" {
		t.Fatalf("unexpected sidecar after connect: %s", string(raw))
	}
}

func TestLoomyReconnectRepairsSidecarWithoutChangingOpenCode(t *testing.T) {
	root := t.TempDir()
	sidecar := filepath.Join(root, loomySidecarFile)
	if err := os.WriteFile(sidecar, []byte(`{"customModelsEnabled":false,"modelUserSelected":false,"theme":"keep"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"model":"api_router/group/coding"}`)
	refreshLoomyRuntime(root, []snapshot{{FilePlan: FilePlan{Name: "opencode.json", Existed: true, Before: raw, After: raw}}})
	doc, err := readJSON(get(t, sidecar))
	if err != nil || doc["customModelsEnabled"] != true || doc["customModelBackup"] != "api_router/group/coding" || doc["theme"] != "keep" {
		t.Fatal("reconnect did not repair Loomy's independent custom-model state:", doc, err)
	}
}
