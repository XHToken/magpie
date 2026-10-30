package routerservice

import (
	"github.com/yetone/magpie/internal/routerconfig"
	"github.com/yetone/magpie/internal/routerredact"
	"github.com/yetone/magpie/internal/routerruntime"
)

type redactionRuntimeState struct {
	ModelID, EngineVersion string
	Integrated             bool
	Capability             routerconfig.RedactionCapability
	Reason                 string
	ModelReady             bool
	RecognitionReady       bool
	CloudActive            bool
	Statistics             bool
	Experimental           bool
	TrialReady             bool
	BudgetMS               int64
	TrialSuite             string
	TrialConfig            string
}

func (r *RedactionService) state() redactionRuntimeState {
	if r == nil || r.local == nil || r.local.Manager == nil {
		return redactionRuntimeState{Capability: routerconfig.RedactionUnavailable, Reason: "runtime_unavailable"}
	}
	if r.resultsError != nil || r.results == nil {
		reason := "redaction_store_unavailable"
		if r.resultsError != nil {
			reason = routerredact.SafeFailureCode(r.resultsError.Error())
		}
		return redactionRuntimeState{Integrated: true, Statistics: true, Capability: routerconfig.RedactionUnavailable, Reason: reason}
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
		return redactionRuntimeState{Integrated: true, ModelID: status.Intent.Selected, EngineVersion: routerredact.EngineVersion, Capability: routerconfig.RedactionExperimental, Reason: "experimental_quality_not_accepted", ModelReady: modelReady, RecognitionReady: false, CloudActive: trialReady, Statistics: true, Experimental: true, TrialReady: trialReady, BudgetMS: budgetMS, TrialSuite: trialSuite, TrialConfig: trialConfig}
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
		Integrated: true, ModelID: status.Intent.Selected, EngineVersion: routerredact.EngineVersion, Capability: state, Reason: reason, ModelReady: modelReady,
		RecognitionReady: formalAccepted,
		CloudActive:      formalAccepted && status.Intent.Enabled, Statistics: true,
		BudgetMS: budgetMS,
	}
}
