//go:build !windows

package routerhost

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"

	"github.com/yetone/magpie/internal/proc"
)

func loomyOpencodeExecutableName() string {
	return "opencode"
}

func terminateExecutableProcesses(targetExe string, matchLoomyBundled bool) {
	cleanTarget := filepath.Clean(targetExe)
	if st, err := os.Stat(cleanTarget); err == nil && st.Mode().IsRegular() {
		_ = proc.Command("pkill", "-f", "^"+regexp.QuoteMeta(cleanTarget)+"([[:space:]]|$)").Run()
	}
	if matchLoomyBundled {
		_ = proc.Command("pkill", "-f", "/Loomy.app/Contents/Resources/opencode/").Run()
		_ = proc.Command("pkill", "-f", "/loomy/resources/opencode/").Run()
		// Loomy copies a quarantined/non-executable bundle binary into its
		// own state directory on macOS/Linux (BinaryRuntimeCoordinator).
		if home, err := os.UserHomeDir(); err == nil {
			// Loomy may use a different architecture (for example Rosetta).
			for _, arch := range []string{"arm64", "x64"} {
				cached := filepath.Join(home, ".local", "state", "loomy", "bin", runtime.GOOS+"-"+arch, "opencode")
				if st, err := os.Stat(cached); err == nil && st.Mode().IsRegular() {
					_ = proc.Command("pkill", "-f", "^"+regexp.QuoteMeta(cached)+"([[:space:]]|$)").Run()
				}
			}
		}
	}
}
