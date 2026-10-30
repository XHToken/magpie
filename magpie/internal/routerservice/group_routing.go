package routerservice

import (
	"context"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/yetone/magpie/internal/cloudcodec"
	"github.com/yetone/magpie/internal/routercloud"
	"github.com/yetone/magpie/internal/routerconfig"
	"github.com/yetone/magpie/internal/routerledger"
	"github.com/yetone/magpie/internal/routerrequest"
	"github.com/yetone/magpie/internal/routerruntime"
)

var (
	groupWeightSeq atomic.Uint64
	patienceUnit   = time.Second
)

func resolveBoundTarget(selectorModel string, target routerconfig.AppTarget, options ModelOptions) (string, bool) {
	req, p := strings.TrimSpace(selectorModel), target.Profile
	if options.store == nil || len(p.Models) == 0 {
		return "", false
	}
	if req == "" {
		req = p.Models[0]
	}
	req = hostRoutingModel(&p, req)
	reqBare := strings.TrimPrefix(req, routerconfig.GroupPrefix)
	matched := ""
	for _, m := range p.Models {
		gid, isGrp := routerconfig.GroupIDOf(m)
		if m == req {
			matched = m
			break
		}
		if matched == "" && ((isGrp && (reqBare == gid || routerconfig.GroupSlug(reqBare) == gid)) || (!isGrp && strings.HasSuffix(m, "/"+req))) {
			matched = m
		}
	}
	if matched == "" && req != "" {
		if cfg, err := options.store.LoadConfigContext(context.Background()); err == nil {
			groups := effectiveGroups(cfg, options.LocalRuntime)
			reqSlug := routerconfig.GroupSlug(reqBare)
			for _, m := range p.Models {
				gid, isGrp := routerconfig.GroupIDOf(m)
				if !isGrp {
					continue
				}
				if g, ok := findRuntimeGroup(cfg.Groups, groups, gid); ok && !g.Hidden {
					if (g.Family != "" && (strings.EqualFold(g.Family, reqBare) || routerconfig.GroupSlug(g.Family) == reqSlug)) ||
						(g.Name != "" && (strings.EqualFold(g.Name, reqBare) || routerconfig.GroupSlug(g.Name) == reqSlug)) {
						matched = m
						break
					}
				}
			}
		}
	}
	def := p.Cloud.ProviderID + "/" + p.Cloud.Model
	return matched, matched != "" && (strings.HasPrefix(matched, routerconfig.GroupPrefix) || strings.HasPrefix(matched, "local/") || target.Provider.Config.ID == "" || p.Cloud.ProviderID == "" || matched != def)
}

type groupExecOptions struct {
	group       *routerconfig.Group
	matchedRule *routerconfig.Rule
	affinityKey string
}

