package routerprocess

import (
	"context"

	"golang.org/x/sys/windows"
)

// BackendHints decides only which optional packages an explicit installation
// may prepare. ProbeDevices and actual offload must still confirm usability.
func BackendHints(ctx context.Context) []string {
	var kinds []string
	for _, v := range []struct{ library, entry, backend string }{
		{"nvcuda.dll", "cuInit", "cuda"},
		{"vulkan-1.dll", "vkGetInstanceProcAddr", "vulkan"},
	} {
		if ctx.Err() != nil {
			break
		}
		if windows.NewLazySystemDLL(v.library).NewProc(v.entry).Find() == nil {
			kinds = append(kinds, v.backend)
		}
	}
	return kinds
}
