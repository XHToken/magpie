package routerservice

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/routermanifest"
	"github.com/yetone/magpie/internal/routerprocess"
	"github.com/yetone/magpie/internal/routerrequest"
	"github.com/yetone/magpie/internal/routerruntime"
)

type observationIntentStore struct {
	mu     sync.Mutex
	intent routerruntime.Intent
}

func (s *observationIntentStore) Load(context.Context) (routerruntime.Intent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.intent, nil
}

func (s *observationIntentStore) Save(_ context.Context, value routerruntime.Intent) error {
	s.mu.Lock()
	s.intent = value
	s.mu.Unlock()
	return nil
}

type observationBackendInstaller struct{}

func (observationBackendInstaller) Verify(_ context.Context, _ routermanifest.Model, rt routermanifest.Runtime) (routerprocess.Spec, error) {
	return routerprocess.Spec{Backend: rt.Backend}, nil
}

func (i observationBackendInstaller) Install(ctx context.Context, model routermanifest.Model, rt routermanifest.Runtime, _ func(routerruntime.Progress)) (routerprocess.Spec, error) {
	return i.Verify(ctx, model, rt)
}

type observationBackendProcess struct {
	*integrationProcess
	device string
}

func (p *observationBackendProcess) Acceleration() routerprocess.AccelerationEvidence {
	return routerprocess.AccelerationEvidence{Device: p.device, Offloaded: 20, Total: 30, DeviceBuffer: true, Verified: p.device != ""}
}

type observationBackendFactory struct{ endpoint string }

func (f observationBackendFactory) Start(_ context.Context, spec routerprocess.Spec) (routerprocess.Process, error) {
	device := ""
	if spec.GPU != nil {
		device = spec.GPU.Device
	}
	return &observationBackendProcess{integrationProcess: &integrationProcess{endpoint: f.endpoint, done: make(chan struct{})}, device: device}, nil
}

func (observationBackendFactory) ProbeDevices(context.Context, routerprocess.Spec) ([]routerprocess.Device, error) {
	return []routerprocess.Device{{ID: "CUDA0", Name: "fixture GPU", TotalBytes: 8 << 30, FreeBytes: 6 << 30}}, nil
}

func (observationBackendFactory) ValidateAcceleration(context.Context, routerprocess.Process) error {
	return nil
}

