package routerservice

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"time"

	"github.com/yetone/magpie/internal/routerdevice"
	"github.com/yetone/magpie/internal/routerruntime"
)

func (l *LocalModels) modelAdvice(ctx context.Context, operation string, cpuOnly bool) routerdevice.Report {
	before := l.Manager.Status()
	var snapshot, runtimeDisk routerdevice.Snapshot
	for i := 0; i < 3; i++ {
		if i > 0 && l.SampleDelay > 0 {
			timer := time.NewTimer(l.SampleDelay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return routerdevice.Report{Advice: &routerdevice.ModelAdvice{Policy: routerdevice.ModelAdvicePolicy, State: "deferred", Reason: "detection_canceled"}}
			case <-timer.C:
			}
		}
		now := l.Detector.Detect(ctx, l.Layout.Models, routerdevice.Requirements{}).Device
		if operation != "load" {
			disk := l.Detector.Detect(ctx, l.Layout.Runtime, routerdevice.Requirements{}).Device
			if i == 0 {
				runtimeDisk = disk
			} else if runtimeDisk.DiskFreeBytes.Value == nil || disk.DiskFreeBytes.Value == nil {
				runtimeDisk.DiskFreeBytes = routerdevice.Unknown[uint64]("disk_unknown")
			} else {
				runtimeDisk.DiskFreeBytes = routerdevice.Known(min(*runtimeDisk.DiskFreeBytes.Value, *disk.DiskFreeBytes.Value))
			}
		}
		if i == 0 {
			snapshot = now
			continue
		}
		if snapshot.AvailableMemoryBytes.Value == nil || now.AvailableMemoryBytes.Value == nil {
			snapshot.AvailableMemoryBytes = routerdevice.Unknown[uint64]("available_memory_unknown")
		} else {
			snapshot.AvailableMemoryBytes = routerdevice.Known(min(*snapshot.AvailableMemoryBytes.Value, *now.AvailableMemoryBytes.Value))
		}
		if snapshot.MemoryBytes.Value == nil || now.MemoryBytes.Value == nil || *snapshot.MemoryBytes.Value != *now.MemoryBytes.Value {
			snapshot.MemoryBytes = routerdevice.Unknown[uint64]("physical_memory_changed")
		}
		if snapshot.System.Value == nil || now.System.Value == nil {
			snapshot.System = routerdevice.Unknown[routerdevice.System]("system_unknown")
		}
		if snapshot.DiskFreeBytes.Value == nil || now.DiskFreeBytes.Value == nil {
			snapshot.DiskFreeBytes = routerdevice.Unknown[uint64]("disk_unknown")
		} else {
			snapshot.DiskFreeBytes = routerdevice.Known(min(*snapshot.DiskFreeBytes.Value, *now.DiskFreeBytes.Value))
		}
	}
	if snapshot.DiskVolume.Value == nil {
		snapshot.DiskVolume = routerdevice.VolumeID(ctx, l.Layout.Models)
	}
	if !cpuOnly {
		mergeRecommendationHardware(&snapshot, l.Manager.ProbeRecommendationHardware(ctx, l.SampleDelay))
	} else if snapshot.Backends.Value != nil {
		var cpu []routerdevice.Backend
		for _, b := range *snapshot.Backends.Value {
			if b.Kind == routerdevice.CPU {
				cpu = append(cpu, b)
			}
		}
		snapshot.Backends = routerdevice.Known(cpu)
	}
	if operation != "load" {
		if runtimeDisk.DiskVolume.Value == nil {
			runtimeDisk.DiskVolume = routerdevice.VolumeID(ctx, l.Layout.Runtime)
		}
	}
	_, available := l.Catalog.Runtime(routerruntime.PlatformRuntime(l.Catalog, runtime.GOOS, runtime.GOARCH))
	options := []routerdevice.ModelOption{}
	for _, model := range l.Catalog.Models {
		installed := operation == "load" || l.Manager.IsModelInstalled(ctx, model.ID)
		option := routerdevice.ModelOption{ID: model.ID, Name: model.DisplayName(), Quantization: model.Quantization, RuntimeAvailable: available, Installed: installed && operation != "install"}
		if !option.Installed {
			plan := l.Manager.InstallationPlan(ctx, model.ID)
			option.Disks = modelDiskBudgets(l.Layout.Models, l.Layout.Runtime, snapshot, runtimeDisk, plan)
		}
		options = append(options, option)
	}
	after := l.Manager.Status()
	running := routerdevice.RunningModel{ID: after.Intent.Selected, Healthy: after.Healthy && after.PID != 0 && before.PID == after.PID && before.Intent.Selected == after.Intent.Selected && after.Intent.Enabled, Slow: after.Performance.State == "slow"}
	if operation == "load" {
		running.Healthy = false
	}
	advice := routerdevice.AdviseModels(snapshot, options, running)
	report := routerdevice.Report{Device: snapshot, Advice: &advice, Recommendation: routerdevice.Recommendation{Tier: routerdevice.TierNone, MaxTier: routerdevice.TierNone, Reason: advice.Reason}, Detection: routerdevice.Check{State: routerdevice.CheckFailed, Reasons: []string{advice.Reason}}}
	if advice.State == "recommended" {
		report.Recommendation.Tier, report.Recommendation.MaxTier, report.Detection.State = routerdevice.Tier4B, routerdevice.Tier4B, routerdevice.CheckPassed
		report.Detection.Reasons = []string{}
	} else if advice.State == "deferred" {
		report.Detection.State = routerdevice.CheckUnknown
	}
	return report
}

