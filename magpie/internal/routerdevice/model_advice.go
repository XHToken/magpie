package routerdevice

import "strings"

const ModelAdvicePolicy = "spark26-64k-v1"

// ResourceIssue contains only safe, structured resource observations.
type ResourceIssue struct {
	Code      string  `json:"code"`
	Available *uint64 `json:"available,omitempty"`
	Required  uint64  `json:"required,omitempty"`
}

type DiskBudget struct {
	Path      string       `json:"path"`
	Volume    Fact[string] `json:"volume"`
	Available Fact[uint64] `json:"available"`
	Required  uint64       `json:"required"`
}

// ModelOption is supplied by the fixed catalog/installer owner, never HTTP.
type ModelOption struct {
	ID               string       `json:"id"`
	Name             string       `json:"name"`
	Quantization     string       `json:"quantization"`
	RuntimeAvailable bool         `json:"runtimeAvailable"`
	Installed        bool         `json:"installed"`
	Disks            []DiskBudget `json:"disks"`
}

type ModelAssessment struct {
	ModelOption
	Hardware       CheckState      `json:"hardware"`
	Automatic      CheckState      `json:"automatic"`
	Manual         CheckState      `json:"manual"`
	Issues         []ResourceIssue `json:"issues"`
	MemoryRequired uint64          `json:"memoryRequired"`
	Reuse          bool            `json:"reuse"`
	weak           bool
}

type ModelAdvice struct {
	Policy       string            `json:"policy"`
	State        string            `json:"state"` // recommended, not_recommended, deferred
	ModelID      string            `json:"modelID,omitempty"`
	ModelName    string            `json:"modelName,omitempty"`
	Quantization string            `json:"quantization,omitempty"`
	Reason       string            `json:"reason"`
	Reuse        bool              `json:"reuse"`
	Assessments  []ModelAssessment `json:"assessments"`
}

type RunningModel struct {
	ID      string
	Healthy bool
	Slow    bool
}

func (a ModelAdvice) Model(id string) (ModelAssessment, bool) {
	for _, candidate := range a.Assessments {
		if strings.EqualFold(candidate.ID, id) {
			return candidate, true
		}
	}
	return ModelAssessment{}, false
}

// AdviseModels is pure. It distinguishes automatic suitability from explicit
// manual admission and never downloads, probes a process or changes intent.
func AdviseModels(s Snapshot, options []ModelOption, running RunningModel) ModelAdvice {
	s = cloneSnapshot(s)
	a := ModelAdvice{Policy: ModelAdvicePolicy, State: "deferred", Reason: "device_information_unknown", Assessments: []ModelAssessment{}}
	for _, option := range options {
		c := assessModel(s, option)
		if running.Healthy && !running.Slow && strings.EqualFold(option.ID, running.ID) && option.RuntimeAvailable {
			c.Automatic, c.Manual, c.Reuse = CheckPassed, CheckPassed, true
			c.Issues = []ResourceIssue{}
		}
		a.Assessments = append(a.Assessments, c)
	}
	if running.Healthy && running.Slow {
		a.State, a.Reason = "deferred", "performance_slow"
		return a
	}
	selectModel := func(c ModelAssessment, reason string) {
		a.State, a.ModelID, a.ModelName, a.Quantization, a.Reason, a.Reuse = "recommended", c.ID, c.Name, c.Quantization, reason, c.Reuse
	}
	for _, c := range a.Assessments {
		if c.Reuse {
			selectModel(c, "reuse_healthy_model")
			return a
		}
	}
	for _, quant := range []string{"Q8_0", "Q4_K_M"} {
		for _, c := range a.Assessments {
			if c.Quantization == quant && c.Automatic == CheckPassed {
				reason := "resources_suitable"
				if quant == "Q4_K_M" {
					for _, other := range a.Assessments {
						if other.Quantization == "Q8_0" && other.Hardware == CheckPassed {
							for _, issue := range other.Issues {
								if issue.Code == "disk_insufficient" {
									reason = "q8_disk_insufficient"
								}
							}
						}
					}
				}
				selectModel(c, reason)
				return a
			}
		}
	}
	if len(a.Assessments) == 0 {
		a.State, a.Reason = "not_recommended", "runtime_unavailable"
		return a
	}
	allWeak := true
	for _, c := range a.Assessments {
		allWeak = allWeak && c.weak
	}
	if allWeak {
		a.State = "not_recommended"
	}
	// Prefer the Q4 reason: Q8's stricter GPU policy must not hide a fixable
	// Q4 memory/disk shortage or classify missing data as weak hardware.
	for _, c := range a.Assessments {
		if c.Quantization == "Q4_K_M" && len(c.Issues) > 0 {
			a.Reason = c.Issues[0].Code
			break
		}
	}
	return a
}

