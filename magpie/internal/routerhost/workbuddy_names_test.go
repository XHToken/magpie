package routerhost

import (
	"bytes"
	"context"
	"errors"
	"testing"
)

type legacyWorkBuddyNamesAdapter struct{ WorkBuddyAdapter }

func (legacyWorkBuddyNamesAdapter) Contract() AdapterContract { return workBuddyV1Contract() }

func TestWorkBuddyNamesUpgradeV1RollbackAndAlias(t *testing.T) {
	e, o := workBuddySetup(t)
	e.Adapter = legacyWorkBuddyNamesAdapter{}
	path := hostPath(o.ConfigRoot, "models.json")
	original := []byte(`[{"id":"unrelated","name":"User provider","url":"https://unrelated.invalid/v1"}]`)
	put(t, path, original)
	if _, err := e.ConnectAndCommit(context.Background(), o, nil); err != nil {
		t.Fatal(err)
	}
	e.Adapter = WorkBuddyAdapter{}
	o.ModelNames = map[string]string{o.Model: "deepseek-chat · DeepSeek"}
	record := e.recordPath(o.Host, o.ConfigRoot)
	beforeFile, beforeRecord := get(t, path), get(t, record)
	if _, err := e.ConnectAndCommit(context.Background(), o, func() error { return errors.New("Profile save failed") }); err == nil {
		t.Fatal("failed Profile save reported success")
	}
	if !bytes.Equal(get(t, path), beforeFile) || !bytes.Equal(get(t, record), beforeRecord) {
		t.Fatal("name upgrade did not roll back host and journal together")
	}
	if _, err := e.Connect(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	if workBuddyTarget(t, get(t, path), o.Model)["name"] != o.ModelNames[o.Model] {
		t.Fatal("legacy generic label was not upgraded")
	}
	j, _, err := e.readJournal(o.Host, o.ConfigRoot)
	if err != nil || j.Adapter.Version != 2 || !bytes.Equal(j.Files[0].Before, original) {
		t.Fatal("v1 upgrade lost its baseline or contract", err)
	}
	// Later user aliases survive provider renames and rebuilding the model set.
	raw, err := workBuddyPatchModel(get(t, path), 1, map[string]any{"name": "My chat"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	put(t, path, raw)
	o.Models = []string{o.Model, "group/new"}
	o.ModelNames[o.Model] = "deepseek-chat · Renamed provider"
	o.ModelNames["group/new"] = "Coding"
	if _, err := e.Connect(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	if workBuddyTarget(t, get(t, path), o.Model)["name"] != "My chat" {
		t.Fatal("rebuilding targets discarded the user's alias")
	}
	p, err := e.PreviewRestore(o.Host, o.ConfigRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Restore(p); err != nil {
		t.Fatal(err)
	}
	target := workBuddyTarget(t, get(t, path), o.Model)
	if target["name"] != "My chat" || target["url"] != nil || workBuddyTarget(t, get(t, path), "unrelated")["name"] != "User provider" {
		t.Fatal("restore lost the alias or retained Router connection fields")
	}
}

func TestWorkBuddyNamesRestoreExistingLabels(t *testing.T) {
	for _, label := range []string{"API Router", "Wefto", "端云协同", "My endpoint"} {
		t.Run(label, func(t *testing.T) {
			e, o := workBuddySetup(t)
			path := hostPath(o.ConfigRoot, "models.json")
			original := []byte(`[{"id":"target-model","name":"` + label + `","url":"https://original.invalid/v1","apiKey":"original-fixture"}]`)
			put(t, path, original)
			o.ModelNames = map[string]string{o.Model: "deepseek-chat · DeepSeek"}
			if _, err := e.Connect(context.Background(), o); err != nil {
				t.Fatal(err)
			}
			want := label
			if label == "API Router" || label == "Wefto" || label == "端云协同" {
				want = o.ModelNames[o.Model]
			}
			if workBuddyTarget(t, get(t, path), o.Model)["name"] != want {
				t.Fatal("wrong treatment of an existing label")
			}
			raw, err := workBuddyAppendModel(get(t, path), map[string]any{"id": "later", "name": "Later user model"})
			if err != nil {
				t.Fatal(err)
			}
			put(t, path, raw)
			p, err := e.PreviewRestore(o.Host, o.ConfigRoot)
			if err != nil {
				t.Fatal(err)
			}
			if err := e.Restore(p); err != nil {
				t.Fatal(err)
			}
			entry := workBuddyTarget(t, get(t, path), o.Model)
			if entry["name"] != label || entry["url"] != "https://original.invalid/v1" || workBuddyTarget(t, get(t, path), "later")["name"] != "Later user model" {
				t.Fatal("restore lost the original label or a later user entry")
			}
		})
	}
}
