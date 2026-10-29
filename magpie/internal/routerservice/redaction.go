package routerservice

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/yetone/magpie/internal/routerconfig"
	"github.com/yetone/magpie/internal/routerledger"
	"github.com/yetone/magpie/internal/routerredact"
)

type redactionIntent struct {
	ID       string `json:"id"`
	Revision int    `json:"revision"`
	Enabled  bool   `json:"enabled"`
	Active   bool   `json:"active"`
}

// redactionAPI deliberately has no runtime-ready or enable-override input.
// Capability and readiness are read from the service-owned Manager; clients
// can only edit their saved intent.
func redactionAPI(w http.ResponseWriter, r *http.Request, store *routerconfig.Store, stats func() routerredact.Stats, stateFns ...func() redactionRuntimeState) {
	fail := func(status int, code string) {
		providerJSON(w, status, map[string]any{"error": map[string]string{"code": code}})
	}
	if r.URL.RawQuery != "" {
		fail(400, "invalid_query")
		return
	}
	if store == nil {
		fail(503, "configuration_unavailable")
		return
	}
	capability := routerconfig.RedactionUnavailable
	if len(stateFns) > 0 && stateFns[0] != nil {
		state := stateFns[0]()
		capability = state.Capability
		if !state.RecognitionReady && !state.TrialReady {
			capability = routerconfig.RedactionUnknown
		}
	}
	if r.Method == http.MethodPost {
		var input struct {
			Edits []struct {
				ID       string `json:"id"`
				Revision int    `json:"revision"`
				Enabled  *bool  `json:"enabled"`
			} `json:"edits"`
		}
		if !providerInput(w, r, &input) {
			return
		}
		edits := make([]routerconfig.RedactionIntentEdit, 0, len(input.Edits))
		for _, edit := range input.Edits {
			if edit.Enabled == nil {
				fail(400, "invalid_redaction_selection")
				return
			}
			edits = append(edits, routerconfig.RedactionIntentEdit{ID: edit.ID, Revision: edit.Revision, Enabled: *edit.Enabled})
		}
		_, err := store.SetRedactionIntentsContext(r.Context(), edits, capability)
		if err != nil {
			switch {
			case errors.Is(err, routerconfig.ErrRedactionUnavailable):
				fail(409, "redaction_unavailable")
			case errors.Is(err, routerconfig.ErrAppConflict):
				fail(409, "profile_conflict")
			case errors.Is(err, routerconfig.ErrAppNotFound):
				fail(404, "app_not_registered")
			case errors.Is(err, context.Canceled):
				fail(499, "request_canceled")
			case errors.Is(err, context.DeadlineExceeded):
				fail(504, "budget_exhausted")
			default:
				_, status := routerconfig.ProviderError(err)
				if status == 400 {
					fail(400, "invalid_redaction_selection")
				} else {
					fail(503, "configuration_unavailable")
				}
			}
			return
		}
	}
	apps, err := store.AppsContext(r.Context())
	if err != nil {
		fail(503, "configuration_unavailable")
		return
	}
	projection := redactionRuntimeState{Capability: routerconfig.RedactionUnavailable, Reason: "recognition_acceptance_pending"}
	if len(stateFns) > 0 && stateFns[0] != nil {
		projection = stateFns[0]()
	}
	intents := make([]redactionIntent, 0, len(apps))
	for _, p := range apps {
		active := false
		active = p.RedactionEnabled && projection.CloudActive
		intents = append(intents, redactionIntent{ID: p.ID, Revision: p.Revision, Enabled: p.RedactionEnabled, Active: active})
	}
	w.Header().Set("Content-Type", "application/json")
	if r.Method == http.MethodHead {
		return
	}
	var statistics any
	if stats != nil {
		v := stats()
		failures := map[string]uint64{}
		for code, count := range v.Failures {
			failures[routerredact.SafeFailureCode(code)] += count
		}
		statistics = map[string]any{"processed": v.Processed, "entities": v.Entities, "cacheHits": v.CacheHits, "blocked": v.Blocked, "durationMs": v.DurationMS, "failures": failures}
	}
	if !projection.Integrated {
		projection.Statistics = stats != nil
	}
	providerJSON(w, 200, map[string]any{
		"schemaVersion": 1, "capability": projection.Capability,
		"reason": projection.Reason, "productionIntegrated": projection.Integrated,
		"modelReady": projection.ModelReady, "recognitionReady": projection.RecognitionReady,
		"experimental": projection.Experimental, "trialReady": projection.TrialReady,
		"budgetMs": projection.BudgetMS, "formalQualityAccepted": projection.RecognitionReady,
		"trialSuite": projection.TrialSuite, "trialConfigVersion": projection.TrialConfig,
		"cloudRedactionActive": projection.CloudActive, "localExecutionRedacted": false, "intents": intents,
		"statsAvailable": projection.Statistics && stats != nil, "statistics": statistics,
	})
}