func executeBoundTarget(w http.ResponseWriter, prepared *http.Request, protocol, sub string, body []byte, targetRef string, options ModelOptions, emit func(routercloud.Event), writer *modelWriter) (routercloud.Result, error) {
	groupID, isGroup := routerconfig.GroupIDOf(targetRef)
	if !isGroup {
		return dispatchCandidates(w, prepared, protocol, sub, body, []string{targetRef}, options, emit, writer)
	}
	op := operationFor(sub)
	cfg, err := options.store.LoadConfigContext(prepared.Context())
	if err != nil {
		return routercloud.Result{Operation: op}, err
	}
	groups := groupExpansionCatalog(cfg, options.LocalRuntime)
	g, found := findRuntimeGroup(cfg.Groups, groups, groupID)
	if !found || g.Hidden {
		return routercloud.Result{Operation: op}, &routercloud.Error{Code: "target_missing", Message: "configured routing group does not exist", Status: http.StatusConflict}
	}
	if g.Disabled {
		return routercloud.Result{Operation: op}, &routercloud.Error{Code: "group_disabled", Message: "routing group is disabled", Status: http.StatusConflict}
	}
	expansion, expandErr := routerconfig.ResolveActiveGroupExpansion(g, groups)
	if expandErr != nil {
		return routercloud.Result{Operation: op}, privacyPolicyError("redaction_policy_invalid")
	}
	baseMembers := expansion.Members
	state, _ := routerrequest.FromContext(prepared.Context())
	if len(baseMembers) == 0 {
		plan, policyErr := groupPrivacyPlan(redactionRequired(prepared.Context(), state.Target.Profile.RedactionEnabled), expansion.PrivacySources)
		prepared = installRedactionPlan(prepared, plan)
		if policyErr != nil {
			return routercloud.Result{Operation: op}, policyErr
		}
		return routercloud.Result{Operation: op}, &routercloud.Error{Code: "target_missing", Message: "routing group has no active members", Status: http.StatusConflict}
	}
	ruleMembers, matchedRule := evaluateGroupRules(g, groups, protocol, body, baseMembers)
	ruleHasTarget := matchedRule != nil && (strings.TrimSpace(matchedRule.Use) != "" || strings.TrimSpace(matchedRule.Target) != "")
	if ruleHasTarget {
		ruleTarget := strings.TrimSpace(matchedRule.Use)
		if ruleTarget == "" {
			ruleTarget = strings.TrimSpace(matchedRule.Target)
		}
		if id, isGroup := routerconfig.GroupIDOf(ruleTarget); isGroup {
			subGroup, exists := findRuntimeGroup(cfg.Groups, groups, id)
			if !exists {
				return routercloud.Result{Operation: op}, privacyPolicyError("redaction_policy_invalid")
			}
			if !subGroup.Disabled && !subGroup.Hidden {
				subGroup.Off = append(append([]string(nil), subGroup.Off...), g.Off...)
				ruleExpansion, err := routerconfig.ResolveActiveGroupExpansion(subGroup, groups)
				if err != nil {
					return routercloud.Result{Operation: op}, privacyPolicyError("redaction_policy_invalid")
				}
				expansion.PrivacySources = append(expansion.PrivacySources, ruleExpansion.PrivacySources...)
			}
		}
	}
	plan, planErr := groupPrivacyPlan(redactionRequired(prepared.Context(), state.Target.Profile.RedactionEnabled), expansion.PrivacySources)
	plan.RedactLocal = g.LocalRedaction
	prepared = installRedactionPlan(prepared, plan)
	if planErr != nil {
		return routercloud.Result{Operation: op}, planErr
	}

	strategy := strings.ToLower(strings.TrimSpace(g.Routing))
	gOpts := &groupExecOptions{group: &g, matchedRule: matchedRule}
	if plan.RequiredModelID != "" {
		if g.LocalRedaction && !ruleHasTarget {
			var clouds []string
			for _, m := range ruleMembers {
				if !strings.HasPrefix(bareMember(m), "local/") {
					clouds = append(clouds, m)
				}
			}
			if len(clouds) > 0 {
				ruleMembers = clouds
			}
		}
	}

	if !ruleHasTarget && isSmartSplitStrategy(strategy) {
		return executeSmartSplitGroupWithOpts(w, prepared, protocol, sub, body, ruleMembers, options, emit, writer, gOpts)
	}
	candidates := orderGroupCandidates(g, ruleMembers, ruleHasTarget)
	if !ruleHasTarget || strings.EqualFold(g.Affinity, routerconfig.AffinityPin) || strings.EqualFold(g.Affinity, routerconfig.AffinityStrict) {
		candidates, gOpts.affinityKey, _ = applyGroupAffinity(g, prepared, protocol, body, candidates)
	} else {
		gOpts.affinityKey, _ = deriveGroupAffinityKey(g.ID, prepared, protocol, body)
	}
	return dispatchGroupWithPatience(w, prepared, protocol, sub, body, candidates, ruleMembers, ruleHasTarget, options, emit, writer, gOpts)
}

func isSmartSplitStrategy(strategy string) bool {
	switch strategy {
	case "", "smart_split", "smart_hybrid", "smart", "auto", "edge-cloud", "edge_cloud":
		return true
	default:
		return false
	}
}

func orderGroupCandidates(g routerconfig.Group, members []string, ruleHasTarget bool) []string {
	if len(members) == 0 {
		return nil
	}
	if ruleHasTarget && len(members) > 1 {
		rest := orderStrategyMembers(g, members[1:])
		return append([]string{members[0]}, rest...)
	}
	return orderStrategyMembers(g, members)
}

func orderStrategyMembers(g routerconfig.Group, members []string) []string {
	if len(members) == 0 {
		return nil
	}
	switch strings.ToLower(strings.TrimSpace(g.Routing)) {
	case "manual":
		chosen := members[0]
		if pick := strings.TrimSpace(g.Pick); pick != "" {
			pickBare := bareMember(pick)
			if idx := slices.IndexFunc(members, func(m string) bool { return m == pick || bareMember(m) == pickBare }); idx >= 0 {
				chosen = members[idx]
			}
		}
		return []string{chosen}
	case "weight", "weighted":
		return applySinkOrder(orderWeightedMembers(members, g.Weights), g.Sink)
	case "rotate", "round_robin", "round-robin":
		return orderRotateMembers(g.ID, members, g.Sink)
	case "usage", "least", "least_used", "least-used":
		return orderLeastUsedMembers(members, g.Sink)
	case "pace", "drain_reset", "drain-reset":
		return orderPaceMembers(members, g.Weights, g.Sink)
	default:
		return applySinkOrder(members, g.Sink)
	}
}

