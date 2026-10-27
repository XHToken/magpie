package routerruntime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yetone/magpie/internal/routerdevice"
	"github.com/yetone/magpie/internal/routermanifest"
	"github.com/yetone/magpie/internal/routerprocess"
)

const QueueLimit = 16

var defaultManager atomic.Pointer[Manager]

// Default returns the process-wide local runtime Manager, or nil when none has
// been configured.Its Status() method is nil-safe.
func Default() *Manager {
	return defaultManager.Load()
}

// SetDefault registers m as the process-wide local runtime Manager.
func SetDefault(m *Manager) {
	defaultManager.Store(m)
}

type waiter struct{ occupied bool }

type Manager struct {
	performance      Performance
	performanceEpoch uint64 // increments for each adopted process, including PID reuse
	memoryBeforeLoad routerdevice.Fact[uint64]
	memoryAfterLoad  routerdevice.Fact[uint64]
	mu               sync.Mutex
	op               sync.Mutex // serializes explicit intent changes, never inference or callbacks
	o                Options
	intent           Intent
	installation     string
	installingModel  string
	selectedReady    bool
	state            RunState
	healthy          bool
	lastError        string
	progress         Progress
	dependencies     DependencyStatus
	capabilities     map[Purpose]Capability
	changed          chan struct{}
	proc             routerprocess.Process
	stopRequested    routerprocess.Process // identity of the child whose Stop was requested
	loading          chan struct{}
	loadCancel       context.CancelFunc
	installDone      chan struct{}
	installCancel    context.CancelFunc
	waiters          []*waiter
	active           bool
	activeCancel     context.CancelFunc
	lastUsed         time.Time
	closed           bool
	stopping         bool
	updating         bool
	epoch            uint64
	ctx              context.Context
	cancel           context.CancelFunc
	workers          sync.WaitGroup
	observer         LocalCallObserver
	runtimeID        string
	executionPlan    string
	backend          BackendStatus
	backendFailures  map[string]time.Time
}

func New(ctx context.Context, o Options) (*Manager, error) {
	if o.Store == nil || o.Installer == nil || o.Factory == nil {
		return nil, errors.New("runtime dependencies required")
	}
	if o.LoadTimeout <= 0 {
		o.LoadTimeout = 60 * time.Second
	}
	if o.JudgmentBudget <= 0 {
		o.JudgmentBudget = 5 * time.Second
	}
	if o.ExecutionQueueBudget <= 0 {
		o.ExecutionQueueBudget = 8 * time.Second
	}
	if o.ExecutionBudget <= 0 {
		o.ExecutionBudget = 120 * time.Second
	}
	if o.RedactionBudget <= 0 {
		o.RedactionBudget = DefaultRedactionBudget
		if o.ExperimentalRedaction {
			o.RedactionBudget = experimentalRedactionBudget
		}
	}
	if o.ExperimentalRedaction && o.RedactionBudget > experimentalRedactionBudget {
		o.RedactionBudget = experimentalRedactionBudget
	}
	if o.AvailableMemory == nil {
		o.AvailableMemory = routerdevice.AvailableMemory
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.After == nil {
		o.After = time.After
	}
	if o.BackendHints == nil {
		o.BackendHints = routerprocess.BackendHints
	}
	i, err := o.Store.Load(ctx)
	if err != nil {
		return nil, err
	}
	if i.Selected != "" {
		if _, ok := o.Catalog.Model(i.Selected); !ok {
			return nil, errors.New("unknown persisted local model")
		}
	}
	if i.Enabled && i.Selected == "" {
		return nil, errors.New("enabled local model has no selection")
	}
	if err := i.Settings.Validate(); err != nil {
		return nil, err
	}
	if model, ok := o.Catalog.Model(i.Selected); ok && !i.Settings.IsZero() && model.NativeContextTokens > 0 && i.Settings.ContextTokens > model.NativeContextTokens {
		return nil, errors.New("context exceeds model native limit")
	}
	life, cancel := context.WithCancel(context.Background())
	m := &Manager{o: o, intent: i, installation: "not_installed", state: Unloaded, changed: make(chan struct{}), ctx: life, cancel: cancel, observer: o.LocalObserver, runtimeID: o.RuntimeID, backend: BackendStatus{Mode: "fixed", State: "unloaded"}}
	if o.AutoBackend {
		m.backend.Mode = "auto"
	}
	m.resetCapabilitiesLocked()
	m.dependencies = DependencyStatus{State: "not_required"}
	if rt, ok := o.Catalog.Runtime(o.RuntimeID); ok {
		m.updateDependenciesLocked(rt, ErrNotInstalled)
	}
	// Reading installed files is allowed at startup; loading/downloading is not.
	if i.Selected != "" {
		model, rt, err := m.resources()
		if err == nil {
			_, err = o.Installer.Verify(ctx, model, rt)
		}
		if err == nil {
			m.installation = "installed"
			m.selectedReady = true
		}
		m.updateDependenciesLocked(rt, err)
		if o.AutoBackend && err != nil && m.IsModelInstalled(ctx, i.Selected) {
			m.installation, m.selectedReady = "installed", true
		}
	}
	SetDefault(m)
	return m, nil
}

// SetLocalObserver installs a trusted process-local completion hook. It is
// intentionally unavailable through management HTTP and does not alter model
// admission or output.
func (m *Manager) SetLocalObserver(observer LocalCallObserver) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.observer = observer
	m.mu.Unlock()
}

