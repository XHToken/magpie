package routerhost

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"
)

func TestWorkBuddyPortSynchronizationUpdatesEveryManagedModel(t *testing.T) {
	for _, format := range []string{"array", "object"} {
		t.Run(format, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "workbuddy")
			e := Engine{StateDir: filepath.Join(t.TempDir(), "recovery"), Adapter: WorkBuddyAdapter{}, Checker: checkerFunc(func(context.Context, Options) error { return nil })}
			original := []byte(`[{"id":"other-router","url":"http://127.0.0.1:14000/app/workbuddy/v1","apiKey":"api-router-local-placeholder","supportsToolCall":true,"useCustomProtocol":false},{"id":"vendor","url":"https://example.test/v1","apiKey":"user-key"}]`)
			if format == "object" {
				original = append(append([]byte(`{"future":true,"models":`), original...), '}')
			}
			put(t, filepath.Join(root, "models.json"), original)
			settings := []byte("{\n // retain settings\n \"model\": \"previous-model\", \"theme\": \"dark\"\n}")
			put(t, filepath.Join(root, "settings.json"), settings)
			models := []string{"group/code", "provider/model", "local/model"}
			o := Options{Host: "workbuddy", ConfigRoot: root, RouterURL: "http://127.0.0.1:13145", Models: models}
			if _, err := e.ConnectAndCommit(context.Background(), o, nil); err != nil {
				t.Fatal(err)
			}
			connectedSettings := get(t, filepath.Join(root, "settings.json"))
			targets := []PortTarget{{Host: "workbuddy", ConfigRoot: root, Model: models[0]}}
			for _, origins := range [][2]string{{"http://127.0.0.1:13145", "http://127.0.0.1:13146"}, {"http://127.0.0.1:13146", "http://127.0.0.1:13147"}} {
				if err := e.synchronizePorts(context.Background(), origins[0], origins[1], targets, func() error { return nil }); err != nil {
					t.Fatal(err)
				}
				doc, err := workBuddyModels(get(t, filepath.Join(root, "models.json")))
				if err != nil {
					t.Fatal(err)
				}
				for _, model := range models {
					entry, _ := workBuddyModel(doc, model)
					if entry["url"] != origins[1]+"/app/workbuddy/v1" {
						t.Errorf("managed model %q retained a stale endpoint: %v", model, entry["url"])
					}
				}
				other, _ := workBuddyModel(doc, "other-router")
				vendor, _ := workBuddyModel(doc, "vendor")
				if other["url"] != "http://127.0.0.1:14000/app/workbuddy/v1" || vendor["url"] != "https://example.test/v1" || !bytes.Equal(connectedSettings, get(t, filepath.Join(root, "settings.json"))) {
					t.Fatal("port synchronization changed another connection or the selected model")
				}
				journal, _, err := e.readJournal("workbuddy", root)
				if err != nil || !bytes.Equal(journal.Files[0].Before, original) {
					t.Fatal("port synchronization replaced the first backup:", err)
				}
			}
			p, err := e.PreviewRestore("workbuddy", root)
			if err != nil {
				t.Fatal(err)
			}
			if err := e.Restore(p); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(get(t, filepath.Join(root, "models.json")), original) || !bytes.Equal(get(t, filepath.Join(root, "settings.json")), settings) {
				t.Fatal("offline restore lost the original host configuration")
			}
		})
	}
}

func TestWorkBuddyPortSynchronizationPreflightsSecondaryModels(t *testing.T) {
	e, targets, engines := connectedPortFixtures(t)
	o := Options{Host: "workbuddy", ConfigRoot: targets[0].ConfigRoot, RouterURL: "http://127.0.0.1:13145", Models: []string{"fixture-model", "secondary-model"}}
	if _, err := engines[0].ConnectAndCommit(context.Background(), o, nil); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(o.ConfigRoot, "models.json")
	raw := get(t, path)
	doc, err := workBuddyModels(raw)
	if err != nil {
		t.Fatal(err)
	}
	_, index := workBuddyModel(doc, "secondary-model")
	raw, err = workBuddyPatchModel(raw, index, map[string]any{"supportsToolCall": false}, nil)
	if err != nil {
		t.Fatal(err)
	}
	put(t, path, raw)
	before := portFixtureBytes(t, e, targets)
	err = e.synchronizePorts(context.Background(), "http://127.0.0.1:13145", "http://127.0.0.1:13146", targets, func() error {
		t.Fatal("saved a new address despite a conflicting secondary model")
		return nil
	})
	if err == nil {
		t.Fatal("ignored a conflicting secondary Router connection")
	}
	assertPortFixtureBytes(t, before)
}
