package routerredact

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/godbus/dbus/v5"

	"github.com/yetone/magpie/internal/dirs"
	"github.com/yetone/magpie/internal/testenv"
)

// Run only inside scripts/test-redaction-secret-service.sh, whose private bus
// and password-protected keyring disappear at exit. Ordinary tests never touch
// the desktop user's real keyring.
func TestLinuxNativeKeyringRoundTrip(t *testing.T) {
	if os.Getenv("WEFTO_TEST_PRIVATE_KEYRING") != "1" {
		t.Skip("requires the isolated native Secret Service acceptance script")
	}
	t.Setenv(testenv.Marker, "")
	layout := dirs.Layout{State: t.TempDir()}
	store, err := OpenResultStore(layout)
	if err != nil {
		t.Fatal("native Secret Service open:", err)
	}
	text := "phone 13800001234"
	if err := store.Save(context.Background(), "app/1", "native", []string{text}, Result{Complete: true, Entities: []Entity{{0, 6, len(text), "13800001234", "PHONE"}}}); err != nil {
		store.Close()
		t.Fatal(err)
	}
	store.Close()
	cmd := exec.Command(os.Args[0], "-test.run=^TestLinuxNativeKeyringChild$", "-test.v")
	cmd.Env = append(os.Environ(), "WEFTO_TEST_RESULT_STATE="+layout.State)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fresh process failed to reuse system keyring: %v\n%s", err, output)
	}
	ref, err := os.ReadFile(filepath.Join(layout.RedactionResults(), resultKeyFile()))
	if err != nil || string(ref) == "" {
		t.Fatal("missing native vault reference", err)
	}
	var reference resultVaultReference
	if json.Unmarshal(ref, &reference) != nil {
		t.Fatal("invalid native vault reference")
	}
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	service := conn.Object("org.freedesktop.secrets", "/org/freedesktop/secrets")
	var found, locked []dbus.ObjectPath
	if service.Call(resultSecretService+".SearchItems", 0, resultSecretAttributes(reference.Account)).Store(&found, &locked) != nil || len(found) != 1 {
		t.Fatal("native vault item missing")
	}
	var completed []dbus.ObjectPath
	var prompt dbus.ObjectPath
	if service.Call(resultSecretService+".Lock", 0, found).Store(&completed, &prompt) != nil || prompt != "/" || len(completed) == 0 {
		t.Fatal("could not lock isolated native item")
	}
	db := filepath.Join(layout.RedactionResults(), "results.sqlite")
	before, err := os.ReadFile(db)
	if err != nil {
		t.Fatal(err)
	}
	if opened, err := OpenResultStore(layout); err == nil {
		opened.Close()
		t.Fatal("native locked item accepted")
	}
	after, err := os.ReadFile(db)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("locked keyring damaged saved evidence", err)
	}
}

func TestLinuxNativeKeyringChild(t *testing.T) {
	state := os.Getenv("WEFTO_TEST_RESULT_STATE")
	if state == "" || os.Getenv("WEFTO_TEST_PRIVATE_KEYRING") != "1" {
		t.Skip("native acceptance child only")
	}
	t.Setenv(testenv.Marker, "")
	store, err := OpenResultStore(dirs.Layout{State: state})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	loaded, err := store.Load(context.Background(), "app/1", "native", [][]string{{"phone 13800001234"}})
	if err != nil || loaded[0] == nil || len(loaded[0].Entities) != 1 || loaded[0].Entities[0].Text != "13800001234" {
		t.Fatal("native persisted recognition lost", err)
	}
}
