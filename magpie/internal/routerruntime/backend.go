package routerruntime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/yetone/magpie/internal/routermanifest"
	"github.com/yetone/magpie/internal/routerprocess"
)

// BackendStatus describes the effective process, not just a GPU name or package.
// Dynamic free memory is an observation and never part of cache identity.
type BackendStatus struct {
	Mode           string    `json:"mode"`
	State          string    `json:"state"`
	RuntimeID      string    `json:"runtimeID,omitempty"`
	Effective      string    `json:"effective,omitempty"`
	Device         string    `json:"device,omitempty"`
	DeviceName     string    `json:"deviceName,omitempty"`
	Offloaded      int       `json:"offloadedLayers"`
	Total          int       `json:"totalLayers"`
	FreeBytes      uint64    `json:"freeBytes,omitempty"`
	ReserveBytes   uint64    `json:"reserveBytes,omitempty"`
	FallbackReason string    `json:"fallbackReason,omitempty"`
	CheckedAt      time.Time `json:"checkedAt,omitempty"`
}

type acceleratorFactory interface {
	ProbeDevices(context.Context, routerprocess.Spec) ([]routerprocess.Device, error)
	ValidateAcceleration(context.Context, routerprocess.Process) error
}

// RuntimeCandidates consumes fixed metadata and hints, never guesses an
// executable path or sums memory across GPUs. CPU remains an explicit fallback.
func RuntimeCandidates(c routermanifest.Catalog, baseID string, hints []string) []routermanifest.Runtime {
	base, ok := c.Runtime(baseID)
	if !ok {
		return nil
	}
	var result []routermanifest.Runtime
	seen := map[string]bool{}
	for _, kind := range append(append([]string(nil), hints...), "cpu") {
		for _, r := range c.Runtimes {
			if r.Platform == base.Platform && r.Architecture == base.Architecture && r.Backend == kind && !seen[r.ID] {
				result = append(result, r)
				seen[r.ID] = true
			}
		}
	}
	return result
}

func (m *Manager) candidateRuntimes(ctx context.Context) []routermanifest.Runtime {
	if !m.o.AutoBackend {
		r, ok := m.o.Catalog.Runtime(m.o.RuntimeID)
		if ok {
			return []routermanifest.Runtime{r}
		}
		return nil
	}
	return RuntimeCandidates(m.o.Catalog, m.o.RuntimeID, m.o.BackendHints(ctx))
}