func (m *Manager) resources() (routermanifest.Model, routermanifest.Runtime, error) {
	model, ok := m.o.Catalog.Model(m.intent.Selected)
	if !ok {
		return model, routermanifest.Runtime{}, ErrNotInstalled
	}
	if !m.intent.Settings.IsZero() {
		model.ContextTokens = m.intent.Settings.ContextTokens
	}
	rt, ok := m.o.Catalog.Runtime(m.runtimeID)
	if !ok {
		return model, rt, errors.New("runtime artifact unavailable on this platform")
	}
	rt.Execution = m.executionPlan
	return model, rt, nil
}
func (m *Manager) fingerprintLocked() string {
	model, rt, err := m.resources()
	if err != nil {
		return ""
	}
	return model.Fingerprint(rt)
}
func (m *Manager) selectedInstalledLocked() bool {
	if m.installation == "installed" && (m.installingModel == "" || strings.EqualFold(m.installingModel, m.intent.Selected)) {
		return true
	}
	if m.installingModel != "" && !strings.EqualFold(m.installingModel, m.intent.Selected) {
		return m.selectedReady || m.proc != nil
	}
	return false
}
func (m *Manager) resetCapabilitiesLocked() {
	m.performance = Performance{State: "unknown"}
	fp := m.fingerprintLocked()
	m.capabilities = make(map[Purpose]Capability)
	for _, p := range []Purpose{Judgment, Execution, Redaction} {
		m.capabilities[p] = Capability{State: "unknown", Fingerprint: fp, Reason: "acceptance_suite_not_provided"}
	}
}
func (m *Manager) notifyLocked() { close(m.changed); m.changed = make(chan struct{}) }
func (m *Manager) Status() Status {
	if m == nil {
		return Status{Runtime: Unloaded, Healthy: false}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.statusLocked()
}

// WatchStatus returns a snapshot and a signal closed by the next change. Taking
// both under the same lock prevents missed updates without retaining subscribers.
// A nil signal means the Manager has closed.
func (m *Manager) WatchStatus() (Status, <-chan struct{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var changed <-chan struct{}
	if !m.closed {
		changed = m.changed
	}
	return m.statusLocked(), changed
}

func (m *Manager) statusLocked() Status {
	fp := m.fingerprintLocked()
	selectedInstalled := m.selectedInstalledLocked()
	s := Status{MemoryBeforeLoad: m.memoryBeforeLoad, MemoryAfterLoad: m.memoryAfterLoad, Settings: m.intent.Settings, EffectiveSettings: m.effectiveSettingsLocked(), Intent: m.intent, Installation: m.installation, InstallingModel: m.installingModel, Runtime: m.state, Healthy: m.healthy, Waiting: len(m.waiters), QueueLimit: QueueLimit, ExecutionSlots: 1, Progress: m.progress, Error: m.lastError, LastUsed: m.lastUsed, Capabilities: make(map[Purpose]Capability), Dependencies: m.dependencies}
	if m.active {
		s.Active = 1
	}
	s.Backend = m.backend
	s.Performance = m.performance
	s.Performance.Samples = append([]PerformanceSample(nil), m.performance.Samples...)
	if m.proc != nil {
		s.PID = m.proc.PID()
	}
	for p, c := range m.capabilities {
		c.Ready = m.intent.Enabled && !m.stopping && !m.closed && selectedInstalled && m.state != Fault && c.State == "available" && c.Fingerprint == fp
		c.TrialEnabled = trialEnabled(m.intent.Trial, p)
		c.TrialReady = c.TrialEnabled && c.TrialEvidence && m.intent.Enabled && !m.stopping && !m.closed && selectedInstalled && m.state != Fault && c.Fingerprint == fp && m.performanceReadyLocked()
		s.Capabilities[p] = c
	}
	return s
}

// Identity returns the current server-owned model/runtime binding without
// loading or starting a process. An empty identity means no valid selection is
// available; callers must leave those fields unknown in observations.
func (m *Manager) Identity() LocalCallIdentity {
	m.mu.Lock()
	defer m.mu.Unlock()
	model, runtime, err := m.resources()
	if err != nil {
		return LocalCallIdentity{}
	}
	return LocalCallIdentity{ExecutionPlan: runtime.Execution, ModelID: model.ID, ModelFingerprint: model.Fingerprint(runtime), RuntimeID: runtime.ID, RuntimeFingerprint: RuntimeFingerprint(runtime)}
}

// RedactionBudget returns the configured per-call redaction budget owned by the Manager.
func (m *Manager) RedactionBudget() time.Duration {
	if m == nil {
		return 0
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.o.RedactionBudget
}

// Select changes one fixed catalog tier only while no model is enabled.
func (m *Manager) Select(ctx context.Context, id string) error {
	m.op.Lock()
	defer m.op.Unlock()
	if _, ok := m.o.Catalog.Model(id); !ok {
		return errors.New("unknown local model")
	}
	m.mu.Lock()
	if m.closed || m.stopping || m.intent.Enabled || m.installDone != nil {
		m.mu.Unlock()
		return ErrBusy
	}
	i := Intent{Selected: id, UserDisabled: m.intent.UserDisabled, Settings: m.intent.Settings}
	m.mu.Unlock()
	if err := m.o.Store.Save(ctx, i); err != nil {
		return err
	}
	m.mu.Lock()
	m.intent = i
	m.installation = "not_installed"
	m.runtimeID, m.executionPlan = m.o.RuntimeID, ""
	m.backend.State = "unloaded"
	m.installingModel = ""
	m.selectedReady = false
	m.lastError = ""
	m.progress = Progress{}
	m.resetCapabilitiesLocked()
	m.notifyLocked()
	m.mu.Unlock()
	return m.RefreshInstallation(ctx)
}

// EnableTrial persists explicit experimental routing/execution authorization
// only after the corresponding server-owned trial check has passed for the
// current model/runtime/material binding.
func (m *Manager) EnableTrial(ctx context.Context, purposes ...Purpose) error {
	m.op.Lock()
	defer m.op.Unlock()
	m.mu.Lock()
	if m.closed || m.stopping || !m.intent.Enabled || !m.selectedInstalledLocked() {
		m.mu.Unlock()
		return ErrDisabled
	}
	if len(purposes) == 0 {
		purposes = []Purpose{Judgment, Execution}
	}
	fp := m.fingerprintLocked()
	next := m.intent.Trial
	if !m.performanceReadyLocked() {
		m.mu.Unlock()
		return ErrCapability
	}
	for _, p := range purposes {
		if p != Judgment && p != Execution {
			m.mu.Unlock()
			return ErrCapability
		}
		c, ok := m.capabilities[p]
		if !ok || !c.Trial || !c.TrialEvidence || c.Fingerprint == "" || c.Fingerprint != fp {
			m.mu.Unlock()
			return ErrCapability
		}
		setTrialEnabled(&next, p, true)
	}
	i := m.intent
	i.Trial = next
	m.mu.Unlock()
	if err := m.o.Store.Save(ctx, i); err != nil {
		return err
	}
	m.mu.Lock()
	m.intent = i
	m.notifyLocked()
	m.mu.Unlock()
	return nil
}

// DisableTrial persists explicit experimental routing/execution shutdown. It
// never changes the independent redaction trial or runtime enable intent.
func (m *Manager) DisableTrial(ctx context.Context, purposes ...Purpose) error {
	m.op.Lock()
	defer m.op.Unlock()
	m.mu.Lock()
	next := m.intent.Trial
	if len(purposes) == 0 {
		purposes = []Purpose{Judgment, Execution}
	}
	for _, p := range purposes {
		if p != Judgment && p != Execution {
			m.mu.Unlock()
			return ErrCapability
		}
		setTrialEnabled(&next, p, false)
	}
	i := m.intent
	i.Trial = next
	m.mu.Unlock()
	if err := m.o.Store.Save(ctx, i); err != nil {
		return err
	}
	m.mu.Lock()
	m.intent = i
	m.notifyLocked()
	m.mu.Unlock()
	return nil
}

func trialEnabled(in TrialIntent, p Purpose) bool {
	switch p {
	case Judgment:
		return in.Judgment
	case Execution:
		return in.Execution
	default:
		return false
	}
}

func setTrialEnabled(in *TrialIntent, p Purpose, enabled bool) {
	if in == nil {
		return
	}
	switch p {
	case Judgment:
		in.Judgment = enabled
	case Execution:
		in.Execution = enabled
	}
}
func (m *Manager) Enable(ctx context.Context) error {
	return m.EnableSelected(ctx, "")
}

// EnableSelected binds a potentially slow device check to the same selection.
// An intervening selection edit fails instead of enabling unchecked resources.
func (m *Manager) EnableSelected(ctx context.Context, expected string) error {
	return m.enableSelected(ctx, expected, false)
}

// AutoEnableSelected restores startup intent without undoing an explicit user
// disable that completed while device/installation checks were in progress.
func (m *Manager) AutoEnableSelected(ctx context.Context, expected string) error {
	return m.enableSelected(ctx, expected, true)
}

func (m *Manager) enableSelected(ctx context.Context, expected string, startup bool) error {
	m.op.Lock()
	defer m.op.Unlock()
	m.mu.Lock()
	if m.closed || m.stopping {
		m.mu.Unlock()
		return ErrDisabled
	}
	i := m.intent
	if startup && i.UserDisabled {
		m.mu.Unlock()
		return ErrDisabled
	}
	if expected != "" && i.Selected != expected {
		m.mu.Unlock()
		return ErrBusy
	}
	if i.Selected == "" {
		m.mu.Unlock()
		return ErrNotInstalled
	}
	i.Enabled = true
	i.UserDisabled = false
	m.mu.Unlock()
	if err := m.o.Store.Save(ctx, i); err != nil {
		return err
	}
	m.mu.Lock()
	m.intent = i
	m.notifyLocked()
	m.mu.Unlock()
	return nil
}

// IsModelInstalled verifies whether a fixed catalog model and the platform
// runtime are physically installed without starting a process or mutating state.
func (m *Manager) IsModelInstalled(ctx context.Context, modelID string) bool {
	if m == nil {
		return false
	}
	model, ok := m.o.Catalog.Model(modelID)
	if !ok {
		return false
	}
	return m.verifyAnyInstallation(ctx, model) == nil
}

func (m *Manager) RefreshInstallation(ctx context.Context) error {
	m.mu.Lock()
	if m.installDone != nil || m.loading != nil || m.proc != nil {
		m.mu.Unlock()
		return ErrBusy
	}
	model, rt, err := m.resources()
	id := m.intent.Selected
	m.mu.Unlock()
	if err == nil {
		err = m.verifyAnyInstallation(ctx, model)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.intent.Selected == id && m.installDone == nil && m.loading == nil && m.proc == nil {
		m.updateDependenciesLocked(rt, err)
		m.installingModel = ""
		if err == nil {
			m.installation = "installed"
			m.selectedReady = true
		} else {
			m.installation = "not_installed"
			m.selectedReady = false
		}
		m.notifyLocked()
	}
	// A missing installation is a state, not a failed selection operation.
	return ctx.Err()
}

// Install returns after starting an owned job. Request disconnect does not
// abandon that job; CancelInstall, Disable and Close synchronously join it.
func (m *Manager) Install() error {
	return m.installTarget("")
}

// InstallModel starts an owned installation job for a specific catalog model,
// allowing another catalog model to be downloaded without disabling or stopping
// a currently enabled/running model.
func (m *Manager) InstallModel(modelID string) error {
	return m.installTarget(modelID)
}

func (m *Manager) installTarget(targetModelID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || m.stopping {
		return ErrDisabled
	}
	if targetModelID == "" && !m.intent.Enabled {
		return ErrDisabled
	}
	if m.installDone != nil {
		return nil
	}
	var (
		model routermanifest.Model
		rt    routermanifest.Runtime
		err   error
	)
	if targetModelID == "" {
		if m.proc != nil || m.loading != nil {
			return ErrBusy
		}
		model, rt, err = m.resources()
		if err != nil {
			return err
		}
	} else {
		var ok bool
		model, ok = m.o.Catalog.Model(targetModelID)
		if !ok {
			return ErrNotInstalled
		}
		if (m.proc != nil || m.loading != nil) && strings.EqualFold(model.ID, m.intent.Selected) {
			return ErrBusy
		}
		rt, ok = m.o.Catalog.Runtime(m.o.RuntimeID)
		if !ok {
			return errors.New("runtime artifact unavailable on this platform")
		}
	}
	ctx, cancel := context.WithCancel(m.ctx)
	done := make(chan struct{})
	m.installDone, m.installCancel = done, cancel
	m.installingModel = model.ID
	m.installation, m.lastError, m.progress = "downloading", "", Progress{}
	if rt.Dependencies != nil && m.dependencies.State != "ready" {
		m.dependencies = DependencyStatus{State: "preparing", ID: rt.Dependencies.ID, Version: rt.Dependencies.Version}
	}
	m.notifyLocked()
	m.workers.Add(1)
	go func() {
		defer m.workers.Done()
		defer cancel()
		if m.o.AutoBackend {
			// Explicit installs prepare the fallback runtime, independent of the
			// backend currently serving another selected model.
			rt, _ = m.o.Catalog.Runtime(m.o.RuntimeID)
		}
		err := m.installBackendPackages(ctx, model, rt, func(p Progress) {
			m.mu.Lock()
			m.progress = p
			if rt.Dependencies != nil && m.dependencies.State == "preparing" && p.Stage != "" && p.Stage != "dependency" {
				m.dependencies = DependencyStatus{State: "ready", ID: rt.Dependencies.ID, Version: rt.Dependencies.Version}
			}
			m.notifyLocked()
			m.mu.Unlock()
		})
		if err == nil {
			_, err = m.o.Installer.Verify(ctx, model, rt)
		}
		m.mu.Lock()
		if rt.Dependencies != nil && m.dependencies.State == "ready" && err != nil && dependencyCode(err) == "" {
			// Keep verified runtime dependency status when a later download stage fails or is canceled.
		} else {
			m.updateDependenciesLocked(rt, err)
		}
		if err == nil {
			m.installation, m.progress = "installed", Progress{}
			if strings.EqualFold(model.ID, m.intent.Selected) {
				m.selectedReady = true
			}
		} else if errors.Is(err, context.Canceled) {
			m.installation = "canceled"
		} else {
			m.progress.Reason = classifyInstallError(err)
			m.installation, m.lastError = "failed", operationCode(err, installErrorCode(m.progress.Stage, m.progress.Reason))
		}
		m.installDone, m.installCancel = nil, nil
		close(done)
		m.notifyLocked()
		m.mu.Unlock()
	}()
	return nil
}
func (m *Manager) CancelInstall(ctx context.Context) error {
	m.mu.Lock()
	done, cancel := m.installDone, m.installCancel
	m.mu.Unlock()
	if cancel == nil {
		return nil
	}
	cancel()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// ensure merges cold starts independently of any individual waiter's deadline.
func (m *Manager) ensure(ctx context.Context, epoch uint64) error {
	for {
		m.mu.Lock()
		if !m.intent.Enabled || m.closed || m.stopping || epoch != m.epoch {
			m.mu.Unlock()
			return ErrDisabled
		}
		if m.proc != nil {
			healthy := m.healthy
			m.mu.Unlock()
			if healthy {
				return nil
			}
			return ErrFault
		}
		if m.installation != "installed" {
			dependencyError := m.dependencies.Error
			m.mu.Unlock()
			if dependencyError != "" {
				return &routerprocess.DependencyError{Code: dependencyError, Err: ErrNotInstalled}
			}
			return ErrNotInstalled
		}
		done := m.loading
		if done == nil {
			model, rt, err := m.resources()
			if err != nil {
				m.mu.Unlock()
				return err
			}
			loadCtx, cancel := context.WithCancel(m.ctx)
			done = make(chan struct{})
			m.loading, m.loadCancel = done, cancel
			m.state, m.lastError = Loading, ""
			m.notifyLocked()
			m.workers.Add(1)
			go m.load(loadCtx, cancel, done, model, rt, m.loadTimeoutLocked())
		}
		m.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-done:
		}
		m.mu.Lock()
		failed := m.state == Fault
		m.mu.Unlock()
		if failed {
			m.mu.Lock()
			dependencyError := m.dependencies.Error
			m.mu.Unlock()
			if dependencyError != "" {
				return &routerprocess.DependencyError{Code: dependencyError, Err: ErrFault}
			}
			return ErrFault
		}
	}
}
func (m *Manager) load(ctx context.Context, cancel context.CancelFunc, done chan struct{}, model routermanifest.Model, rt routermanifest.Runtime, loadTimeout time.Duration) {
	defer m.workers.Done()
	defer cancel()
	// A cancelable timer also supports deterministic loading-timeout tests.
	finished := make(chan struct{})
	go func() {
		select {
		case <-m.o.After(loadTimeout):
			cancel()
		case <-finished:
		case <-ctx.Done():
		}
	}()
	before, beforeErr := m.o.AvailableMemory(ctx)
	m.mu.Lock()
	m.memoryBeforeLoad = routerdevice.Unknown[uint64]("available_memory_probe_failed")
	m.memoryAfterLoad = routerdevice.Unknown[uint64]("load_not_completed")
	if beforeErr == nil {
		m.memoryBeforeLoad = routerdevice.Known(before)
	}
	m.mu.Unlock()
	var spec routerprocess.Spec
	var err error
	var p routerprocess.Process
	var backend BackendStatus
	verificationFailed := false
	if m.o.AutoBackend {
		bounded, end := context.WithTimeout(ctx, loadTimeout)
		p, rt, backend, err = m.startBackend(bounded, model)
		end()
		verificationFailed = errors.Is(err, ErrNotInstalled) && ctx.Err() == nil
	} else {
		spec, err = m.o.Installer.Verify(ctx, model, rt)
		if err == nil {
			spec.ContextSize = model.ContextTokens
		}
		verificationFailed = err != nil && ctx.Err() == nil
		if err == nil && m.o.BeforeStart != nil {
			err = m.o.BeforeStart(ctx, model.ID, rt.Backend)
		}
		if err == nil {
			for attempt := 0; attempt < 2; attempt++ {
				p, err = m.o.Factory.Start(ctx, spec)
				if err != nil {
					break
				}
				exitedBeforeReady := false
				for {
					if err = ctx.Err(); err != nil {
						break
					}
					probe, stop := context.WithTimeout(ctx, time.Second)
					err = p.Health(probe)
					stop()
					if err == nil {
						break
					}
					select {
					case <-ctx.Done():
						err = ctx.Err()
					case <-p.Done():
						err = errors.Join(ErrFault, p.Err())
						exitedBeforeReady = true
					case <-m.o.After(50 * time.Millisecond):
						continue
					}
					break
				}
				if !exitedBeforeReady || attempt > 0 || dependencyCode(err) != "" || ctx.Err() != nil {
					break
				}
				stop, finish := context.WithTimeout(context.Background(), 5*time.Second)
				_ = p.Stop(stop)
				finish()
				p = nil
			}
		}
	}
	after, afterErr := m.o.AvailableMemory(ctx)
	close(finished)
	m.mu.Lock()
	m.memoryAfterLoad = routerdevice.Unknown[uint64]("available_memory_probe_failed")
	if afterErr == nil {
		m.memoryAfterLoad = routerdevice.Known(after)
	}
	if verificationFailed {
		m.installation = "not_installed"
		m.selectedReady = false
	}
	if dependencyCode(err) != "" {
		m.installation = "not_installed"
	}
	m.updateDependenciesLocked(rt, err)
	adopt := err == nil && ctx.Err() == nil && m.intent.Enabled && !m.closed && !m.stopping
	if adopt {
		if m.o.AutoBackend {
			previous := m.fingerprintLocked()
			m.runtimeID, m.executionPlan, m.backend = rt.ID, rt.Execution, backend
			if previous != m.fingerprintLocked() {
				m.resetCapabilitiesLocked()
			}
		}
		m.proc, m.healthy, m.state = p, true, Ready
		m.performanceEpoch++
		m.performance = Performance{State: "unknown"}
		m.lastUsed = m.o.Now()
		m.workers.Add(1)
		go m.watch(p)
	}
	m.mu.Unlock()
	unreaped := false
	if !adopt && p != nil {
		stop, finish := context.WithTimeout(context.Background(), 5*time.Second)
		_ = p.Stop(stop)
		finish()
		select {
		case <-p.Done():
		default:
			unreaped = true
		}
	}
	m.mu.Lock()
	if unreaped {
		m.proc = p
		m.workers.Add(1)
		go m.watch(p)
	}
	if !adopt {
		if m.o.AutoBackend {
			backend.State = "unavailable"
			m.backend = backend
		}
		m.healthy = false
		if m.intent.Enabled && !m.stopping && !m.closed {
			m.state, m.lastError = Fault, operationCode(err, "load_failed_or_timed_out")
		} else {
			m.state = Unloaded
		}
		if unreaped {
			m.state, m.lastError = Fault, "load_cleanup_pending"
		}
	}
	m.loading, m.loadCancel = nil, nil
	close(done)
	m.notifyLocked()
	m.mu.Unlock()
}
func (m *Manager) watch(p routerprocess.Process) {
	defer m.workers.Done()
	<-p.Done()
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.proc == p {
		m.proc, m.healthy = nil, false
		m.performance = Performance{State: "unknown"}
		if m.stopRequested != p {
			m.state = Fault
			if m.o.AutoBackend && m.backend.Effective != "cpu" {
				m.coolBackendLocked(m.runtimeID)
				m.backend.State, m.backend.FallbackReason = "unavailable", "accelerator_process_exited"
				m.resetCapabilitiesLocked()
			}
			if m.lastError == "" {
				m.lastError = "process_exited"
			}
		}
		if m.activeCancel != nil {
			m.activeCancel()
		}
		m.notifyLocked()
	}
	if m.stopRequested == p {
		m.stopRequested = nil
	}
}

// Draining work is not a requested process exit. Bind the expected exit to the
// owned child immediately before Stop, without hiding an already observed exit.
func (m *Manager) stopProcess(ctx context.Context, p routerprocess.Process) error {
	m.mu.Lock()
	if m.proc == p {
		select {
		case <-p.Done():
			// Stop may clear ownership before the watcher gets its lock. Record
			// this prior exit now, retaining any more specific runtime failure.
			if m.stopRequested != p {
				m.healthy = false
				m.state = Fault
				if m.lastError == "" {
					m.lastError = "process_exited"
				}
				if m.activeCancel != nil {
					m.activeCancel()
				}
				m.notifyLocked()
			}
		default:
			m.stopRequested = p
		}
	}
	m.mu.Unlock()
	return p.Stop(ctx)
}

func validPurpose(p Purpose) bool { return p == Judgment || p == Execution || p == Redaction }

// Use holds the single execution slot until fn returns, including streamed body
// consumption. The callback must honor ctx. Cold loading precedes stage budgets.
func (m *Manager) Use(ctx context.Context, p Purpose, fn func(context.Context, string) error) error {
	if !validPurpose(p) || fn == nil {
		return errors.New("invalid runtime invocation")
	}
	w := &waiter{}
	m.mu.Lock()
	if !m.intent.Enabled || m.closed || m.stopping {
		m.mu.Unlock()
		return ErrDisabled
	}
	if len(m.waiters) >= QueueLimit {
		m.mu.Unlock()
		return ErrQueueFull
	}
	settings, snapshot := RuntimeSettingsFrom(ctx)
	if !snapshot {
		settings = m.intent.Settings
	}
	if settings != m.intent.Settings {
		m.mu.Unlock()
		return ErrSettingsChanged
	}
	epoch := m.epoch
	m.waiters = append(m.waiters, w)
	m.notifyLocked()
	m.mu.Unlock()
	defer m.removeWaiter(w)
	if err := m.ensure(ctx, epoch); err != nil {
		return err
	}
	budget := m.o.JudgmentBudget
	if p == Execution {
		budget = m.o.ExecutionQueueBudget
	}
	if p == Redaction {
		budget = m.o.RedactionBudget
	}
	if !settings.IsZero() {
		if p == Judgment {
			budget = time.Duration(settings.JudgmentQueueSeconds) * time.Second
		}
		if p == Execution {
			budget = time.Duration(settings.ExecutionQueueSeconds) * time.Second
		}
	}
	stage, stageCancel := context.WithTimeout(ctx, budget)
	defer stageCancel()
	for {
		m.mu.Lock()
		if !m.intent.Enabled || m.closed || m.stopping || epoch != m.epoch {
			m.mu.Unlock()
			return ErrDisabled
		}
		if m.proc == nil || !m.healthy {
			m.mu.Unlock()
			return ErrFault
		}
		if err := stage.Err(); err != nil {
			m.mu.Unlock()
			if errors.Is(err, context.DeadlineExceeded) {
				return purposeTimeout(ctx, p, true)
			}
			return err
		}
		if !m.active && len(m.waiters) > 0 && m.waiters[0] == w {
			m.waiters = m.waiters[1:]
			pctx := stage
			var finish context.CancelFunc
			if p == Execution {
				executionBudget := m.o.ExecutionBudget
				if !settings.IsZero() {
					executionBudget = time.Duration(settings.ExecutionSeconds) * time.Second
				}
				pctx, finish = context.WithTimeout(ctx, executionBudget)
			} else if p == Judgment && !settings.IsZero() {
				pctx, finish = context.WithTimeout(ctx, time.Duration(settings.JudgmentInferenceSeconds)*time.Second)
			} else {
				pctx, finish = context.WithTimeout(ctx, budget)
			}
			m.active, m.activeCancel, m.state = true, finish, Busy
			process := m.proc
			m.notifyLocked()
			m.mu.Unlock()
			err := fn(pctx, process.Endpoint())
			stageErr := pctx.Err()
			if err == nil {
				err = stageErr
			}
			finish()
			m.mu.Lock()
			// A crashed child cancels its call context, but that is not a
			// client cancellation. W4 must be able to distinguish fallback
			// eligible faults from cancellation, shutdown and stage expiry.
			if err != nil {
				switch {
				case ctx.Err() != nil:
					err = ctx.Err()
				case epoch != m.epoch || m.closed || !m.intent.Enabled:
					err = ErrDisabled
				case errors.Is(stageErr, context.DeadlineExceeded):
					err = purposeTimeout(ctx, p, false)
				default:
					select {
					case <-process.Done():
						err = ErrFault
					default:
					}
				}
			}
			m.active, m.activeCancel = false, nil
			m.lastUsed = m.o.Now()
			if m.proc == process {
				if m.stopping {
					m.state = Unloading
				} else {
					m.state = Idle
				}
			}
			m.notifyLocked()
			m.mu.Unlock()
			return err
		}
		changed := m.changed
		m.mu.Unlock()
		select {
		case <-stage.Done():
			if errors.Is(stage.Err(), context.DeadlineExceeded) {
				return purposeTimeout(ctx, p, true)
			}
			return stage.Err()
		case <-changed:
		}
	}
}
func (m *Manager) removeWaiter(w *waiter) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, c := range m.waiters {
		if c == w {
			m.waiters = append(m.waiters[:i], m.waiters[i+1:]...)
			m.notifyLocked()
			break
		}
	}
}

// AcquireSlot acquires the single execution slot on the local runtime Manager
// and returns a release function and the execution timeout budget. When m is
// nil, it returns a no-op release and 0 timeout.
func (m *Manager) AcquireSlot(ctx context.Context) (func(), time.Duration, error) {
	if m == nil {
		return func() {}, 0, nil
	}
	w := &waiter{}
	m.mu.Lock()
	if !m.intent.Enabled || m.closed || m.stopping {
		m.mu.Unlock()
		return nil, 0, ErrDisabled
	}
	if len(m.waiters) >= QueueLimit {
		m.mu.Unlock()
		return nil, 0, ErrQueueFull
	}
	epoch := m.epoch
	m.waiters = append(m.waiters, w)
	m.notifyLocked()
	budget := m.o.ExecutionQueueBudget
	if !m.intent.Settings.IsZero() && m.intent.Settings.ExecutionQueueSeconds > 0 {
		budget = time.Duration(m.intent.Settings.ExecutionQueueSeconds) * time.Second
	}
	m.mu.Unlock()

	if err := m.ensure(ctx, epoch); err != nil {
		m.removeWaiter(w)
		return nil, 0, err
	}
	stage, stageCancel := context.WithTimeout(ctx, budget)
	defer stageCancel()
	for {
		m.mu.Lock()
		if !m.intent.Enabled || m.closed || m.stopping || epoch != m.epoch {
			m.mu.Unlock()
			m.removeWaiter(w)
			return nil, 0, ErrDisabled
		}
		if m.proc == nil || !m.healthy {
			m.mu.Unlock()
			m.removeWaiter(w)
			return nil, 0, ErrFault
		}
		if err := stage.Err(); err != nil {
			m.mu.Unlock()
			m.removeWaiter(w)
			if errors.Is(err, context.DeadlineExceeded) {
				return nil, 0, purposeTimeout(ctx, Execution, true)
			}
			return nil, 0, err
		}
		if !m.active && len(m.waiters) > 0 && m.waiters[0] == w {
			m.waiters = m.waiters[1:]
			execBudget := m.o.ExecutionBudget
			if !m.intent.Settings.IsZero() && m.intent.Settings.ExecutionSeconds > 0 {
				execBudget = time.Duration(m.intent.Settings.ExecutionSeconds) * time.Second
			}
			m.active, m.state = true, Busy
			process := m.proc
			m.notifyLocked()
			m.mu.Unlock()
			var once sync.Once
			release := func() {
				once.Do(func() {
					m.mu.Lock()
					m.active, m.activeCancel = false, nil
					m.lastUsed = m.o.Now()
					if m.proc == process {
						if m.stopping {
							m.state = Unloading
						} else {
							m.state = Idle
						}
					}
					m.notifyLocked()
					m.mu.Unlock()
				})
			}
			return release, execBudget, nil
		}
		changed := m.changed
		m.mu.Unlock()
		select {
		case <-stage.Done():
			m.removeWaiter(w)
			if errors.Is(stage.Err(), context.DeadlineExceeded) {
				return nil, 0, purposeTimeout(ctx, Execution, true)
			}
			return nil, 0, stage.Err()
		case <-changed:
		}
	}
}

// CheckCapability records only versioned sample evidence and never infers
// judgment/redaction from a healthy process or a normal execution answer.
func (m *Manager) CheckCapability(ctx context.Context, c Check) error {
	if !validPurpose(c.Purpose) {
		return errors.New("invalid capability")
	}
	m.mu.Lock()
	fingerprint := m.fingerprintLocked()
	epoch := m.epoch
	m.mu.Unlock()
	if c.Run == nil || c.Suite == "" {
		m.mu.Lock()
		m.capabilities[c.Purpose] = Capability{State: "unknown", Fingerprint: fingerprint, Reason: "acceptance_suite_not_provided"}
		m.mu.Unlock()
		return ErrCapability
	}
	var evidence string
	err := m.Use(ctx, c.Purpose, func(ctx context.Context, endpoint string) error {
		// Automatic cold loading may choose a different verified runtime.
		// Bind the sample to the process that actually receives it.
		m.mu.Lock()
		fingerprint = m.fingerprintLocked()
		m.mu.Unlock()
		var err error
		evidence, err = c.Run(ctx, endpoint)
		return err
	})
	state, reason := "available", ""
	if err != nil || evidence == "" {
		state, reason = "unavailable", "sample_validation_failed"
		if err == nil {
			err = ErrCapability
		}
	}
	trialEvidence := c.Trial && err == nil && evidence != ""
	if c.Trial {
		state, reason = "unknown", "experimental_trial_quality_not_formally_accepted"
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if fingerprint != m.fingerprintLocked() || epoch != m.epoch {
		return ErrCapability
	}
	m.capabilities[c.Purpose] = Capability{State: state, Fingerprint: fingerprint, Suite: c.Suite, Evidence: evidence, CheckedAt: m.o.Now(), Reason: reason, Trial: c.Trial, TrialEvidence: trialEvidence}
	m.notifyLocked()
	return err
}
func (m *Manager) Require(p Purpose) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.intent.Enabled || m.closed || m.stopping {
		return ErrDisabled
	}
	c, ok := m.capabilities[p]
	if !ok || c.Fingerprint != m.fingerprintLocked() {
		return ErrCapability
	}
	if c.State == "available" {
		return nil
	}
	if (p == Judgment || p == Execution) && c.Trial && c.TrialEvidence && trialEnabled(m.intent.Trial, p) && m.performanceReadyLocked() {
		return nil
	}
	if c.State != "available" {
		return ErrCapability
	}
	return nil
}

// Sweep supports trusted callers that explicitly configure an idle timeout.
// The product keeps enabled models resident and does not schedule this check.
func (m *Manager) Sweep(ctx context.Context) error {
	m.op.Lock()
	defer m.op.Unlock()
	m.mu.Lock()
	if m.o.IdleTimeout <= 0 || !m.intent.Enabled || m.closed || m.stopping || m.active || len(m.waiters) > 0 || m.loading != nil || m.proc == nil || m.o.Now().Sub(m.lastUsed) < m.o.IdleTimeout {
		m.mu.Unlock()
		return nil
	}
	p := m.proc
	m.healthy, m.state = false, Unloading
	m.stopping = true
	m.notifyLocked()
	m.mu.Unlock()
	err := m.stopProcess(ctx, p)
	m.mu.Lock()
	m.stopping = false
	if err == nil {
		if m.proc == p {
			m.proc = nil
		}
		m.state = Unloaded
		m.backend.State = "unloaded"
	} else {
		m.state, m.lastError = Fault, "unload_failed"
	}
	m.notifyLocked()
	m.mu.Unlock()
	return err
}

func (m *Manager) Disable(ctx context.Context) error {
	m.op.Lock()
	defer m.op.Unlock()
	m.mu.Lock()
	i := m.intent
	i.Enabled = false
	i.UserDisabled = true
	m.intent = i
	m.stopping = true
	m.state = Unloading
	m.epoch++
	m.notifyLocked()
	m.mu.Unlock()
	// Persist before acknowledging disable; failure still stops this session.
	saveErr := m.o.Store.Save(ctx, i)
	err := m.stop(ctx, false)
	return errors.Join(saveErr, err)
}
func (m *Manager) stop(ctx context.Context, closing bool) error {
	m.mu.Lock()
	m.stopping = true
	if closing {
		m.closed = true
		m.epoch++
	}
	if m.loadCancel != nil {
		m.loadCancel()
	}
	if m.installCancel != nil {
		m.installCancel()
	}
	m.notifyLocked()
	for m.active || m.loading != nil || m.installDone != nil {
		changed := m.changed
		m.mu.Unlock()
		select {
		case <-changed:
		case <-ctx.Done():
			m.mu.Lock()
			if m.activeCancel != nil {
				m.activeCancel()
			}
			p := m.proc
			m.mu.Unlock()
			if p != nil {
				kill, finish := context.WithTimeout(context.Background(), 5*time.Second)
				_ = m.stopProcess(kill, p)
				finish()
			}
			m.mu.Lock()
			m.stopping = m.updating
			m.healthy = false
			m.state = Fault
			m.lastError = "drain_timed_out"
			m.notifyLocked()
			m.mu.Unlock()
			return ctx.Err()
		}
		m.mu.Lock()
	}
	p := m.proc
	m.healthy, m.state = false, Unloading
	m.notifyLocked()
	m.mu.Unlock()
	var err error
	if p != nil {
		err = m.stopProcess(ctx, p)
	}
	m.mu.Lock()
	m.stopping = m.updating
	m.state = Unloaded
	m.backend.State = "unloaded"
	if err != nil {
		m.state, m.lastError = Fault, "unload_failed"
	} else if m.proc == p {
		m.proc = nil
	}
	m.notifyLocked()
	m.mu.Unlock()
	return err
}

// Close retains persisted enable intent but prevents all new work in this
// Router lifetime. It cancels every owned worker and joins process monitors.
func (m *Manager) Close(ctx context.Context) error {
	m.op.Lock()
	defer m.op.Unlock()
	err := m.stop(ctx, true)
	m.cancel()
	done := make(chan struct{})
	go func() { m.workers.Wait(); close(done) }()
	select {
	case <-done:
		return err
	case <-ctx.Done():
		return fmt.Errorf("runtime close: %w", ctx.Err())
	}
}
