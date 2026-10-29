package routerhost

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
)

func restoredRenamedLoomyFixture(t *testing.T) (Engine, Options, *journal) {
	t.Helper()
	e, o := loomySetup(t)
	o.Model = "group/ttt"
	o.RouterURL = "http://127.0.0.1:13145"
	original := []byte(`{
  "model":"api_router/group/2222",
  "small_model":"imodel/qwen3.8-flash",
  "provider":{
    "imodel":{"name":"Keep native provider"},
    "api_router":{
      "npm":"@ai-sdk/openai-compatible","name":"API Router",
      "options":{"baseURL":"http://127.0.0.1:13145/app/loomy/v1","apiKey":"api-router-local-placeholder","useSessionAuth":false,"timeout":1200000,"chunkTimeout":1200000},
      "models":{"group/2222":{"name":"Old group","tool_call":true}}
    }
  }
}`)
	put(t, hostPath(o.ConfigRoot, "opencode.json"), original)
	j := &journal{Version: 2, Host: "loomy", Root: o.ConfigRoot, Status: "restored",
		Adapter: &AdapterContract{ID: "loomy", Version: 3, Files: []FileSpec{
			{Name: "opencode.json", Fields: []string{"model", "small_model",
				"provider.api_router.npm", "provider.api_router.name", "provider.api_router.options.baseURL",
				"provider.api_router.options.apiKey", "provider.api_router.options.useSessionAuth", "provider.api_router.models",
				"provider.wefto.npm", "provider.wefto.name", "provider.wefto.options.baseURL",
				"provider.wefto.options.apiKey", "provider.wefto.options.useSessionAuth", "provider.wefto.models",
				"plugin (managed title entry only)"}},
			{Name: loomyTitleFile, Fields: []string{"managed title extension source"}},
		}},
		Files: []snapshot{
			{FilePlan: FilePlan{Name: "opencode.json", Before: original, After: []byte(`{"model":"wefto/group/old"}`), Existed: true}, Mode: 0o600},
			{FilePlan: FilePlan{Name: loomyTitleFile, After: []byte("// previous managed title extension")}, Mode: 0o600},
		},
	}
	writeRenamedLoomyJournal(t, e, j)
	return e, o, j
}

func writeRenamedLoomyJournal(t *testing.T, e Engine, j *journal) {
	t.Helper()
	j.Checksum = j.checksum()
	raw, err := json.MarshalIndent(j, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	put(t, e.recordPath(j.Host, j.Root), raw)
}

func TestReconnectRestoredRenamedLoomy(t *testing.T) {
	for _, target := range []string{"group/2222", "group/ttt"} {
		t.Run(target, func(t *testing.T) {
			e, o, original := restoredRenamedLoomyFixture(t)
			o.Model = target
			if _, err := e.PreviewRestore(o.Host, o.ConfigRoot); err == nil {
				t.Fatal("older adapter must not restore a newer contract")
			}
			if _, err := e.ConnectAndCommit(context.Background(), o, nil); err != nil {
				t.Fatal("fully restored renamed installation could not reconnect:", err)
			}
			current, _, err := e.readJournal(o.Host, o.ConfigRoot)
			if err != nil || !sameContract(*current.Adapter, e.Adapter.Contract()) {
				t.Fatal("new connection lacks its current recovery contract:", err)
			}
			for i, f := range current.Files {
				if f.Existed != original.Files[i].Existed || !bytes.Equal(f.Before, original.Files[i].Before) {
					t.Fatal("reconnect changed the original restoration baseline")
				}
			}
			mustRestore(t, e, o)
			if !bytes.Equal(get(t, hostPath(o.ConfigRoot, "opencode.json")), original.Files[0].Before) {
				t.Fatal("disconnect failed to restore the original bytes")
			}
			if _, err := os.Stat(hostPath(o.ConfigRoot, loomyTitleFile)); !os.IsNotExist(err) {
				t.Fatal("disconnect retained the new title extension:", err)
			}
		})
	}
}

func TestRestoredRenamedLoomyProfileFailureRollsBack(t *testing.T) {
	e, o, _ := restoredRenamedLoomyFixture(t)
	configPath, journalPath := hostPath(o.ConfigRoot, "opencode.json"), e.recordPath(o.Host, o.ConfigRoot)
	configBefore, journalBefore := get(t, configPath), get(t, journalPath)
	called := false
	if _, err := e.ConnectAndCommit(context.Background(), o, func() error {
		called = true
		return errors.New("Profile save failed")
	}); err == nil || !called {
		t.Fatal("connection did not attempt its Profile transaction:", err)
	}
	if !bytes.Equal(get(t, configPath), configBefore) || !bytes.Equal(get(t, journalPath), journalBefore) {
		t.Fatal("failed Profile save changed host files or the newer recovery record")
	}
}

func TestRenamedLoomyReconnectRejectsUnverifiedRecovery(t *testing.T) {
	for _, change := range []string{"active", "unknown-version", "changed-config", "unexpected-title"} {
		t.Run(change, func(t *testing.T) {
			e, o, j := restoredRenamedLoomyFixture(t)
			switch change {
			case "active":
				j.Status = "applied"
			case "unknown-version":
				j.Adapter.Version++
			case "changed-config":
				put(t, hostPath(o.ConfigRoot, "opencode.json"), append(bytes.Clone(j.Files[0].Before), '\n'))
			case "unexpected-title":
				put(t, hostPath(o.ConfigRoot, loomyTitleFile), loomyTitleSource)
			}
			writeRenamedLoomyJournal(t, e, j)
			configBefore := get(t, hostPath(o.ConfigRoot, "opencode.json"))
			journalBefore := get(t, e.recordPath(o.Host, o.ConfigRoot))
			if _, err := e.ConnectAndCommit(context.Background(), o, nil); err == nil {
				t.Fatal("unverified newer recovery record was accepted")
			}
			if !bytes.Equal(get(t, hostPath(o.ConfigRoot, "opencode.json")), configBefore) ||
				!bytes.Equal(get(t, e.recordPath(o.Host, o.ConfigRoot)), journalBefore) {
				t.Fatal("rejected connection changed host configuration or recovery record")
			}
		})
	}
}