func (m *Manager) verifyAnyInstallation(ctx context.Context, model routermanifest.Model) error {
	var last error = ErrNotInstalled
	for _, r := range m.candidateRuntimes(ctx) {
		_, err := m.o.Installer.Verify(ctx, model, r)
		if err == nil {
			return nil
		}
		last = err
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
	return errors.Join(ErrNotInstalled, last)
}

func chooseDevice(kind string, devices []routerprocess.Device) (routerprocess.Device, bool) {
	// Every candidate comes from the exact engine. Device names are matched to
	// its backend prefix, not another adapter's advertised memory.
	devices = append([]routerprocess.Device(nil), devices...)
	sort.SliceStable(devices, func(i, j int) bool { return devices[i].FreeBytes > devices[j].FreeBytes })
	for _, d := range devices {
		if !strings.HasPrefix(strings.ToLower(d.ID), kind) {
			continue
		}
		name := strings.ToLower(d.Name)
		software := false
		for _, marker := range []string{"llvmpipe", "lavapipe", "swiftshader", "software", "microsoft basic", "warp"} {
			software = software || strings.Contains(name, marker)
		}
		if software {
			continue
		}
		reserve := max(uint64(1<<30), d.TotalBytes/100*15)
		if d.FreeBytes <= reserve+(256<<20) {
			continue
		}
		return d, true
	}
	return routerprocess.Device{}, false
}

func executionKey(spec routerprocess.Spec, device routerprocess.Device, e routerprocess.AccelerationEvidence) string {
	b, _ := json.Marshal(struct {
		Revision               string
		Backend, Device, Name  string
		Layers, Total, Context int
	}{"backend-plan-v1", spec.Backend, device.ID, device.Name, e.Offloaded, e.Total, spec.ContextSize})
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func (m *Manager) installBackendPackages(ctx context.Context, model routermanifest.Model, base routermanifest.Runtime, progress func(Progress)) error {
	// Install CPU first, then optional accelerators as part of this explicit job.
	// Failure of an optional package is visible but preserves usable CPU files.
	if _, err := m.o.Installer.Install(ctx, model, base, progress); err != nil {
		return err
	}
	if !m.o.AutoBackend {
		return nil
	}
	for _, r := range m.candidateRuntimes(ctx) {
		if r.ID == base.ID || r.Backend == "cpu" {
			continue
		}
		if _, err := m.o.Installer.Install(ctx, model, r, progress); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			m.mu.Lock()
			m.backend.FallbackReason = "accelerator_install_failed"
			m.notifyLocked()
			m.mu.Unlock()
		} else {
			m.mu.Lock()
			delete(m.backendFailures, r.ID)
			m.mu.Unlock()
		}
	}
	return nil
}

// startBackend serially tries verified installed candidates under the one load
// deadline. It never downloads and never starts a replacement until Done closes.
func (m *Manager) startBackend(ctx context.Context, model routermanifest.Model) (routerprocess.Process, routermanifest.Runtime, BackendStatus, error) {
	status := BackendStatus{Mode: "auto", State: "loading"}
	factory, canProbe := m.o.Factory.(acceleratorFactory)
	var last error = ErrNotInstalled
	verifiedInstallation := false
	attempts := 0
	var lastRuntime routermanifest.Runtime
	for _, rt := range m.candidateRuntimes(ctx) {
		lastRuntime = rt
		if ctx.Err() != nil {
			return nil, rt, status, ctx.Err()
		}
		if attempts >= 3 {
			break
		}
		m.mu.Lock()
		cooling := m.o.Now().Before(m.backendFailures[rt.ID])
		m.mu.Unlock()
		if rt.Backend != "cpu" && cooling {
			status.FallbackReason = "accelerator_cooling_down"
			continue
		}
		spec, err := m.o.Installer.Verify(ctx, model, rt)
		if err != nil {
			last = err
			if rt.Backend != "cpu" {
				status.FallbackReason = "accelerator_not_installed"
			}
			continue
		}
		verifiedInstallation = true
		spec.ContextSize = model.ContextTokens
		var device routerprocess.Device
		if rt.Backend != "cpu" {
			if !canProbe {
				status.FallbackReason = "accelerator_probe_unavailable"
				continue
			}
			probeCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
			devices, probeErr := factory.ProbeDevices(probeCtx, spec)
			cancel()
			device, _ = chooseDevice(rt.Backend, devices)
			if probeErr != nil || device.ID == "" {
				status.FallbackReason = "accelerator_device_or_memory_unavailable"
				continue
			}
			reserve := max(uint64(1<<30), device.TotalBytes/100*15)
			spec.GPU = &routerprocess.GPUOptions{Device: device.ID, Layers: -1, ReserveMiB: int(reserve >> 20)}
		}
		attempts++
		// Leave time in the shared deadline for a CPU fallback when a GPU hangs.
		attemptCtx := ctx
		cancelAttempt := func() {}
		if rt.Backend != "cpu" {
			limit := 45 * time.Second
			if deadline, ok := ctx.Deadline(); ok {
				limit = min(limit, max(time.Millisecond, time.Until(deadline)/2))
			}
			attemptCtx, cancelAttempt = context.WithTimeout(ctx, limit)
		}
		p, err := m.o.Factory.Start(attemptCtx, spec)
		if err == nil {
			err = m.awaitHealth(attemptCtx, p)
		}
		var evidence routerprocess.AccelerationEvidence
		failureReason := "accelerator_load_failed"
		if err == nil && rt.Backend != "cpu" {
			reporter, ok := p.(routerprocess.AccelerationReporter)
			if ok {
				evidence = reporter.Acceleration()
			}
			if !evidence.Verified || evidence.Device != device.ID {
				err = errors.New("accelerator_offload_unverified")
				failureReason = "accelerator_offload_unverified"
			} else {
				err = factory.ValidateAcceleration(attemptCtx, p)
				failureReason = "accelerator_inference_invalid"
			}
		}
		cancelAttempt()
		if err == nil {
			status.State, status.RuntimeID, status.Effective = "cpu", rt.ID, "cpu"
			status.CheckedAt = m.o.Now()
			if spec.GPU != nil {
				status.State = "accelerated"
				if evidence.Offloaded < evidence.Total {
					status.State = "hybrid"
				}
				status.Effective, status.Device, status.DeviceName = rt.Backend, device.ID, device.Name
				status.Offloaded, status.Total = evidence.Offloaded, evidence.Total
				status.FreeBytes, status.ReserveBytes = device.FreeBytes, uint64(spec.GPU.ReserveMiB)<<20
				rt.Execution = executionKey(spec, device, evidence)
			}
			return p, rt, status, nil
		}
		last = err
		status.FallbackReason = failureReason
		if rt.Backend != "cpu" {
			m.mu.Lock()
			m.coolBackendLocked(rt.ID)
			m.mu.Unlock()
		}
		if rt.Backend == "cpu" {
			status.FallbackReason = "cpu_load_failed"
		}
		if p != nil {
			stop, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = p.Stop(stop)
			cancel()
			select {
			case <-p.Done():
			default:
				// Return the unreaped child to the existing cleanup/watcher path.
				return p, rt, status, errors.New("load_cleanup_pending")
			}
		}
	}
	status.State = "unavailable"
	if !verifiedInstallation && ctx.Err() == nil {
		last = errors.Join(ErrNotInstalled, last)
	}
	return nil, lastRuntime, status, last
}

func (m *Manager) coolBackendLocked(id string) {
	if m.backendFailures == nil {
		m.backendFailures = map[string]time.Time{}
	}
	m.backendFailures[id] = m.o.Now().Add(10 * time.Minute)
}

func (m *Manager) awaitHealth(ctx context.Context, p routerprocess.Process) error {
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		probe, cancel := context.WithTimeout(ctx, time.Second)
		err := p.Health(probe)
		cancel()
		if err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-p.Done():
			return ErrFault
		case <-m.o.After(50 * time.Millisecond):
		}
	}
}