func findRuntimeGroup(saved, effective []routerconfig.Group, id string) (routerconfig.Group, bool) {
	match := func(g routerconfig.Group) bool {
		return strings.EqualFold(g.ID, id) || routerconfig.GroupSlug(g.ID) == routerconfig.GroupSlug(id)
	}
	if i := slices.IndexFunc(saved, func(g routerconfig.Group) bool { return g.Hidden && match(g) }); i >= 0 {
		return saved[i], true
	}
	if i := slices.IndexFunc(effective, match); i >= 0 {
		return effective[i], true
	}
	return routerconfig.Group{}, false
}

func expandActiveGroupMembers(g routerconfig.Group, all []routerconfig.Group, _ []byte) []string {
	return routerconfig.ExpandActiveGroupMembers(g, all)
}

func orderWeightedMembers(members []string, weights map[string]int) []string {
	if len(members) <= 1 {
		return members
	}
	w, div, total := make([]int, len(members)), 0, 0
	for i, m := range members {
		w[i] = lookupMemberWeight(weights, m)
		for a, b := div, w[i]; b != 0; div = a {
			a, b = b, a%b
		}
	}
	for i := range w {
		w[i] /= max(div, 1)
		total += w[i]
	}
	pick, idx := int(groupWeightSeq.Add(1)-1)%total, 0
	for i, weight := range w {
		if pick < weight {
			idx = i
			break
		}
		pick -= weight
	}
	return append(append([]string{members[idx]}, members[idx+1:]...), members[:idx]...)
}

func executeSmartSplitGroup(w http.ResponseWriter, prepared *http.Request, protocol, sub string, body []byte, members []string, options ModelOptions, emit func(routercloud.Event), writer *modelWriter) (routercloud.Result, error) {
	return executeSmartSplitGroupWithOpts(w, prepared, protocol, sub, body, members, options, emit, writer, nil)
}

