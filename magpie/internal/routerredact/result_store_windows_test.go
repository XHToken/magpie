package routerredact

import (
	"context"
	"database/sql"
	"github.com/yetone/magpie/internal/dirs"
	"golang.org/x/sys/windows"
	"os"
	"path/filepath"
	"testing"
)

func TestResultStoreWindowsProtection(t *testing.T) {
	state := t.TempDir()
	path := filepath.Join(state, "redaction-results")
	if err := checkResultPath(state); err != nil {
		t.Fatal("path", err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := secureResultDirectory(path); err != nil {
		sd, _ := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
		sid, _ := resultUserSID()
		t.Fatalf("acl: %v user %s sd %s", err, sid.String(), sd.String())
	}
	os.Remove(path)
	s, err := OpenResultStore(dirs.Layout{State: state})
	if err != nil {
		t.Fatal("open", err)
	}
	defer s.Close()
	texts := []string{"电话13800001234"}
	r := Result{Complete: true, Entities: []Entity{{Field: 0, Start: 6, End: 17, Text: "13800001234", Category: "PHONE"}}}
	if err = s.Save(context.Background(), "workbuddy/1", "material", texts, r); err != nil {
		t.Fatal(err)
	}
}

func TestResultStorePreservesMissingKeyAndUnknownSchemaFailures(t *testing.T) {
	for _, damage := range []string{"key", "schema"} {
		t.Run(damage, func(t *testing.T) {
			l := dirs.Layout{State: t.TempDir()}
			store, err := OpenResultStore(l)
			if err != nil {
				t.Fatal(err)
			}
			if err = store.Save(context.Background(), "app/1", "material", []string{"zero"}, Result{Complete: true}); err != nil {
				t.Fatal(err)
			}
			store.Close()
			dbPath := filepath.Join(l.RedactionResults(), "results.sqlite")
			if damage == "key" {
				if err = os.Remove(filepath.Join(l.RedactionResults(), "keys.dpapi")); err != nil {
					t.Fatal(err)
				}
			} else {
				db, e := sql.Open("sqlite", dbPath)
				if e != nil {
					t.Fatal(e)
				}
				_, e = db.Exec("PRAGMA user_version=77")
				db.Close()
				if e != nil {
					t.Fatal(e)
				}
			}
			before, err := os.ReadFile(dbPath)
			if err != nil {
				t.Fatal(err)
			}
			if opened, e := OpenResultStore(l); e == nil {
				opened.Close()
				t.Fatal("unsafe automatic store initialization")
			}
			after, err := os.ReadFile(dbPath)
			if err != nil || string(before) != string(after) {
				t.Fatal("failed opening mutated existing evidence", err)
			}
		})
	}
}
