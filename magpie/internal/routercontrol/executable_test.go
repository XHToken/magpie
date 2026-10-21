package routercontrol

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExecutableIdentityResolvesAliasesAndRejectsOtherPaths(t *testing.T) {
	stamp, executable, err := processIdentity(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	record := Record{PID: os.Getpid(), Executable: executable, ProcessStamp: stamp}
	t.Run("symlink", func(t *testing.T) {
		alias := filepath.Join(t.TempDir(), "service-alias")
		if err := os.Symlink(executable, alias); err != nil {
			t.Skip("symbolic links unavailable: ", err)
		}
		record.Executable = alias
		if err := Verify(record); err != nil {
			t.Fatalf("the same executable's resolved alias was rejected: %v", err)
		}
	})
	t.Run("hard-link-is-another-path", func(t *testing.T) {
		// A running Windows image cannot be unlinked until the process exits.
		// Use closed fixture files to exercise the same path-ownership rule.
		root := t.TempDir()
		owned := filepath.Join(root, "owned.exe")
		if err := os.WriteFile(owned, []byte("executable identity fixture"), 0o600); err != nil {
			t.Fatal(err)
		}
		alias := filepath.Join(root, "another-installation.exe")
		if err := os.Link(owned, alias); err != nil {
			t.Skip("hard links unavailable: ", err)
		}
		if samePath(owned, alias) {
			t.Fatal("another executable path was adopted merely because it shares a file ID")
		}
	})
	t.Run("missing-path-is-unknown", func(t *testing.T) {
		missing := filepath.Join(t.TempDir(), "missing-executable")
		if samePath(missing, missing) {
			t.Fatal("unavailable executable paths were treated as the same identity")
		}
	})
}
