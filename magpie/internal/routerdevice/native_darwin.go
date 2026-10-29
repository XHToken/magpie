package routerdevice

import (
	"context"
	"fmt"
	"runtime"

	"golang.org/x/sys/unix"
)

func init() { nativeArch = darwinNativeArch }

func darwinNativeArch() string {
	if runtime.GOARCH == "amd64" {
		if v, err := unix.SysctlUint32("hw.optional.arm64"); err == nil && v == 1 {
			return "arm64"
		}
		if v, err := unix.SysctlUint32("sysctl.proc_translated"); err == nil && v == 1 {
			return "arm64"
		}
	}
	return runtime.GOARCH
}

func nativeProbes() Probes {
	return Probes{
		DiskVolume: nativeVolume,
		System: func(ctx context.Context) (System, error) {
			version := observe(ctx, func(context.Context) (string, error) { return unix.Sysctl("kern.osproductversion") })
			name := observe(ctx, func(context.Context) (string, error) { return unix.Sysctl("machdep.cpu.brand_string") })
			return System{OS: "darwin", Version: version, Architecture: Known(darwinNativeArch()), CPU: name, LogicalCPUs: Known(runtime.NumCPU())}, nil
		},
		Memory:          func(context.Context) (uint64, error) { return unix.SysctlUint64("hw.memsize") },
		AvailableMemory: AvailableMemory,
		Adapters: func(context.Context) ([]Adapter, error) {
			// Apple silicon uses unified memory and does not support eGPUs.
			// Neither unified RAM nor Metal's working-set budget is discrete VRAM.
			if darwinNativeArch() != "arm64" {
				return []Adapter{}, nil
			}
			return []Adapter{{Name: "Apple unified memory GPU", Discrete: Unavailable, MemoryBytes: Known(uint64(0))}}, nil
		},
		Disk: func(_ context.Context, path string) (uint64, error) {
			path, err := existingDirectory(path)
			if err != nil {
				return 0, err
			}
			var stat unix.Statfs_t
			if err := unix.Statfs(path, &stat); err != nil {
				return 0, err
			}
			if stat.Bsize <= 0 {
				return 0, fmt.Errorf("invalid filesystem block size")
			}
			return byteSize(stat.Bavail, uint64(stat.Bsize))
		},
		Backends: func(context.Context) ([]Backend, error) {
			arch := darwinNativeArch()
			backends := baselineBackends(arch)
			if arch == "arm64" {
				backends[3] = metalBackend()
			}
			return backends, nil
		},
	}
}
