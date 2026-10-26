package routerservice

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/yetone/magpie/internal/routerconfig"
	"github.com/yetone/magpie/internal/routerredact"
	"github.com/yetone/magpie/internal/routerruntime"
)

// runtimeRedactor is the only production adapter between W4 and W5. It keeps
// the manager, the fixed privacy specification and all pipeline instances in
// one service-owned lifetime. A model selection change creates a new
// specification and clears the previous cache epoch; prepared request
// snapshots remain valid until their owner closes them.
type runtimeRedactor struct {
	mu                sync.Mutex
	local             *LocalModels
	active            *routerredact.Pipeline
	activeID          string
	activeRT          string
	activeFingerprint string
	activeSP          *routerredact.PrivacySpecification
	retired           []*routerredact.Pipeline
	closed            bool
	experimental      bool
}

func newRuntimeRedactor(local *LocalModels, experimental ...bool) *runtimeRedactor {
	return &runtimeRedactor{local: local, experimental: len(experimental) > 0 && experimental[0]}
}

func (r *runtimeRedactor) currentLocked() (*routerredact.Pipeline, *routerredact.PrivacySpecification, error) {
	if r == nil || r.local == nil || r.local.Manager == nil || r.closed {
		return nil, nil, errors.New("redaction_unavailable")
	}
	identity, settings := r.local.Manager.SnapshotBinding(context.Background())
	selected := identity.ModelID
	runtimeID := identity.RuntimeID
	if selected == "" || runtimeID == "" {
		return nil, nil, errors.New("redaction_unavailable")
	}
	if r.active != nil && r.activeSP != nil && r.activeID == selected && r.activeRT == runtimeID && r.activeFingerprint == identity.ModelFingerprint {
		return r.active, r.activeSP, nil
	}
	binding := routerredact.ModelBinding{ModelID: selected, RuntimeID: runtimeID, ContextTokens: settings.Effective().ContextTokens, ExecutionPlan: identity.ExecutionPlan}
	spec, err := routerredact.NewPrivacySpecification(binding)
	if r.experimental {
		spec, err = routerredact.NewExperimentalPrivacySpecification(binding)
	}
	if err != nil {
		return nil, nil, err
	}
	if spec.RuntimeFingerprint() != identity.ModelFingerprint {
		return nil, nil, errors.New("redaction_unavailable")
	}
	extractor := routerredact.NewRuntimeExtractor(r.local.Manager, spec)
	budget := spec.Budget()
	if r.experimental {
		invoker, trialErr := r.local.Manager.NewExperimentalRedactionInvoker(spec.RuntimeFingerprint(), spec.CapabilitySuite())
		if trialErr != nil {
			return nil, nil, errors.New("redaction_unavailable")
		}
		extractor = routerredact.NewExperimentalRuntimeExtractor(invoker, spec)
	}
	pipeline := routerredact.New(routerredact.Options{Extractor: extractor, Budget: budget, Experimental: r.experimental})
	if r.active != nil {
		// Clear the old cache immediately. Its existing snapshots are independent
		// and are retained until the request closes them.
		r.active.Clear()
		r.retired = append(r.retired, r.active)
	}
	r.active, r.activeSP, r.activeID, r.activeRT, r.activeFingerprint = pipeline, spec, selected, runtimeID, identity.ModelFingerprint
	return pipeline, spec, nil
}

func (r *runtimeRedactor) current() (*routerredact.Pipeline, *routerredact.PrivacySpecification, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.currentLocked()
}

func (r *runtimeRedactor) RoutingVersions() RoutingVersions {
	_, spec, err := r.current()
	if err != nil || spec == nil {
		return RoutingVersions{}
	}
	v := spec.RoutingVersions()
	return RoutingVersions{
		LocalRuntimeFingerprint:  v.LocalRuntimeFingerprint,
		LocalModelFingerprint:    v.LocalModelFingerprint,
		LocalTemplateFingerprint: v.LocalTemplateFingerprint,
		RedactionPromptVersion:   v.RedactionPromptVersion,
		RedactionSchemaVersion:   v.RedactionSchemaVersion,
		ConfigVersion:            v.ConfigVersion,
	}
}

func redactProtocol(value string) (routerredact.Protocol, error) {
	switch value {
	case "chat":
		return routerredact.Chat, nil
	case "responses":
		return routerredact.Responses, nil
	case "anthropic":
		return routerredact.Anthropic, nil
	default:
		return "", errors.New("redaction_protocol_unsupported")
	}
}