// Accept CPU evidence, evict its child, then expose an installed GPU candidate.
// The next business call passes preflight but the newly loaded plan requires a
// fresh capability check. Its observation must identify that admitted GPU plan.
func observationColdBackendManager(t *testing.T, purpose routerruntime.Purpose) *routerruntime.Manager {
	t.Helper()
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"local"}}]}`)
	}))
	t.Cleanup(local.Close)
	var gpu atomic.Bool
	var elapsed atomic.Int64
	initial := time.Now()
	manager, err := routerruntime.New(context.Background(), routerruntime.Options{
		Catalog: routermanifest.Fixed(), RuntimeID: "windows-amd64-cpu", AutoBackend: true,
		Store:     &observationIntentStore{intent: routerruntime.Intent{Enabled: true, Selected: localModelID}},
		Installer: observationBackendInstaller{}, Factory: observationBackendFactory{endpoint: local.URL},
		IdleTimeout: time.Minute, Now: func() time.Time { return initial.Add(time.Duration(elapsed.Load())) },
		BackendHints: func(context.Context) []string {
			if gpu.Load() {
				return []string{"cuda"}
			}
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := manager.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	if err := manager.CheckCapability(context.Background(), routerruntime.Check{Purpose: purpose, Suite: "binding-fixture-v1", Run: func(context.Context, string) (string, error) { return "fixture-evidence", nil }}); err != nil {
		t.Fatal(err)
	}
	elapsed.Store(int64(2 * time.Minute))
	if err := manager.Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := manager.Require(purpose); err != nil {
		t.Fatal("CPU evidence did not survive expected idle eviction", err)
	}
	gpu.Store(true)
	return manager
}

func assertObservedGPUAdmission(t *testing.T, manager *routerruntime.Manager, events []routerruntime.LocalCallObservation) {
	t.Helper()
	actual := manager.Identity()
	if actual.RuntimeID != "windows-amd64-cuda12" || len(events) != 1 {
		t.Fatal("fixture did not select GPU and emit exactly once", actual, events)
	}
	event := events[0]
	if event.RuntimeID != actual.RuntimeID || event.RuntimeFingerprint != actual.RuntimeFingerprint || event.ModelFingerprint != actual.ModelFingerprint || event.Attempted || event.ErrorCode != routerruntime.ErrCapability.Error() {
		t.Fatal("business observation retained the pre-load CPU binding", event, actual)
	}
}

func TestBusinessLocalObservationBindsColdAutomaticBackend(t *testing.T) {
	for _, route := range []string{"routed", "group"} {
		t.Run(route, func(t *testing.T) {
			manager := observationColdBackendManager(t, routerruntime.Execution)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, cloudHTTPReply("chat", false))
			}))
			defer upstream.Close()
			var events []routerruntime.LocalCallObservation
			options := ModelOptions{LocalRuntime: &LocalModels{Manager: manager},
				Routing:       &RoutingOptions{Judge: &scriptedJudge{decision: DecisionLocal}, LocalInvoker: manager},
				LocalObserver: routerruntime.LocalCallObserverFunc(func(event routerruntime.LocalCallObservation) { events = append(events, event) })}
			body := []byte(`{"model":"profile-pinned","messages":[{"role":"user","content":"hello"}]}`)
			prepared, cancel, _ := routedRequest(t, context.Background(), upstream.URL, false, body)
			defer cancel()
			w := httptest.NewRecorder()
			if route == "group" {
				if _, err := executeLocalCandidate(w, prepared, "chat", "/v1/chat/completions", body, localModelID, options, modelWriterForTest(w)); err == nil {
					t.Fatal("GPU plan inherited CPU capability")
				}
			} else if result, err := executeRoutedModel(w, prepared, "chat", body, options, nil, modelWriterForTest(w)); err != nil || result.Destination != "cloud" {
				t.Fatal("pre-output capability failure did not fall back", result, err)
			}
			assertObservedGPUAdmission(t, manager, events)
		})
	}
}

func TestRuntimeJudgeObservationBindsColdAutomaticBackend(t *testing.T) {
	manager := observationColdBackendManager(t, routerruntime.Judgment)
	var events []routerruntime.LocalCallObservation
	ctx := withLocalCallContext(context.Background(), localCallContext{RequestID: "binding-request", Mode: routerruntime.LocalCallFormal,
		Observer: routerruntime.LocalCallObserverFunc(func(event routerruntime.LocalCallObservation) { events = append(events, event) })})
	_, err := (RuntimeJudge{Manager: manager}).Judge(ctx, routerrequest.JudgmentView{History: []routerrequest.JudgmentMessage{{Role: "user", Text: "hello"}}})
	if !errors.Is(err, routerruntime.ErrCapability) {
		t.Fatal("GPU judge inherited CPU capability", err)
	}
	assertObservedGPUAdmission(t, manager, events)
}

func TestLocalObservationUsesSlotBindingAfterModelChange(t *testing.T) {
	manager := observationColdBackendManager(t, routerruntime.Execution)
	before, settings := manager.SnapshotBinding(context.Background())
	diagnostics := &routerruntime.GenerationDiagnostics{}
	ctx := routerruntime.WithGenerationDiagnostics(context.Background(), diagnostics)
	// The trusted diagnostic transport can evaluate a newly selected backend
	// before formal business capability evidence has been registered for it.
	if err := manager.Generate(ctx, routerruntime.Execution, []byte(`{"messages":[]}`), func(response *http.Response) error {
		_, err := io.ReadAll(response.Body)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	actual := manager.Identity()
	if before.RuntimeID == actual.RuntimeID || !diagnostics.Snapshot().Attempted {
		t.Fatal("fixture did not execute on the replacement GPU", before, actual)
	}
	if err := manager.Disable(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := manager.Select(context.Background(), "spark-x2.5-4B"); err != nil {
		t.Fatal(err)
	}
	var events []routerruntime.LocalCallObservation
	meta := localCallContext{Binding: before, BindingCaptured: true, Settings: settings,
		Observer: routerruntime.LocalCallObserverFunc(func(event routerruntime.LocalCallObservation) { events = append(events, event) })}
	emitObservedLocalCall(meta, manager, routerruntime.Execution, time.Now(), diagnostics.Snapshot(), nil, nil)
	if len(events) != 1 || events[0].ModelFingerprint != actual.ModelFingerprint || events[0].RuntimeFingerprint != actual.RuntimeFingerprint || events[0].ModelID != actual.ModelID {
		t.Fatal("completion used entry-time or replacement-model identity instead of the captured GPU slot", events, actual, manager.Identity())
	}
	// A rejected call never acquired a slot. It retains its original admission
	// identity rather than borrowing the now-selected model's identity.
	emitObservedLocalCall(meta, manager, routerruntime.Execution, time.Now(), routerruntime.GenerationDiagnosticSnapshot{}, nil, routerruntime.ErrCapability)
	if events[1].ModelFingerprint != before.ModelFingerprint || events[1].RuntimeID != before.RuntimeID || events[1].Attempted {
		t.Fatal("unadmitted call borrowed another runtime's identity", events[1], before)
	}
}
