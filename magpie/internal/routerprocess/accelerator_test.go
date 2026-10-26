package routerprocess

import (
	"strings"
	"testing"
)

func TestPinnedEngineDeviceListingRequiresKnownMemoryAndUniqueIDs(t *testing.T) {
	raw := "Available devices:\n  CUDA0: NVIDIA synthetic device (8192 MiB, 6144 MiB free)\n  Vulkan1: AMD synthetic device (4096 MiB, 3072 MiB free)\n"
	devices, err := parseDevices(raw)
	if err != nil || len(devices) != 2 || devices[0].ID != "CUDA0" || devices[0].FreeBytes != 6144<<20 {
		t.Fatal(devices, err)
	}
	for _, bad := range []string{"driver failed", raw + "  CUDA0: duplicate (8192 MiB, 6144 MiB free)\n", "Available devices:\n  CUDA0: broken (4096 MiB, 8192 MiB free)\n"} {
		if _, err := parseDevices(bad); err == nil {
			t.Fatal("ambiguous or invalid probe accepted")
		}
	}
	if got, err := parseDevices("Available devices:\n  (none)\n"); err != nil || len(got) != 0 {
		t.Fatal(got, err)
	}
}

func TestGPUStartupEvidenceNeedsDeviceBufferAndActualOffload(t *testing.T) {
	for _, test := range []struct {
		device, log string
		want        bool
	}{
		{"CUDA0", "load_tensors: offloaded 25/25 layers to GPU\nload_tensors: CUDA0 model buffer size = 1000.00 MiB\n", true},
		{"Metal", "load_tensors: offloaded 20/25 layers to GPU\nload_tensors: Metal model buffer size = 1000.00 MiB\n", true},
		{"CUDA0", "load_tensors: offloaded 0/25 layers to GPU\nload_tensors: CUDA0 model buffer size = 1000.00 MiB\n", false},
		{"CUDA0", "load_tensors: offloaded 25/25 layers to GPU\nload_tensors: CUDA01 model buffer size = 1000.00 MiB\n", false},
		{"CUDA0", "load_tensors: offloaded 25/25 layers to GPU\nload_tensors: CPU model buffer size = 1000.00 MiB\n", false},
	} {
		e := &startupEvidence{device: test.device}
		for _, b := range []byte(test.log) {
			_, _ = e.Write([]byte{b})
		}
		if got := e.snapshot(true); got.Verified != test.want {
			t.Fatal(got, test.want)
		}
		_, _ = e.Write([]byte("PRIVATE USER CONTENT\n"))
		if e.line != nil {
			t.Fatal("engine logs retained after startup")
		}
	}
}

func TestGPUPlanIsExplicitAndCannotBeOverridden(t *testing.T) {
	s := fixtureSpec(t)
	s.Backend = "cuda"
	s.ContextSize = 65536
	s.GPU = &GPUOptions{Device: "CUDA1", Layers: -1, ReserveMiB: 1024}
	if err := validate(s); err != nil {
		t.Fatal(err)
	}
	args := strings.Join(arguments(s, 8123), " ")
	for _, want := range []string{"--ctx-size 65536", "--device CUDA1", "--split-mode none", "--n-gpu-layers auto", "--fit-target 1024", "--log-verbosity 4", "--log-colors off"} {
		if !strings.Contains(args, want) {
			t.Fatal(args)
		}
	}
	for _, extra := range [][]string{{"--device", "CUDA0"}, {"--fit-ctx=8192"}, {"--rpc", "remote"}, {"--n-gpu-layers", "0"}} {
		s.ExtraArgs = extra
		if validate(s) == nil {
			t.Fatal("GPU contract override admitted", extra)
		}
	}
	s.ExtraArgs = nil
	s.GPU.Device = "CUDA0,CUDA1"
	if validate(s) == nil {
		t.Fatal("unbounded multi-device plan admitted")
	}
	for _, key := range []string{"LLAMA_ARG_DEVICE", "CUDA_VISIBLE_DEVICES", "GGML_BACKEND_PATH", "VK_DRIVER_FILES", "DYLD_LIBRARY_PATH"} {
		if !acceleratorEnvironmentKey(key) {
			t.Fatal("inherited plan override", key)
		}
	}
}