func (r *runtimeRedactor) Prepare(ctx context.Context, req RedactionRequest) (PreparedCloudRequest, error) {
	targetProtocol, err := redactProtocol(req.TargetProtocol)
	if err != nil {
		return PreparedCloudRequest{}, err
	}
	clientProtocol, err := redactProtocol(req.SourceProtocol)
	if err != nil {
		return PreparedCloudRequest{}, err
	}
	pipeline, spec, err := r.current()
	if err != nil {
		return PreparedCloudRequest{}, err
	}
	privacyVersions, err := spec.ResolveVersions(routerredact.PrivacyRoutingVersions{
		LocalRuntimeFingerprint:  req.Versions.LocalRuntimeFingerprint,
		LocalModelFingerprint:    req.Versions.LocalModelFingerprint,
		LocalTemplateFingerprint: req.Versions.LocalTemplateFingerprint,
		RedactionPromptVersion:   req.Versions.RedactionPromptVersion,
		RedactionSchemaVersion:   req.Versions.RedactionSchemaVersion,
		ConfigVersion:            req.Versions.ConfigVersion,
	})
	if err != nil {
		return PreparedCloudRequest{}, err
	}
	seedSessionID := req.SeedSessionID
	if !req.SessionVerified && req.SessionID == "" && seedSessionID == "" {
		_, _, seedSessionID = parseRedactionSessionMaterial(req.AppID, string(targetProtocol), req.Body)
	}
	prepared, err := pipeline.Prepare(ctx, routerredact.Input{
		RequestID: req.RequestID, AppID: req.AppID, Protocol: targetProtocol,
		Operation: req.Operation, SessionID: req.SessionID, SessionVerified: req.SessionVerified,
		SeedSessionID: seedSessionID,
		Versions:      privacyVersions, Body: append([]byte(nil), req.Body...),
	})
	if err != nil {
		return PreparedCloudRequest{}, err
	}
	body, err := prepared.Body()
	if err != nil {
		prepared.Close()
		return PreparedCloudRequest{}, err
	}
	var obsMeta *routerredact.ObservationMeta
	if m, metaErr := prepared.ObservationMeta(); metaErr == nil {
		obsMeta = &m
	}
	return PreparedCloudRequest{
		Body:            body,
		ObservationMeta: obsMeta,
		Restore: func(restoreCtx context.Context, protocol string, _ string, response []byte) ([]byte, error) {
			if err := restoreCtx.Err(); err != nil {
				return nil, err
			}
			actual, protocolErr := redactProtocol(protocol)
			if protocolErr != nil || actual != clientProtocol {
				return nil, errors.New("redaction_protocol_mismatch")
			}
			return prepared.Snapshot.RestoreResponse(actual, response)
		},
		RestoreSSE: func(restoreCtx context.Context, protocol string, input io.Reader, output io.Writer) error {
			actual, protocolErr := redactProtocol(protocol)
			if protocolErr != nil || actual != clientProtocol {
				return errors.New("redaction_protocol_mismatch")
			}
			return prepared.Snapshot.RestoreSSE(restoreCtx, actual, input, output)
		},
		Close: func() error {
			prepared.Close()
			return nil
		},
	}, nil
}

func (r *runtimeRedactor) Stats() routerredact.Stats {
	r.mu.Lock()
	pipelines := append([]*routerredact.Pipeline(nil), r.retired...)
	if r.active != nil {
		pipelines = append(pipelines, r.active)
	}
	r.mu.Unlock()
	var out routerredact.Stats
	out.Failures = map[string]uint64{}
	for _, pipeline := range pipelines {
		stats := pipeline.Stats()
		out.Processed += stats.Processed
		out.Entities += stats.Entities
		out.CacheHits += stats.CacheHits
		out.Blocked += stats.Blocked
		out.DurationMS += stats.DurationMS
		for code, count := range stats.Failures {
			out.Failures[routerredact.SafeFailureCode(code)] += count
		}
	}
	return out
}

func (r *runtimeRedactor) close() {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	r.closed = true
	pipelines := append([]*routerredact.Pipeline(nil), r.retired...)
	if r.active != nil {
		pipelines = append(pipelines, r.active)
	}
	r.active, r.activeSP, r.retired = nil, nil, nil
	r.mu.Unlock()
	for _, pipeline := range pipelines {
		pipeline.Close()
	}
}

