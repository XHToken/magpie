package routerservice

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"runtime"
	"strings"
	"time"

	"github.com/yetone/magpie/internal/dirs"
	"github.com/yetone/magpie/internal/routerconfig"
	"github.com/yetone/magpie/internal/routerdevice"
	"github.com/yetone/magpie/internal/routerdownload"
	"github.com/yetone/magpie/internal/routermanifest"
	"github.com/yetone/magpie/internal/routerprocess"
	"github.com/yetone/magpie/internal/routerruntime"
)

// LocalModels is one Router-owned manager, shared across all applications.
// It is independent of host adapters, provider credentials and redaction intent.
type LocalModels struct {
	SampleDelay   time.Duration
	Manager       *routerruntime.Manager
	Detector      *routerdevice.Detector
	Layout        dirs.Layout
	Catalog       routermanifest.Catalog
	Checks        []routerruntime.Check // Registered by W4/W5, never supplied over HTTP.
	Observer      routerruntime.LocalCallObserver
	AutoLoad      bool
	dynamicChecks bool
	checkFactory  func(routerruntime.LocalCallIdentity, string) []routerruntime.Check
}

func newLocalModels(ctx context.Context, l dirs.Layout, store *routerconfig.Store, observed ...func(routerprocess.Process, routerprocess.Spec)) (*LocalModels, error) {
	return newLocalModelsWithTrial(ctx, l, store, false, observed...)
}

func newLocalModelsWithTrial(ctx context.Context, l dirs.Layout, store *routerconfig.Store, experimental bool, observed ...func(routerprocess.Process, routerprocess.Spec)) (*LocalModels, error) {
	catalog := routermanifest.Fixed()
	var local *LocalModels
	var onStart func(routerprocess.Process, routerprocess.Spec)
	if len(observed) > 0 {
		onStart = observed[0]
	}
	manager, err := routerruntime.New(ctx, routerruntime.Options{BeforeStart: func(ctx context.Context, id, backend string) error {
		return local.admitModel(ctx, id, "load", backend == "cpu")
	}, PerformanceRequired: true, AutoBackend: true, Catalog: catalog, RuntimeID: routerruntime.PlatformRuntime(catalog, runtime.GOOS, runtime.GOARCH), Store: routerruntime.ConfigIntentStore{Store: store, StandardSettings: true}, Installer: &routerruntime.FileInstaller{Layout: l, Downloader: &routerdownload.Downloader{}}, Factory: routerprocess.NativeFactory{OnStart: onStart}, ExperimentalRedaction: experimental})
	if err != nil {
		return nil, err
	}
	local = &LocalModels{SampleDelay: time.Second, Manager: manager, Detector: routerdevice.Native(), Layout: l, Catalog: catalog, AutoLoad: true, dynamicChecks: true, checkFactory: localCapabilityChecks}
	return local, nil
}
func (l *LocalModels) detect(ctx context.Context, path string) routerdevice.Report {
	return l.modelAdvice(ctx, "detect", false)
}

func (l *LocalModels) runCapabilityChecks(ctx context.Context) (int, error) {
	// Bind the shared server-owned contracts and their prompt to one
	// selection/settings snapshot. HTTP cannot invent capability evidence.
	identity, settings := l.Manager.SnapshotBinding(ctx)
	promptVersion := localPromptVersion(settings)
	checks := append([]routerruntime.Check(nil), l.Checks...)
	factory := l.checkFactory
	if factory == nil && l.dynamicChecks {
		factory = localCapabilityChecks
		checks = nil
	}
	if factory != nil {
		checks = append(factory(identity, promptVersion), checks...)
	}
	if len(checks) == 0 {
		return 0, nil
	}
	var err error
	for _, check := range checks {
		run := check.Run
		check.Run = func(ctx context.Context, endpoint string) (string, error) {
			// This executes inside the acquired slot. A concurrent model or
			// settings change must not attach stale evidence to its replacement.
			currentIdentity, currentSettings := l.Manager.SnapshotBinding(ctx)
			if currentIdentity != identity || currentSettings != settings || run == nil {
				return "", routerruntime.ErrCapability
			}
			return run(ctx, endpoint)
		}
		checkCtx := routerruntime.WithRuntimeSettings(ctx, settings)
		checkCtx = routerruntime.WithLocalCallContext(checkCtx, routerruntime.LocalCallContext{Check: true, Mode: routerruntime.LocalCallDiagnostic, PromptVersion: promptVersion, Material: check.Suite})
		material := localJudgmentMaterial
		if check.Purpose == routerruntime.Execution {
			material = localExecutionMaterial
		}
		checkCtx = withLocalCheckCallObserver(checkCtx, localCheckCallObserver{target: l.Observer, manager: l.Manager, purpose: check.Purpose, suite: check.Suite, prompt: promptVersion, material: material})
		checkErr := l.Manager.CheckCapability(checkCtx, check)
		err = errors.Join(err, checkErr)
	}
	if err == nil && l.dynamicChecks {
		err = l.checkPerformance(ctx)
	}
	return len(checks), err
}

