//go:build !windows

package steady

import "os"

func rename(oldpath, newpath string) error { return os.Rename(oldpath, newpath) }
