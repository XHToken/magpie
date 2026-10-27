package routerhost

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/edit"
)

// legacyConnection freezes the on-disk provider/key/contract of the released
// API-router build, including a checksummed version-1 adapter record.
func legacyConnection(t *testing.T, host string) (Engine, Options) {
	t.Helper()
	var e Engine
	var o Options
	switch host {
	case "loomy":
		e, o = loomySetup(t)
	case "workbuddy":
		e, o = workBuddySetup(t)
	default:
		e, o = setup(t, host)
	}
	if host == "loomy" {
		put(t, hostPath(o.ConfigRoot, "opencode.json"), []byte(`{"model":"original/model","theme":"system"}`))
	}
	if host == "workbuddy" {
		put(t, hostPath(o.ConfigRoot, "settings.json"), []byte(`{"model":"builtin-model"}`))
	}
	mustApply(t, e, o)
	j, _, err := e.readJournal(host, o.ConfigRoot)
	if err != nil {
		t.Fatal(err)
	}
	if host == "loomy" {
		// A released v1 backup predates the managed title extension.
		if err := os.Remove(hostPath(o.ConfigRoot, loomyTitleFile)); err != nil {
			t.Fatal(err)
		}
		j.Files = j.Files[:1]
		j.Files[0].After, err = deleteJSONField(j.Files[0].After, "plugin")
		if err != nil {
			t.Fatal(err)
		}
	}
	for i := range j.Files {
		raw := strings.ReplaceAll(string(j.Files[i].After), "wefto-local-placeholder", "api-router-local-placeholder")
		raw = strings.ReplaceAll(raw, "端云协同", "API Router")
		raw = strings.ReplaceAll(strings.ReplaceAll(raw, "Wefto", "API Router"), "wefto", "api_router")
		j.Files[i].After = []byte(raw)
		put(t, hostPath(o.ConfigRoot, j.Files[i].Name), []byte(raw))
	}
	var contract AdapterContract
	switch host {
	case "loomy":
		contract = legacyLoomyContract()
	case "workbuddy":
		contract = (WorkBuddyAdapter{}).Contract()
	default:
		contract = legacyV1Contract(host)
	}
	j.Adapter = &contract
	if err := e.saveJournal(j); err != nil {
		t.Fatal(err)
	}
	return e, o
}

func TestLegacyBackupRestoresWithLaterUserChanges(t *testing.T) {
	for _, host := range []string{"loomy", "workbuddy", "codex"} {
		t.Run(host, func(t *testing.T) {
			e, o := legacyConnection(t, host)
			j, _, err := e.readJournal(host, o.ConfigRoot)
			if err != nil {
				t.Fatal(err)
			}
			file := hostPath(o.ConfigRoot, j.Files[0].Name)
			if host == "codex" {
				put(t, file, append(get(t, file), []byte("\n# later user comment\n")...))
			} else if host == "workbuddy" {
				current := get(t, file)
				entry, err := edit.PatchJSON(workBuddyModelRaw(current, 0), []edit.KV{{Path: "userNote", Value: "preserved"}}, nil)
				if err != nil {
					t.Fatal(err)
				}
				raw, err := workBuddyReplaceModel(current, 0, entry)
				if err != nil {
					t.Fatal(err)
				}
				put(t, file, raw)
			} else {
				raw, err := edit.PatchJSON(get(t, file), []edit.KV{{Path: "later", Value: "preserved"}}, nil)
				if err != nil {
					t.Fatal(err)
				}
				put(t, file, raw)
			}
			mustRestore(t, e, o)
			got := get(t, file)
			if bytes.Contains(got, []byte("api-router-local-placeholder")) {
				t.Fatal("legacy managed connection was not restored")
			}
			if host == "codex" {
				doc, err := readTOML(got)
				if err != nil || doc["model_provider"] != nil {
					t.Fatal("legacy Codex provider is still selected", err)
				}
				if !bytes.Contains(got, []byte("later user comment")) {
					t.Fatal("lost user comment")
				}
			} else if !bytes.Contains(got, []byte("preserved")) {
				t.Fatal("lost later user field")
			}
		})
	}
}

func TestLegacyConnectionRebrandsAndKeepsRecovery(t *testing.T) {
	for _, host := range []string{"loomy", "workbuddy"} {
		t.Run(host, func(t *testing.T) {
			e, o := legacyConnection(t, host)
			original, _, _ := e.readJournal(host, o.ConfigRoot)
			o.Model = "new-model"
			if _, err := e.Connect(context.Background(), o); err != nil {
				t.Fatal("cannot reconnect a released legacy installation", err)
			}
			updated, _, err := e.readJournal(host, o.ConfigRoot)
			if err != nil {
				t.Fatal(err)
			}
			for i, file := range original.Files {
				if !bytes.Equal(updated.Files[i].Before, file.Before) {
					t.Fatal("replaced the original recovery snapshot")
				}
			}
			got := get(t, hostPath(o.ConfigRoot, updated.Files[0].Name))
			if !bytes.Contains(got, []byte("端云协同")) || !bytes.Contains(got, []byte("wefto-local-placeholder")) || bytes.Contains(got, []byte("API Router")) {
				t.Fatal("connection was not rebranded", string(got))
			}
			if host == "loomy" && !bytes.Contains(got, []byte("wefto/new-model")) {
				t.Fatal("new Loomy connection retained an old provider selection")
			}
			mustRestore(t, e, o)
		})
	}
}

func TestLegacyPortRewritePreservesConnectionIdentity(t *testing.T) {
	for _, host := range []string{"loomy", "workbuddy"} {
		t.Run(host, func(t *testing.T) {
			e, o := legacyConnection(t, host)
			j, _, _ := e.readJournal(host, o.ConfigRoot)
			raw := get(t, hostPath(o.ConfigRoot, j.Files[0].Name))
			updated, previous, connected, err := rewritePort(host, o.Model, "http://127.0.0.1:45678", raw)
			if err != nil || !connected || previous != o.RouterURL {
				t.Fatal("legacy port rewrite failed", previous, connected, err)
			}
			if !bytes.Contains(updated, []byte("api-router-local-placeholder")) || !bytes.Contains(updated, []byte("45678")) {
				t.Fatal("port update changed legacy credentials or lost endpoint")
			}
		})
	}
}

func TestLegacyHostLockBlocksNewWriter(t *testing.T) {
	e, o := loomySetup(t)
	if err := os.MkdirAll(filepath.Join(o.ConfigRoot, ".api-router-host.lock"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Connect(context.Background(), o); err == nil {
		t.Fatal("ignored an active legacy host writer")
	}
	if _, err := os.Stat(hostPath(o.ConfigRoot, "opencode.json")); !os.IsNotExist(err) {
		t.Fatal("changed host configuration while locked", err)
	}
}
