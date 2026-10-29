//go:build darwin

package routercontrol

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"github.com/yetone/magpie/internal/dirs"
	"golang.org/x/sys/unix"
)

func hide(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

func startHidden(makeCmd func() *exec.Cmd) (*exec.Cmd, error) {
	cmd := makeCmd()
	hide(cmd)
	return cmd, cmd.Start()
}

func reparse(fi os.FileInfo) bool { return false }

func processIdentity(pid int) (string, string, error) {
	if pid <= 0 {
		return "", "", os.ErrProcessDone
	}
	kinfo, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		if errors.Is(err, unix.ESRCH) || errors.Is(err, os.ErrNotExist) {
			return "", "", fmt.Errorf("%w: %v", os.ErrProcessDone, err)
		}
		return "", "", err
	}
	// SZOMB == 5 in XNU <sys/proc.h>.
	if kinfo.Proc.P_stat == 5 {
		return "", "", fmt.Errorf("process exited: %w", os.ErrProcessDone)
	}
	stamp := fmt.Sprintf("%d:%d", kinfo.Proc.P_starttime.Sec, kinfo.Proc.P_starttime.Usec)
	if pid == os.Getpid() {
		exe, err := os.Executable()
		if err != nil {
			return "", "", err
		}
		if real, err := filepath.EvalSymlinks(exe); err == nil {
			exe = real
		}
		return stamp, exe, nil
	}
	raw, err := unix.SysctlRaw("kern.procargs2", pid)
	if err != nil {
		// Another user's or system's process reused the PID.
		if errors.Is(err, unix.EACCES) || errors.Is(err, unix.EPERM) || errors.Is(err, unix.EINVAL) {
			if errors.Is(unix.Kill(pid, 0), unix.EPERM) {
				return stamp, "", nil
			}
			if errors.Is(unix.Kill(pid, 0), unix.ESRCH) {
				return "", "", fmt.Errorf("%w: %v", os.ErrProcessDone, err)
			}
		}
		return "", "", err
	}
	exe := parseDarwinProcArgs2ExecPath(raw)
	if exe == "" {
		return "", "", errors.New("darwin process executable path unavailable")
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	return stamp, exe, nil
}

func parseDarwinProcArgs2ExecPath(raw []byte) string {
	if len(raw) <= 4 {
		return ""
	}
	_ = binary.LittleEndian.Uint32(raw[:4])
	rest := raw[4:]
	end := bytes.IndexByte(rest, 0)
	if end <= 0 {
		return ""
	}
	p := string(rest[:end])
	if !filepath.IsAbs(p) {
		return ""
	}
	return filepath.Clean(p)
}

func AutostartStatus(l dirs.Layout, exe string) (Autostart, error) {
	return Autostart{Name: entryName(l)}, nil
}

func SetAutostart(l dirs.Layout, exe string, enabled bool) (Autostart, error) {
	return Autostart{}, errors.New("login autostart is supported only on Windows")
}