// redactionRuntimeState is the management projection. It contains only
// aggregate state and no model output, mapping, credentials or error text.
type redactionRuntimeState struct {
	Integrated       bool
	Capability       routerconfig.RedactionCapability
	Reason           string
	ModelReady       bool
	RecognitionReady bool
	CloudActive      bool
	Statistics       bool
	Experimental     bool
	TrialReady       bool
	BudgetMS         int64
	TrialSuite       string
	TrialConfig      string
}

func (r *runtimeRedactor) state() redactionRuntimeState {
	if r == nil || r.local == nil || r.local.Manager == nil {
		return redactionRuntimeState{Capability: routerconfig.RedactionUnavailable, Reason: "runtime_unavailable"}
	}
	status := r.local.Manager.Status()
	budgetMS := r.local.Manager.RedactionBudget().Milliseconds()
	capability := status.Capabilities[routerruntime.Redaction]
	state := routerconfig.RedactionUnknown
	switch capability.State {
	case "available":
		state = routerconfig.RedactionAvailable
	case "unavailable":
		state = routerconfig.RedactionUnavailable
	}
	modelReady := status.Installation == "installed" && status.Dependencies.State != "missing" && status.Dependencies.State != "corrupt" && status.Dependencies.State != "failed"
	if r.experimental {
		_, spec, err := r.current()
		trialReady := false
		trialSuite, trialConfig := "", ""
		if err == nil && spec != nil && modelReady {
			invoker, err := r.local.Manager.NewExperimentalRedactionInvoker(spec.RuntimeFingerprint(), spec.CapabilitySuite())
			trialReady = err == nil && invoker.VerifyBinding(spec.RuntimeFingerprint(), spec.CapabilitySuite()) == nil
			trialSuite, trialConfig = spec.CapabilitySuite(), spec.Fingerprints().Config
		}
		// The projection identifies the selected policy even while disabled.
		// Writers independently require TrialReady before enabling new intent.
		return redactionRuntimeState{Integrated: true, Capability: routerconfig.RedactionExperimental, Reason: "experimental_quality_not_accepted", ModelReady: modelReady, RecognitionReady: false, CloudActive: trialReady, Statistics: true, Experimental: true, TrialReady: trialReady, BudgetMS: budgetMS, TrialSuite: trialSuite, TrialConfig: trialConfig}
	}
	// Manager.Ready binds the model/runtime fingerprint, but the recognition
	// suite is a second versioned material binding. Re-derive the current spec
	// here so management state, enablement and the production extractor agree.
	// An old suite must never be projected as currently accepted merely because
	// the selected model has not changed.
	_, currentSpec, specErr := r.current()
	formalAccepted := specErr == nil && currentSpec != nil &&
		state == routerconfig.RedactionAvailable && capability.Ready && modelReady &&
		capability.Fingerprint == currentSpec.RuntimeFingerprint() &&
		capability.Suite != "" && capability.Suite == currentSpec.CapabilitySuite()
	reason := "recognition_acceptance_pending"
	if formalAccepted {
		reason = "recognition_accepted"
	} else if state == routerconfig.RedactionAvailable {
		reason = "recognition_material_mismatch"
	}
	return redactionRuntimeState{
		Integrated: true, Capability: state, Reason: reason, ModelReady: modelReady,
		RecognitionReady: formalAccepted,
		CloudActive:      formalAccepted && status.Intent.Enabled, Statistics: true,
		BudgetMS: budgetMS,
	}
}

type groupLLMInvoker struct {
	invoker          LocalInvoker
	manager          *routerruntime.Manager
	experimental     *routerruntime.ExperimentalRedactionInvoker
	useExperimental  bool
	requiredModel    string
	boundModel       string
	boundFingerprint string
	boundSuite       string
}

func (g *groupLLMInvoker) modelMatches(liveModelID string) bool {
	if g.requiredModel != "" && g.requiredModel != "local-redactor" {
		if g.boundModel == "" || !strings.EqualFold(g.requiredModel, g.boundModel) {
			return false
		}
		if liveModelID != "" && !strings.EqualFold(g.requiredModel, liveModelID) {
			return false
		}
	}
	return true
}

func (g *groupLLMInvoker) checkExperimental(purpose routerruntime.Purpose) error {
	if purpose != routerruntime.Redaction {
		return errors.New("redaction_unavailable")
	}
	if g.experimental == nil || g.boundFingerprint == "" || g.boundSuite == "" {
		return errors.New("redaction_unavailable")
	}
	if !g.modelMatches("") {
		return errors.New("redaction_unavailable")
	}
	if err := g.experimental.VerifyBinding(g.boundFingerprint, g.boundSuite); err != nil {
		return err
	}
	return g.experimental.Require(purpose)
}

