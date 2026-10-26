package routerruntime

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/routermanifest"
	"github.com/yetone/magpie/internal/routerprocess"
)

type backendInstaller struct {
	mu            sync.Mutex
	installed     map[string]bool
	installs      []string
	fail          string
	verifyFailure error
}

func (i *backendInstaller) Verify(ctx context.Context, _ routermanifest.Model, rt routermanifest.Runtime) (routerprocess.Spec, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if ctx.Err() != nil {
		return routerprocess.Spec{}, ctx.Err()
	}
	if i.verifyFailure != nil {
		return routerprocess.Spec{}, i.verifyFailure
	}
	if !i.installed[rt.ID] {
		return routerprocess.Spec{}, ErrNotInstalled
	}
	return routerprocess.Spec{Backend: rt.Backend}, nil
}

func (i *backendInstaller) Install(ctx context.Context, m routermanifest.Model, rt routermanifest.Runtime, _ func(Progress)) (routerprocess.Spec, error) {
	i.mu.Lock()
	i.installs = append(i.installs, rt.ID)
	if rt.ID != i.fail {
		i.installed[rt.ID] = true
	}
	i.mu.Unlock()
	return i.Verify(ctx, m, rt)
}

type backendProcess struct {
	*fixtureProcess
	evidence routerprocess.AccelerationEvidence
}

func (p *backendProcess) Acceleration() routerprocess.AccelerationEvidence { return p.evidence }

type backendFactory struct {
	mu                sync.Mutex
	endpoint          string
	devices           map[string][]routerprocess.Device
	failed            map[string]bool
	zero              bool
	validationFailure bool
	started           []routerprocess.Spec
	processes         []*backendProcess
	probeCanceled     bool
}