// autoLoadInstalled automatically enables, loads, checks, and enables trial
// routing/execution for an installed local model on service startup unless
// the user explicitly disabled the local model.
func (l *LocalModels) autoLoadInstalled(ctx context.Context) {
	if l == nil || l.Manager == nil || l.Detector == nil {
		return
	}
	status := l.Manager.Status()
	if status.Intent.UserDisabled {
		return
	}
	var installed []routermanifest.Model
	for _, m := range l.Catalog.Models {
		if l.Manager.IsModelInstalled(ctx, m.ID) {
			installed = append(installed, m)
		}
	}
	if len(installed) == 0 || ctx.Err() != nil {
		return
	}
	report := l.detect(ctx, l.Layout.Models)
	if report.Advice == nil || ctx.Err() != nil {
		return
	}
	var target routermanifest.Model
	for _, model := range installed {
		assessment, ok := report.Advice.Model(model.ID)
		if ok && strings.EqualFold(model.ID, status.Intent.Selected) && status.Intent.Enabled && assessment.Manual == routerdevice.CheckPassed {
			target = model
			break
		}
	}
	if target.ID == "" && report.Advice.State == "recommended" {
		for _, model := range installed {
			if model.ID == report.Advice.ModelID {
				target = model
				break
			}
		}
		if target.ID == "" {
			for _, model := range installed {
				if assessment, ok := report.Advice.Model(model.ID); ok && assessment.Automatic == routerdevice.CheckPassed {
					target = model
					break
				}
			}
		}
	}
	if target.ID == "" {
		return
	}
	status = l.Manager.Status()
	if status.Intent.UserDisabled {
		return
	}
	if !strings.EqualFold(status.Intent.Selected, target.ID) {
		if status.Intent.Enabled {
			return
		}
		if err := l.Manager.Select(ctx, target.ID); err != nil {
			return
		}
		status = l.Manager.Status()
	}
	if !status.Intent.Enabled {
		if err := l.Manager.AutoEnableSelected(ctx, status.Intent.Selected); err != nil {
			return
		}
	}
	if err := l.Manager.Load(ctx); err != nil {
		return
	}
	if count, err := l.runCapabilityChecks(ctx); err != nil || count == 0 {
		return
	}
	st := l.Manager.Status()
	if st.Capabilities[routerruntime.Judgment].Trial && st.Capabilities[routerruntime.Judgment].TrialEvidence &&
		st.Capabilities[routerruntime.Execution].Trial && st.Capabilities[routerruntime.Execution].TrialEvidence &&
		(!st.Intent.Trial.Judgment || !st.Intent.Trial.Execution) {
		_ = l.Manager.EnableTrial(ctx, routerruntime.Judgment, routerruntime.Execution)
	}
}

func localModelPath(r *http.Request) bool {
	switch r.URL.EscapedPath() {
	case "/api/local-model", "/api/local-model/settings", "/api/local-model/detect", "/api/local-model/select", "/api/local-model/enable", "/api/local-model/install", "/api/local-model/cancel", "/api/local-model/load", "/api/local-model/disable", "/api/local-model/health", "/api/local-model/check", "/api/local-model/trial/enable", "/api/local-model/trial/disable":
		return true
	}
	return false
}
func localModelRead(r *http.Request) bool {
	return r.URL.Path == "/api/local-model" || r.URL.Path == "/api/local-model/settings"
}

