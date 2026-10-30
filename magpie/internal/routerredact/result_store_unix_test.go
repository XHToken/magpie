//go:build linux || darwin

package routerredact

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/yetone/magpie/internal/dirs"
	"github.com/yetone/magpie/internal/testkeyvault"
)

func TestUnixResultStorePrivateFilesAndLock(t *testing.T) {
	state := t.TempDir()
	if err := checkResultPath(state); err != nil {
		t.Fatal("supported Unix state path rejected:", err)
	}
	dir := filepath.Join(state, "results")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := secureResultDirectory(dir); err != nil {
		t.Fatal(err)
	}
	name := filepath.Join(dir, "owner.lock")
	first, err := lockResultStore(name)
	if err != nil {
		t.Fatal(err)
	}
	if second, err := lockResultStore(name); err == nil {
		second.Close()
		t.Fatal("second writer acquired the same store")
	}
	first.Close()
	second, err := lockResultStore(name)
	if err != nil {
		t.Fatal("lock was not released:", err)
	}
	second.Close()
	if err := os.Chmod(name, 0644); err != nil {
		t.Fatal(err)
	}
	if verifyResultPrivate(name) == nil {
		t.Fatal("publicly readable key/store file accepted")
	}
	link := filepath.Join(dir, "alias")
	if err := os.Symlink(name, link); err != nil {
		t.Fatal(err)
	}
	if checkResultPath(link) == nil || verifyResultPrivate(link) == nil {
		t.Fatal("linked store file accepted")
	}
	if err := os.Chmod(name, 0600); err != nil {
		t.Fatal(err)
	}
	hard := filepath.Join(dir, "hard")
	if err := os.Link(name, hard); err != nil {
		t.Fatal(err)
	}
	if verifyResultPrivate(name) == nil {
		t.Fatal("multiply linked private file accepted")
	}
}

func TestUnixStoreRetriesUnavailableVaultWithoutReplacingEvidence(t *testing.T) {
	layout := dirs.Layout{State: t.TempDir()}
	vault := &fixtureResultVault{items: map[string][]byte{}, locked: true}
	keys := vaultResultKeyProtector{"fixture", vault}
	if store, err := OpenResultStoreWithKeyProtector(layout, keys); err == nil {
		store.Close()
		t.Fatal("locked vault accepted")
	}
	entries, err := os.ReadDir(layout.RedactionResults())
	if err != nil || len(entries) != 1 || entries[0].Name() != "owner.lock" {
		t.Fatal("failed vault wrote partial keys or database", err)
	}
	vault.locked = false
	store, err := OpenResultStoreWithKeyProtector(layout, keys)
	if err != nil {
		t.Fatal("empty initialization could not be retried", err)
	}
	if err := store.Save(context.Background(), "app/1", "fixture", []string{"zero"}, Result{Complete: true}); err != nil {
		t.Fatal(err)
	}
	store.Close()
	name := filepath.Join(layout.RedactionResults(), "results.sqlite")
	before, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	vault.locked = true
	if store, err := OpenResultStoreWithKeyProtector(layout, keys); err == nil {
		store.Close()
		t.Fatal("existing store ignored locked vault")
	}
	after, err := os.ReadFile(name)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("failed open modified evidence", err)
	}
}

func TestUnixStoreRejectsMissingKeyAndPublicFiles(t *testing.T) {
	for _, damage := range []string{"key", "file", "directory"} {
		t.Run(damage, func(t *testing.T) {
			layout := dirs.Layout{State: t.TempDir()}
			store, err := OpenResultStoreWithKeyProtector(layout, testkeyvault.Protector{})
			if err != nil {
				t.Fatal(err)
			}
			store.Close()
			dir := layout.RedactionResults()
			switch damage {
			case "key":
				err = os.Remove(filepath.Join(dir, resultKeyFile()))
			case "file":
				err = os.Chmod(filepath.Join(dir, "results.sqlite"), 0644)
			case "directory":
				err = os.Chmod(dir, 0755)
			}
			if err != nil {
				t.Fatal(err)
			}
			if opened, err := OpenResultStoreWithKeyProtector(layout, testkeyvault.Protector{}); err == nil {
				opened.Close()
				t.Fatal("unsafe existing store accepted")
			}
		})
	}
}