func assessModel(s Snapshot, option ModelOption) ModelAssessment {
	if option.Disks != nil {
		option.Disks = append([]DiskBudget{}, option.Disks...)
	}
	for i := range option.Disks {
		option.Disks[i].Volume = cloneFact(option.Disks[i].Volume)
		option.Disks[i].Available = cloneFact(option.Disks[i].Available)
	}
	c := ModelAssessment{ModelOption: option, Automatic: CheckPassed, Manual: CheckPassed, Issues: []ResourceIssue{}, MemoryRequired: 8 * GiB}
	add := func(code string, state CheckState, available *uint64, required uint64, manual bool) {
		c.Issues = append(c.Issues, ResourceIssue{Code: code, Available: available, Required: required})
		if state == CheckFailed || c.Automatic == CheckPassed {
			c.Automatic = state
		}
		if manual && (state == CheckFailed || c.Manual == CheckPassed) {
			c.Manual = state
		}
	}
	if !option.RuntimeAvailable {
		add("runtime_unavailable", CheckFailed, nil, 0, true)
		c.Hardware = c.Automatic
		c.weak = true
		return c
	}
	if state, reason := checkSystem(s.System); state != CheckPassed {
		add(reason, state, nil, 0, true)
		c.Hardware = c.Automatic
		c.weak = state == CheckFailed
		return c
	}
	if option.Quantization != "Q4_K_M" && option.Quantization != "Q8_0" {
		add("model_unsupported", CheckFailed, nil, 0, true)
		c.Hardware = c.Automatic
		c.weak = true
		return c
	}
	if s.MemoryBytes.Value == nil || *s.MemoryBytes.Value == 0 {
		add("memory_unknown", CheckUnknown, nil, 16*GiB, true)
	} else if *s.MemoryBytes.Value < 16*GiB {
		add("memory_below_16_gib", CheckFailed, s.MemoryBytes.Value, 16*GiB, *s.MemoryBytes.Value < 8*GiB || option.Quantization == "Q8_0")
		c.weak = true
	}
	cpu, metal, gpu4, gpu8, gpuUnknown, gpuBusy := adviceBackends(s)
	threads := s.System.Value.LogicalCPUs.Value
	if threads == nil {
		add("cpu_threads_unknown", CheckUnknown, nil, 8, true)
	} else if *threads < 4 {
		add("cpu_threads_below_4", CheckFailed, nil, 4, true)
		c.weak = true
	}
	if threads != nil && *threads >= 4 {
		if !cpu && !metal && !gpu4 {
			state := CheckFailed
			if gpuUnknown {
				state = CheckUnknown
			}
			add("backend_unconfirmed", state, nil, 0, true)
		}
		if *threads < 8 && !metal && !gpu4 {
			state, code := CheckFailed, "cpu_threads_below_8"
			if gpuBusy {
				code = "gpu_memory_insufficient"
			} else if gpuUnknown {
				state, code = CheckUnknown, "backend_unconfirmed"
			} else {
				c.weak = true
			}
			add(code, state, nil, 8, false)
		}
	}
	strongUnified := metal && s.System.Value.OS == "darwin" && s.System.Value.Architecture.Value != nil && *s.System.Value.Architecture.Value == "arm64" && s.MemoryBytes.Value != nil && *s.MemoryBytes.Value >= 32*GiB && threads != nil && *threads >= 8
	if option.Quantization == "Q8_0" {
		if strongUnified || !gpu8 {
			c.MemoryRequired = 12 * GiB
		}
		if !gpu8 && !strongUnified {
			add("q8_acceleration_required", CheckFailed, nil, 0, false)
		}
		if !gpu8 && !(cpu && threads != nil && *threads >= 8) && !strongUnified {
			add("q8_cpu_threads_or_backend", CheckFailed, nil, 8, true)
		}
	}
	if s.AvailableMemoryBytes.Value == nil {
		add("available_memory_unknown", CheckUnknown, nil, c.MemoryRequired, true)
	} else if *s.AvailableMemoryBytes.Value < c.MemoryRequired {
		add("available_memory_insufficient", CheckFailed, s.AvailableMemoryBytes.Value, c.MemoryRequired, true)
	}
	c.Hardware = c.Automatic
	if !option.Installed {
		if option.Disks == nil {
			add("disk_unknown", CheckUnknown, nil, 0, true)
		}
		for _, disk := range option.Disks {
			if disk.Volume.Value == nil || disk.Available.Value == nil {
				add("disk_unknown", CheckUnknown, nil, disk.Required, true)
			} else if *disk.Available.Value < disk.Required {
				add("disk_insufficient", CheckFailed, disk.Available.Value, disk.Required, true)
			}
		}
	}
	return c
}