func extractUserContentText(content any) (string, bool) {
	switch v := content.(type) {
	case string:
		if strings.TrimSpace(v) == "" {
			return "", false
		}
		return v, true
	case []any:
		var parts []string
		for _, item := range v {
			block, ok := item.(map[string]any)
			if !ok {
				continue
			}
			bType, _ := block["type"].(string)
			if bType == "text" || bType == "input_text" || bType == "" {
				if txt, ok := block["text"].(string); ok && txt != "" {
					parts = append(parts, txt)
				}
			}
		}
		joined := strings.Join(parts, "\n")
		if strings.TrimSpace(joined) == "" {
			return "", false
		}
		return joined, true
	default:
		return "", false
	}
}

// extractLatestUserInput extracts only the latest turn's user role text.
// If the trailing turn is a tool result, assistant message, or has no user text,
// it returns ("", false) so tool-callback loops do not duplicate history.
func extractLatestUserInput(protocol string, body []byte) (string, bool) {
	if len(body) == 0 {
		return "", false
	}
	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		return "", false
	}
	switch protocol {
	case "chat", "anthropic":
		msgs, ok := root["messages"].([]any)
		if !ok || len(msgs) == 0 {
			return "", false
		}
		last, ok := msgs[len(msgs)-1].(map[string]any)
		if !ok {
			return "", false
		}
		role, _ := last["role"].(string)
		if role != "user" {
			return "", false
		}
		return extractUserContentText(last["content"])
	case "responses":
		switch in := root["input"].(type) {
		case string:
			if strings.TrimSpace(in) == "" {
				return "", false
			}
			return in, true
		case []any:
			if len(in) == 0 {
				return "", false
			}
			last, ok := in[len(in)-1].(map[string]any)
			if !ok {
				return "", false
			}
			itemType, _ := last["type"].(string)
			if itemType != "" && itemType != "message" {
				return "", false
			}
			role, _ := last["role"].(string)
			if role != "user" {
				return "", false
			}
			return extractUserContentText(last["content"])
		default:
			return "", false
		}
	default:
		return "", false
	}
}

func recordRedactionObservation(
	store *routerledger.RedactionObservationStore,
	requestID, appID, appName, destination, model, protocol string,
	beforeBody, afterBody []byte,
	meta *routerredact.ObservationMeta,
) {
	if store == nil {
		return
	}
	origText, okOrig := extractLatestUserInput(protocol, beforeBody)
	redactedText, okRed := extractLatestUserInput(protocol, afterBody)
	if !okOrig || !okRed {
		return
	}
	var turnReplacements []routerledger.RedactionReplacementItem
	var cacheHit bool
	var durationMS int64
	if meta != nil {
		cacheHit = meta.CacheHit
		durationMS = meta.DurationMS
		for _, r := range meta.Replacements {
			if r.Placeholder == "" || r.Original == "" {
				continue
			}
			var count int
			if strings.HasPrefix(r.Placeholder, "<") && strings.HasSuffix(r.Placeholder, ">") {
				count = strings.Count(redactedText, r.Placeholder)
			} else if strings.Contains(origText, r.Original) && strings.Contains(redactedText, r.Placeholder) {
				count = strings.Count(origText, r.Original)
			}
			if count > 0 {
				turnReplacements = append(turnReplacements, routerledger.RedactionReplacementItem{
					Placeholder: r.Placeholder,
					Category:    r.Category,
					Raw:         r.Original,
					Count:       count,
				})
			}
		}
	}
	entityCount := 0
	categorySet := make(map[string]struct{})
	var categories []string
	for _, tr := range turnReplacements {
		entityCount += tr.Count
		if tr.Category != "" {
			if _, exists := categorySet[tr.Category]; !exists {
				categorySet[tr.Category] = struct{}{}
				categories = append(categories, tr.Category)
			}
		}
	}
	redactedHit := len(turnReplacements) > 0 || origText != redactedText
	if redactedHit && entityCount == 0 {
		entityCount = 1
	}
	_ = store.Record(routerledger.RedactionObservation{
		RequestID:     requestID,
		AppID:         appID,
		AppName:       appName,
		Destination:   destination,
		Protocol:      protocol,
		Model:         model,
		StartedAt:     time.Now().UTC().Format(time.RFC3339Nano),
		DurationMS:    durationMS,
		CacheHit:      cacheHit,
		Hit:           redactedHit,
		EntityCount:   entityCount,
		Categories:    categories,
		RawInput:      origText,
		RedactedInput: redactedText,
		Replacements:  turnReplacements,
	})
}

