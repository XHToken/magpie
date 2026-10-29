// Package dirs owns Wefto's paths. It never probes Magpie's files or
// portable markers, and resolving paths does not create any directories.
package dirs

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/yetone/magpie/internal/product"
)

// Layout is resolved once at startup and passed to config and runtime owners.
// Install is read-only. Data holds durable downloads; Cache can be discarded.
type Layout struct {
	Install string `json:"install"`
	Config  string `json:"config"`
	Data    string `json:"data"`
	Models  string `json:"models"`
	Runtime string `json:"runtime"`
	Cache   string `json:"cache"`
	State   string `json:"state"`
	Logs    string `json:"logs"`
}

func (l Layout) ConfigFile() string      { return filepath.Join(l.Config, "config.json") }
func (l Layout) CredentialsFile() string { return filepath.Join(l.Config, "credentials.json") }
func (l Layout) ManifestFile() string    { return filepath.Join(l.Models, "manifest.json") }
func (l Layout) LogFile() string         { return filepath.Join(l.Logs, "router.jsonl") }
func (l Layout) ServiceLock() string     { return filepath.Join(l.State, "router.lock") }

// Resolve uses OS conventions, never the current working directory or an
// executable-adjacent data folder. Relative OS directory variables are errors.
func Resolve() (Layout, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Layout{}, err
	}
	exe, err := os.Executable()
	if err != nil {
		return Layout{}, err
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return Layout{}, err
	}
	return resolveExisting(runtime.GOOS, home, exe, os.Getenv)
}

// resolveExisting reuses a complete legacy layout in place. It never splits
// credentials, models or recovery records between old and new directories.
func resolveExisting(goos, home, exe string, getenv func(string) string) (Layout, error) {
	current, err := resolve(goos, home, exe, getenv)
	if err != nil {
		return Layout{}, err
	}
	legacy, err := resolveProduct(goos, home, exe, getenv, product.LegacyID)
	if err != nil {
		return Layout{}, err
	}
	oldExists, err := layoutExists(legacy)
	if err != nil {
		return Layout{}, err
	}
	if !oldExists {
		return current, nil
	}
	newExists, err := layoutExists(current)
	if err != nil {
		return Layout{}, err
	}
	if newExists {
		return Layout{}, fmt.Errorf("both Wefto and legacy API-router data exist; preserve both layouts and reconcile them before starting")
	}
	return legacy, nil
}

func layoutExists(l Layout) (bool, error) {
	found := false
	for _, p := range []string{l.Config, l.Data, l.Cache, l.State, l.Logs} {
		info, err := os.Lstat(p)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return false, fmt.Errorf("cannot inspect product storage %s: %w", p, err)
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return false, fmt.Errorf("product storage must be a plain directory: %s", p)
		}
		found = true
	}
	return found, nil
}

func UsesLegacyStorage(l Layout) bool { return filepath.Base(l.Config) == product.LegacyID }

func resolve(goos, home, exe string, getenv func(string) string) (Layout, error) {
	return resolveProduct(goos, home, exe, getenv, product.ID)
}

func resolveProduct(goos, home, exe string, getenv func(string) string, id string) (Layout, error) {
	if !filepath.IsAbs(home) || !filepath.IsAbs(exe) {
		return Layout{}, fmt.Errorf("wefto requires absolute home and executable paths")
	}
	base := func(key, fallback string) (string, error) {
		p := getenv(key)
		if p == "" {
			p = fallback
		}
		if !filepath.IsAbs(p) {
			return "", fmt.Errorf("%s must be an absolute directory, got %q", key, p)
		}
		return filepath.Join(p, id), nil
	}
	l := Layout{Install: filepath.Dir(exe)}
	var err error
	switch goos {
	case "linux":
		if l.Config, err = base("XDG_CONFIG_HOME", filepath.Join(home, ".config")); err != nil {
			return Layout{}, err
		}
		if l.Data, err = base("XDG_DATA_HOME", filepath.Join(home, ".local", "share")); err != nil {
			return Layout{}, err
		}
		if l.Cache, err = base("XDG_CACHE_HOME", filepath.Join(home, ".cache")); err != nil {
			return Layout{}, err
		}
		if l.State, err = base("XDG_STATE_HOME", filepath.Join(home, ".local", "state")); err != nil {
			return Layout{}, err
		}
		l.Logs = filepath.Join(l.State, "logs")
	case "darwin":
		if l.Config, err = base("XDG_CONFIG_HOME", filepath.Join(home, "Library", "Application Support")); err != nil {
			return Layout{}, err
		}
		if l.Data, err = base("XDG_DATA_HOME", filepath.Join(home, "Library", "Application Support")); err != nil {
			return Layout{}, err
		}
		if l.Cache, err = base("XDG_CACHE_HOME", filepath.Join(home, "Library", "Caches")); err != nil {
			return Layout{}, err
		}
		if stateHome := getenv("XDG_STATE_HOME"); stateHome != "" && filepath.Clean(stateHome) != filepath.Clean(l.Data) {
			if l.State, err = base("XDG_STATE_HOME", ""); err != nil {
				return Layout{}, err
			}
			l.Logs = filepath.Join(l.State, "logs")
		} else {
			if stateHome != "" && !filepath.IsAbs(stateHome) {
				return Layout{}, fmt.Errorf("XDG_STATE_HOME must be an absolute directory, got %q", stateHome)
			}
			l.State = filepath.Join(l.Data, "state")
			l.Logs = filepath.Join(home, "Library", "Logs", id)
		}
	case "windows":
		if l.Config, err = base("APPDATA", ""); err != nil {
			return Layout{}, err
		}
		if l.Data, err = base("LOCALAPPDATA", ""); err != nil {
			return Layout{}, err
		}
		l.Cache = filepath.Join(l.Data, "cache")
		l.State = filepath.Join(l.Data, "state")
		l.Logs = filepath.Join(l.Data, "logs")
	default:
		return Layout{}, fmt.Errorf("wefto does not support OS %q", goos)
	}
	l.Models = filepath.Join(l.Data, "models")
	l.Runtime = filepath.Join(l.Data, "runtime")
	return l, nil
}

// Ensure creates only Wefto's writable directories, with private modes.
// Existing application directories are tightened too; shared OS parents are
// never chmodded. Windows uses the user's inherited directory ACLs.
func (l Layout) Ensure() error {
	for _, p := range []string{l.Config, l.Data, l.Models, l.Runtime, l.Cache, l.State, l.Logs} {
		if !filepath.IsAbs(p) {
			return fmt.Errorf("wefto directory must be absolute: %q", p)
		}
	}
	for _, p := range []string{l.Config, l.Data, l.Models, l.Runtime, l.Cache, l.State, l.Logs} {
		if err := os.MkdirAll(p, 0o700); err != nil {
			return err
		}
		fi, err := os.Lstat(p)
		if err != nil {
			return err
		}
		if !fi.IsDir() {
			return fmt.Errorf("wefto directory must not be a symlink: %s", p)
		}
		if err := os.Chmod(p, 0o700); err != nil {
			return err
		}
	}
	return nil
}