func modelDiskBudgets(modelPath, runtimePath string, modelDisk, runtimeDisk routerdevice.Snapshot, plan routerruntime.InstallationFootprint) []routerdevice.DiskBudget {
	if plan.ModelBytes == 0 && plan.RuntimeBytes == 0 {
		return []routerdevice.DiskBudget{}
	}

	budget := func(path string, s routerdevice.Snapshot, peak uint64) routerdevice.DiskBudget {
		required := peak
		if peak > 0 {
			required += max(2*routerdevice.GiB, peak/10)
		}
		return routerdevice.DiskBudget{Path: path, Volume: s.DiskVolume, Available: s.DiskFreeBytes, Required: required}
	}
	if modelDisk.DiskVolume.Value != nil && runtimeDisk.DiskVolume.Value != nil && *modelDisk.DiskVolume.Value == *runtimeDisk.DiskVolume.Value {
		if modelDisk.DiskFreeBytes.Value != nil && runtimeDisk.DiskFreeBytes.Value != nil {
			modelDisk.DiskFreeBytes = routerdevice.Known(min(*modelDisk.DiskFreeBytes.Value, *runtimeDisk.DiskFreeBytes.Value))
		} else {
			modelDisk.DiskFreeBytes = routerdevice.Unknown[uint64]("disk_unknown")
		}
		return []routerdevice.DiskBudget{budget(modelPath, modelDisk, plan.ModelBytes+plan.RuntimeBytes)}
	}
	result := []routerdevice.DiskBudget{}
	if plan.ModelBytes > 0 {
		result = append(result, budget(modelPath, modelDisk, plan.ModelBytes))
	}
	if plan.RuntimeBytes > 0 {
		result = append(result, budget(runtimePath, runtimeDisk, plan.RuntimeBytes))
	}
	return result
}

func mergeRecommendationHardware(snapshot *routerdevice.Snapshot, observations []routerruntime.BackendDevices) {
	if snapshot.Backends.Value == nil {
		return
	}
	backends := append([]routerdevice.Backend(nil), (*snapshot.Backends.Value)...)
	normalize := func(s string) string { return strings.ToLower(strings.Join(strings.Fields(s), " ")) }
	for _, observation := range observations {
		kind := routerdevice.BackendKind(observation.Kind)
		if kind == routerdevice.Metal && snapshot.System.Value != nil && snapshot.System.Value.OS == "darwin" && len(observation.Devices) > 0 {
			backends = append(backends, routerdevice.Backend{Kind: kind, State: routerdevice.Available})
			continue
		}
		if snapshot.Adapters.Value == nil {
			continue
		}
		for _, device := range observation.Devices {
			match := -1
			for i, adapter := range *snapshot.Adapters.Value {
				if normalize(adapter.Name) == normalize(device.Name) && adapter.Discrete == routerdevice.Available {
					if match != -1 {
						match = -2
						break
					}
					match = i
				}
			}
			if match < 0 || device.FreeBytes > device.TotalBytes {
				continue
			}
			adapter := &(*snapshot.Adapters.Value)[match]
			if adapter.MemoryBytes.Value == nil || device.TotalBytes > *adapter.MemoryBytes.Value {
				continue
			}
			if adapter.FreeBytes.Value != nil {
				adapter.FreeBytes = routerdevice.Known(min(*adapter.FreeBytes.Value, device.FreeBytes))
			} else {
				adapter.FreeBytes = routerdevice.Known(device.FreeBytes)
			}
			backends = append(backends, routerdevice.Backend{Kind: kind, AdapterID: adapter.ID, State: routerdevice.Available})
		}
	}
	snapshot.Backends = routerdevice.Known(backends)
}

func (l *LocalModels) admitModel(ctx context.Context, id, operation string, cpuOnly bool) error {
	report := l.modelAdvice(ctx, operation, cpuOnly)
	if report.Advice == nil {
		return errors.New("device_check_not_passed")
	}
	assessment, ok := report.Advice.Model(id)
	if !ok || !allowsModelOperation(assessment, operation) {
		return errors.New("device_check_not_passed")
	}
	return ctx.Err()
}

func allowsModelOperation(assessment routerdevice.ModelAssessment, operation string) bool {
	if operation == "install" {
		return assessment.AllowsDownload()
	}
	return assessment.AllowsManual()
}
