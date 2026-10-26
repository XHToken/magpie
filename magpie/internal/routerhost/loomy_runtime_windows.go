//go:build windows

package routerhost

import (
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

func loomyOpencodeExecutableName() string {
	return "opencode.exe"
}

func terminateExecutableProcesses(targetExe string, matchLoomyBundled bool) {
	cleanTarget := filepath.Clean(targetExe)
	hasTargetFile := false
	if st, err := os.Stat(cleanTarget); err == nil && st.Mode().IsRegular() {
		hasTargetFile = true
	}
	if !hasTargetFile && !matchLoomyBundled {
		return
	}

	baseName := filepath.Base(cleanTarget)
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return
	}
	defer windows.CloseHandle(snap)

	loomyPIDs := make(map[uint32]bool)
	type candidate struct {
		pid  uint32
		ppid uint32
	}
	var candidates []candidate

	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	for err := windows.Process32First(snap, &entry); err == nil; err = windows.Process32Next(snap, &entry) {
		exeName := windows.UTF16ToString(entry.ExeFile[:])
		if strings.EqualFold(exeName, "Loomy.exe") {
			loomyPIDs[entry.ProcessID] = true
		}
		if strings.EqualFold(exeName, baseName) {
			candidates = append(candidates, candidate{
				pid:  entry.ProcessID,
				ppid: entry.ParentProcessID,
			})
		}
	}

	for _, c := range candidates {
		terminateMatchingProcess(c.pid, cleanTarget, matchLoomyBundled && loomyPIDs[c.ppid], matchLoomyBundled)
	}
}

func terminateMatchingProcess(pid uint32, cleanTarget string, hasLoomyParent bool, matchLoomyBundled bool) {
	h, err := windows.OpenProcess(
		windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.PROCESS_TERMINATE|windows.SYNCHRONIZE,
		false,
		pid,
	)
	if err != nil {
		return
	}
	defer windows.CloseHandle(h)

	var buf [windows.MAX_PATH * 4]uint16
	size := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &size); err != nil {
		return
	}
	fullPath := filepath.Clean(windows.UTF16ToString(buf[:size]))
	if !strings.EqualFold(fullPath, cleanTarget) && !hasLoomyParent && !(matchLoomyBundled && isLoomyBundledOpencodePath(fullPath)) {
		return
	}
	if err := windows.TerminateProcess(h, 1); err == nil {
		_, _ = windows.WaitForSingleObject(h, 2000)
	}
}

func isLoomyBundledOpencodePath(fullPath string) bool {
	slashed := strings.ToLower(filepath.ToSlash(fullPath))
	if !strings.Contains(slashed, "/loomy/") {
		return false
	}
	return strings.HasSuffix(slashed, "/resources/opencode/win32-x64/opencode.exe") ||
		strings.HasSuffix(slashed, "/resources/opencode/win32-arm64/opencode.exe") ||
		strings.HasSuffix(slashed, "/vendor/opencode/win32-x64/opencode.exe") ||
		strings.HasSuffix(slashed, "/vendor/opencode/win32-arm64/opencode.exe") ||
		strings.HasSuffix(slashed, "/runtime/opencode.exe")
}
