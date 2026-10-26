//go:build !windows

package routerhost

import (
	"os"
	"path/filepath"

	"github.com/yetone/magpie/internal/proc"
)

func loomyOpencodeExecutableName() string {
	return "opencode"
}

func terminateExecutableProcesses(targetExe string, matchLoomyBundled bool) {
	cleanTarget := filepath.Clean(targetExe)
	if st, err := os.Stat(cleanTarget); err == nil && st.Mode().IsRegular() {
		_ = proc.Command("pkill", "-f", cleanTarget).Run()
	}
	if matchLoomyBundled {
		_ = proc.Command("pkill", "-f", "/Loomy.app/Contents/Resources/opencode/").Run()
		_ = proc.Command("pkill", "-f", "/loomy/resources/opencode/").Run()
	}
}