func (g *groupLLMInvoker) checkFormal(purpose routerruntime.Purpose) error {
	if g.manager == nil || g.boundFingerprint == "" || g.boundSuite == "" {
		return errors.New("redaction_unavailable")
	}
	if !g.modelMatches("") {
		return errors.New("redaction_unavailable")
	}
	if err := g.manager.Require(purpose); err != nil {
		return err
	}
	identity, _ := g.manager.SnapshotBinding(context.Background())
	if !g.modelMatches(identity.ModelID) {
		return errors.New("redaction_unavailable")
	}
	if identity.ModelFingerprint == "" || identity.ModelFingerprint != g.boundFingerprint {
		return errors.New("redaction_unavailable")
	}
	status := g.manager.Status()
	capState, ok := status.Capabilities[purpose]
	if !ok || capState.State != string(routerconfig.RedactionAvailable) || !capState.Ready || capState.Suite == "" || capState.Suite != g.boundSuite || capState.Fingerprint != g.boundFingerprint {
		return errors.New("redaction_unavailable")
	}
	return nil
}

func (g *groupLLMInvoker) Require(purpose routerruntime.Purpose) error {
	if g == nil {
		return errors.New("local_runtime_unavailable")
	}
	if g.invoker != nil {
		if req, ok := g.invoker.(interface {
			Require(routerruntime.Purpose) error
		}); ok {
			return req.Require(purpose)
		}
		return nil
	}
	if g.useExperimental {
		return g.checkExperimental(purpose)
	}
	if g.manager != nil {
		return g.checkFormal(purpose)
	}
	return errors.New("local_runtime_unavailable")
}

func (g *groupLLMInvoker) Invoke(ctx context.Context, purpose routerruntime.Purpose, body []byte, fn func(*http.Response) error) error {
	if g == nil {
		return errors.New("local_runtime_unavailable")
	}
	if g.invoker != nil {
		return g.invoker.Invoke(ctx, purpose, body, fn)
	}
	if g.useExperimental {
		if err := g.checkExperimental(purpose); err != nil {
			return err
		}
		return g.experimental.Invoke(ctx, routerruntime.Redaction, body, func(resp *http.Response) error {
			if err := g.checkExperimental(purpose); err != nil {
				return err
			}
			return fn(resp)
		})
	}
	if g.manager != nil {
		if err := g.checkFormal(purpose); err != nil {
			return err
		}
		return g.manager.Invoke(ctx, routerruntime.Redaction, body, func(resp *http.Response) error {
			if err := g.checkFormal(purpose); err != nil {
				return err
			}
			return fn(resp)
		})
	}
	return errors.New("local_runtime_unavailable")
}

type groupOrchestratorRedactor struct {
	mu              sync.Mutex
	invoker         LocalInvoker
	manager         *routerruntime.Manager
	modelID         string
	experimental    bool
	ephemeral       bool
	closed          bool
	active          *routerredact.Pipeline
	activeVersions  routerredact.Versions
	activeRouting   RoutingVersions
	activeModelHash string
	activeExpReady  bool
	inFlight        map[*routerredact.Pipeline]int
	retiring        []*routerredact.Pipeline
	retiredStats    routerredact.Stats
}

type groupRedactorCacheKey struct {
	store        *routerconfig.Store
	manager      *routerruntime.Manager
	modelID      string
	experimental bool
}

var groupOrchestratorCache sync.Map

