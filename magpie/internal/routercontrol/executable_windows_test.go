package routercontrol

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/dirs"
	"github.com/yetone/magpie/internal/proc"
	"golang.org/x/sys/windows"
)

func TestLaunchRecordFromWindowsShortExecutablePath(t *testing.T) {
	const child = "routercontrol-short-executable-child"
	if os.Args[len(os.Args)-1] == child {
		layout := dirs.Layout{State: t.TempDir()}
		if err := os.Mkdir(layout.ServiceLock(), 0o700); err != nil {
			t.Fatal(err)
		}
		record, err := LaunchRecord(layout, "test", "short-path-instance", "127.0.0.1:13145", false)
		if err != nil {
			t.Fatalf("launching the same executable through its Windows short path: %v", err)
		}
		if err := Verify(record); err != nil {
			t.Fatal(err)
		}
		return
	}

	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	long, err := filepath.EvalSymlinks(executable)
	if err != nil {
		t.Fatal(err)
	}
	pointer, err := windows.UTF16PtrFromString(long)
	if err != nil {
		t.Fatal(err)
	}
	buffer := make([]uint16, 32768)
	n, err := windows.GetShortPathName(pointer, &buffer[0], uint32(len(buffer)))
	if err != nil || n == 0 || n >= uint32(len(buffer)) {
		t.Fatalf("getting the executable's Windows short path: %d, %v", n, err)
	}
	short := windows.UTF16ToString(buffer[:n])
	if strings.EqualFold(long, short) {
		t.Skip("the executable's volume does not provide a distinct 8.3 alias")
	}
	t.Logf("launching %s through %s", long, short)
	command := proc.Command(short, "-test.run=^TestLaunchRecordFromWindowsShortExecutablePath$", "--", child)
	hide(command)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("short-path service identity failed: %v\n%s", err, output)
	}
}
