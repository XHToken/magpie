package routerruntime

import (
	"context"
	"path/filepath"
	"time"

	"github.com/yetone/magpie/internal/routerdownload"
	"github.com/yetone/magpie/internal/routermanifest"
	"github.com/yetone/magpie/internal/routerprocess"
)

type InstallationFootprint struct{ ModelBytes, RuntimeBytes uint64 }

// InstallationPlan credits only exact verified caches and deduplicates shared
// dependency artifacts. It never repairs, extracts, downloads or changes intent.
func (m *Manager) InstallationPlan(ctx context.Context, id string) InstallationFootprint {
	model, ok := m.o.Catalog.Model(id)
	if !ok {
		return InstallationFootprint{}
	}
	plan := InstallationFootprint{ModelBytes: uint64(model.InstallationBytes)}
	f, physical := m.o.Installer.(*FileInstaller)
	if !physical {
		if m.IsModelInstalled(ctx, id) {
			return InstallationFootprint{}
		}
		if rt, ok := m.o.Catalog.Runtime(m.o.RuntimeID); ok {
			plan.RuntimeBytes = uint64(routermanifest.RuntimeInstallationBytes(rt))
		}
		return plan
	}
	base, ok := m.o.Catalog.Runtime(m.o.RuntimeID)
	if !ok {
		return plan
	}
	weight, template, _, _ := f.paths(model, base)
	if routerdownload.Verify(ctx, model.Artifact, weight) == nil && routerdownload.Verify(ctx, model.Template, template) == nil {
		plan.ModelBytes = 0
	}
	costs := map[string]uint64{}
	for _, rt := range m.candidateRuntimes(ctx) {
		_, runtimeErr := f.VerifyRuntime(ctx, rt)
		ready := runtimeErr == nil
		for _, component := range []struct {
			a      routermanifest.Artifact
			copies uint64
			path   string
		}{
			{rt.Artifact, 2, filepath.Join(f.Layout.Runtime, rt.Artifact.ID+"-archive", rt.Artifact.FileName)},
		} {
			bytes := uint64(component.a.ExpandedBytes) * component.copies
			if ready {
				bytes = 0
			}
			if routerdownload.Verify(ctx, component.a, component.path) != nil {
				bytes += uint64(component.a.Size)
			}
			costs[component.a.ID] = max(costs[component.a.ID], bytes)
		}
		for _, dep := range []*routermanifest.Dependencies{rt.Dependencies, rt.AcceleratorDependencies} {
			if dep == nil {
				continue
			}
			bytes := uint64(dep.Artifact.ExpandedBytes) * 3
			if ready {
				bytes = 0
			}
			path := filepath.Join(f.Layout.Runtime, dep.Artifact.ID+"-archive", dep.Artifact.FileName)
			if routerdownload.Verify(ctx, dep.Artifact, path) != nil {
				bytes += uint64(dep.Artifact.Size)
			}
			costs[dep.Artifact.ID] = max(costs[dep.Artifact.ID], bytes)
		}
	}
	for _, bytes := range costs {
		plan.RuntimeBytes += bytes
	}
	if plan.RuntimeBytes > 0 {
		plan.RuntimeBytes += 64 << 20
	}
	return plan
}

type BackendDevices struct {
	Kind    string
	Devices []routerprocess.Device
}

// ProbeRecommendationHardware enumerates only already verified installed
// engines. Missing components remain unknown; no weights or server are loaded.
func (m *Manager) ProbeRecommendationHardware(parent context.Context, delay time.Duration) []BackendDevices {
	factory, ok := m.o.Factory.(acceleratorFactory)
	installer, verifies := m.o.Installer.(interface {
		VerifyRuntime(context.Context, routermanifest.Runtime) (routerprocess.Spec, error)
	})
	if !ok || !verifies {
		return nil
	}
	ctx, cancel := context.WithTimeout(parent, 8*time.Second)
	defer cancel()
	stop := context.AfterFunc(m.ctx, cancel)
	defer stop()
	var result []BackendDevices
	for _, rt := range m.candidateRuntimes(ctx) {
		if rt.Backend == "cpu" {
			continue
		}
		spec, err := installer.VerifyRuntime(ctx, rt)
		if err != nil {
			continue
		}
		observed := map[string]routerprocess.Device{}
		valid := true
		for sample := 0; sample < 3; sample++ {
			if sample > 0 && delay > 0 {
				timer := time.NewTimer(delay)
				select {
				case <-ctx.Done():
					timer.Stop()
					valid = false
				case <-timer.C:
				}
				if !valid {
					break
				}
			}
			devices, err := factory.ProbeDevices(ctx, spec)
			if err != nil {
				valid = false
				break
			}
			current := map[string]routerprocess.Device{}
			for _, device := range devices {
				current[device.ID] = device
			}
			if sample == 0 {
				observed = current
				continue
			}
			for id, before := range observed {
				now, found := current[id]
				if !found || before.Name != now.Name || before.TotalBytes != now.TotalBytes {
					delete(observed, id)
					continue
				}
				before.FreeBytes = min(before.FreeBytes, now.FreeBytes)
				observed[id] = before
			}
		}
		if valid {
			entry := BackendDevices{Kind: rt.Backend, Devices: []routerprocess.Device{}}
			for _, device := range observed {
				entry.Devices = append(entry.Devices, device)
			}
			result = append(result, entry)
		}
	}
	return result
}
