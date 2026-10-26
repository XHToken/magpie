package routerdevice

import "testing"

func TestUnprovenAcceleratorDoesNotBlockSafeCPUInstallation(t *testing.T) {
	s := fixture("windows", 8*GiB, Adapter{ID: "gpu", Discrete: Available, MemoryBytes: Known(uint64(6 * GiB))})
	if before := Evaluate(s, fixtureRequirements); before.Detection.State == CheckPassed {
		t.Fatal("fixture needs unproven GPU backend")
	}
	r := EvaluateWithCPUFallback(s, fixtureRequirements)
	if r.Detection.State != CheckPassed || r.Recommendation.Tier != Tier1_7B || len(*r.Device.Adapters.Value) != 1 {
		t.Fatal("CPU fallback lost facts or upgraded model", r)
	}
	s.AvailableMemoryBytes = Known(uint64(1 * GiB))
	if EvaluateWithCPUFallback(s, fixtureRequirements).Detection.State == CheckPassed {
		t.Fatal("fallback bypassed RAM guard")
	}
}
