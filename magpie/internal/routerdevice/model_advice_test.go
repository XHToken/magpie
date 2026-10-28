package routerdevice

import "testing"

func adviceOptions() []ModelOption {
	disk := []DiskBudget{{Path: "/models", Volume: Known("one"), Available: Known(100 * GiB), Required: 10 * GiB}}
	return []ModelOption{{ID: "q4", Name: "Q4", Quantization: "Q4_K_M", RuntimeAvailable: true, Disks: disk}, {ID: "q8", Name: "Q8", Quantization: "Q8_0", RuntimeAvailable: true, Disks: disk}}
}

func adviceSnapshot(ram, available uint64, threads int) Snapshot {
	s := fixture("windows", ram)
	s.AvailableMemoryBytes = Known(available)
	s.System.Value.LogicalCPUs = Known(threads)
	return s
}

func addAdviceGPU(s *Snapshot, total, free uint64) {
	s.Adapters = Known([]Adapter{{ID: "gpu", Name: "Fixture GPU", Discrete: Available, MemoryBytes: Known(total), FreeBytes: Known(free)}})
	s.Backends = Known([]Backend{{Kind: CPU, State: Available}, {Kind: CUDA, State: Available, AdapterID: "gpu"}})
}

func TestModelAdviceQuantizationAndResourceBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name              string
		ram, available    uint64
		cpus              int
		gpuTotal, gpuFree uint64
		state, model      string
	}{
		{"8GB cloud", 8 * GiB, 4 * GiB, 4, 0, 0, "not_recommended", ""},
		{"below16GB cloud", 16*GiB - 1, 10 * GiB, 8, 0, 0, "not_recommended", ""},
		{"memory occupied", 16 * GiB, 8*GiB - 1, 8, 0, 0, "deferred", ""},
		{"Q4 at threshold", 16 * GiB, 8 * GiB, 8, 0, 0, "recommended", "q4"},
		{"CPU memory alone does not choose Q8", 64 * GiB, 40 * GiB, 32, 0, 0, "recommended", "q4"},
		{"weak CPU manual only", 16 * GiB, 9 * GiB, 4, 0, 0, "not_recommended", ""},
		{"8GB GPU", 16 * GiB, 9 * GiB, 4, 8 * GiB, 7 * GiB, "recommended", "q4"},
		{"12GB free GPU", 16 * GiB, 9 * GiB, 4, 12 * GiB, 11 * GiB, "recommended", "q8"},
		{"12GB occupied GPU", 16 * GiB, 9 * GiB, 4, 12 * GiB, 9 * GiB, "recommended", "q4"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := adviceSnapshot(tc.ram, tc.available, tc.cpus)
			if tc.gpuTotal != 0 {
				addAdviceGPU(&s, tc.gpuTotal, tc.gpuFree)
			}
			a := AdviseModels(s, adviceOptions(), RunningModel{})
			if a.State != tc.state || a.ModelID != tc.model {
				t.Fatalf("got %+v, want %s/%s", a, tc.state, tc.model)
			}
		})
	}
}

func TestModelAdviceUnknownFactsManualChoicesAndHealthyReuse(t *testing.T) {
	s := adviceSnapshot(32*GiB, 12*GiB, 8)
	options := adviceOptions()
	a := AdviseModels(s, options, RunningModel{})
	q8, _ := a.Model("q8")
	if q8.Manual != CheckPassed || q8.Automatic == CheckPassed || a.ModelID != "q4" {
		t.Fatal("CPU Q8 manual and automatic choices were conflated", a)
	}
	s.MemoryBytes = Known(15 * GiB)
	a = AdviseModels(s, options, RunningModel{})
	q8, _ = a.Model("q8")
	q4, _ := a.Model("q4")
	if q8.Manual != CheckFailed || q4.Manual != CheckPassed {
		t.Fatal("Q8's manual total-memory budget was conflated with Q4's", a)
	}
	s.MemoryBytes = Known(32 * GiB)
	s.AvailableMemoryBytes = Unknown[uint64]("read failed")
	if a := AdviseModels(s, options, RunningModel{}); a.State != "deferred" {
		t.Fatal("unknown memory became a recommendation", a)
	}
	s.AvailableMemoryBytes = Known(2 * GiB)
	if a := AdviseModels(s, options, RunningModel{ID: "q8", Healthy: true}); a.ModelID != "q8" || !a.Reuse {
		t.Fatal("running model was charged for its occupied memory twice", a)
	}
	if a := AdviseModels(s, options, RunningModel{ID: "q8", Healthy: true, Slow: true}); a.State != "deferred" || a.Reason != "performance_slow" {
		t.Fatal("slow evidence was ignored", a)
	}
	s = adviceSnapshot(16*GiB, 9*GiB, 4)
	s.Adapters = Unknown[[]Adapter]("adapter query failed")
	s.Backends = Known([]Backend{{Kind: CPU, State: Available}, {Kind: CUDA, State: UnknownAvailability}})
	if a := AdviseModels(s, options, RunningModel{}); a.State != "deferred" {
		t.Fatal("unknown acceleration was called weak hardware", a)
	}
}

