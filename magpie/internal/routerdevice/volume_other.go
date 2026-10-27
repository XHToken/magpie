//go:build !linux && !darwin && !windows

package routerdevice

import (
	"context"
	"errors"
)

func nativeVolume(context.Context, string) (string, error) {
	return "", errors.New("volume_unsupported")
}