func (f *backendFactory) ProbeDevices(ctx context.Context, spec routerprocess.Spec) ([]routerprocess.Device, error) {
	if f.probeCanceled {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return f.devices[spec.Backend], nil
}
func (f *backendFactory) ValidateAcceleration(context.Context, routerprocess.Process) error {
	if f.validationFailure {
		return errors.New("invalid synthetic completion")
	}
	return nil
}
func (f *backendFactory) Start(ctx context.Context, spec routerprocess.Spec) (routerprocess.Process, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	for _, previous := range f.processes {
		select {
		case <-previous.Done():
		default:
			return nil, errors.New("second live child")
		}
	}
	f.started = append(f.started, spec)
	if f.failed[spec.Backend] {
		return nil, errors.New("native load failed")
	}
	p := &backendProcess{fixtureProcess: &fixtureProcess{done: make(chan struct{}), endpoint: f.endpoint}}
	if spec.GPU != nil {
		p.evidence = routerprocess.AccelerationEvidence{Device: spec.GPU.Device, Offloaded: 20, Total: 30, DeviceBuffer: true, Verified: true}
		if f.zero {
			p.evidence = routerprocess.AccelerationEvidence{}
		}
	}
	f.processes = append(f.processes, p)
	return p, nil
}

func autoBackendFixture(t *testing.T, f *backendFactory, i *backendInstaller, hints ...string) *Manager {
	t.Helper()
	c := routermanifest.Fixed()
	cpu, _ := c.Runtime("windows-amd64-cpu")
	c.Runtimes = []routermanifest.Runtime{cpu}
	for _, kind := range []string{"cuda", "vulkan", "metal"} {
		r := cpu
		r.ID, r.Backend = "fixture-"+kind, kind
		c.Runtimes = append(c.Runtimes, r)
	}
	m, err := New(context.Background(), Options{Catalog: c, RuntimeID: cpu.ID, AutoBackend: true,
		BackendHints: func(context.Context) []string { return hints }, Store: &memoryIntent{i: Intent{Enabled: true, Selected: c.Models[0].ID}}, Installer: i, Factory: f})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := m.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	return m
}

func TestAutomaticBackendVerifiesOffloadAndPreservesModelContext(t *testing.T) {
	i := &backendInstaller{installed: map[string]bool{"windows-amd64-cpu": true, "fixture-cuda": true}}
	f := &backendFactory{devices: map[string][]routerprocess.Device{"cuda": {
		{ID: "CUDA0", Name: "busy GPU", TotalBytes: 8 << 30, FreeBytes: 1 << 30},
		{ID: "CUDA1", Name: "available GPU", TotalBytes: 8 << 30, FreeBytes: 7 << 30},
	}}}
	m := autoBackendFixture(t, f, i, "cuda")
	before := m.Identity()
	if err := m.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	s := m.Status()
	after := m.Identity()
	if s.Backend.State != "hybrid" || s.Backend.Device != "CUDA1" || s.Backend.Offloaded != 20 || s.Backend.Effective != "cuda" || s.Intent.Selected != before.ModelID {
		t.Fatal(s)
	}
	if after.RuntimeID != "fixture-cuda" || after.ExecutionPlan == "" || before.ModelFingerprint == after.ModelFingerprint {
		t.Fatal(before, after)
	}
	if f.started[0].ContextSize != 8192 || f.started[0].GPU.Device != "CUDA1" || f.started[0].GPU.Layers != -1 {
		t.Fatal(f.started)
	}
	for _, c := range s.Capabilities {
		if c.State != "unknown" || c.Ready || c.Fingerprint != after.ModelFingerprint {
			t.Fatal("GPU load granted or inherited acceptance", c)
		}
	}
}

func TestAutomaticBackendFailureFallsBackWithoutOverlappingChildren(t *testing.T) {
	for _, mode := range []string{"driver", "load", "zero-offload", "invalid-inference", "low-memory", "not-installed"} {
		t.Run(mode, func(t *testing.T) {
			i := &backendInstaller{installed: map[string]bool{"windows-amd64-cpu": true, "fixture-cuda": true, "fixture-vulkan": true}}
			device := routerprocess.Device{ID: "CUDA0", Name: "fixture GPU", TotalBytes: 8 << 30, FreeBytes: 6 << 30}
			f := &backendFactory{devices: map[string][]routerprocess.Device{"cuda": {device}}, failed: map[string]bool{}}
			switch mode {
			case "driver":
				f.devices = nil
			case "load":
				f.failed["cuda"] = true
			case "zero-offload":
				f.zero = true
			case "invalid-inference":
				f.validationFailure = true
			case "low-memory":
				device.FreeBytes = 1 << 30
				f.devices["cuda"] = []routerprocess.Device{device}
			case "not-installed":
				delete(i.installed, "fixture-cuda")
			}
			m := autoBackendFixture(t, f, i, "cuda", "vulkan")
			if err := m.Load(context.Background()); err != nil {
				t.Fatal(err)
			}
			s := m.Status()
			if s.Backend.State != "cpu" || s.Backend.Effective != "cpu" || s.Backend.FallbackReason == "" || m.Identity().ExecutionPlan != "" {
				t.Fatal(s)
			}
			for _, p := range f.processes[:len(f.processes)-1] {
				select {
				case <-p.Done():
				default:
					t.Fatal("GPU was not reaped before CPU")
				}
			}
		})
	}
}

func TestBackendInstallPreparesCPUAndOptionalPackagesOnlyExplicitly(t *testing.T) {
	i := &backendInstaller{installed: map[string]bool{}, fail: "fixture-cuda"}
	f := &backendFactory{}
	m := autoBackendFixture(t, f, i, "cuda", "vulkan")
	if len(i.installs) != 0 || len(f.started) != 0 {
		t.Fatal("construction installed or started a runtime")
	}
	if err := m.Install(); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return m.Status().Installation != "downloading" })
	if !reflect.DeepEqual(i.installs, []string{"windows-amd64-cpu", "fixture-cuda", "fixture-vulkan"}) || m.Status().Installation != "installed" || m.Status().Backend.FallbackReason != "accelerator_install_failed" {
		t.Fatal(i.installs, m.Status())
	}
}