func TestModelAdviceUnknownSystemStillChecksKnownShortages(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*Snapshot, []ModelOption)
	}{
		{"memory", func(s *Snapshot, _ []ModelOption) { s.AvailableMemoryBytes = Known(2 * GiB) }},
		{"total memory", func(s *Snapshot, _ []ModelOption) { s.MemoryBytes = Known(4 * GiB) }},
		{"disk", func(_ *Snapshot, options []ModelOption) { options[0].Disks[0].Available = Known(uint64(0)) }},
		{"disk with unknown volume", func(_ *Snapshot, options []ModelOption) {
			options[0].Disks[0].Volume = Unknown[string]("volume probe failed")
			options[0].Disks[0].Available = Known(uint64(0))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, options := adviceSnapshot(32*GiB, 16*GiB, 8), adviceOptions()
			s.System = Unknown[System]("system probe failed")
			tc.change(&s, options)
			assessment, _ := AdviseModels(s, options, RunningModel{}).Model("q4")
			if assessment.Manual != CheckFailed {
				t.Fatalf("unknown system hid known %s shortage: %+v", tc.name, assessment)
			}
		})
	}
}

func TestModelAdviceMissingFactsAllowManualButNotAutomaticUse(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*Snapshot, []ModelOption)
	}{
		{"system", func(s *Snapshot, _ []ModelOption) { s.System = Unknown[System]("read failed") }},
		{"OS version", func(s *Snapshot, _ []ModelOption) { s.System.Value.Version = Unknown[string]("read failed") }},
		{"threads", func(s *Snapshot, _ []ModelOption) { s.System.Value.LogicalCPUs = Unknown[int]("read failed") }},
		{"memory", func(s *Snapshot, _ []ModelOption) { s.MemoryBytes = Unknown[uint64]("read failed") }},
		{"available memory", func(s *Snapshot, _ []ModelOption) { s.AvailableMemoryBytes = Unknown[uint64]("read failed") }},
		{"backends", func(s *Snapshot, _ []ModelOption) { s.Backends = Unknown[[]Backend]("read failed") }},
		{"disk", func(_ *Snapshot, options []ModelOption) {
			for i := range options {
				options[i].Disks[0].Available = Unknown[uint64]("read failed")
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, options := adviceSnapshot(32*GiB, 16*GiB, 8), adviceOptions()
			tc.change(&s, options)
			a := AdviseModels(s, options, RunningModel{})
			for _, c := range a.Assessments {
				if c.Manual != CheckUnknown || !c.AllowsManual() || c.Automatic == CheckPassed {
					t.Fatalf("missing %s blocked manual use or authorized automatic use: %+v", tc.name, c)
				}
			}
		})
	}
}

func TestModelAdviceDistinguishesBusyGPUFromWeakHardware(t *testing.T) {
	s := adviceSnapshot(16*GiB, 9*GiB, 4)
	addAdviceGPU(&s, 12*GiB, 2*GiB)
	if a := AdviseModels(s, adviceOptions(), RunningModel{}); a.State != "deferred" || a.Reason != "gpu_memory_insufficient" {
		t.Fatal("temporary GPU pressure became weak hardware", a)
	}
	s.Adapters = Known([]Adapter{})
	s.Backends = Known([]Backend{{Kind: CPU, State: Available}, {Kind: CUDA, State: UnknownAvailability}})
	if a := AdviseModels(s, adviceOptions(), RunningModel{}); a.State != "not_recommended" {
		t.Fatal("an unavailable discrete adapter became unknown acceleration", a)
	}
}

