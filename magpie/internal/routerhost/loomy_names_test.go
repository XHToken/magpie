package routerhost

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestLoomyLabelUpgradeRollsBackWithProfileAndPreservesBaseline(t *testing.T) {
	e, o := loomySetup(t)
	o.Model = "p_fixture/vendor/model.v1"
	path := filepath.Join(o.ConfigRoot, "opencode.json")
	original := []byte("{\n // original user preferences\n \"model\":\"imodel/default\",\"theme\":\"system\"\n}\n")
	put(t, path, original)
	if _, err := e.ConnectAndCommit(context.Background(), o, nil); err != nil {
		t.Fatal(err)
	}
	record := e.recordPath(o.Host, o.ConfigRoot)
	beforeHost, beforeRecord := get(t, path), get(t, record)
	o.ModelNames = map[string]string{o.Model: "vendor/model.v1 · My provider"}
	if _, err := e.ConnectAndCommit(context.Background(), o, func() error { return errors.New("failed Profile save") }); err == nil {
		t.Fatal("failed Profile save reported successful label upgrade")
	}
	if !bytes.Equal(get(t, path), beforeHost) || !bytes.Equal(get(t, record), beforeRecord) {
		t.Fatal("failed label upgrade did not roll back both host and recovery bytes")
	}
	if _, err := e.Connect(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	doc, err := readJSON(get(t, path))
	if err != nil {
		t.Fatal(err)
	}
	name, _ := lookup(doc, []string{"provider", loomyProvider, "models", o.Model, "name"})
	if name != o.ModelNames[o.Model] || doc["model"] != "wefto/"+o.Model {
		t.Fatal("nested model ID was changed or its label was not upgraded")
	}
	j, _, err := e.readJournal(o.Host, o.ConfigRoot)
	if err != nil || !bytes.Equal(j.Files[0].Before, original) {
		t.Fatal("label upgrade replaced the first recovery baseline", err)
	}
	p, err := e.PreviewRestore(o.Host, o.ConfigRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Restore(p); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(get(t, path), original) {
		t.Fatal("label upgrade prevented exact offline restoration")
	}
}

func TestLoomyNamesKeepLegacyProviderIdentity(t *testing.T) {
	e, o := loomySetup(t)
	path := filepath.Join(o.ConfigRoot, "opencode.json")
	original := []byte(`{"model":"api_router/previous-model"}`)
	put(t, path, original)
	if _, err := e.Connect(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	o.ModelNames = map[string]string{o.Model: "Readable cloud model"}
	if _, err := e.Connect(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	doc, err := readJSON(get(t, path))
	if err != nil {
		t.Fatal(err)
	}
	name, _ := lookup(doc, []string{"provider", "api_router", "models", o.Model, "name"})
	if name != o.ModelNames[o.Model] || doc["model"] != "api_router/"+o.Model {
		t.Fatalf("legacy provider label/selector = %v / %v", name, doc["model"])
	}
	if _, exists := lookup(doc, []string{"provider", "wefto"}); exists {
		t.Fatal("label update created a second provider")
	}
	plan, err := e.PreviewRestore(o.Host, o.ConfigRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Restore(plan); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(get(t, path), original) {
		t.Fatal("legacy label update changed recovery baseline")
	}
}
