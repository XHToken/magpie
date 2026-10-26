package routerhost

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/yetone/magpie/internal/edit"
)

func TestLoomyTitleExtensionRecoversAndKeepsOtherPlugins(t *testing.T) {
	e, o := loomySetup(t)
	file := filepath.Join(o.ConfigRoot, "opencode.json")
	original := []byte(`{"plugin":["user-extension"],"model":"imodel/original"}`)
	put(t, file, original)
	mustApply(t, e, o)
	bridge := filepath.Join(o.ConfigRoot, loomyTitleFile)
	if !bytes.Equal(get(t, bridge), loomyTitleSource) {
		t.Fatal("title extension not installed with the provider")
	}
	doc, _ := readJSON(get(t, file))
	plugins := append(doc["plugin"].([]any), "later-extension")
	updated, err := edit.PatchJSON(get(t, file), []edit.KV{{Path: "plugin", Value: plugins}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	put(t, file, updated)
	mustRestore(t, e, o)
	if _, err := os.Stat(bridge); !os.IsNotExist(err) {
		t.Fatal("managed title extension left behind", err)
	}
	doc, _ = readJSON(get(t, file))
	want := []any{"user-extension", "later-extension"}
	if !equalValue(doc["plugin"], true, want, true) {
		t.Fatal("disconnect changed other plugins", doc["plugin"])
	}
}

func TestLoomyTitleExtensionUpgradesV1AndPreservesFirstBackup(t *testing.T) {
	e, o := loomySetup(t)
	file := filepath.Join(o.ConfigRoot, "opencode.json")
	original := []byte(`{"model":"imodel/original","plugin":["user-extension"]}`)
	put(t, file, original)
	built, err := buildLoomyModels(o, []FilePlan{{Name: "opencode.json", Before: original, Existed: true}})
	if err != nil {
		t.Fatal(err)
	}
	put(t, file, built[0].After)
	contract := loomyV1Contract()
	j := &journal{Version: 1, Host: o.Host, Root: o.ConfigRoot, Status: "applied", Adapter: &contract, Files: []snapshot{{FilePlan: built[0], Mode: 0o600}}}
	if err := os.MkdirAll(e.StateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := e.saveJournal(j); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Connect(context.Background(), o); err != nil {
		t.Fatal("V1 connection cannot upgrade:", err)
	}
	var saved journal
	if err := json.Unmarshal(get(t, e.recordPath(o.Host, o.ConfigRoot)), &saved); err != nil {
		t.Fatal(err)
	}
	if saved.Adapter.Version != 2 || len(saved.Files) != 2 || !bytes.Equal(saved.Files[0].Before, original) {
		t.Fatal("upgrade replaced the original recovery baseline")
	}
	mustRestore(t, e, o)
	if !bytes.Equal(get(t, file), original) {
		t.Fatal("V1 baseline was not restored exactly")
	}
}

func TestLoomyTitleExtensionConflictDoesNotClobberUserFile(t *testing.T) {
	e, o := loomySetup(t)
	file := filepath.Join(o.ConfigRoot, loomyTitleFile)
	put(t, file, []byte("user content"))
	if _, err := e.Connect(context.Background(), o); err == nil {
		t.Fatal("unowned title extension was overwritten")
	}
	if string(get(t, file)) != "user content" {
		t.Fatal("conflicting extension changed")
	}
}
