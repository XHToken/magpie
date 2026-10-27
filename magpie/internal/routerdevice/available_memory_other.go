//go:build !windows && !linux && !darwin

package routerdevice

import (
	"context"
	"errors"
)

func availableMemory(context.Context) (uint64, error) {
	return 0, errors.New("available physical memory probe unavailable")
}
