package routerdevice

import (
	"errors"
	"math"
)

// Mach free_count already includes speculative pages. Inactive pages are
// reclaimable; active, wired, compressed and swap memory are not headroom.
func darwinMemoryBytes(free, inactive, pageSize, total uint64) (uint64, error) {
	if free > math.MaxUint64-inactive {
		return 0, errors.New("overflowing Mach page count")
	}
	available, err := byteSize(free+inactive, pageSize)
	if err != nil {
		return 0, err
	}
	if total == 0 || available > total {
		return 0, errors.New("invalid available physical memory size")
	}
	return available, nil
}
