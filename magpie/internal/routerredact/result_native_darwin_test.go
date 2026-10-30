package routerredact

import (
	"context"
	"crypto/rand"
	"path/filepath"
	"runtime"
	"testing"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/yetone/magpie/internal/dirs"
)

func TestMacNativeKeychainRoundTripAndLock(t *testing.T) {
	a, err := resultMacAPI()
	if err != nil {
		t.Fatal(err)
	}
	var create func(string, uint32, unsafe.Pointer, bool, uintptr, *uintptr) int32
	var remove func(uintptr) int32
	var lock func(uintptr) int32
	var unlock func(uintptr, uint32, unsafe.Pointer, bool) int32
	purego.RegisterLibFunc(&create, a.security, "SecKeychainCreate")
	purego.RegisterLibFunc(&remove, a.security, "SecKeychainDelete")
	purego.RegisterLibFunc(&lock, a.security, "SecKeychainLock")
	purego.RegisterLibFunc(&unlock, a.security, "SecKeychainUnlock")
	password := make([]byte, 32)
	if _, err := rand.Read(password); err != nil {
		t.Fatal(err)
	}
	var keychain uintptr
	if status := create(filepath.Join(t.TempDir(), "isolated.keychain"), uint32(len(password)), unsafe.Pointer(&password[0]), false, 0, &keychain); status != 0 || keychain == 0 {
		t.Fatalf("create isolated native keychain: %d", status)
	}
	runtime.KeepAlive(password)
	defer a.release(keychain)
	defer remove(keychain)
	keys := vaultResultKeyProtector{"keychain", macResultVault{keychain: keychain}}
	layout := dirs.Layout{State: t.TempDir()}
	store, err := OpenResultStoreWithKeyProtector(layout, keys)
	if err != nil {
		t.Fatal("native Keychain open:", err)
	}
	if err := store.Save(context.Background(), "app/1", "native", []string{"phone 13800001234"}, Result{Complete: true, Entities: []Entity{{0, 6, 17, "13800001234", "PHONE"}}}); err != nil {
		store.Close()
		t.Fatal(err)
	}
	store.Close()
	if lock(keychain) != 0 {
		t.Fatal("could not lock isolated keychain")
	}
	if opened, err := OpenResultStoreWithKeyProtector(layout, keys); err == nil {
		opened.Close()
		t.Fatal("locked keychain silently accepted")
	}
	if unlock(keychain, uint32(len(password)), unsafe.Pointer(&password[0]), true) != 0 {
		t.Fatal("could not unlock isolated keychain")
	}
	runtime.KeepAlive(password)
	store, err = OpenResultStoreWithKeyProtector(layout, keys)
	if err != nil {
		t.Fatal("keychain reopen after unlock:", err)
	}
	defer store.Close()
	loaded, err := store.Load(context.Background(), "app/1", "native", [][]string{{"phone 13800001234"}})
	if err != nil || loaded[0] == nil || len(loaded[0].Entities) != 1 {
		t.Fatal("recognition lost after keychain unlock", err)
	}
}
