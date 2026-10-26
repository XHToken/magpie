package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"time"

	"github.com/tidwall/gjson"
	"github.com/yetone/magpie/internal/provider"
	"github.com/yetone/magpie/internal/routerruntime"
)

const (
	// edgeCloudMaxTokens is the maximum estimated context tokens for Stage 1
	// fast filter; requests larger than this go directly to the cloud.
	edgeCloudMaxTokens = 2048

	edgeCloudJudgePrompt = `You are an edge-cloud routing classifier. Evaluate whether the user's request is a simple short text task suitable for a lightweight local model ("local") or a complex task requiring deep reasoning, coding, multi-step analysis, or broad knowledge ("cloud"). Reply ONLY with valid JSON: {"decision":"local"} or {"decision":"cloud"}.`
)

var (
	// edgeCloudAssessTimeout bounds Stage 2 difficulty evaluation on the local model.
	edgeCloudAssessTimeout = 1500 * time.Millisecond

	// edgeCloudLocalAttemptTimeout bounds a primary local attempt in an EdgeCloud
	// group when cloud fallback candidates follow it.
	edgeCloudLocalAttemptTimeout = 15 * time.Second

	// edgeCloudRuntimeStatus returns the current local runtime status; tests may override it.
	edgeCloudRuntimeStatus = func() routerruntime.Status {
		return routerruntime.Default().Status()
	}

	// edgeCloudAcquireSlot acquires a concurrency slot from the local runtime manager.
	edgeCloudAcquireSlot = func(ctx context.Context) (func(), time.Duration, error) {
		return routerruntime.Default().AcquireSlot(ctx)
	}
)

type edgeCloudAssessKey struct{}

func withEdgeCloudAssess(ctx context.Context) context.Context {
	return context.WithValue(ctx, edgeCloudAssessKey{}, true)
}

func isEdgeCloudAssess(ctx context.Context) bool {
	v, _ := ctx.Value(edgeCloudAssessKey{}).(bool)
	return v
}

// isLocalCandidate reports whether a candidate belongs to the local model provider.
func isLocalCandidate(c candidate) bool {
	return c.p.ID == "local" ||
		strings.HasPrefix(c.rest, "local/") ||
		strings.HasPrefix(c.rest, "local#") ||
		strings.HasPrefix(c.rest, "local@") ||
		strings.HasPrefix(c.model, "local/")
}

// splitEdgeCloudCandidates partitions candidates and their weighed trace entries
// into local and cloud slices while preserving their relative order.
func splitEdgeCloudCandidates(cands []candidate, pl planned) (localCands, cloudCands []candidate, localOrder, cloudOrder []Weighed) {
	hasOrder := len(pl.order) == len(cands)
	for i, c := range cands {
		if isLocalCandidate(c) {
			localCands = append(localCands, c)
			if hasOrder {
				localOrder = append(localOrder, pl.order[i])
			}
		} else {
			cloudCands = append(cloudCands, c)
			if hasOrder {
				cloudOrder = append(cloudOrder, pl.order[i])
			}
		}
	}
	return localCands, cloudCands, localOrder, cloudOrder
}

// edgeCloudBodyHasTools checks whether the raw request body declares any tools
// or tool_calls across supported protocols.
func edgeCloudBodyHasTools(body []byte) bool {
	if len(body) == 0 {
		return false
	}
	if tools := gjson.GetBytes(body, "tools"); tools.IsArray() && len(tools.Array()) > 0 {
		return true
	}
	if fn := gjson.GetBytes(body, "functions"); fn.IsArray() && len(fn.Array()) > 0 {
		return true
	}
	return false
}

