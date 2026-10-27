package routerdevice

import "testing"

func TestCatalogMinimumTierPreservesHardwareAndDiskGates(t *testing.T) {
	requirements := Requirements{MinimumTier: Tier4B, Tier4BBytes: 10 * GiB}
	for _, tc := range []struct {
		name                 string
		ram, available, disk uint64
		tier                 Tier
		state                CheckState
	}{
		{"16GB supported", 16 * GiB, 6 * GiB, 10 * GiB, Tier4B, CheckPassed},
		{"disk below reservation", 16 * GiB, 6 * GiB, 10*GiB - 1, Tier4B, CheckFailed},
		{"12GB unsupported", 12 * GiB, 6 * GiB, 20 * GiB, TierNone, CheckFailed},
		{"insufficient headroom", 16 * GiB, 3 * GiB, 20 * GiB, TierNone, CheckFailed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			snapshot := fixture("windows", tc.ram)
			snapshot.AvailableMemoryBytes, snapshot.DiskFreeBytes = Known(tc.available), Known(tc.disk)
			report := EvaluateWithCPUFallback(snapshot, requirements)
			if report.Recommendation.Tier != tc.tier || report.Detection.State != tc.state {
				t.Fatalf("catalog floor bypassed a gate or recommended an absent tier: %+v", report)
			}
		})
	}
}
