//go:build !windows && !linux && !darwin

package routercontrol

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/yetone/magpie/internal/dirs"
)

func hide(cmd *exec.Cmd) {}

func startHidden(makeCmd func() *exec.Cmd) (*exec.Cmd, error) {
	cmd := makeCmd()
	hide(cmd)
	return cmd, cmd.Start()
}
func reparse(fi os.FileInfo) bool { return false }

var selfStamp = fmt.Sprint(time.Now().UnixNano())

func processIdentity(pid int) (string, string, error) {
	if pid == os.Getpid() {
		exe, err := os.Executable()
		if err == nil {
			if real, linkErr := filepath.EvalSymlinks(exe); linkErr == nil {
				exe = real
			}
		}
		return selfStamp, exe, err
	}
	return "", "", errors.New("external process control is supported on Windows, macOS and Linux")
}
func AutostartStatus(l dirs.Layout, exe string) (Autostart, error) {
	return Autostart{Name: entryName(l)}, nil
}
func SetAutostart(l dirs.Layout, exe string, enabled bool) (Autostart, error) {
	return Autostart{}, errors.New("login autostart is supported only on Windows")
}