func TestBackendCandidatesHaveStableCPUFallbackOnAppleAndWindows(t *testing.T) {
	c := routermanifest.Fixed()
	base := c.Runtimes[0]
	for _, platform := range []struct{ os, arch, kind string }{{"windows", "amd64", "vulkan"}, {"darwin", "arm64", "metal"}, {"darwin", "amd64", "metal"}} {
		cpu, gpu := base, base
		cpu.ID, cpu.Platform, cpu.Architecture = "cpu", platform.os, platform.arch
		gpu = cpu
		gpu.ID, gpu.Backend = "gpu", platform.kind
		catalog := routermanifest.Catalog{Runtimes: []routermanifest.Runtime{gpu, cpu}}
		if PlatformRuntime(catalog, platform.os, platform.arch) != "cpu" {
			t.Fatal("baseline depends on catalog order")
		}
		got := RuntimeCandidates(catalog, "cpu", []string{platform.kind, platform.kind})
		if len(got) != 2 || got[0].ID != "gpu" || got[1].ID != "cpu" {
			t.Fatal(got)
		}
	}
}

func TestBackendCanceledProbeHasNoLoadOrDownload(t *testing.T) {
	i := &backendInstaller{installed: map[string]bool{"fixture-cuda": true}}
	f := &backendFactory{probeCanceled: true}
	m := autoBackendFixture(t, f, i, "cuda")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, _, err := m.startBackend(ctx, routermanifest.Fixed().Models[0])
	if !errors.Is(err, context.Canceled) || len(i.installs) != 0 || len(f.started) != 0 {
		t.Fatal(err)
	}
}

func TestBackendIdentityExcludesChangingFreeMemory(t *testing.T) {
	s := routerprocess.Spec{Backend: "vulkan", ContextSize: 65536}
	d := routerprocess.Device{ID: "Vulkan0", Name: "fixture", TotalBytes: 8 << 30, FreeBytes: 6 << 30}
	e := routerprocess.AccelerationEvidence{Offloaded: 20, Total: 30}
	first := executionKey(s, d, e)
	d.FreeBytes = 5 << 30
	if first != executionKey(s, d, e) || len(first) != 64 || strings.Trim(first, "0123456789abcdef") != "" {
		t.Fatal(first)
	}
	e.Offloaded++
	if first == executionKey(s, d, e) {
		t.Fatal("offload change inherited identity")
	}
}

func TestAutomaticBackendClearsInstallationAfterAllFilesFailVerification(t *testing.T) {
	i := &backendInstaller{installed: map[string]bool{"windows-amd64-cpu": true, "fixture-cuda": true}}
	f := &backendFactory{}
	m := autoBackendFixture(t, f, i, "cuda")
	if m.Status().Installation != "installed" {
		t.Fatal("fixture was not initially installed")
	}
	i.mu.Lock()
	i.installed = map[string]bool{}
	i.verifyFailure = errors.New("fixture model SHA256 mismatch")
	i.mu.Unlock()
	if err := m.Load(context.Background()); err == nil {
		t.Fatal("load accepted an invalid installation")
	}
	if s := m.Status(); s.Installation != "not_installed" || s.Healthy || s.PID != 0 {
		t.Fatal("invalid files left the installed state or a child behind", s)
	}
	if len(f.started) != 0 || len(i.installs) != 0 {
		t.Fatal("verification failure launched or downloaded")
	}
}