func getGroupOrchestratorRedactor(options ModelOptions, localModel string) *groupOrchestratorRedactor {
	modelID := strings.TrimSpace(localModel)
	if modelID == "" {
		modelID = "local-redactor"
	}
	var invoker LocalInvoker
	if routing := options.routingOptions(); routing != nil {
		invoker = routing.LocalInvoker
	}
	var mgr *routerruntime.Manager
	if options.LocalRuntime != nil {
		mgr = options.LocalRuntime.Manager
	}
	if m, ok := invoker.(*routerruntime.Manager); ok {
		if mgr == nil {
			mgr = m
		}
		invoker = nil
	}
	experimental := options.ExperimentalRedaction
	if !experimental && options.Routing != nil {
		if rr, ok := options.Routing.Redactor.(*runtimeRedactor); ok && rr != nil {
			experimental = rr.experimental
		}
	}
	cacheable := options.store != nil && invoker == nil
	if cacheable {
		key := groupRedactorCacheKey{store: options.store, manager: mgr, modelID: modelID, experimental: experimental}
		if cached, ok := groupOrchestratorCache.Load(key); ok {
			if existing := cached.(*groupOrchestratorRedactor); !existing.isClosed() {
				_, _, _ = existing.current(context.Background())
				return existing
			}
			groupOrchestratorCache.CompareAndDelete(key, cached)
		}
		candidate := &groupOrchestratorRedactor{
			invoker:      invoker,
			manager:      mgr,
			modelID:      modelID,
			experimental: experimental,
			inFlight:     map[*routerredact.Pipeline]int{},
			retiredStats: routerredact.Stats{Failures: map[string]uint64{}},
		}
		for {
			actual, loaded := groupOrchestratorCache.LoadOrStore(key, candidate)
			winner := actual.(*groupOrchestratorRedactor)
			if loaded && winner.isClosed() {
				groupOrchestratorCache.CompareAndDelete(key, actual)
				continue
			}
			if loaded && winner != candidate {
				candidate.close()
			}
			_, _, _ = winner.current(context.Background())
			return winner
		}
	}
	return &groupOrchestratorRedactor{
		invoker:      invoker,
		manager:      mgr,
		modelID:      modelID,
		experimental: experimental,
		ephemeral:    true,
		inFlight:     map[*routerredact.Pipeline]int{},
		retiredStats: routerredact.Stats{Failures: map[string]uint64{}},
	}
}

type groupOrchestratorBinding struct {
	effectiveModel  string
	versions        routerredact.Versions
	routingVersions RoutingVersions
	budget          time.Duration
	boundModel      string
	boundFP         string
	boundSuite      string
	modelHash       string
	expInvoker      *routerruntime.ExperimentalRedactionInvoker
}

func (r *groupOrchestratorRedactor) computeBindingLocked(ctx context.Context) groupOrchestratorBinding {
	if ctx == nil {
		ctx = context.Background()
	}
	b := groupOrchestratorBinding{
		effectiveModel: r.modelID,
		budget:         30 * time.Second,
	}
	modelFP := r.modelID
	runtimeFP := "group-local-invoker"
	configVer := fmt.Sprintf("group-orchestrator-config-v2:exp=%t", r.experimental)
	if r.manager != nil {
		identity, settings := r.manager.SnapshotBinding(ctx)
		b.boundModel = identity.ModelID
		b.boundFP = identity.ModelFingerprint
		b.modelHash = identity.ModelFingerprint
		if identity.ModelID != "" {
			b.effectiveModel = identity.ModelID
		}
		if identity.ModelFingerprint != "" {
			modelFP = identity.ModelFingerprint
		}
		if identity.RuntimeFingerprint != "" {
			runtimeFP = identity.RuntimeFingerprint
		}
		binding := routerredact.ModelBinding{
			ModelID:       identity.ModelID,
			RuntimeID:     identity.RuntimeID,
			ContextTokens: settings.Effective().ContextTokens,
			ExecutionPlan: identity.ExecutionPlan,
		}
		if r.experimental {
			if spec, err := routerredact.NewExperimentalPrivacySpecification(binding); err == nil {
				b.boundSuite = spec.CapabilitySuite()
			}
			if b.boundFP != "" && b.boundSuite != "" && (r.modelID == "" || r.modelID == "local-redactor" || strings.EqualFold(identity.ModelID, r.modelID)) {
				b.expInvoker, _ = r.manager.NewExperimentalRedactionInvoker(b.boundFP, b.boundSuite)
			}
		} else {
			if spec, err := routerredact.NewPrivacySpecification(binding); err == nil {
				b.boundSuite = spec.CapabilitySuite()
			}
		}
		configVer = fmt.Sprintf("group-orchestrator-config-v2:exp=%t:ctx=%d:fp=%s", r.experimental, settings.Effective().ContextTokens, modelFP)
		if mgrBudget := r.manager.RedactionBudget(); mgrBudget > 0 {
			b.budget = mgrBudget
		}
	}
	b.versions = routerredact.Versions{
		Model:  modelFP,
		Prompt: "group-orchestrator-prompt-v2",
		Config: configVer,
	}
	b.routingVersions = RoutingVersions{
		LocalRuntimeFingerprint:  runtimeFP,
		LocalModelFingerprint:    modelFP,
		LocalTemplateFingerprint: "group-orchestrator-template-v2",
		RedactionPromptVersion:   "group-orchestrator-prompt-v2",
		RedactionSchemaVersion:   "group-orchestrator-schema-v2",
		ConfigVersion:            configVer,
	}
	return b
}

