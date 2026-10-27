//go:build linux || darwin

package routerdevice

import (
	"context"
	"strconv"

	"golang.org/x/sys/unix"
)

func nativeVolume(ctx context.Context, path string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	path, err := existingDirectory(path)
	if err != nil {
		return "", err
	}
	var stat unix.Stat_t
	if err := unix.Stat(path, &stat); err != nil {
		return "", err
	}
	return strconv.FormatUint(uint64(stat.Dev), 10), nil
}