func TestAutomaticBackendColdCapabilityCheckBindsLoadedRuntime(t *testing.T) {
	i := &backendInstaller{installed: map[string]bool{"windows-amd64-cpu": true, "fixture-cuda": true}}
	f := &backendFactory{devices: map[string][]routerprocess.Device{"cuda": {
		{ID: "CUDA0", Name: "fixture GPU", TotalBytes: 8 << 30, FreeBytes: 6 << 30},
	}}}
	m := autoBackendFixture(t, f, i, "cuda")
	var checked LocalCallIdentity
	err := m.CheckCapability(context.Background(), Check{Purpose: Execution, Suite: "execution-fixture-v1", Run: func(context.Context, string) (string, error) {
		checked = m.Identity()
		return "fixture-sample-sha256", nil
	}})
	if err != nil {
		t.Fatalf("cold GPU sample was rejected after successful validation: %v", err)
	}
	c := m.Status().Capabilities[Execution]
	if checked.RuntimeID != "fixture-cuda" || checked.ExecutionPlan == "" || c.Fingerprint != checked.ModelFingerprint || !c.Ready {
		t.Fatal("sample did not bind its actual loaded runtime", checked, c)
	}
}

func TestAutomaticBackendReloadKeepsEvidenceForUnchangedBinding(t *testing.T) {
	for _, backend := range []string{"cpu", "cuda"} {
		t.Run(backend, func(t *testing.T) {
			i := &backendInstaller{installed: map[string]bool{"windows-amd64-cpu": true, "fixture-cuda": true}}
			f := &backendFactory{devices: map[string][]routerprocess.Device{"cuda": {
				{ID: "CUDA0", Name: "fixture GPU", TotalBytes: 8 << 30, FreeBytes: 6 << 30},
			}}}
			m := autoBackendFixture(t, f, i, backend)
			if err := m.Load(context.Background()); err != nil {
				t.Fatal(err)
			}
			if err := m.CheckCapability(context.Background(), Check{Purpose: Execution, Suite: "execution-fixture-v1", Run: func(context.Context, string) (string, error) {
				return "fixture-sample-sha256", nil
			}}); err != nil {
				t.Fatal(err)
			}
			accepted := m.Status().Capabilities[Execution]
			m.mu.Lock()
			m.o.IdleTimeout = time.Minute
			m.lastUsed = m.o.Now().Add(-2 * time.Minute)
			m.mu.Unlock()
			if err := m.Sweep(context.Background()); err != nil {
				t.Fatal(err)
			}
			if err := m.Load(context.Background()); err != nil {
				t.Fatal(err)
			}
			if c := m.Status().Capabilities[Execution]; c != accepted || !c.Ready {
				t.Fatal("reloading the same runtime discarded current sample evidence", accepted, c)
			}
		})
	}
}

func TestAutomaticBackendReloadInvalidatesEvidenceWhenFallingBack(t *testing.T) {
	i := &backendInstaller{installed: map[string]bool{"windows-amd64-cpu": true, "fixture-cuda": true}}
	f := &backendFactory{devices: map[string][]routerprocess.Device{"cuda": {
		{ID: "CUDA0", Name: "fixture GPU", TotalBytes: 8 << 30, FreeBytes: 6 << 30},
	}}}
	m := autoBackendFixture(t, f, i, "cuda")
	if err := m.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := m.CheckCapability(context.Background(), Check{Purpose: Execution, Suite: "execution-fixture-v1", Run: func(context.Context, string) (string, error) {
		return "fixture-sample-sha256", nil
	}}); err != nil {
		t.Fatal(err)
	}
	accepted := m.Status().Capabilities[Execution]
	m.mu.Lock()
	m.o.IdleTimeout = time.Minute
	m.lastUsed = m.o.Now().Add(-2 * time.Minute)
	m.mu.Unlock()
	if err := m.Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.zero = true
	if err := m.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	s := m.Status()
	if c := s.Capabilities[Execution]; c.State != "unknown" || c.Ready || c.Fingerprint == accepted.Fingerprint || s.Backend.Effective != "cpu" {
		t.Fatal("CPU fallback inherited the GPU sample evidence", accepted, s)
	}
}