func (r *groupOrchestratorRedactor) current(ctx context.Context) (*routerredact.Pipeline, routerredact.Versions, RoutingVersions) {
	if r == nil {
		return nil, routerredact.Versions{}, RoutingVersions{}
	}
	r.mu.Lock()
	pipeline, versions, routing, toClose := r.currentLocked(ctx)
	r.mu.Unlock()
	if toClose != nil {
		toClose.Close()
	}
	return pipeline, versions, routing
}

func (r *groupOrchestratorRedactor) currentLocked(ctx context.Context) (*routerredact.Pipeline, routerredact.Versions, RoutingVersions, *routerredact.Pipeline) {
	if r.closed {
		return nil, routerredact.Versions{}, r.activeRouting, nil
	}
	b := r.computeBindingLocked(ctx)
	expReady := b.expInvoker != nil
	if r.active != nil && r.activeVersions == b.versions && r.activeModelHash == b.modelHash && r.activeExpReady == expReady {
		return r.active, r.activeVersions, r.activeRouting, nil
	}
	llm := routerredact.NewLocalLLMDetector(routerredact.LocalLLMDetectorOptions{
		Invoker: &groupLLMInvoker{
			invoker:          r.invoker,
			manager:          r.manager,
			experimental:     b.expInvoker,
			useExperimental:  r.experimental,
			requiredModel:    r.modelID,
			boundModel:       b.boundModel,
			boundFingerprint: b.boundFP,
			boundSuite:       b.boundSuite,
		},
		ModelID: b.effectiveModel,
	})
	orch := routerredact.DefaultOrchestrator(llm).WithVersions(b.versions)
	pipeline := routerredact.New(routerredact.Options{
		Extractor:    orch,
		Budget:       b.budget,
		Experimental: r.experimental,
	})
	var toClose *routerredact.Pipeline
	if old := r.active; old != nil {
		old.Clear()
		if r.inFlight[old] > 0 {
			r.retiring = append(r.retiring, old)
		} else {
			mergeRedactStats(&r.retiredStats, old.Stats())
			toClose = old
		}
	}
	r.active = pipeline
	r.activeVersions = b.versions
	r.activeRouting = b.routingVersions
	r.activeModelHash = b.modelHash
	r.activeExpReady = expReady
	return r.active, r.activeVersions, r.activeRouting, toClose
}

func (r *groupOrchestratorRedactor) acquirePipeline(ctx context.Context) (*routerredact.Pipeline, routerredact.Versions, error) {
	if r == nil {
		return nil, routerredact.Versions{}, ErrRedactionUnavailable
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil, routerredact.Versions{}, ErrRedactionUnavailable
	}
	if r.inFlight == nil {
		r.inFlight = map[*routerredact.Pipeline]int{}
	}
	pipeline, versions, _, toClose := r.currentLocked(ctx)
	if pipeline != nil {
		r.inFlight[pipeline]++
	}
	r.mu.Unlock()
	if toClose != nil {
		toClose.Close()
	}
	if pipeline == nil {
		return nil, routerredact.Versions{}, ErrRedactionUnavailable
	}
	return pipeline, versions, nil
}

func (r *groupOrchestratorRedactor) releasePipeline(pipeline *routerredact.Pipeline, closeIfEphemeral bool) {
	if r == nil || pipeline == nil {
		return
	}
	var toClose *routerredact.Pipeline
	r.mu.Lock()
	if r.inFlight != nil {
		if count := r.inFlight[pipeline]; count <= 1 {
			delete(r.inFlight, pipeline)
		} else {
			r.inFlight[pipeline] = count - 1
		}
	}
	if r.inFlight[pipeline] == 0 {
		if r.active == pipeline {
			if closeIfEphemeral || r.closed {
				mergeRedactStats(&r.retiredStats, pipeline.Stats())
				r.active = nil
				toClose = pipeline
			}
		} else {
			for i, ret := range r.retiring {
				if ret == pipeline {
					r.retiring = append(r.retiring[:i], r.retiring[i+1:]...)
					mergeRedactStats(&r.retiredStats, pipeline.Stats())
					toClose = pipeline
					break
				}
			}
		}
	}
	r.mu.Unlock()
	if toClose != nil {
		toClose.Close()
	}
}