func TestModelAdviceDoesNotPoolVRAMAndChecksEachModelDisk(t *testing.T) {
	s := adviceSnapshot(16*GiB, 9*GiB, 4)
	addAdviceGPU(&s, 8*GiB, 7*GiB)
	*s.Adapters.Value = append(*s.Adapters.Value, Adapter{ID: "other", Discrete: Available, MemoryBytes: Known(8 * GiB), FreeBytes: Known(7 * GiB)})
	*s.Backends.Value = append(*s.Backends.Value, Backend{Kind: CUDA, State: Available, AdapterID: "other"})
	if a := AdviseModels(s, adviceOptions(), RunningModel{}); a.ModelID != "q4" {
		t.Fatal("VRAM from multiple devices was pooled", a)
	}
	addAdviceGPU(&s, 12*GiB, 11*GiB)
	options := adviceOptions()
	options[1].Disks = []DiskBudget{{Volume: Known("one"), Available: Known(10 * GiB), Required: 12 * GiB}}
	if a := AdviseModels(s, options, RunningModel{}); a.ModelID != "q4" || a.Reason != "q8_disk_insufficient" {
		t.Fatal("Q4 was blocked by Q8's larger reservation", a)
	}
	options[0].Disks = []DiskBudget{{Volume: Known("one"), Available: Known(10*GiB - 1), Required: 10 * GiB}}
	if a := AdviseModels(s, options, RunningModel{}); a.State != "deferred" {
		t.Fatal("insufficient disk became poor hardware or a recommendation", a)
	}
}

func TestModelAdviceUnifiedMemoryIsNotDiscreteVRAM(t *testing.T) {
	s := fixture("darwin", 32*GiB)
	s.AvailableMemoryBytes = Known(12 * GiB)
	if a := AdviseModels(s, adviceOptions(), RunningModel{}); a.ModelID != "q8" {
		t.Fatal("confirmed Metal unified-memory path was lost", a)
	}
	s.AvailableMemoryBytes = Known(12*GiB - 1)
	if a := AdviseModels(s, adviceOptions(), RunningModel{}); a.ModelID != "q4" {
		t.Fatal("Q8's unified-memory threshold was ignored", a)
	}
	s = adviceSnapshot(32*GiB, 12*GiB, 8)
	addAdviceGPU(&s, 32*GiB, 30*GiB)
	(*s.Adapters.Value)[0].Discrete = Unavailable
	if a := AdviseModels(s, adviceOptions(), RunningModel{}); a.ModelID != "q4" {
		t.Fatal("shared memory was counted as dedicated VRAM", a)
	}
}

func TestMacQ4With4Point7GiBAllowsManualTrial(t *testing.T) {
	s := fixture("darwin", 16*GiB)
	s.AvailableMemoryBytes = Known(47 * GiB / 10)
	a := AdviseModels(s, adviceOptions(), RunningModel{})
	q4, _ := a.Model("q4")
	q8, _ := a.Model("q8")
	if !q4.AllowsManual() || q4.Automatic == CheckPassed || a.State == "recommended" {
		t.Fatalf("4.7 GiB should allow an explicit Mac Q4 trial without automatic recommendation: %+v", a)
	}
	if q8.AllowsManual() {
		t.Fatal("Q4 trial relaxed Q8's memory budget", q8)
	}
}

func TestMacDownloadAndTrialBoundaries(t *testing.T) {
	for _, arch := range []string{"arm64", "amd64"} {
		for _, available := range []uint64{0, 4*GiB - 1, 4 * GiB, 47 * GiB / 10, 8*GiB - 1, 8 * GiB} {
			s := fixture("darwin", 16*GiB)
			s.System.Value.Architecture = Known(arch)
			s.AvailableMemoryBytes = Known(available)
			a := AdviseModels(s, adviceOptions(), RunningModel{})
			q4, _ := a.Model("q4")
			if !q4.AllowsDownload() || q4.AllowsManual() != (available >= 4*GiB) || (q4.Automatic == CheckPassed) != (available >= 8*GiB) {
				t.Fatalf("Mac %s/%d headroom: %+v", arch, available, q4)
			}
		}
	}
	for _, osName := range []string{"windows", "linux"} {
		s := fixture(osName, 16*GiB)
		s.AvailableMemoryBytes = Known(47 * GiB / 10)
		q4, _ := AdviseModels(s, adviceOptions(), RunningModel{}).Model("q4")
		if q4.AllowsManual() || q4.AllowsDownload() {
			t.Fatalf("Mac trial changed %s admission: %+v", osName, q4)
		}
	}
	for _, issue := range []string{"disk", "runtime", "platform", "version"} {
		s, options := fixture("darwin", 16*GiB), adviceOptions()
		s.AvailableMemoryBytes = Known(47 * GiB / 10)
		switch issue {
		case "disk":
			options[0].Disks[0].Available = Known(uint64(0))
		case "runtime":
			options[0].RuntimeAvailable = false
		case "platform":
			s.System.Value.Architecture = Known("386")
		case "version":
			s.System.Value.Version = Known("13.0")
		}
		q4, _ := AdviseModels(s, options, RunningModel{}).Model("q4")
		if q4.AllowsDownload() || q4.AllowsManual() {
			t.Fatalf("Mac trial bypassed %s: %+v", issue, q4)
		}
	}
}