func localModelAPI(w http.ResponseWriter, r *http.Request, l *LocalModels) {
	w.Header().Set("Content-Type", "application/json")
	fail := func(code string, status int) {
		w.WriteHeader(status)
		if r.Method != http.MethodHead {
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": code}})
		}
	}
	if l == nil || l.Manager == nil {
		fail("local_runtime_unavailable", http.StatusServiceUnavailable)
		return
	}
	if r.URL.RawQuery != "" {
		fail("invalid_query", 400)
		return
	}
	if localModelRead(r) {
		if r.Method != http.MethodHead {
			_ = json.NewEncoder(w).Encode(map[string]any{"schemaVersion": 1, "status": l.Manager.Status(), "catalog": l.Catalog, "models": aggregateLocalModelCandidates(r.Context(), nil, l)})
		}
		return
	}
	var input struct {
		Model     string   `json:"model"`
		Judgment  *bool    `json:"judgment"`
		Execution *bool    `json:"execution"`
		Purposes  []string `json:"purposes"`
	}
	if r.Body != nil {
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&input); err != nil && !errors.Is(err, io.EOF) {
			fail("invalid_request", 400)
			return
		}
		var extra any
		if decoder.Decode(&extra) != io.EOF {
			fail("invalid_request", 400)
			return
		}
	}
	ctx := r.Context()
	var err error
	status := l.Manager.Status()
	admit := func(id, operation string) bool {
		report := l.modelAdvice(ctx, operation, false)
		if report.Advice != nil {
			if candidate, ok := report.Advice.Model(id); ok && candidate.Manual == routerdevice.CheckPassed {
				return true
			}
		}
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": "device_check_not_passed"}, "device": report})
		return false
	}
	switch r.URL.Path {
	case "/api/local-model/detect":
		_ = json.NewEncoder(w).Encode(map[string]any{"schemaVersion": 1, "device": l.detect(ctx, l.Layout.Models)})
		return
	case "/api/local-model/select":
		err = l.Manager.Select(ctx, input.Model)
	case "/api/local-model/enable":
		model, ok := l.Catalog.Model(status.Intent.Selected)
		if !ok {
			fail("model_selection_required", 409)
			return
		}
		if !admit(model.ID, "detect") {
			return
		}
		err = l.Manager.EnableSelected(ctx, status.Intent.Selected)
	case "/api/local-model/install":
		// Model and runtime can be on separate volumes. Recheck both explicit
		// destinations immediately before installation, using the T3.1 provider.
		targetModelID := status.Intent.Selected
		if input.Model != "" {
			targetModelID = input.Model
			if _, ok := l.Catalog.Model(targetModelID); !ok {
				fail("invalid_request", 400)
				return
			}
		}
		if !admit(targetModelID, "install") {
			return
		}

		if input.Model != "" {
			err = l.Manager.InstallModel(input.Model)
		} else {
			err = l.Manager.Install()
		}
	case "/api/local-model/cancel":
		err = l.Manager.CancelInstall(ctx)
	case "/api/local-model/load":
		err = l.Manager.Load(ctx)
	case "/api/local-model/disable":
		drain, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		err = l.Manager.Disable(drain)
	case "/api/local-model/health":
		err = l.Manager.Health(ctx)
	case "/api/local-model/trial/enable":
		purposes, parseErr := localTrialPurposes(input.Judgment, input.Execution, input.Purposes)
		if parseErr != nil {
			fail("invalid_request", http.StatusBadRequest)
			return
		}
		err = l.Manager.EnableTrial(ctx, purposes...)
	case "/api/local-model/trial/disable":
		purposes, parseErr := localTrialPurposes(input.Judgment, input.Execution, input.Purposes)
		if parseErr != nil {
			fail("invalid_request", http.StatusBadRequest)
			return
		}
		err = l.Manager.DisableTrial(ctx, purposes...)
	case "/api/local-model/check":
		var count int
		count, err = l.runCapabilityChecks(ctx)
		if count == 0 {
			fail("capability_acceptance_suites_not_provided", http.StatusConflict)
			return
		}
	}
	if err != nil {
		code, status := "local_runtime_operation_failed", http.StatusConflict
		for _, known := range []error{routerruntime.ErrDisabled, routerruntime.ErrNotInstalled, routerruntime.ErrQueueFull, routerruntime.ErrBusy, routerruntime.ErrFault} {
			if errors.Is(err, known) {
				code = known.Error()
				break
			}
		}
		var dependency *routerprocess.DependencyError
		if errors.As(err, &dependency) {
			code = dependency.Code
		}
		if errors.Is(err, context.DeadlineExceeded) {
			code, status = "local_runtime_timed_out", 504
		}
		if errors.Is(err, context.Canceled) {
			code, status = "local_runtime_canceled", 499
		}
		fail(code, status)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"schemaVersion": 1, "status": l.Manager.Status(), "models": aggregateLocalModelCandidates(r.Context(), nil, l)})
}

func localTrialPurposes(judgment, execution *bool, names []string) ([]routerruntime.Purpose, error) {
	if judgment == nil && execution == nil && len(names) == 0 {
		return []routerruntime.Purpose{routerruntime.Judgment, routerruntime.Execution}, nil
	}
	seen := map[routerruntime.Purpose]bool{}
	add := func(p routerruntime.Purpose, enabled bool) {
		if enabled {
			seen[p] = true
		}
	}
	if judgment != nil {
		add(routerruntime.Judgment, *judgment)
	}
	if execution != nil {
		add(routerruntime.Execution, *execution)
	}
	for _, name := range names {
		switch name {
		case string(routerruntime.Judgment):
			seen[routerruntime.Judgment] = true
		case string(routerruntime.Execution):
			seen[routerruntime.Execution] = true
		default:
			return nil, errors.New("invalid trial purpose")
		}
	}
	out := make([]routerruntime.Purpose, 0, len(seen))
	for _, p := range []routerruntime.Purpose{routerruntime.Judgment, routerruntime.Execution} {
		if seen[p] {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return nil, errors.New("trial purpose required")
	}
	return out, nil
}

//go:embed assets/local-model.js
var localModelScript []byte
