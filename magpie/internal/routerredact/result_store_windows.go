package routerredact

import (
	"os"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

func resultKeyFile() string { return "keys.dpapi" }

func checkResultPath(path string) error {
	for {
		p, e := windows.UTF16PtrFromString(path)
		if e != nil {
			return e
		}
		attr, e := windows.GetFileAttributes(p)
		if e != nil || attr&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
			return fail("redaction_store_permissions")
		}
		parent := filepath.Dir(path)
		if parent == path {
			return nil
		}
		path = parent
	}
}
func resultUserSID() (*windows.SID, error) {
	user, e := windows.GetCurrentProcessToken().GetTokenUser()
	if e != nil {
		return nil, e
	}
	return user.User.Sid, nil
}
func secureResultDirectory(path string) error {
	sid, e := resultUserSID()
	if e != nil {
		return fail("redaction_store_permissions")
	}
	sd, e := windows.SecurityDescriptorFromString("D:P(A;OICI;FA;;;" + sid.String() + ")(A;OICI;FA;;;SY)")
	if e != nil {
		return fail("redaction_store_permissions")
	}
	acl, _, e := sd.DACL()
	if e != nil {
		return fail("redaction_store_permissions")
	}
	if e = windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, acl, nil); e != nil {
		return fail("redaction_store_permissions")
	}
	return verifyResultPrivate(path)
}
func verifyResultPrivate(path string) error {
	sid, e := resultUserSID()
	if e != nil {
		return fail("redaction_store_permissions")
	}
	sd, e := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if e != nil {
		return fail("redaction_store_permissions")
	}
	owner, _, e := sd.Owner()
	administrators, adminErr := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
	// Elevated Windows processes create objects owned by Administrators. The
	// DACL below still grants access only to this user and SYSTEM; administrators
	// can take ownership of any local file independently of this store.
	if e != nil || adminErr != nil || (!owner.Equals(sid) && !owner.Equals(administrators)) {
		return fail("redaction_store_permissions")
	}
	acl, _, e := sd.DACL()
	if e != nil || acl == nil || acl.AceCount == 0 {
		return fail("redaction_store_permissions")
	}
	system, e := windows.CreateWellKnownSid(windows.WinLocalSystemSid)
	if e != nil {
		return fail("redaction_store_permissions")
	}
	for i := uint32(0); i < uint32(acl.AceCount); i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if windows.GetAce(acl, i, &ace) != nil || ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
			return fail("redaction_store_permissions")
		}
		who := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		if !who.Equals(sid) && !who.Equals(system) {
			return fail("redaction_store_permissions")
		}
	}
	return nil
}
func protectResultKey(raw []byte) ([]byte, error) {
	in := windows.DataBlob{Size: uint32(len(raw)), Data: &raw[0]}
	var out windows.DataBlob
	if windows.CryptProtectData(&in, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out) != nil {
		return nil, fail("redaction_store_key_unavailable")
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return append([]byte(nil), unsafe.Slice(out.Data, out.Size)...), nil
}
func unprotectResultKey(raw []byte) ([]byte, error) {
	if len(raw) == 0 {
		return nil, fail("redaction_store_key_unavailable")
	}
	in := windows.DataBlob{Size: uint32(len(raw)), Data: &raw[0]}
	var out windows.DataBlob
	if windows.CryptUnprotectData(&in, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out) != nil {
		return nil, fail("redaction_store_key_unavailable")
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return append([]byte(nil), unsafe.Slice(out.Data, out.Size)...), nil
}
func lockResultStore(path string) (*os.File, error) {
	f, e := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, fail("redaction_store_unavailable")
	}
	if verifyResultPrivate(path) != nil || windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &windows.Overlapped{}) != nil {
		f.Close()
		return nil, fail("redaction_store_unavailable")
	}
	return f, nil
}
