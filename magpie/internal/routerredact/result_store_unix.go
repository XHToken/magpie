//go:build linux || darwin

package routerredact

import (
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
)

func resultKeyFile() string { return "keys.vault" }

func checkResultPath(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return fail("redaction_store_permissions")
	}
	for {
		info, err := os.Lstat(path)
		if err != nil {
			return fail("redaction_store_permissions")
		}
		if info.Mode()&os.ModeSymlink != 0 && !resultSystemAlias(path, info) {
			return fail("redaction_store_permissions")
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || (stat.Uid != uint32(os.Geteuid()) && stat.Uid != 0) {
			return fail("redaction_store_permissions")
		}
		if info.IsDir() && info.Mode().Perm()&0022 != 0 && info.Mode()&os.ModeSticky == 0 {
			return fail("redaction_store_permissions")
		}
		parent := filepath.Dir(path)
		if parent == path {
			return nil
		}
		path = parent
	}
}

func secureResultDirectory(path string) error {
	if os.Chmod(path, 0700) != nil || clearResultACL(path) != nil {
		return fail("redaction_store_permissions")
	}
	return verifyResultPrivate(path)
}

func verifyResultPrivate(path string) error {
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || (!info.IsDir() && !info.Mode().IsRegular()) {
		return fail("redaction_store_permissions")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	want := os.FileMode(0600)
	if info.IsDir() {
		want = 0700
	}
	if !ok || stat.Uid != uint32(os.Geteuid()) || info.Mode().Perm() != want || (!info.IsDir() && stat.Nlink != 1) || verifyResultACL(path) != nil {
		return fail("redaction_store_permissions")
	}
	return nil
}

func lockResultStore(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, fail("redaction_store_unavailable")
	}
	f := os.NewFile(uintptr(fd), path)
	if verifyResultPrivate(path) != nil || unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB) != nil {
		f.Close()
		return nil, fail("redaction_store_unavailable")
	}
	return f, nil
}
