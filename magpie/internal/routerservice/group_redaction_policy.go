package routerservice

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/yetone/magpie/internal/routercloud"
	"github.com/yetone/magpie/internal/routerconfig"
	"github.com/yetone/magpie/internal/routerrequest"
	"github.com/yetone/magpie/internal/routerruntime"
)

const groupRedactionPolicyVersion = "group-redaction-inheritance-v1"

// requestRedactionPlan is fixed before dispatch and independent of state.Target,
// which is replaced and restored as candidates fail. It is never client input.
type requestRedactionPlan struct {
	Required        bool
	RequiredModelID string
	SourceGroupIDs  []string
	ProfileRequired bool
	RedactLocal     bool
	ConfigIdentity  string
}

type requestRedactionKey struct{}

// requestRedactionWork is owned by the serial request executor. Every candidate
// shares its preparation deadline and binding; retries cannot renew either.
type requestRedactionWork struct {
	plan     requestRedactionPlan
	deadline time.Time
	versions RoutingVersions
	bound    bool
	status   string
}

func redactionWork(ctx context.Context) *requestRedactionWork {
	work, _ := ctx.Value(requestRedactionKey{}).(*requestRedactionWork)
	return work
}

func redactionRequired(ctx context.Context, profile bool) bool {
	work := redactionWork(ctx)
	return profile || work != nil && work.plan.Required
}

func groupPrivacyPlan(profile bool, sources []routerconfig.GroupPrivacySource) (requestRedactionPlan, error) {
	plan := requestRedactionPlan{Required: profile || len(sources) > 0, ProfileRequired: profile}
	for _, source := range sources {
		if !slices.Contains(plan.SourceGroupIDs, source.GroupID) {
			plan.SourceGroupIDs = append(plan.SourceGroupIDs, source.GroupID)
		}
	}
	slices.Sort(plan.SourceGroupIDs)
	for _, source := range sources {
		model := strings.ToLower(strings.TrimSpace(source.ModelID))
		if model == "" {
			return plan, privacyPolicyError("redaction_model_missing")
		}
		if plan.RequiredModelID != "" && plan.RequiredModelID != model {
			return plan, privacyPolicyError("redaction_policy_conflict")
		}
		plan.RequiredModelID = model
	}
	encoded, _ := json.Marshal(struct {
		Version string
		Plan    requestRedactionPlan
	}{groupRedactionPolicyVersion, plan})
	hash := sha256.Sum256(encoded)
	plan.ConfigIdentity = hex.EncodeToString(hash[:])
	return plan, nil
}

func privacyPolicyError(code string) error {
	message := "routing group privacy policy could not be resolved"
	switch code {
	case "redaction_model_missing":
		message = "required redaction model is missing from the routing group"
	case "redaction_policy_conflict":
		message = "routing groups require different redaction models"
	}
	return &routercloud.Error{Code: code, Message: message, Status: http.StatusConflict}
}

func terminalRedactionFailure(err error) bool {
	if errors.Is(err, ErrRedactionUnavailable) {
		return true
	}
	var cloudErr *routercloud.Error
	return errors.As(err, &cloudErr) && strings.HasPrefix(cloudErr.Code, "redaction_")
}

func installRedactionPlan(r *http.Request, plan requestRedactionPlan) *http.Request {
	if work := redactionWork(r.Context()); work != nil {
		if !work.bound {
			work.plan = plan
		} // Admission only; later dispatch cannot replace it.
		return r
	}
	return r.WithContext(context.WithValue(r.Context(), requestRedactionKey{}, &requestRedactionWork{plan: plan}))
}

func ensureProfileRedactionPlan(r *http.Request) *http.Request {
	if redactionWork(r.Context()) != nil {
		return r
	}
	state, _ := routerrequest.FromContext(r.Context())
	plan, _ := groupPrivacyPlan(state.Target.Profile.RedactionEnabled, nil)
	return installRedactionPlan(r, plan)
}

func redactionSnapshot(ctx context.Context, err error) *routercloud.RedactionPolicy {
	work := redactionWork(ctx)
	if work == nil {
		return nil
	}
	state := work.status
	required := work.plan.Required || terminalRedactionFailure(err)
	if terminalRedactionFailure(err) {
		state = "blocked"
	} else if state == "" {
		state = "not_required"
		if required {
			state = "not_sent"
		}
	}
	return &routercloud.RedactionPolicy{Required: required, SourceGroupIDs: slices.Clone(work.plan.SourceGroupIDs[:min(8, len(work.plan.SourceGroupIDs))]), SourceGroupCount: len(work.plan.SourceGroupIDs), ModelID: work.plan.RequiredModelID, State: state}
}

func prepareRequiredRedaction(ctx context.Context, options ModelOptions, redactor Redactor, versions RoutingVersions, req RedactionRequest) (PreparedCloudRequest, error) {
	if dynamic, ok := redactor.(interface{ RoutingVersions() RoutingVersions }); ok {
		versions = dynamic.RoutingVersions()
	}
	req.Versions = versions
	work := redactionWork(ctx)
	if work == nil {
		return redactor.Prepare(ctx, req)
	}
	if work.bound && work.versions != versions {
		work.status = "blocked"
		return PreparedCloudRequest{}, ErrRedactionUnavailable
	}
	work.versions, work.bound = versions, true
	if work.deadline.IsZero() {
		budget := routerruntime.DefaultRedactionBudget
		if options.LocalRuntime != nil && options.LocalRuntime.Manager != nil {
			if configured := options.LocalRuntime.Manager.RedactionBudget(); configured > 0 && configured < budget {
				budget = configured
			}
		}
		work.deadline = time.Now().Add(budget)
	}
	ctx, cancel := context.WithDeadline(ctx, work.deadline)
	defer cancel()
	req.Context = ctx
	work.status = "pending"
	prepared, err := redactor.Prepare(ctx, req)
	if err == nil {
		if dynamic, ok := redactor.(interface{ RoutingVersions() RoutingVersions }); ok && work.versions != dynamic.RoutingVersions() {
			err = ErrRedactionUnavailable
		}
	}
	if err == nil && ctx.Err() != nil {
		err = ErrRedactionUnavailable
	}
	work.status = "prepared"
	if err != nil || len(prepared.Body) == 0 {
		work.status = "blocked"
	}
	return prepared, err
}
