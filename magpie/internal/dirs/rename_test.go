package dirs

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestFreshWeftoLayout(t *testing.T) {
	root := t.TempDir()
	for _, key := range []string{"HOME", "USERPROFILE", "APPDATA", "LOCALAPPDATA", "XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_CACHE_HOME", "XDG_STATE_HOME"} {
		t.Setenv(key, filepath.Join(root, key))
	}
	l, err := Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(l.Config) != "wefto" || filepath.Base(l.Data) != "wefto" {
		t.Fatalf("fresh installation did not select Wefto storage: %+v", l)
	}
	if _, err := os.Stat(l.Config); !os.IsNotExist(err) {
		t.Fatal("resolving paths created configuration", err)
	}
}

func TestLegacyStorageUsesOneWholeLayout(t *testing.T) {
	for _, platform := range []string{"windows", "darwin", "linux"} {
		t.Run(platform, func(t *testing.T) {
			root := t.TempDir()
			exe := filepath.Join(root, "program", "wefto")
			get := func(key string) string { return filepath.Join(root, key) }
			legacy, err := resolveProduct(platform, root, exe, get, "api-router")
			if err != nil {
				t.Fatal(err)
			}
			files := map[string]string{legacy.ConfigFile(): "private-config-sentinel", legacy.CredentialsFile(): "private-credential-sentinel", filepath.Join(legacy.Models, "model.gguf"): "model-sentinel", filepath.Join(legacy.State, "hosts", "recovery.json"): "recovery-sentinel"}
			for file, value := range files {
				if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(file, []byte(value), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			actual, err := resolveExisting(platform, root, exe, get)
			if err != nil || actual != legacy {
				t.Fatalf("legacy state was split or reset: %+v %v", actual, err)
			}
			for file, value := range files {
				if got, err := os.ReadFile(file); err != nil || string(got) != value {
					t.Fatal("legacy data changed", file, err)
				}
			}
			fresh, _ := resolve(platform, root, exe, get)
			if _, err := os.Stat(fresh.Config); !os.IsNotExist(err) {
				t.Fatal("created competing Wefto state", err)
			}
			if err := os.MkdirAll(fresh.Config, 0o700); err != nil {
				t.Fatal(err)
			}
			if _, err := resolveExisting(platform, root, exe, get); err == nil {
				t.Fatal("silently chose between two data layouts")
			}
		})
	}
}

func TestLegacyStorageRejectsUnknownPaths(t *testing.T) {
	for _, kind := range []string{"file", "link"} {
		t.Run(kind, func(t *testing.T) {
			if kind == "link" && runtime.GOOS == "windows" {
				t.Skip("symlink creation needs Windows privileges")
			}
			root := t.TempDir()
			get := func(key string) string { return filepath.Join(root, key) }
			exe := filepath.Join(root, "wefto")
			legacy, _ := resolveProduct("linux", root, exe, get, "api-router")
			if err := os.MkdirAll(filepath.Dir(legacy.Config), 0o700); err != nil {
				t.Fatal(err)
			}
			var err error
			if kind == "file" {
				err = os.WriteFile(legacy.Config, []byte("unknown"), 0o600)
			} else {
				err = os.Symlink(t.TempDir(), legacy.Config)
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err := resolveExisting("linux", root, exe, get); err == nil {
				t.Fatal("accepted an unknown legacy storage path")
			}
		})
	}
}
