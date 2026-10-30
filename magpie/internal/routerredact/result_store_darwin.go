package routerredact

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"unsafe"

	"github.com/ebitengine/purego"
)

// Security.framework works in CGO-disabled portable builds. Secret bytes never
// enter a subprocess's argument list or a plaintext key file.
type macResultAPI struct {
	core, security, libc uintptr
	release              func(uintptr)
	stringCreate         func(uintptr, string, uint32) uintptr
	dictCreate           func(uintptr, int64, uintptr, uintptr) uintptr
	dictSet              func(uintptr, uintptr, uintptr)
	arrayCreate          func(uintptr, unsafe.Pointer, int64, uintptr) uintptr
	dataCreate           func(uintptr, unsafe.Pointer, int64) uintptr
	dataLength           func(uintptr) int64
	dataBytes            func(uintptr) unsafe.Pointer
	typeID               func(uintptr) uintptr
	dataTypeID           func() uintptr
	add                  func(uintptr, *uintptr) int32
	copy                 func(uintptr, *uintptr) int32
	defaultKeychain      func(*uintptr) int32
	keychainStatus       func(uintptr, *uint32) int32
	aclGet               func(string, int32) uintptr
	aclEntry             func(uintptr, int32, *uintptr) int32
	aclFree              func(uintptr) int32
	aclInit              func(int32) uintptr
	aclSet               func(string, int32, uintptr) int32
	aclValid             func(uintptr) int32
	errno                func() *int32
	copyMemory           func(unsafe.Pointer, uintptr, uintptr) uintptr
}

var resultMacAPI = sync.OnceValues(func() (api *macResultAPI, err error) {
	defer func() {
		if recover() != nil {
			api, err = nil, errors.New("system keychain API unavailable")
		}
	}()
	api = &macResultAPI{}
	for _, library := range []struct {
		path string
		into *uintptr
	}{
		{"/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation", &api.core},
		{"/System/Library/Frameworks/Security.framework/Security", &api.security},
		{"/usr/lib/libSystem.B.dylib", &api.libc},
	} {
		*library.into, err = purego.Dlopen(library.path, purego.RTLD_NOW|purego.RTLD_LOCAL)
		if err != nil {
			return nil, errors.New("system keychain API unavailable")
		}
	}
	for _, fn := range []struct {
		library uintptr
		name    string
		into    any
	}{
		{api.core, "CFRelease", &api.release}, {api.core, "CFStringCreateWithCString", &api.stringCreate},
		{api.core, "CFDictionaryCreateMutable", &api.dictCreate}, {api.core, "CFDictionarySetValue", &api.dictSet},
		{api.core, "CFArrayCreate", &api.arrayCreate}, {api.core, "CFDataCreate", &api.dataCreate},
		{api.core, "CFDataGetLength", &api.dataLength}, {api.core, "CFDataGetBytePtr", &api.dataBytes},
		{api.core, "CFGetTypeID", &api.typeID}, {api.core, "CFDataGetTypeID", &api.dataTypeID},
		{api.security, "SecItemAdd", &api.add}, {api.security, "SecItemCopyMatching", &api.copy},
		{api.security, "SecKeychainCopyDefault", &api.defaultKeychain}, {api.security, "SecKeychainGetStatus", &api.keychainStatus},
		{api.libc, "acl_get_file", &api.aclGet}, {api.libc, "acl_get_entry", &api.aclEntry},
		{api.libc, "acl_free", &api.aclFree}, {api.libc, "acl_init", &api.aclInit},
		{api.libc, "acl_set_file", &api.aclSet}, {api.libc, "acl_valid", &api.aclValid}, {api.libc, "__error", &api.errno},
		{api.libc, "memcpy", &api.copyMemory},
	} {
		purego.RegisterLibFunc(fn.into, fn.library, fn.name)
	}
	return api, nil
})

func (a *macResultAPI) symbol(library uintptr, name string) uintptr {
	address, err := purego.Dlsym(library, name)
	if err != nil {
		return 0
	}
	return address
}
func (a *macResultAPI) constant(library uintptr, name string) uintptr {
	address := a.symbol(library, name)
	if address == 0 {
		return 0
	}
	var value uintptr
	a.copyMemory(unsafe.Pointer(&value), address, unsafe.Sizeof(value))
	return value
}
func (a *macResultAPI) setString(dict uintptr, name, value string) bool {
	key := a.constant(a.security, name)
	text := a.stringCreate(0, value, 0x08000100) // kCFStringEncodingUTF8
	if key == 0 || text == 0 {
		return false
	}
	defer a.release(text)
	a.dictSet(dict, key, text)
	return true
}
func (a *macResultAPI) setConstant(dict uintptr, key, value string) bool {
	k, v := a.constant(a.security, key), a.constant(a.security, value)
	if k == 0 || v == 0 {
		return false
	}
	a.dictSet(dict, k, v)
	return true
}

type macResultVault struct{ keychain uintptr } // nonzero only for an isolated test keychain

