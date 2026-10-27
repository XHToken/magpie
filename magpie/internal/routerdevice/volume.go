package routerdevice

import "context"

// VolumeID reads the existing destination's actual volume; drive letters and
// parent path prefixes alone are not proof that two destinations share space.
func VolumeID(ctx context.Context, path string) Fact[string] {
	volume, err := nativeVolume(ctx, path)
	if err != nil || volume == "" {
		return Unknown[string]("volume_unavailable")
	}
	return Known(volume)
}
