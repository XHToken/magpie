package routerledger

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestSchemaTwoMigrationKeepsRedactionUnknown(t *testing.T) {
	path := filepath.Join(t.TempDir(), "requests.sqlite")
	s, err := Open(path, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Start(metadata("historical")); err != nil {
		t.Fatal(err)
	}
	if err := s.Finish("historical", success()); err != nil {
		t.Fatal(err)
	}
	r := get(t, s, "historical")
	r.SchemaVersion = 2
	r.Redaction = nil
	encoded, _ := json.Marshal(r)
	if _, err := s.db.Exec("UPDATE requests SET data=? WHERE request_id=?", string(encoded), r.RequestID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("PRAGMA user_version=2"); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	r = get(t, s, "historical")
	if r.SchemaVersion != SchemaVersion || r.Redaction != nil || r.Status != "succeeded" {
		t.Fatalf("fabricated historical privacy: %+v", r)
	}
	if err := s.Start(metadata("blocked")); err != nil {
		t.Fatal(err)
	}
	outcome := Outcome{Status: "failed", Destination: "none", SafeErrorCode: "redaction_policy_conflict", Redaction: &RedactionPolicy{Required: true, State: "blocked", SourceGroupIDs: []string{"private"}, SourceGroupCount: 1}}
	if err := s.Finish("blocked", outcome); err != nil {
		t.Fatal(err)
	}
	stored := get(t, s, "blocked")
	if stored.Attempts != 0 || stored.Redaction == nil || stored.Redaction.State != "blocked" {
		t.Fatalf("blocked policy lost: %+v", stored)
	}
}