func (r *groupOrchestratorRedactor) closeIdleActive() {
	if r == nil {
		return
	}
	var toClose *routerredact.Pipeline
	r.mu.Lock()
	if r.active != nil && r.inFlight[r.active] == 0 {
		mergeRedactStats(&r.retiredStats, r.active.Stats())
		toClose = r.active
		r.active = nil
	}
	r.mu.Unlock()
	if toClose != nil {
		toClose.Close()
	}
}

func (r *groupOrchestratorRedactor) isClosed() bool {
	if r == nil {
		return true
	}
	r.mu.Lock()
	closed := r.closed
	r.mu.Unlock()
	return closed
}

func (r *groupOrchestratorRedactor) close() {
	if r == nil {
		return
	}
	var toClose []*routerredact.Pipeline
	r.mu.Lock()
	r.closed = true
	if r.active != nil {
		mergeRedactStats(&r.retiredStats, r.active.Stats())
		toClose = append(toClose, r.active)
		r.active = nil
	}
	for _, ret := range r.retiring {
		if ret != nil {
			mergeRedactStats(&r.retiredStats, ret.Stats())
			toClose = append(toClose, ret)
		}
	}
	r.retiring = nil
	r.inFlight = nil
	r.manager = nil
	r.invoker = nil
	r.mu.Unlock()
	for _, p := range toClose {
		p.Close()
	}
}

func (r *groupOrchestratorRedactor) Close() error {
	r.close()
	return nil
}

func closeEphemeralGroupRedactor(options ModelOptions) {
	if options.Routing == nil || options.Routing.Redactor == nil {
		return
	}
	glr, ok := options.Routing.Redactor.(*groupLocalRedactor)
	if !ok || glr == nil {
		return
	}
	gor, ok := glr.Redactor.(*groupOrchestratorRedactor)
	if !ok || gor == nil || !gor.ephemeral {
		return
	}
	gor.closeIdleActive()
}

func closeServiceGroupRedactors(store *routerconfig.Store, local *LocalModels) {
	var mgr *routerruntime.Manager
	if local != nil {
		mgr = local.Manager
	}
	closeGroupOrchestratorRedactors(store, mgr)
}

func closeGroupOrchestratorRedactors(store *routerconfig.Store, manager *routerruntime.Manager) {
	groupOrchestratorCache.Range(func(k, v any) bool {
		key, ok := k.(groupRedactorCacheKey)
		if !ok {
			return true
		}
		match := (store == nil && manager == nil) ||
			(store != nil && key.store == store) ||
			(manager != nil && key.manager == manager)
		if !match {
			return true
		}
		if groupOrchestratorCache.CompareAndDelete(k, v) {
			if redactor, ok := v.(*groupOrchestratorRedactor); ok && redactor != nil {
				redactor.close()
			}
		}
		return true
	})
}

func mergeRedactStats(dst *routerredact.Stats, src routerredact.Stats) {
	if dst.Failures == nil {
		dst.Failures = map[string]uint64{}
	}
	dst.Processed += src.Processed
	dst.Entities += src.Entities
	dst.CacheHits += src.CacheHits
	dst.Blocked += src.Blocked
	dst.DurationMS += src.DurationMS
	for code, count := range src.Failures {
		dst.Failures[routerredact.SafeFailureCode(code)] += count
	}
}

func (r *groupOrchestratorRedactor) RoutingVersions() RoutingVersions {
	if r == nil {
		return RoutingVersions{}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return r.activeRouting
	}
	return r.computeBindingLocked(context.Background()).routingVersions
}

func (r *groupOrchestratorRedactor) Stats() routerredact.Stats {
	if r == nil {
		return routerredact.Stats{Failures: map[string]uint64{}}
	}
	var out routerredact.Stats
	out.Failures = map[string]uint64{}
	r.mu.Lock()
	mergeRedactStats(&out, r.retiredStats)
	pipelines := append([]*routerredact.Pipeline(nil), r.retiring...)
	if r.active != nil {
		pipelines = append(pipelines, r.active)
	}
	r.mu.Unlock()
	for _, pipeline := range pipelines {
		mergeRedactStats(&out, pipeline.Stats())
	}
	return out
}