func adviceBackends(s Snapshot) (cpu, metal, gpu4, gpu8, unknown, busy bool) {
	if s.Backends.Value == nil {
		return false, false, false, false, true, false
	}
	possibleDiscrete := s.Adapters.Value == nil
	if s.Adapters.Value != nil {
		for _, a := range *s.Adapters.Value {
			possibleDiscrete = possibleDiscrete || a.Discrete != Unavailable
		}
	}
	for _, b := range *s.Backends.Value {
		if (b.Kind == CUDA || b.Kind == Vulkan) && !possibleDiscrete {
			continue
		}
		if b.Kind == Metal && s.System.Value != nil && s.System.Value.Architecture.Value != nil && *s.System.Value.Architecture.Value != "arm64" {
			continue
		}
		if b.Kind == Metal && (s.System.Value == nil || s.System.Value.OS != "darwin") {
			continue
		}
		if (b.Kind == CUDA || b.Kind == Vulkan) && s.System.Value != nil && s.System.Value.OS == "darwin" {
			continue
		}
		if b.State == UnknownAvailability {
			unknown = true
			continue
		}
		if b.State != Available {
			continue
		}
		if b.Kind == CPU {
			cpu = true
			continue
		}
		if b.Kind == Metal && s.System.Value != nil && s.System.Value.OS == "darwin" && s.System.Value.Architecture.Value != nil && *s.System.Value.Architecture.Value == "arm64" {
			metal = true
			continue
		}
		if b.Kind != CUDA && b.Kind != Vulkan {
			continue
		}
		if s.Adapters.Value == nil {
			unknown = true
			continue
		}
		for _, adapter := range *s.Adapters.Value {
			if b.AdapterID == "" || adapter.ID != b.AdapterID || adapter.Discrete != Available {
				continue
			}
			if adapter.MemoryBytes.Value == nil || adapter.FreeBytes.Value == nil {
				unknown = true
				continue
			}
			total, free := *adapter.MemoryBytes.Value, *adapter.FreeBytes.Value
			if free > total {
				unknown = true
				continue
			}
			reserve := max(GiB, total/100*15)
			busy = busy || (total > reserve && total-reserve >= 4*GiB && (free <= reserve || free-reserve < 4*GiB))
			if free > reserve {
				gpu4 = gpu4 || free-reserve >= 4*GiB
				gpu8 = gpu8 || (total >= 12*GiB && free-reserve >= 9*GiB)
			}
		}
	}
	return
}