func executeSmartSplitGroupWithOpts(w http.ResponseWriter, prepared *http.Request, protocol, sub string, body []byte, members []string, options ModelOptions, emit func(routercloud.Event), writer *modelWriter, gOpts *groupExecOptions) (routercloud.Result, error) {
	var locals, clouds []string
	for _, m := range members {
		if strings.HasPrefix(m, "local/") {
			locals = append(locals, m)
		} else {
			clouds = append(clouds, m)
		}
	}
	if gOpts != nil && gOpts.group != nil {
		locals = applySinkOrder(locals, gOpts.group.Sink)
		clouds = applySinkOrder(clouds, gOpts.group.Sink)
		clouds, gOpts.affinityKey, _ = applyGroupAffinity(*gOpts.group, prepared, protocol, body, clouds)
	}
	routing := options.routingOptions()
	if len(clouds) == 0 {
		return dispatchGroupWithPatience(w, prepared, protocol, sub, body, locals, locals, false, options, emit, writer, gOpts)
	}
	if len(locals) == 0 || routing == nil || routing.Judge == nil || operationFor(sub) == "count_tokens" {
		return dispatchGroupWithPatience(w, prepared, protocol, sub, body, clouds, clouds, false, options, emit, writer, gOpts)
	}
	if gOpts != nil && gOpts.group != nil {
		rt := strings.ToLower(strings.TrimSpace(gOpts.group.Routing))
		if (rt == "edge-cloud" || rt == "edge_cloud") && options.LocalRuntime != nil && options.LocalRuntime.Manager != nil {
			st := options.LocalRuntime.Manager.Status()
			if !st.Healthy || (st.Runtime != routerruntime.Ready && st.Runtime != routerruntime.Idle) || st.Active > 0 || st.Waiting > 0 {
				res, cloudErr := dispatchGroupWithPatience(w, prepared, protocol, sub, body, clouds, clouds, false, options, emit, writer, gOpts)
				res.Decision = string(DecisionCloud)
				if res.CapabilityState == "" {
					if st.Active > 0 || st.Waiting > 0 {
						res.CapabilityState = "judgment_queue_timeout"
					} else {
						res.CapabilityState = "unavailable"
					}
				}
				return res, cloudErr
			}
		}
	}
	state, _ := routerrequest.FromContext(prepared.Context())
	settings, _ := routerruntime.RuntimeSettingsFrom(prepared.Context())
	promptVer := judgmentPromptVersion(routing.PromptVersion, true)
	normReq, err := routerrequest.Normalize(routerrequest.Input{RequestID: state.ID, AppID: state.AppID, Protocol: protocol, Operation: operationFor(sub), SessionID: state.Target.Profile.ID, Model: strings.TrimPrefix(bareMember(locals[0]), "local/"), PromptVersion: promptVer, OriginalBody: body, Context: prepared.Context()})
	if err != nil {
		return routercloud.Result{Operation: operationFor(sub)}, err
	}
	localCtx, cancelLocal := localWorkContext(prepared.Context(), options)
	defer cancelLocal()
	if localCtx.Err() != nil && prepared.Context().Err() == nil {
		res, cloudErr := dispatchGroupWithPatience(w, prepared, protocol, sub, body, clouds, clouds, false, options, emit, writer, gOpts)
		res.Decision = string(DecisionCloud)
		if res.CapabilityState == "" {
			res.CapabilityState = "local_budget_reserved"
		}
		return res, cloudErr
	}
	judgeCtx := localCtx
	if options.LocalObserver != nil {
		var manager *routerruntime.Manager
		if options.LocalRuntime != nil {
			manager = options.LocalRuntime.Manager
		}
		requestID, check := state.ID, !businessModelRequest(options)
		if check {
			requestID = ""
		}
		mode := localCallMode(manager, routerruntime.Judgment)
		if check {
			mode = routerruntime.LocalCallDiagnostic
		}
		judgeCtx = withLocalCallContext(judgeCtx, localCallContext{RequestID: requestID, AppID: state.AppID, Observer: options.LocalObserver, Check: check, Mode: mode, PromptVersion: promptVer, Material: localJudgmentMaterial, Settings: settings})
	}
	started := time.Now()
	decision, judgeErr := routing.Judge.Judge(judgeCtx, normReq.Judgment())
	judgeDur := time.Since(started)
	if judgeErr != nil && prepared.Context().Err() != nil {
		return routercloud.Result{Operation: operationFor(sub), Decision: string(DecisionCloud), JudgmentDuration: judgeDur}, judgeErr
	}
	var capState string
	if judgeErr != nil {
		decision = DecisionCloud
		capState = localFailureState(routerruntime.Judgment, judgeErr, localCtx)
	} else if decision != DecisionLocal && decision != DecisionCloud {
		decision = DecisionCloud
		capState = "invalid_decision"
	}
	if judgeErr == nil && decision == DecisionLocal {
		res, localErr := dispatchGroupWithPatience(w, prepared, protocol, sub, body, locals, locals, false, options, emit, writer, gOpts)
		if localErr != nil && !terminalRedactionFailure(localErr) && !writer.committed && prepared.Context().Err() == nil {
			localCapState := res.CapabilityState
			res, localErr = dispatchGroupWithPatience(w, prepared, protocol, sub, body, clouds, clouds, false, options, emit, writer, gOpts)
			if res.CapabilityState == "" {
				res.CapabilityState = localCapState
			}
		}
		res.Decision, res.JudgmentDuration = string(DecisionLocal), judgeDur
		return res, localErr
	}
	res, cloudErr := dispatchGroupWithPatience(w, prepared, protocol, sub, body, clouds, clouds, false, options, emit, writer, gOpts)
	res.Decision, res.JudgmentDuration = string(DecisionCloud), judgeDur
	if res.CapabilityState == "" && capState != "" {
		res.CapabilityState = capState
	}
	return res, cloudErr
}

func dispatchCandidates(w http.ResponseWriter, prepared *http.Request, protocol, sub string, body []byte, candidates []string, options ModelOptions, emit func(routercloud.Event), writer *modelWriter) (routercloud.Result, error) {
	return dispatchGroupWithPatience(w, prepared, protocol, sub, body, candidates, candidates, false, options, emit, writer, nil)
}

func shiftEmitAttempts(emit func(routercloud.Event), offset int) func(routercloud.Event) {
	if emit == nil || offset <= 0 {
		return emit
	}
	return func(e routercloud.Event) {
		if e.Attempt > 0 {
			e.Attempt += offset
		}
		emit(e)
	}
}

func mergeGroupAttempts(prior, next []routercloud.Attempt) []routercloud.Attempt {
	merged := make([]routercloud.Attempt, 0, min(len(prior)+len(next), routerledger.MaxAttempts))
	for _, a := range prior {
		if len(merged) >= routerledger.MaxAttempts {
			break
		}
		merged = append(merged, a)
	}
	for _, a := range next {
		if len(merged) >= routerledger.MaxAttempts {
			break
		}
		merged = append(merged, a)
	}
	for i := range merged {
		merged[i].Number = i + 1
		merged[i].Retried = i+1 < len(merged)
	}
	return merged
}

