package routerdevice

// EvaluateWithCPUFallback keeps the observed GPU facts, but does not make an
// unproven GPU backend a prerequisite for installing a CPU-capable model. The
// runtime subsequently verifies acceleration using its exact installed engine.
func EvaluateWithCPUFallback(snapshot Snapshot, requirements Requirements) Report {
	report := Evaluate(snapshot, requirements)
	if report.Detection.State == CheckPassed {
		return report
	}
	cpu := cloneSnapshot(snapshot)
	if cpu.Backends.Value == nil {
		return report
	}
	var backends []Backend
	for _, b := range *cpu.Backends.Value {
		if b.Kind == CPU && b.State == Available {
			backends = append(backends, b)
		}
	}
	if len(backends) == 0 {
		return report
	}
	cpu.Backends = Known(backends)
	// This is a separate CPU-only recommendation, not a claim that an observed
	// discrete adapter disappeared or its VRAM is available to a CPU process.
	cpu.Adapters = Known([]Adapter{})
	fallback := Evaluate(cpu, requirements)
	if fallback.Detection.State == CheckPassed {
		fallback.Device = cloneSnapshot(snapshot)
		return fallback
	}
	return report
}