// edgeCloudFastFilter performs Stage 1 zero-latency filtering.
// It returns goCloud=true when the local runtime is not ready/healthy, is busy
// or queued (Active > 0 || Waiting > 0), or when the request contains images,
// tools, or > 2048 estimated tokens.
func edgeCloudFastFilter(from provider.Protocol, body []byte, req *Request) (goCloud bool, parsed *Request) {
	st := edgeCloudRuntimeStatus()
	if !st.Healthy || (st.Runtime != routerruntime.Ready && st.Runtime != routerruntime.Idle) {
		return true, req
	}
	if st.Active > 0 || st.Waiting > 0 {
		return true, req
	}
	if hasImage(from, body) || edgeCloudBodyHasTools(body) {
		return true, req
	}
	if req == nil {
		var err error
		req, err = parse(from, body)
		if err != nil || req == nil {
			return true, nil
		}
	}
	for _, m := range req.Messages {
		if slices.ContainsFunc(m.Parts, func(p Part) bool {
			return p.Kind == Image || p.Kind == ToolCall || p.Kind == ToolResult
		}) {
			return true, req
		}
	}
	if len(req.Tools) > 0 {
		return true, req
	}
	if estimate(req) > edgeCloudMaxTokens {
		return true, req
	}
	return false, req
}

// edgeCloudAssessBody constructs the lightweight Stage 2 difficulty evaluation request
// for the local model with thinking disabled, max_tokens=16, and JSON output format.
func edgeCloudAssessBody(model, text string) []byte {
	payload := map[string]any{
		"model": model,
		"messages": []map[string]string{
			{"role": "system", "content": edgeCloudJudgePrompt},
			{"role": "user", "content": text},
		},
		"stream":               false,
		"temperature":          0,
		"max_tokens":           16,
		"enable_thinking":      false,
		"chat_template_kwargs": map[string]any{"enable_thinking": false},
		"response_format":      map[string]any{"type": "json_object"},
	}
	b, _ := json.Marshal(payload)
	return b
}

// parseEdgeCloudDecision extracts "local" or "cloud" from the local model's JSON reply.
func parseEdgeCloudDecision(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "cloud", errors.New("empty edge-cloud decision response")
	}
	// Strip markdown code fences if present
	if strings.HasPrefix(trimmed, "```") {
		if idx := strings.Index(trimmed, "\n"); idx >= 0 {
			trimmed = trimmed[idx+1:]
		}
		if idx := strings.LastIndex(trimmed, "```"); idx >= 0 {
			trimmed = trimmed[:idx]
		}
		trimmed = strings.TrimSpace(trimmed)
	}
	if start := strings.IndexByte(trimmed, '{'); start >= 0 {
		if end := strings.LastIndexByte(trimmed, '}'); end > start {
			trimmed = trimmed[start : end+1]
		}
	}
	var out struct {
		Decision string `json:"decision"`
	}
	if err := json.Unmarshal([]byte(trimmed), &out); err != nil {
		return "cloud", err
	}
	switch strings.ToLower(strings.TrimSpace(out.Decision)) {
	case "local":
		return "local", nil
	case "cloud":
		return "cloud", nil
	default:
		return "cloud", fmt.Errorf("unexpected edge-cloud decision %q", out.Decision)
	}
}

func extractEdgeCloudUserText(req *Request) string {
	if req == nil {
		return ""
	}
	if text := strings.TrimSpace(userText(req)); text != "" {
		return text
	}
	for i := len(req.Messages) - 1; i >= 0; i-- {
		m := req.Messages[i]
		if m.Role != "user" {
			continue
		}
		var b strings.Builder
		for _, p := range m.Parts {
			if p.Kind == Text && strings.TrimSpace(p.Text) != "" {
				if b.Len() > 0 {
					b.WriteByte('\n')
				}
				b.WriteString(strings.TrimSpace(p.Text))
			}
		}
		if b.Len() > 0 {
			return b.String()
		}
	}
	return ""
}