func dispatchGroupWithPatience(w http.ResponseWriter, prepared *http.Request, protocol, sub string, body []byte, candidates, baseMembers []string, ruleHasTarget bool, options ModelOptions, emit func(routercloud.Event), writer *modelWriter, gOpts *groupExecOptions) (routercloud.Result, error) {
	res, err := dispatchCandidatesOnce(w, prepared, protocol, sub, body, candidates, options, emit, writer, gOpts)
	if err == nil || writer.committed || prepared.Context().Err() != nil || gOpts == nil || gOpts.group == nil || gOpts.group.Patience <= 0 || !isRetriableGroupFailure(err) {
		return res, err
	}
	deadline := time.Now().Add(time.Duration(gOpts.group.Patience) * patienceUnit)
	totalAttempts := res.Attempts
	allDetails := mergeGroupAttempts(nil, res.AttemptDetails)

	for time.Now().Before(deadline) && !writer.committed && prepared.Context().Err() == nil {
		waitDur := min(patiencePollStep, time.Until(deadline))
		if waitDur <= 0 {
			break
		}
		timer := time.NewTimer(waitDur)
		select {
		case <-prepared.Context().Done():
			timer.Stop()
			return res, err
		case <-timer.C:
		}
		if prepared.Context().Err() != nil || writer.committed {
			break
		}
		nextCandidates := candidates
		if len(baseMembers) > 0 && !strings.EqualFold(gOpts.group.Affinity, routerconfig.AffinityPin) && !strings.EqualFold(gOpts.group.Affinity, routerconfig.AffinityStrict) {
			nextCandidates = orderGroupCandidates(*gOpts.group, baseMembers, ruleHasTarget)
		}
		res, err = dispatchCandidatesOnce(w, prepared, protocol, sub, body, nextCandidates, options, shiftEmitAttempts(emit, totalAttempts), writer, gOpts)
		totalAttempts += res.Attempts
		allDetails = mergeGroupAttempts(allDetails, res.AttemptDetails)
		res.Attempts, res.AttemptDetails = totalAttempts, allDetails
		if err == nil || writer.committed || prepared.Context().Err() != nil || !isRetriableGroupFailure(err) {
			return res, err
		}
	}
	return res, err
}

func dispatchCandidatesOnce(w http.ResponseWriter, prepared *http.Request, protocol, sub string, body []byte, candidates []string, options ModelOptions, emit func(routercloud.Event), writer *modelWriter, gOpts *groupExecOptions) (routercloud.Result, error) {
	if len(candidates) == 0 {
		return routercloud.Result{Operation: operationFor(sub)}, &routercloud.Error{Code: "target_missing", Message: "routing group has no active members", Status: http.StatusConflict}
	}
	var res, lastRes routercloud.Result
	var err, lastErr error
	var total int
	var details []routercloud.Attempt
	for _, cand := range candidates {
		candBody := body
		if gOpts != nil && gOpts.group != nil {
			eff, isFast := resolveCandidateEffortAndFast(*gOpts.group, cand, gOpts.matchedRule, protocol, body)
			candBody = rewriteCandidateRequestBody(protocol, body, eff, isFast)
		}
		attemptReq := prepared
		var cancelAttempt context.CancelCauseFunc
		var ftTimer *time.Timer
		prevCommit := writer.commit
		if gOpts != nil && gOpts.group != nil && gOpts.group.FirstToken > 0 {
			var attemptCtx context.Context
			attemptCtx, cancelAttempt = context.WithCancelCause(prepared.Context())
			attemptReq = prepared.WithContext(attemptCtx)
			ftDur := time.Duration(gOpts.group.FirstToken) * firstTokenStep
			ftTimer = time.AfterFunc(ftDur, func() {
				cancelAttempt(errFirstTokenTimeout)
			})
			writer.commit = func() {
				ftTimer.Stop()
				if prevCommit != nil {
					prevCommit()
				}
			}
		}

		if localModel, ok := strings.CutPrefix(bareMember(cand), "local/"); ok {
			res, err = executeLocalCandidate(w, attemptReq, protocol, sub, candBody, localModel, options, writer)
		} else {
			res, err = executeCloudCandidate(w, attemptReq, protocol, sub, candBody, cand, options, shiftEmitAttempts(emit, total), writer)
		}

		if ftTimer != nil {
			ftTimer.Stop()
			writer.commit = prevCommit
			if err != nil && !terminalRedactionFailure(err) && errors.Is(context.Cause(attemptReq.Context()), errFirstTokenTimeout) && prepared.Context().Err() == nil {
				err = &routercloud.Error{Code: "cloud_timeout", Message: "first token timeout exceeded", Status: http.StatusGatewayTimeout}
			}
			cancelAttempt(nil)
		}

		now := time.Now()
		if err == nil {
			var usedTok int64
			if res.ReportedUsage != nil {
				if res.ReportedUsage.TotalTokens != nil {
					usedTok = *res.ReportedUsage.TotalTokens
				} else {
					if res.ReportedUsage.InputTokens != nil {
						usedTok += *res.ReportedUsage.InputTokens
					}
					if res.ReportedUsage.OutputTokens != nil {
						usedTok += *res.ReportedUsage.OutputTokens
					}
				}
			}
			recordMemberSuccess(cand, usedTok, now)
			if gOpts != nil && gOpts.group != nil && gOpts.affinityKey != "" {
				recordGroupAffinity(*gOpts.group, gOpts.affinityKey, cand, now)
			}
		} else if isRetriableGroupFailure(err) {
			recordMemberSunk(cand, now)
		}

		total += res.Attempts
		details = mergeGroupAttempts(details, res.AttemptDetails)
		res.Attempts, res.AttemptDetails = total, details
		if err == nil || terminalRedactionFailure(err) || writer.committed || prepared.Context().Err() != nil {
			return res, err
		}
		lastRes, lastErr = res, err
	}
	return lastRes, lastErr
}

