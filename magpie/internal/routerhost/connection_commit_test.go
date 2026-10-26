package routerhost

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestHostProfileCommitFailurePreservesConnectionAndRecovery(t *testing.T) {
	for _, operation := range []string{"unchanged", "switch", "restore"} {
		t.Run(operation, func(t *testing.T) {
			e, targets, engines := connectedPortFixtures(t)
			before := portFixtureBytes(t, e, targets)
			for i, target := range targets {
				called := false
				persist := func() error { called = true; return errors.New("Profile write failed") }
				o := Options{Host: target.Host, ConfigRoot: target.ConfigRoot, RouterURL: "http://127.0.0.1:13145", Model: target.Model}
				var err error
				if operation == "restore" {
					p, planErr := engines[i].PreviewRestore(o.Host, o.ConfigRoot)
					if planErr != nil {
						t.Fatal(planErr)
					}
					err = engines[i].RestoreAndCommit(context.Background(), p, persist)
				} else {
					if operation == "switch" {
						o.Models = []string{"group/new", "provider/model"}
					}
					_, err = engines[i].ConnectAndCommit(context.Background(), o, persist)
				}
				if err == nil || !called {
					t.Fatalf("%s/%s did not attempt the Profile commit: %v", target.Host, operation, err)
				}
				assertPortFixtureBytes(t, before)
			}
		})
	}
}

func TestAtomicConnectionRetiresModelsAndRetainsOriginalBackup(t *testing.T) {
	for _, host := range []string{"loomy", "workbuddy"} {
		t.Run(host, func(t *testing.T) {
			e, o, original := retiredFixture(t, host)
			persisted := false
			if _, err := e.ConnectAndCommit(context.Background(), o, func() error { persisted = true; return nil }); err != nil || !persisted {
				t.Fatal("connection was not committed with its Profile:", err)
			}
			path := filepath.Join(o.ConfigRoot, e.Adapter.Contract().Files[0].Name)
			models, err := catalogModels(host, get(t, path))
			if err != nil || models[o.Model] == nil || models["deepseek-flash"] != nil {
				t.Fatal("connection retained an obsolete managed model:", err)
			}
			p, err := e.PreviewRestore(host, o.ConfigRoot)
			if err != nil {
				t.Fatal(err)
			}
			if err := e.RestoreAndCommit(context.Background(), p, func() error { return nil }); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(get(t, path), original[filepath.Base(path)]) {
				t.Fatal("disconnect did not restore the original model catalog")
			}
		})
	}
}

func TestReconnectAfterHostRestoresRecordedBaseline(t *testing.T) {
	_, targets, engines := connectedPortFixtures(t)
	for i, target := range targets {
		t.Run(target.Host, func(t *testing.T) {
			e := engines[i]
			original, _, err := e.readJournal(target.Host, target.ConfigRoot)
			if err != nil {
				t.Fatal(err)
			}
			for _, f := range original.Files {
				if f.Existed {
					put(t, hostPath(target.ConfigRoot, f.Name), f.Before)
				} else if err := os.Remove(hostPath(target.ConfigRoot, f.Name)); err != nil && !os.IsNotExist(err) {
					t.Fatal(err)
				}
			}
			o := Options{Host: target.Host, ConfigRoot: target.ConfigRoot, RouterURL: "http://127.0.0.1:13145", Model: "provider/new-model"}
			if _, err := e.ConnectAndCommit(context.Background(), o, func() error { return nil }); err != nil {
				t.Fatal("could not reconnect after the host restored its recorded baseline:", err)
			}
			current, _, err := e.readJournal(target.Host, target.ConfigRoot)
			if err != nil {
				t.Fatal(err)
			}
			for i, f := range current.Files {
				if !bytes.Equal(f.Before, original.Files[i].Before) {
					t.Fatal("reconnect replaced the original recovery baseline")
				}
			}
		})
	}
}
