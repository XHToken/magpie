package routerhost

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"

	"github.com/yetone/magpie/internal/edit"
)

func TestLoomyReconnectAfterRestoringHostTimeoutDefaults(t *testing.T) {
	e, o := loomySetup(t)
	path := filepath.Join(o.ConfigRoot, "opencode.json")
	put(t, path, []byte(`{"model":"original/model","keep":"original"}`))
	mustApply(t, e, o)
	current, err := edit.PatchJSON(get(t, path), []edit.KV{{Path: "provider.wefto.options.timeout", Value: 1200000}, {Path: "provider.wefto.options.chunkTimeout", Value: 1200000}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	put(t, path, current)
	mustRestore(t, e, o)
	restored := get(t, path)
	p, err := e.Preview(o)
	if err != nil {
		t.Fatalf("host defaults retained by restore prevent reconnection: %v", err)
	}
	if err = e.Apply(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	mustApply(t, e, o)
	mustRestore(t, e, o)
	if !bytes.Equal(restored, get(t, path)) {
		t.Fatal("reconnection/restore changed preserved host defaults or original settings")
	}
}

func TestLoomyDefaultsOnlyProviderDoesNotAdoptUnknownValues(t *testing.T) {
	for _, provider := range []string{
		`{"options":{}}`,
		`{"options":{"timeout":1200001}}`,
		`{"options":{"timeout":1200000.0}}`,
		`{"options":{"timeout":1200000,"useSessionAuth":false}}`,
		`{"options":{"timeout":1200000,"chunkTimeout":null}}`,
		`{"options":{"timeout":"1200000"}}`,
		`{"options":{"timeout":1200000,"apiKey":"user-key"}}`,
		`{"options":{"timeout":1200000,"baseURL":"https://user.invalid"}}`,
		`{"options":{"timeout":1200000,"headers":{}}}`,
		`{"options":{"timeout":1200000},"name":"user provider"}`,
		`{"options":{"timeout":1200000},"models":{}}`,
	} {
		e, o := loomySetup(t)
		put(t, filepath.Join(o.ConfigRoot, "opencode.json"), []byte(`{"provider":{"wefto":`+provider+`}}`))
		if _, err := e.Preview(o); err == nil {
			t.Fatal("adopted unverified provider", provider)
		}
	}
}

func TestLoomyRuntimeSmallModelRewriteCompatibility(t *testing.T) {
	e, o := loomySetup(t)
	path := filepath.Join(o.ConfigRoot, "opencode.json")
	put(t, path, []byte(`{"model":"imodel/default","small_model":"imodel/default","provider":{"imodel":{"name":"iModel"}}}`))
	o.Model = ""
	o.Models = []string{"spark-x2.5"}
	if _, err := e.Connect(context.Background(), o); err != nil {
		t.Fatalf("first Connect failed: %v", err)
	}
	o.Models = []string{"group/111"}
	if _, err := e.Connect(context.Background(), o); err != nil {
		t.Fatalf("second Connect with model retirement failed: %v", err)
	}
	rewriteSmallModel := func() {
		t.Helper()
		updated, err := edit.PatchJSON(get(t, path), []edit.KV{
			{Path: "small_model", Value: "imodel/qwen3.8-flash"},
			{Path: "provider.imodel.models.spark-x2\\.6", Value: map[string]any{"name": "Spark X2.6"}},
		}, nil)
		if err != nil {
			t.Fatal(err)
		}
		put(t, path, updated)
	}
	rewriteSmallModel()
	if _, err := e.Connect(context.Background(), o); err != nil {
		t.Fatalf("Connect with same model after Loomy runtime small_model rewrite failed: %v", err)
	}
	rewriteSmallModel()
	o.Models = []string{"group/222"}
	if _, err := e.Connect(context.Background(), o); err != nil {
		t.Fatalf("Connect switching model after Loomy runtime small_model rewrite failed: %v", err)
	}
	rewriteSmallModel()
	if _, err := e.Preview(o); err != nil {
		t.Fatalf("Preview after Loomy runtime small_model rewrite failed: %v", err)
	}
	rp, err := e.PreviewRestore(o.Host, o.ConfigRoot)
	if err != nil {
		t.Fatalf("PreviewRestore after Loomy runtime small_model rewrite failed: %v", err)
	}
	if err := e.Restore(rp); err != nil {
		t.Fatalf("Restore after Loomy runtime small_model rewrite failed: %v", err)
	}
	doc, err := readJSON(get(t, path))
	if err != nil {
		t.Fatal(err)
	}
	if doc["model"] != "imodel/default" || doc["small_model"] != "imodel/qwen3.8-flash" {
		t.Fatalf("unexpected restored selectors: model=%v small_model=%v", doc["model"], doc["small_model"])
	}
}

func TestLoomyRuntimeMetadataDisplayLabelsAndModelSwitching(t *testing.T) {
	e, o := loomySetup(t)
	path := filepath.Join(o.ConfigRoot, "opencode.json")
	put(t, path, []byte(`{"model":"imodel/default","small_model":"imodel/qwen3.8-flash","provider":{"wefto":{"options":{"timeout":1200000,"chunkTimeout":1200000},"models":{"spark-x2.5":{"limit":{"context":262144,"output":32768}}},"localNote":"local opencode.json may retain model/provider references from local runtime state"}}}`))
	o.Model = ""
	o.Models = []string{"group/111", "spark-x2.5"}
	if _, err := e.Connect(context.Background(), o); err != nil {
		t.Fatalf("Connect on Loomy runtime stub failed: %v", err)
	}
	updated, err := edit.PatchJSON(get(t, path), []edit.KV{
		{Path: "model", Value: "wefto/spark-x2.5"},
		{Path: "provider.wefto.models.group/111.name", Value: "111 · 路由组"},
		{Path: "provider.wefto.models.spark-x2\\.5.name", Value: "spark-x2.5 · 讯飞星辰 MaaS"},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	put(t, path, updated)
	o.Models = []string{"group/111"}
	if _, err := e.Connect(context.Background(), o); err != nil {
		t.Fatalf("Reconnect after Loomy display label and model switch failed: %v", err)
	}
	rp, err := e.PreviewRestore(o.Host, o.ConfigRoot)
	if err != nil {
		t.Fatalf("PreviewRestore after Loomy display label and model switch failed: %v", err)
	}
	if err := e.Restore(rp); err != nil {
		t.Fatalf("Restore after Loomy display label and model switch failed: %v", err)
	}
}

func TestWorkBuddyRestoreAfterSwitchingCustomLocalModel(t *testing.T) {
	e, o := workBuddySetup(t)
	settingsPath := hostPath(o.ConfigRoot, "settings.json")
	put(t, settingsPath, []byte(`{"model":"builtin-model"}`))
	o.Model = ""
	o.Models = []string{"group/111", "spark-x2.5"}
	if _, err := e.Connect(context.Background(), o); err != nil {
		t.Fatalf("WorkBuddy Connect failed: %v", err)
	}
	put(t, settingsPath, []byte(`{"model":"custom-local:spark-x2.5"}`))
	rp, err := e.PreviewRestore(o.Host, o.ConfigRoot)
	if err != nil {
		t.Fatalf("WorkBuddy PreviewRestore after custom-local switch failed: %v", err)
	}
	if err := e.Restore(rp); err != nil {
		t.Fatalf("WorkBuddy Restore after custom-local switch failed: %v", err)
	}
	doc, err := readJSON(get(t, settingsPath))
	if err != nil || doc["model"] != "builtin-model" {
		t.Fatalf("unexpected restored WorkBuddy model: %v (err=%v)", doc["model"], err)
	}
}

func TestLoomyRuntimeClearedModelsCatalogAndSwitchedModel(t *testing.T) {
	e, o := loomySetup(t)
	path := filepath.Join(o.ConfigRoot, "opencode.json")
	orig := []byte(`{"model":"imodel/default","small_model":"imodel/qwen3.8-flash","provider":{"imodel":{"name":"iModel"}}}`)
	put(t, path, orig)
	o.Model = ""
	o.Models = []string{"group/111"}
	if _, err := e.Connect(context.Background(), o); err != nil {
		t.Fatalf("initial Connect failed: %v", err)
	}
	// Simulate Loomy runtime clearing provider.wefto.models to {} and switching model to imodel/spark-x2.6-preview.
	runtimeEdited, err := edit.PatchJSON(get(t, path), []edit.KV{
		{Path: "model", Value: "imodel/spark-x2.6-preview"},
		{Path: "small_model", Value: "imodel/qwen3.8-flash"},
		{Path: "provider.wefto.models", Value: map[string]any{}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	put(t, path, runtimeEdited)

	// Port synchronization must still succeed across both live opencode.json and the journal.
	nextURL := "http://127.0.0.1:14388"
	targets := []PortTarget{{Host: "loomy", ConfigRoot: o.ConfigRoot, Model: "group/111"}}
	if err := e.synchronizePorts(context.Background(), o.RouterURL, nextURL, targets, func() error { return nil }); err != nil {
		t.Fatalf("synchronizePorts after Loomy cleared models catalog failed: %v", err)
	}
	o.RouterURL = nextURL
	o.Models = []string{"deepseek-flash"}
	if _, err := e.Connect(context.Background(), o); err != nil {
		t.Fatalf("Reconnect after Loomy cleared models catalog failed: %v", err)
	}
	// If Loomy or the user both clears models and switches model to a new non-Router selector,
	// PreviewRestore must reject overwriting the new selection.
	runtimeEdited2, err := edit.PatchJSON(get(t, path), []edit.KV{
		{Path: "model", Value: "imodel/spark-x2.6-preview"},
		{Path: "provider.wefto.models", Value: map[string]any{}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	put(t, path, runtimeEdited2)
	if _, err := e.PreviewRestore(o.Host, o.ConfigRoot); err == nil {
		t.Fatal("PreviewRestore accepted conflicting non-Router model after catalog was cleared")
	}
	// When only the Router catalog was cleared while model remains a Router selection, Restore succeeds.
	runtimeClearedCatalogOnly, err := edit.PatchJSON(get(t, path), []edit.KV{
		{Path: "model", Value: "wefto/deepseek-flash"},
		{Path: "provider.wefto.models", Value: map[string]any{}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	put(t, path, runtimeClearedCatalogOnly)
	rp, err := e.PreviewRestore(o.Host, o.ConfigRoot)
	if err != nil {
		t.Fatalf("PreviewRestore after Loomy cleared models catalog failed: %v", err)
	}
	if err := e.Restore(rp); err != nil {
		t.Fatalf("Restore after Loomy cleared models catalog failed: %v", err)
	}
	doc, err := readJSON(get(t, path))
	if err != nil || doc["model"] != "imodel/default" || doc["small_model"] != "imodel/qwen3.8-flash" {
		t.Fatalf("unexpected restored Loomy document: %s (err=%v)", get(t, path), err)
	}
}

func TestLoomySidecarSyncOnConnectAndRestore(t *testing.T) {
	e, o := loomySetup(t)
	path := filepath.Join(o.ConfigRoot, "opencode.json")
	sidecar := filepath.Join(o.ConfigRoot, "loomy-config.json")
	put(t, path, []byte(`{"model":"imodel/default","small_model":"imodel/qwen3.8-flash","provider":{"imodel":{"name":"iModel"}}}`))
	put(t, sidecar, []byte(`{"customModelsEnabled":false,"modelUserSelected":false,"migrations":{"v1":true}}`))

	o.Model = ""
	o.Models = []string{"group/2222", "deepseek-chat"}
	if _, err := e.Connect(context.Background(), o); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	scDoc, err := readJSON(get(t, sidecar))
	if err != nil {
		t.Fatal(err)
	}
	if scDoc["customModelsEnabled"] != true || scDoc["modelUserSelected"] != true || scDoc["customModelBackup"] != "wefto/group/2222" {
		t.Fatalf("unexpected sidecar after Connect: %s", get(t, sidecar))
	}

	rp, err := e.PreviewRestore(o.Host, o.ConfigRoot)
	if err != nil {
		t.Fatalf("PreviewRestore failed: %v", err)
	}
	if err := e.Restore(rp); err != nil {
		t.Fatalf("Restore failed: %v", err)
	}
	scRestored, err := readJSON(get(t, sidecar))
	if err != nil {
		t.Fatal(err)
	}
	if scRestored["customModelBackup"] != "imodel/default" {
		t.Fatalf("unexpected sidecar after Restore: %s", get(t, sidecar))
	}
}

func TestLoomyRestoreRejectsUserModelEditWhenCatalogIntact(t *testing.T) {
	e, o := loomySetup(t)
	path := filepath.Join(o.ConfigRoot, "opencode.json")
	put(t, path, []byte("{\n  \"model\": \"original/model\",\n  \"small_model\": \"original/small\",\n  \"provider\": {\"original\": {\"options\": {\"apiKey\": \"secret\"}}},\n  \"theme\": \"before\"\n}\n"))
	mustApply(t, e, o)
	later, err := edit.PatchJSON(get(t, path), []edit.KV{
		{Path: "model", Value: "user/changed"},
		{Path: "theme", Value: "later-user-theme"},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	put(t, path, later)
	if _, err := e.PreviewRestore(o.Host, o.ConfigRoot); err == nil {
		t.Fatal("PreviewRestore accepted conflicting user model edit when catalog was intact")
	}
	doc, err := readJSON(get(t, path))
	if err != nil || doc["model"] != "user/changed" {
		t.Fatalf("PreviewRestore modified user model choice: %v (err=%v)", doc["model"], err)
	}
}

func TestLoomyRestoreRejectsUserModelEditWhenCatalogCleared(t *testing.T) {
	e, o := loomySetup(t)
	path := filepath.Join(o.ConfigRoot, "opencode.json")
	put(t, path, []byte("{\n  \"model\": \"original/model\",\n  \"small_model\": \"original/small\",\n  \"provider\": {\"original\": {\"options\": {\"apiKey\": \"secret\"}}},\n  \"theme\": \"before\"\n}\n"))
	mustApply(t, e, o)
	later, err := edit.PatchJSON(get(t, path), []edit.KV{
		{Path: "model", Value: "user/changed"},
		{Path: "provider.wefto.models", Value: map[string]any{}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	put(t, path, later)
	if _, err := e.PreviewRestore(o.Host, o.ConfigRoot); err == nil {
		t.Fatal("PreviewRestore accepted user model edit after Router catalog was cleared")
	}
	doc, err := readJSON(get(t, path))
	if err != nil || doc["model"] != "user/changed" {
		t.Fatalf("PreviewRestore modified user model choice: %v (err=%v)", doc["model"], err)
	}
}