func (v macResultVault) query(id string, adding bool) (*macResultAPI, uintptr, error) {
	if v.keychain == 0 && os.Getenv("MAGPIE_TEST_SANDBOX") != "" {
		return nil, 0, fail("redaction_store_key_unavailable")
	}
	a, err := resultMacAPI()
	if err != nil {
		return nil, 0, fail("redaction_store_key_unavailable")
	}
	keychain := v.keychain
	if keychain == 0 {
		if a.defaultKeychain(&keychain) != 0 || keychain == 0 {
			return nil, 0, fail("redaction_store_key_unavailable")
		}
		defer a.release(keychain)
	}
	var status uint32
	if a.keychainStatus(keychain, &status) != 0 || status&1 == 0 {
		return nil, 0, fail("redaction_store_key_unavailable")
	}
	dict := a.dictCreate(0, 0, a.symbol(a.core, "kCFTypeDictionaryKeyCallBacks"), a.symbol(a.core, "kCFTypeDictionaryValueCallBacks"))
	if dict == 0 {
		return nil, 0, fail("redaction_store_key_unavailable")
	}
	ok := a.setConstant(dict, "kSecClass", "kSecClassGenericPassword") && a.setConstant(dict, "kSecUseAuthenticationUI", "kSecUseAuthenticationUIFail") && a.setString(dict, "kSecAttrService", resultVaultService) && a.setString(dict, "kSecAttrAccount", id)
	if adding {
		a.dictSet(dict, a.constant(a.security, "kSecUseKeychain"), keychain)
	} else {
		array := a.arrayCreate(0, unsafe.Pointer(&keychain), 1, a.symbol(a.core, "kCFTypeArrayCallBacks"))
		if array == 0 {
			ok = false
		} else {
			a.dictSet(dict, a.constant(a.security, "kSecMatchSearchList"), array)
			a.release(array)
		}
	}
	if !ok {
		a.release(dict)
		return nil, 0, fail("redaction_store_key_unavailable")
	}
	return a, dict, nil
}

func (v macResultVault) put(id string, raw []byte) error {
	a, dict, err := v.query(id, true)
	if err != nil {
		return err
	}
	defer a.release(dict)
	data := a.dataCreate(0, unsafe.Pointer(&raw[0]), int64(len(raw)))
	runtime.KeepAlive(raw)
	if data == 0 {
		return fail("redaction_store_key_unavailable")
	}
	defer a.release(data)
	a.dictSet(dict, a.constant(a.security, "kSecValueData"), data)
	if a.add(dict, nil) != 0 {
		return fail("redaction_store_key_unavailable")
	}
	return nil
}
func (v macResultVault) get(id string) ([]byte, error) {
	a, dict, err := v.query(id, false)
	if err != nil {
		return nil, err
	}
	defer a.release(dict)
	a.dictSet(dict, a.constant(a.security, "kSecReturnData"), a.constant(a.core, "kCFBooleanTrue"))
	if !a.setConstant(dict, "kSecMatchLimit", "kSecMatchLimitOne") {
		return nil, fail("redaction_store_key_unavailable")
	}
	var data uintptr
	if a.copy(dict, &data) != 0 || data == 0 {
		return nil, fail("redaction_store_key_unavailable")
	}
	defer a.release(data)
	if a.typeID(data) != a.dataTypeID() {
		return nil, fail("redaction_store_key_unavailable")
	}
	n := a.dataLength(data)
	if n < 1 || n > 16384 {
		return nil, fail("redaction_store_key_unavailable")
	}
	return append([]byte(nil), unsafe.Slice((*byte)(a.dataBytes(data)), int(n))...), nil
}

func protectResultKey(raw []byte) ([]byte, error) {
	return vaultProtectResultKey(raw, "keychain", macResultVault{})
}
func unprotectResultKey(raw []byte) ([]byte, error) {
	return vaultUnprotectResultKey(raw, "keychain", macResultVault{})
}

func resultSystemAlias(path string, info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 {
		return false
	}
	if path != "/var" && path != "/tmp" && path != "/etc" {
		return false
	}
	target, err := os.Readlink(path)
	if !filepath.IsAbs(target) {
		target = filepath.Join("/", target)
	}
	return err == nil && filepath.Clean(target) == "/private"+path
}

const resultMacACLExtended = 0x100

func clearResultACL(path string) error {
	a, err := resultMacAPI()
	if err != nil {
		return fail("redaction_store_permissions")
	}
	acl := a.aclInit(0)
	if acl == 0 {
		return fail("redaction_store_permissions")
	}
	defer a.aclFree(acl)
	if a.aclSet(path, resultMacACLExtended, acl) != 0 {
		return fail("redaction_store_permissions")
	}
	return nil
}
func verifyResultACL(path string) error {
	a, err := resultMacAPI()
	if err != nil {
		return fail("redaction_store_permissions")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	*a.errno() = 0
	acl := a.aclGet(path, resultMacACLExtended)
	if acl == 0 {
		if *a.errno() == 2 { // ENOENT from filesec_get_property: no extended ACL
			return nil
		}
		return fail("redaction_store_permissions")
	}
	defer a.aclFree(acl)
	if a.aclValid(acl) != 0 {
		return fail("redaction_store_permissions")
	}
	var entry uintptr
	// Darwin returns 0 for an entry and -1/EINVAL for an empty, valid ACL.
	// This differs from Linux's 1/0 convention.
	if a.aclEntry(acl, 0, &entry) != -1 || *a.errno() != 22 {
		return fail("redaction_store_permissions")
	}
	return nil
}