func executeLocalCandidate(w http.ResponseWriter, prepared *http.Request, protocol, sub string, body []byte, localModel string, options ModelOptions, writer *modelWriter) (routercloud.Result, error) {
	op, routing, promptVer := operationFor(sub), options.routingOptions(), defaultPromptVersion
	settings, _ := routerruntime.RuntimeSettingsFrom(prepared.Context())
	if settings != (routerconfig.RuntimeSettings{}) {
		promptVer = judgmentPromptVersion(promptVer, true)
	}
	res := routercloud.Result{Operation: op, Decision: string(DecisionLocal)}
	if op == "count_tokens" {
		return res, &routercloud.Error{Code: "unsupported_operation", Message: "model operation is not supported", Status: http.StatusNotImplemented}
	}
	var invoker LocalInvoker
	if routing != nil {
		invoker = routing.LocalInvoker
		if routing.PromptVersion != "" {
			promptVer = judgmentPromptVersion(routing.PromptVersion, settings != (routerconfig.RuntimeSettings{}))
		}
	}
	if invoker == nil && options.LocalRuntime != nil && options.LocalRuntime.Manager != nil {
		invoker = options.LocalRuntime.Manager
	}
	if invoker == nil {
		res.CapabilityState = "execution_unavailable"
		return res, classifyLocalCandidateError(errors.New("local_runtime_unavailable"), prepared.Context())
	}
	state, _ := routerrequest.FromContext(prepared.Context())
	requestID, check := state.ID, !businessModelRequest(options)
	if check {
		requestID = ""
	}
	localCtx, cancel := localWorkContext(prepared.Context(), options)
	defer cancel()
	if mgr, ok := invoker.(*routerruntime.Manager); ok {
		if reqErr := mgr.Require(routerruntime.Execution); reqErr != nil {
			if options.LocalObserver != nil {
				mode := localCallMode(mgr, routerruntime.Execution)
				if check {
					mode = routerruntime.LocalCallDiagnostic
				}
				meta := localCallContext{RequestID: requestID, AppID: state.AppID, Observer: options.LocalObserver, Check: check, Mode: mode, PromptVersion: promptVer, Material: localExecutionMaterial, Settings: settings}
				meta.Binding, meta.Settings = mgr.SnapshotBinding(localCtx)
				meta.BindingCaptured = true
				emitObservedLocalCall(meta, mgr, routerruntime.Execution, time.Now(), routerruntime.GenerationDiagnosticSnapshot{}, nil, reqErr)
			}
			res.CapabilityState = localFailureState(routerruntime.Execution, reqErr, localCtx)
			return res, classifyLocalCandidateError(reqErr, prepared.Context())
		}
	}
	normReq, err := routerrequest.Normalize(routerrequest.Input{RequestID: state.ID, AppID: state.AppID, Protocol: protocol, Operation: op, SessionID: state.Target.Profile.ID, Model: localModel, PromptVersion: promptVer, OriginalBody: body, Context: prepared.Context()})
	if err != nil {
		return res, err
	}
	execBody := normReq.ExecutionBody()
	var prep PreparedCloudRequest
	work := redactionWork(prepared.Context())
	if work != nil && work.plan.RedactLocal {
		beginRedactionPreparation(prepared.Context(), options)
		if routing == nil || routing.Redactor == nil {
			return res, ErrRedactionUnavailable
		}
		beforeBody := append([]byte(nil), execBody...)
		sessionID, sessionVerified, seedSessionID := parseRedactionSessionMaterial(state.AppID, protocol, execBody)
		var prepErr error
		prep, prepErr = prepareRequiredRedaction(prepared.Context(), options, routing.Redactor, routing.Versions, RedactionRequest{
			SourceBody:      execBody,
			RequestID:       state.ID,
			AppID:           state.AppID,
			SourceProtocol:  protocol,
			TargetProtocol:  protocol,
			Operation:       op,
			SessionID:       sessionID,
			SeedSessionID:   seedSessionID,
			SessionVerified: sessionVerified,
			Body:            execBody,
			Context:         prepared.Context(),
		})
		if prep.Close != nil || prep.Release != nil {
			defer func() { _ = closePreparedCloud(prep) }()
		}
		if prepErr != nil || len(prep.Body) == 0 {
			res.CapabilityState = "redaction_unavailable"
			if prepared.Context().Err() != nil {
				return res, prepared.Context().Err()
			}
			if terminalRedactionFailure(prepErr) {
				return res, prepErr
			}
			return res, ErrRedactionUnavailable
		}
		if !check && op != "count_tokens" {
			recordRedactionObservation(options.RedactionObservations, state.ID, state.AppID, state.Target.Profile.Name, string(DecisionLocal), localModel, protocol, beforeBody, prep.Body, prep.ObservationMeta)
		}
		execBody = prep.Body
	}
	localBody, err := localExecutionBody(protocol, execBody, localModel)
	if err != nil {
		res.CapabilityState = "execution_protocol_mismatch"
		return res, classifyLocalCandidateError(err, prepared.Context())
	}
	executionCtx := routerruntime.WithClampOutputToWindow(localCtx)
	diagnostics := &routerruntime.GenerationDiagnostics{}
	executionCtx = routerruntime.WithGenerationDiagnostics(executionCtx, diagnostics)
	if options.LocalObserver != nil {
		mgr, _ := invoker.(*routerruntime.Manager)
		if mgr == nil && options.LocalRuntime != nil {
			mgr = options.LocalRuntime.Manager
		}
		mode := localCallMode(mgr, routerruntime.Execution)
		if check {
			mode = routerruntime.LocalCallDiagnostic
		}
		meta := localCallContext{RequestID: requestID, AppID: state.AppID, Observer: options.LocalObserver, Check: check, Mode: mode, PromptVersion: promptVer, Material: localExecutionMaterial, Settings: settings}
		if mgr != nil {
			meta.Binding, meta.Settings = mgr.SnapshotBinding(executionCtx)
			meta.BindingCaptured = true
		}
		executionCtx = withLocalCallContext(executionCtx, meta)
	}
	localStarted := time.Now()
	err = invoker.Invoke(executionCtx, routerruntime.Execution, localBody, func(resp *http.Response) error {
		return writeRestoredLocalResponse(prepared.Context(), w, resp, protocol, body, writer, &res.ReportedUsage, prep)
	})
	diagnostics.SetReportedUsage(res.ReportedUsage)
	if meta, ok := localCallContextFrom(executionCtx); ok {
		mgr, _ := invoker.(*routerruntime.Manager)
		if mgr == nil && options.LocalRuntime != nil {
			mgr = options.LocalRuntime.Manager
		}
		emitObservedLocalCall(meta, mgr, routerruntime.Execution, localStarted, diagnostics.Snapshot(), res.ReportedUsage, err)
	}
	res.OutputCommitted = writer.committed
	if err == nil {
		state.Stage, state.Destination = "local", "local"
		res.Destination = "local"
		return res, nil
	}
	if writer.committed || prepared.Context().Err() != nil {
		res.Destination = "local"
	}
	res.CapabilityState = localFailureState(routerruntime.Execution, err, localCtx)
	return res, classifyLocalCandidateError(err, prepared.Context())
}