// assessEdgeCloudDifficulty runs Stage 2 lightweight difficulty evaluation on localCand.
func (s *Server) assessEdgeCloudDifficulty(ctx context.Context, localCand candidate, req *Request) (string, error) {
	text := extractEdgeCloudUserText(req)
	if text == "" {
		return "cloud", errors.New("empty user message")
	}
	st := edgeCloudRuntimeStatus()
	if !st.Healthy || (st.Runtime != routerruntime.Ready && st.Runtime != routerruntime.Idle) || st.Active > 0 || st.Waiting > 0 {
		return "cloud", errors.New("local runtime busy or unavailable")
	}

	targetModel := localCand.p.ID + "/" + strings.TrimPrefix(localCand.model, localCand.p.ID+"/")
	body := edgeCloudAssessBody(targetModel, text)

	assessCtx, cancel := context.WithTimeout(withEdgeCloudAssess(ctx), edgeCloudAssessTimeout)
	defer cancel()

	r, err := http.NewRequestWithContext(assessCtx, http.MethodPost, "http://magpie/v1/chat/completions", nil)
	if err != nil {
		return "cloud", err
	}
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("User-Agent", RouterAgent)

	w := httptest.NewRecorder()
	s.serve(w, r, provider.Chat, body)
	if assessCtx.Err() != nil {
		return "cloud", assessCtx.Err()
	}

	var resp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		return "cloud", fmt.Errorf("invalid judge response (%d): %w", w.Code, err)
	}
	if w.Code >= 300 || resp.Error != nil {
		msg := http.StatusText(w.Code)
		if resp.Error != nil && resp.Error.Message != "" {
			msg = resp.Error.Message
		}
		return "cloud", fmt.Errorf("judge status %d: %s", w.Code, msg)
	}
	if len(resp.Choices) == 0 {
		return "cloud", errors.New("judge returned no choices")
	}
	return parseEdgeCloudDecision(resp.Choices[0].Message.Content)
}

// planEdgeCloud applies the 3-stage edge-cloud routing strategy for a group:
//   - Stage 1 (Fast Filter): zero-latency check of local runtime health/load, images, tools, and >2048 tokens.
//   - Stage 2 (Edge Difficulty Assessment): lightweight non-thinking JSON classification on the local model.
//   - Stage 3 (Shunting & Fallback): places local candidates first with cloud fallback when "local",
//     or places cloud candidates first when "cloud" or on any timeout/error.
func (s *Server) planEdgeCloud(ctx context.Context, g provider.Group, from provider.Protocol, body []byte, req *Request, isRouterCall bool, cands []candidate, pl planned) ([]candidate, planned) {
	if routingOf(g.Routing) != provider.EdgeCloud || len(cands) < 2 || isRouterCall || isEdgeCloudAssess(ctx) {
		return cands, pl
	}
	localCands, cloudCands, localOrder, cloudOrder := splitEdgeCloudCandidates(cands, pl)
	if len(localCands) == 0 || len(cloudCands) == 0 {
		return cands, pl
	}

	cloudFirst := func() ([]candidate, planned) {
		out := append(slices.Clone(cloudCands), localCands...)
		if len(localOrder)+len(cloudOrder) == len(cands) {
			pl.order = append(slices.Clone(cloudOrder), localOrder...)
		}
		return out, pl
	}
	localFirst := func() ([]candidate, planned) {
		out := append(slices.Clone(localCands), cloudCands...)
		if len(localOrder)+len(cloudOrder) == len(cands) {
			pl.order = append(slices.Clone(localOrder), cloudOrder...)
		}
		return out, pl
	}

	// Stage 1: Fast Filter
	goCloud, parsedReq := edgeCloudFastFilter(from, body, req)
	if goCloud {
		return cloudFirst()
	}

	// Stage 2: Edge Difficulty Assessment
	decision, err := s.assessEdgeCloudDifficulty(ctx, localCands[0], parsedReq)
	if err != nil || decision != "local" {
		return cloudFirst()
	}

	// Stage 3: Simple task -> Local first, Cloud candidates behind as automatic fallback
	return localFirst()
}
