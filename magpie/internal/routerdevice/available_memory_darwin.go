package routerdevice

import (
	"context"
	"fmt"
	"sync"

	"github.com/ebitengine/purego"
	"golang.org/x/sys/unix"
)

// Keep the library and one host send right for the process lifetime. Only the
// bindings are cached; each invocation reads fresh memory statistics.
var darwinMemoryReader = sync.OnceValues(func() (func() (uint64, error), error) {
	lib, err := purego.Dlopen("/usr/lib/libSystem.B.dylib", purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		return nil, err
	}
	var hostSelf func() uint32
	var pageSize func(uint32, *uint32) int32
	var statistics func(uint32, int32, *uint32, *uint32) int32
	for _, binding := range []struct {
		name string
		fn   any
	}{{"mach_host_self", &hostSelf}, {"host_page_size", &pageSize}, {"host_statistics64", &statistics}} {
		address, err := purego.Dlsym(lib, binding.name)
		if err != nil {
			return nil, err
		}
		purego.RegisterFunc(binding.fn, address)
	}
	host := hostSelf()
	if host == 0 {
		return nil, fmt.Errorf("Mach host unavailable")
	}
	return func() (uint64, error) {
		var size uint32
		if code := pageSize(host, &size); code != 0 {
			return 0, fmt.Errorf("host_page_size failed: %d", code)
		}
		// HOST_VM_INFO64 (4), with room for later ABI revisions. The first
		// four natural_t fields are free, active, inactive and wired pages.
		var stats [256]uint32
		count := uint32(len(stats))
		if code := statistics(host, 4, &stats[0], &count); code != 0 {
			return 0, fmt.Errorf("host_statistics64 failed: %d", code)
		}
		if count < 4 || count > uint32(len(stats)) {
			return 0, fmt.Errorf("invalid Mach memory statistics size")
		}
		total, err := unix.SysctlUint64("hw.memsize")
		if err != nil {
			return 0, err
		}
		return darwinMemoryBytes(uint64(stats[0]), uint64(stats[2]), uint64(size), total)
	}, nil
})

func availableMemory(context.Context) (uint64, error) {
	read, err := darwinMemoryReader()
	if err != nil {
		return 0, err
	}
	return read()
}