func writeRestoredLocalResponse(ctx context.Context, w http.ResponseWriter, resp *http.Response, protocol string, requestBody []byte, writer *modelWriter, usage **cloudcodec.ReportedUsage, prep PreparedCloudRequest) error {
	if prep.Restore == nil && prep.RestoreSSE == nil {
		return writeLocalResponseObserved(w, resp, protocol, requestBody, writer, usage)
	}
	var downstream http.ResponseWriter = w
	if writer != nil {
		downstream = writer
	}
	contentType := ""
	if resp != nil {
		contentType = resp.Header.Get("Content-Type")
	}
	stream := strings.Contains(strings.ToLower(contentType), "text/event-stream") || isStreamRequest(requestBody)
	if stream && prep.RestoreSSE != nil {
		reader, pipeWriter := io.Pipe()
		streamRestore := make(chan error, 1)
		go func() {
			err := prep.RestoreSSE(ctx, protocol, reader, flushWriter{downstream: downstream})
			_ = reader.Close()
			streamRestore <- err
		}()
		pipeTarget := &restorePipeWriter{downstream: downstream, pipe: pipeWriter}
		pipeModelWriter := &modelWriter{ResponseWriter: pipeTarget, commit: func() {}}
		writeErr := writeLocalResponseObserved(pipeTarget, resp, protocol, requestBody, pipeModelWriter, usage)
		if writeErr != nil {
			_ = pipeWriter.CloseWithError(writeErr)
		} else {
			_ = pipeWriter.Close()
		}
		restoreErr := <-streamRestore
		if writeErr != nil {
			return writeErr
		}
		return restoreErr
	}
	if prep.Restore == nil {
		return writeLocalResponseObserved(w, resp, protocol, requestBody, writer, usage)
	}
	capture := newCaptureWriter()
	captureModelWriter := &modelWriter{ResponseWriter: capture, commit: func() {}}
	if err := writeLocalResponseObserved(capture, resp, protocol, requestBody, captureModelWriter, usage); err != nil {
		return err
	}
	processed, err := prep.Restore(ctx, protocol, capture.header.Get("Content-Type"), capture.body.Bytes())
	if err != nil {
		return err
	}
	return writeCaptured(downstream, capture, processed)
}