func redactionObservationsQuery(r *http.Request) (routerledger.RedactionObservationFilter, error) {
	q := routerledger.RedactionObservationFilter{Limit: 50}
	if len(r.URL.RawQuery) > 2048 {
		return q, routerledger.ErrInvalid
	}
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return q, routerledger.ErrInvalid
	}
	for key, v := range values {
		if len(v) != 1 || len(v[0]) > 256 {
			return q, routerledger.ErrInvalid
		}
		val := strings.TrimSpace(v[0])
		switch key {
		case "limit":
			if val == "" {
				continue
			}
			q.Limit, err = strconv.Atoi(val)
			if err != nil || q.Limit < 1 || q.Limit > 200 {
				return q, routerledger.ErrInvalid
			}
		case "before":
			q.Before = val
		case "appId":
			if val != "" && !routerconfig.ValidAppID(val) {
				return q, routerledger.ErrInvalid
			}
			q.AppID = val
		case "hitOnly":
			switch val {
			case "true", "1":
				q.HitOnly = true
			case "false", "0", "":
				q.HitOnly = false
			default:
				return q, routerledger.ErrInvalid
			}
		case "q":
			q.Query = val
		default:
			return q, routerledger.ErrInvalid
		}
	}
	return q, nil
}

func redactionObservationsAPI(w http.ResponseWriter, r *http.Request, obsStore *routerledger.RedactionObservationStore) {
	fail := func(status int, code string) {
		providerJSON(w, status, map[string]any{"error": map[string]string{"code": code}})
	}
	if r.Method == http.MethodDelete || r.Method == http.MethodPost {
		if r.URL.RawQuery != "" {
			fail(400, "invalid_query")
			return
		}
		if r.Method == http.MethodPost {
			var input struct {
				Action string `json:"action"`
			}
			if !providerInput(w, r, &input) {
				return
			}
			if input.Action != "clear" {
				fail(400, "invalid_redaction_action")
				return
			}
		}
		if obsStore != nil {
			if err := obsStore.Clear(); err != nil {
				fail(503, "redaction_observations_unavailable")
				return
			}
		}
		providerJSON(w, 200, routerledger.RedactionObservationListResult{
			SchemaVersion: 1,
			Available:     obsStore != nil,
			Summary:       routerledger.RedactionObservationSummary{CategoryBreakdown: map[string]int{}},
			Observations:  []routerledger.RedactionObservation{},
		})
		return
	}
	q, err := redactionObservationsQuery(r)
	if err != nil {
		fail(400, "invalid_query")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if r.Method == http.MethodHead {
		return
	}
	if obsStore == nil {
		providerJSON(w, 200, routerledger.RedactionObservationListResult{
			SchemaVersion: 1,
			Available:     false,
			Summary:       routerledger.RedactionObservationSummary{CategoryBreakdown: map[string]int{}},
			Observations:  []routerledger.RedactionObservation{},
		})
		return
	}
	page, err := obsStore.Query(q)
	if errors.Is(err, routerledger.ErrInvalid) {
		fail(400, "invalid_query")
		return
	}
	if err != nil {
		fail(503, "redaction_observations_unavailable")
		return
	}
	providerJSON(w, 200, page)
}
