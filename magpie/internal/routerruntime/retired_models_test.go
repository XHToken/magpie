package routerruntime

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/yetone/magpie/internal/dirs"
	"github.com/yetone/magpie/internal/routerconfig"
)

func TestRetiredModelIntentLoadsDisabledWithoutRewritingConfiguration(t *testing.T) {
	for _, retired := range []string{"spark-x2.5-1.7B", "spark-x2.5-4B"} {
		t.Run(retired, func(t *testing.T) {
			root := t.TempDir()
			store := routerconfig.New(dirs.Layout{Install: filepath.Join(root, "install"), Config: filepath.Join(root, "config"), Data: filepath.Join(root, "data"), State: filepath.Join(root, "state"), Models: filepath.Join(root, "data", "models"), Runtime: filepath.Join(root, "data", "runtime"), Cache: filepath.Join(root, "cache"), Logs: filepath.Join(root, "state", "logs")})
			if _, err := store.Initialize(); err != nil {
				t.Fatal(err)
			}
			if err := store.Update(func(d routerconfig.Document) error {
				raw, err := json.Marshal(map[string]any{"selected": retired, "enabled": true, "trial": TrialIntent{Judgment: true, Execution: true}, "future": 17})
				d["runtime"] = raw
				return err
			}); err != nil {
				t.Fatal(err)
			}
			before, err := store.Load()
			if err != nil {
				t.Fatal(err)
			}
			intents := ConfigIntentStore{Store: store, StandardSettings: true}
			intent, err := intents.Load(context.Background())
			if err != nil || intent.Selected != "" || intent.Enabled || intent.Trial != (TrialIntent{}) {
				t.Fatalf("retired model retained runtime authorization: %+v %v", intent, err)
			}
			after, err := store.Load()
			if err != nil || string(before["runtime"]) != string(after["runtime"]) {
				t.Fatal("startup rewrote the saved configuration", err)
			}
			intent.Selected = "spark-x2.6-4B-Q8_0"
			if err := intents.Save(context.Background(), intent); err != nil {
				t.Fatal(err)
			}
			after, err = store.Load()
			var saved struct {
				Selected string
				Enabled  bool
				Future   int
			}
			if err != nil || json.Unmarshal(after["runtime"], &saved) != nil || saved.Selected != intent.Selected || saved.Enabled || saved.Future != 17 {
				t.Fatal("explicit selection lost unknown runtime fields", saved, err)
			}
		})
	}
}
