package routercontrol

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"github.com/yetone/magpie/internal/dirs"
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
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return "", "", err
	}
	idx := strings.LastIndex(string(b), ")")
	if idx < 0 {
		return "", "", fmt.Errorf("process exited or unknown: %w", os.ErrProcessDone)
	}
	fields := strings.Fields(string(b)[idx+1:])
	if len(fields) < 20 || fields[0] == "Z" || fields[0] == "X" {
		return "", "", fmt.Errorf("process exited or unknown: %w", os.ErrProcessDone)
	}
	exe, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
	if err != nil {
		if errors.Is(err, os.ErrPermission) && pid != os.Getpid() {
			return fields[19], "", nil
		}
		if errors.Is(err, os.ErrNotExist) {
			return "", "", fmt.Errorf("%w: %v", os.ErrProcessDone, err)
		}
		return "", "", err
	}
	exe = strings.TrimSuffix(exe, " (deleted)")
	return fields[19], exe, nil
}
func AutostartStatus(l dirs.Layout, exe string) (Autostart, error) {
	return Autostart{Name: entryName(l)}, nil
}
func SetAutostart(l dirs.Layout, exe string, enabled bool) (Autostart, error) {
	return Autostart{}, errors.New("login autostart is supported only on Windows")
}
