package routerhost

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"

	"github.com/yetone/magpie/internal/edit"
)

func TestLoomyReconnectAfterNativeModelSelectionWithRetainedCatalog(t *testing.T) {
	for _, failure := range []string{"none", "profile", "provider"} {
		t.Run(failure, func(t *testing.T) {
			e, o := loomySetup(t)
			o.Model = "group/2222"
			mustApply(t, e, o)
			// Older connections can already contain Router settings in their baseline.
			if err := os.Remove(e.recordPath(o.Host, o.ConfigRoot)); err != nil {
				t.Fatal(err)
			}
			o.Model = "p_fixture/deepseek-chat"
			if _, err := e.ConnectAndCommit(context.Background(), o, nil); err != nil {
				t.Fatal(err)
			}
			original, _, err := e.readJournal(o.Host, o.ConfigRoot)
			if err != nil {
				t.Fatal(err)
			}
			path := hostPath(o.ConfigRoot, "opencode.json")
			patches := []edit.KV{
				{Path: "model", Value: "imodel/spark-x2.6"},
				{Path: "small_model", Value: "imodel/qwen3.8-flash"},
				{Path: "plugin", Value: []string{"file:///fixture/loomy-chat-headers.mjs"}},
				{Path: "later", Value: "keep user setting"},
			}
			if failure == "provider" {
				patches = append(patches, edit.KV{Path: "provider.wefto.options.useSessionAuth", Value: true})
			}
			raw, err := edit.PatchJSON(get(t, path), patches, nil)
			if err != nil {
				t.Fatal(err)
			}
			put(t, path, raw)
			record := get(t, e.recordPath(o.Host, o.ConfigRoot))
			o.Model = "group/test"
			o.ModelNames = map[string]string{o.Model: "test"}
			committed := false
			_, err = e.ConnectAndCommit(context.Background(), o, func() error {
				committed = true
				if failure == "profile" {
					return errors.New("fixture Profile save failed")
				}
				return nil
			})
			if failure != "none" {
				if err == nil || committed != (failure == "profile") {
					t.Fatal("reconnect did not enforce the provider and Profile transaction checks:", err)
				}
				if !bytes.Equal(get(t, path), raw) || !bytes.Equal(get(t, e.recordPath(o.Host, o.ConfigRoot)), record) {
					t.Fatal("failed reconnect changed host or recovery bytes")
				}
				return
			}
			if err != nil || !committed {
				t.Fatal("native model selection blocked explicit reconnect with a retained Router catalog:", err)
			}
			doc, err := readJSON(get(t, path))
			if err != nil || doc["model"] != "wefto/group/test" || doc["small_model"] != "imodel/qwen3.8-flash" || doc["later"] != "keep user setting" {
				t.Fatal("reconnect did not select the requested model and preserve independent user settings:", err)
			}
			models, err := catalogModels(o.Host, get(t, path))
			if err != nil || len(models) != 1 || models[o.Model] == nil {
				t.Fatal("reconnect did not replace the obsolete Router catalog:", err)
			}
			current, _, err := e.readJournal(o.Host, o.ConfigRoot)
			if err != nil {
				t.Fatal(err)
			}
			for i, f := range current.Files {
				if !bytes.Equal(f.Before, original.Files[i].Before) {
					t.Fatal("reconnect replaced the original recovery baseline")
				}
			}
			if _, err := e.PreviewRestore(o.Host, o.ConfigRoot); err != nil {
				t.Fatal("reconnected host cannot be restored offline:", err)
			}
		})
	}
}