func classifyLocalCandidateError(err error, reqCtx context.Context) error {
	if err == nil {
		return nil
	}
	var cloudErr *routercloud.Error
	if errors.As(err, &cloudErr) {
		return err
	}
	var reqErr *routerrequest.Error
	if errors.As(err, &reqErr) {
		return err
	}
	var normErr *routerrequest.NormalizeError
	if errors.As(err, &normErr) {
		return err
	}
	switch {
	case errors.Is(err, routerruntime.ErrContextExceeded), errors.Is(err, routerruntime.ErrJudgmentInputExceeded):
		return &routercloud.Error{Code: "request_too_large", Message: "local model context window exceeded", Status: http.StatusBadRequest}
	case errors.Is(err, routerruntime.ErrExecutionQueueTimeout), errors.Is(err, routerruntime.ErrExecutionTimeout), errors.Is(err, routerruntime.ErrJudgmentQueueTimeout), errors.Is(err, routerruntime.ErrJudgmentInferenceTimeout):
		return &routercloud.Error{Code: "cloud_timeout", Message: "local model execution timed out", Status: http.StatusGatewayTimeout}
	case errors.Is(err, routerruntime.ErrDisabled), errors.Is(err, routerruntime.ErrNotInstalled), errors.Is(err, routerruntime.ErrCapability), errors.Is(err, routerruntime.ErrQueueFull), errors.Is(err, routerruntime.ErrFault), errors.Is(err, routerruntime.ErrBusy):
		return &routercloud.Error{Code: "router_error", Message: "local model runtime is not ready", Status: http.StatusServiceUnavailable}
	}
	if reqCtx != nil && reqCtx.Err() != nil {
		return reqCtx.Err()
	}
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return &routercloud.Error{Code: "cloud_timeout", Message: "local model execution timed out", Status: http.StatusGatewayTimeout}
	}
	return &routercloud.Error{Code: "router_error", Message: "local model execution failed", Status: http.StatusBadGateway}
}

func executeCloudCandidate(w http.ResponseWriter, prepared *http.Request, protocol, sub string, body []byte, member string, options ModelOptions, emit func(routercloud.Event), writer *modelWriter) (routercloud.Result, error) {
	provID, modelID, ok := strings.Cut(member, "/")
	if !ok || provID == "" || modelID == "" {
		return routercloud.Result{Operation: operationFor(sub), Decision: string(DecisionCloud)}, &routercloud.Error{Code: "target_missing", Message: "configured provider does not exist", Status: http.StatusConflict}
	}
	modelID = bareMember(modelID)
	state, _ := routerrequest.FromContext(prepared.Context())
	appTarget, err := options.store.ResolveAppProviderTargetContext(prepared.Context(), state.Target.Profile, provID, modelID, state.Target.Authorization, "", options.managementToken)
	if err != nil {
		return routercloud.Result{Operation: operationFor(sub), Decision: string(DecisionCloud)}, err
	}
	if redactionRequired(prepared.Context(), state.Target.Profile.RedactionEnabled) {
		appTarget.Profile.RedactionEnabled = true
	}
	state.BeginMemberDispatch()
	prev := state.Target
	state.Target = appTarget
	res, err := executeCloudModelSub(w, prepared, protocol, sub, body, options, emit, writer)
	if err != nil && !writer.committed && res.Attempts == 0 {
		state.Target = prev
	}
	res.Decision = string(DecisionCloud)
	return res, err
}