func (r *groupOrchestratorRedactor) Prepare(ctx context.Context, req RedactionRequest) (PreparedCloudRequest, error) {
	targetProtocol, err := redactProtocol(req.TargetProtocol)
	if err != nil {
		return PreparedCloudRequest{}, err
	}
	clientProtocol, err := redactProtocol(req.SourceProtocol)
	if err != nil {
		return PreparedCloudRequest{}, err
	}
	appID := req.AppID
	if appID == "" {
		appID = "default"
	}
	op := req.Operation
	if op == "" {
		op = "generation"
	}
	seedSessionID := req.SeedSessionID
	if !req.SessionVerified && req.SessionID == "" && seedSessionID == "" {
		_, _, seedSessionID = parseRedactionSessionMaterial(appID, string(targetProtocol), req.Body)
	}
	pipeline, versions, err := r.acquirePipeline(ctx)
	if err != nil {
		return PreparedCloudRequest{}, err
	}
	prepared, err := pipeline.Prepare(ctx, routerredact.Input{
		RequestID:       req.RequestID,
		AppID:           appID,
		Protocol:        targetProtocol,
		Operation:       op,
		SessionID:       req.SessionID,
		SessionVerified: req.SessionVerified,
		SeedSessionID:   seedSessionID,
		Versions:        versions,
		Body:            append([]byte(nil), req.Body...),
	})
	if err != nil {
		r.releasePipeline(pipeline, r.ephemeral)
		return PreparedCloudRequest{}, err
	}
	body, err := prepared.Body()
	if err != nil {
		prepared.Close()
		r.releasePipeline(pipeline, r.ephemeral)
		return PreparedCloudRequest{}, err
	}
	var obsMeta *routerredact.ObservationMeta
	if m, metaErr := prepared.ObservationMeta(); metaErr == nil {
		obsMeta = &m
	}
	var closeOnce sync.Once
	return PreparedCloudRequest{
		Body:            body,
		ObservationMeta: obsMeta,
		Restore: func(restoreCtx context.Context, protocol string, _ string, response []byte) ([]byte, error) {
			if err := restoreCtx.Err(); err != nil {
				return nil, err
			}
			actual, protocolErr := redactProtocol(protocol)
			if protocolErr != nil || actual != clientProtocol {
				return nil, errors.New("redaction_protocol_mismatch")
			}
			return prepared.Snapshot.RestoreResponse(actual, response)
		},
		RestoreSSE: func(restoreCtx context.Context, protocol string, input io.Reader, output io.Writer) error {
			actual, protocolErr := redactProtocol(protocol)
			if protocolErr != nil || actual != clientProtocol {
				return errors.New("redaction_protocol_mismatch")
			}
			return prepared.Snapshot.RestoreSSE(restoreCtx, actual, input, output)
		},
		Close: func() error {
			closeOnce.Do(func() {
				prepared.Close()
				r.releasePipeline(pipeline, r.ephemeral)
			})
			return nil
		},
	}, nil
}

type groupLocalRedactor struct {
	Redactor
}

func (g *groupLocalRedactor) RoutingVersions() RoutingVersions {
	if g == nil || g.Redactor == nil {
		return RoutingVersions{}
	}
	if rv, ok := g.Redactor.(interface{ RoutingVersions() RoutingVersions }); ok {
		return rv.RoutingVersions()
	}
	return RoutingVersions{}
}

func (g *groupLocalRedactor) Stats() routerredact.Stats {
	if g == nil || g.Redactor == nil {
		return routerredact.Stats{Failures: map[string]uint64{}}
	}
	if st, ok := g.Redactor.(interface{ Stats() routerredact.Stats }); ok {
		return st.Stats()
	}
	return routerredact.Stats{Failures: map[string]uint64{}}
}

func isGroupLocalRedaction(options ModelOptions) bool {
	if options.Routing == nil || options.Routing.Redactor == nil {
		return false
	}
	_, ok := options.Routing.Redactor.(*groupLocalRedactor)
	return ok
}

func withGroupLocalRedaction(options ModelOptions, localModel string) ModelOptions {
	var r RoutingOptions
	if base := options.routingOptions(); base != nil {
		r = *base
	}
	inner := r.Redactor
	if _, isRuntime := inner.(*runtimeRedactor); inner == nil || isRuntime {
		inner = getGroupOrchestratorRedactor(options, localModel)
	}
	r.Redactor = &groupLocalRedactor{Redactor: inner}
	options.Routing = &r
	return options
}
