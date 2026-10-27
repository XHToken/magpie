package routerdevice

import (
	"context"
	"unsafe"

	"golang.org/x/sys/windows"
)

func nativeVolume(ctx context.Context, path string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	path, err := existingDirectory(path)
	if err != nil {
		return "", err
	}
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return "", err
	}
	var mount, volume [windows.MAX_PATH + 1]uint16
	dll := windows.NewLazySystemDLL("kernel32.dll")
	getPath := dll.NewProc("GetVolumePathNameW")
	getName := dll.NewProc("GetVolumeNameForVolumeMountPointW")
	if err := getPath.Find(); err != nil {
		return "", err
	}
	if err := getName.Find(); err != nil {
		return "", err
	}
	if ok, _, err := getPath.Call(uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(&mount[0])), uintptr(len(mount))); ok == 0 {
		return "", err
	}
	if ok, _, err := getName.Call(uintptr(unsafe.Pointer(&mount[0])), uintptr(unsafe.Pointer(&volume[0])), uintptr(len(volume))); ok == 0 {
		return "", err
	}
	return windows.UTF16ToString(volume[:]), nil
}
