package routerservice

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/routerconfig"
	"github.com/yetone/magpie/internal/routermanifest"
	"github.com/yetone/magpie/internal/routerruntime"
)

func TestInheritedGroupRedactionSuccessRestoresAndProtectsFallback(t *testing.T) {
	var calls atomic.Int32
	var leaks atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), "宁祁") || !strings.Contains(string(body), "<NAME:1>") {
			leaks.Add(1)
		}
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"已收到<NAME:1>"},"finish_reason":"stop"}]}`)
	}))
	defer upstream.Close()
	invoker := &dynamicPayloadInvoker{}
	invoker.set(200, `{"choices":[{"finish_reason":"stop","message":{"content":"{\"entities\":[{\"label\":\"NAME\",\"value\":\"宁祁\"}]}"}}]}`)
	h, store := newGroupRoutingEnv(t, ModelOptions{Routing: &RoutingOptions{LocalInvoker: redactionOnlyInvoker{invoker}}})
	p := saveTestCloudProvider(t, store, "FirstCloud", upstream.URL+"/v1", "probe")
	q := saveTestCloudProvider(t, store, "SecondCloud", upstream.URL+"/v1", "probe")
	_, err := store.SaveGroup(routerconfig.Group{ID: "child", Name: "Child", LocalRedaction: true, Members: []string{"local/spark-x2.6-4b", p.ID + "/probe"}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.SaveGroup(routerconfig.Group{ID: "parent", Name: "Parent", Routing: "order", Members: []string{"group/child", q.ID + "/probe"}})
	if err != nil {
		t.Fatal(err)
	}
	saveGroupBoundApp(t, store, []string{"group/parent"})
	rec := doGroupRequest(t, h, "", http.MethodPost, "/app/codex/v1/chat/completions", map[string]any{"model": "group/parent", "messages": []map[string]string{{"role": "user", "content": "宁祁的内部记录"}}})
	if rec.Code != 200 || calls.Load() != 2 || leaks.Load() != 0 || !strings.Contains(rec.Body.String(), "宁祁") || strings.Contains(rec.Body.String(), "<NAME:1>") {
		t.Fatalf("protected fallback/restoration: status=%d calls=%d leaks=%d body=%s", rec.Code, calls.Load(), leaks.Load(), rec.Body.String())
	}
}

func TestGroupPrivacyConflictAndUnavailableIntentRemainVisible(t *testing.T) {
	h, store := newGroupRoutingEnv(t, ModelOptions{})
	for _, group := range []routerconfig.Group{
		{ID: "one", Name: "One", LocalRedaction: true, Members: []string{"local/spark-x2.6-4b", "cloud/probe"}},
		{ID: "two", Name: "Two", LocalRedaction: true, Members: []string{"local/spark-x2.6-4b-q8_0", "cloud/probe"}},
		{ID: "parent", Name: "Parent", Members: []string{"group/one", "group/two"}},
	} {
		if _, err := store.SaveGroup(group); err != nil {
			t.Fatal(err)
		}
	}
	groups, _ := store.ListGroups()
	v := describeGroupRedaction(groups[2], groups)
	if v.State != "invalid" || v.ReasonCode != "redaction_policy_conflict" {
		t.Fatalf("conflict=%+v", v)
	}
	// Losing the local member must preserve intent through both storage and HTTP.
	rec := doGroupRequest(t, h, "mgmt-secret-token", http.MethodPost, "/api/routing-groups", map[string]any{"id": "one", "name": "One", "members": []string{"cloud/probe"}, "localRedaction": true})
	if rec.Code != 200 {
		t.Fatalf("save existing intent: %d %s", rec.Code, rec.Body.String())
	}
	var response struct {
		Group groupView `json:"group"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !response.Group.LocalRedaction || response.Group.RedactionPolicy.ReasonCode != "redaction_model_missing" {
		t.Fatalf("intent lost: %+v", response.Group)
	}
}

func TestGroupRedactionPreviewIsReadOnly(t *testing.T) {
	h, store := newGroupRoutingEnv(t, ModelOptions{})
	_, err := store.SaveGroup(routerconfig.Group{ID: "child", Name: "Child", LocalRedaction: true, Members: []string{"local/spark-x2.6-4b", "cloud/probe"}})
	if err != nil {
		t.Fatal(err)
	}
	before, _ := store.ListGroups()
	rec := doGroupRequest(t, h, "mgmt-secret-token", http.MethodPost, "/api/routing-groups?preview=1", map[string]any{"id": "draft", "name": "Draft", "members": []string{"group/child"}})
	var response struct {
		Group groupView `json:"group"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	after, _ := store.ListGroups()
	if rec.Code != 200 || !response.Group.RedactionPolicy.InheritedRequired || len(before) != len(after) {
		t.Fatalf("preview=%d %+v before=%d after=%d", rec.Code, response.Group, len(before), len(after))
	}
}

func TestGroupRedactionPreviewIncludesPatternMembers(t *testing.T) {
	local := &LocalRuntime{Catalog: routermanifest.Catalog{Models: []routermanifest.Model{{ID: "spark-x2.6-4b"}}}}
	h, _, token := newRoutingGroupTestHandler(t, local)
	rec := doGroupRequest(t, h, token, http.MethodPost, "/api/routing-groups?preview=1", map[string]any{
		"id": "pattern", "name": "Pattern", "members": []string{"cloud/probe"}, "match": []string{"local/*"}, "localRedaction": true,
	})
	var response struct {
		Group groupView `json:"group"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	p := response.Group.RedactionPolicy
	if rec.Code != 200 || p.State != "required" || !p.HasLocalModel || p.ModelID != "spark-x2.6-4b" {
		t.Fatalf("preview ignored matched local member: %d %+v", rec.Code, p)
	}
}

type deadlineRedactor struct{ deadlines []time.Time }

type redactionOnlyInvoker struct{ LocalInvoker }

func (r redactionOnlyInvoker) Invoke(ctx context.Context, purpose routerruntime.Purpose, body []byte, fn func(*http.Response) error) error {
	if purpose != routerruntime.Redaction {
		return errors.New("local execution unavailable")
	}
	return r.LocalInvoker.Invoke(ctx, purpose, body, fn)
}
func (r *deadlineRedactor) Prepare(ctx context.Context, _ RedactionRequest) (PreparedCloudRequest, error) {
	deadline, _ := ctx.Deadline()
	r.deadlines = append(r.deadlines, deadline)
	return PreparedCloudRequest{Body: []byte(`{}`)}, nil
}

func TestRequiredRedactionSharesDeadlineAndPinsBinding(t *testing.T) {
	work := &requestRedactionWork{plan: requestRedactionPlan{Required: true}}
	ctx := context.WithValue(context.Background(), requestRedactionKey{}, work)
	r := &deadlineRedactor{}
	for range 2 {
		if _, err := prepareRequiredRedaction(ctx, ModelOptions{}, r, RoutingVersions{LocalModelFingerprint: "model1"}, RedactionRequest{}); err != nil {
			t.Fatal(err)
		}
	}
	if len(r.deadlines) != 2 || !r.deadlines[0].Equal(r.deadlines[1]) {
		t.Fatalf("renewed budget: %v", r.deadlines)
	}
	if _, err := prepareRequiredRedaction(ctx, ModelOptions{}, r, RoutingVersions{LocalModelFingerprint: "model2"}, RedactionRequest{}); err != ErrRedactionUnavailable || len(r.deadlines) != 2 {
		t.Fatalf("binding switch accepted: err=%v calls=%d", err, len(r.deadlines))
	}
}

func TestInheritedRedactionFailureStopsCandidateLoop(t *testing.T) {
	var calls, prepares atomic.Int32
	cloud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); w.WriteHeader(200) }))
	defer cloud.Close()
	redactor := r1ReviewRedactor(func(context.Context, RedactionRequest) (PreparedCloudRequest, error) {
		prepares.Add(1)
		return PreparedCloudRequest{}, errors.New("synthetic extraction failure")
	})
	h, store := newGroupRoutingEnv(t, ModelOptions{Routing: &RoutingOptions{Redactor: redactor}})
	p := saveTestCloudProvider(t, store, "TerminalPrivacy", cloud.URL+"/v1", "one", "two")
	for _, g := range []routerconfig.Group{
		{ID: "private", Name: "Private", LocalRedaction: true, Members: []string{"local/spark-x2.6-4b", p.ID + "/one"}},
		{ID: "parent", Name: "Parent", Routing: "order", Patience: 60, Members: []string{"group/private", p.ID + "/two"}},
	} {
		if _, err := store.SaveGroup(g); err != nil {
			t.Fatal(err)
		}
	}
	saveGroupBoundApp(t, store, []string{"group/parent"})
	rec := doGroupRequest(t, h, "", http.MethodPost, "/app/codex/v1/chat/completions", map[string]any{"model": "group/parent", "messages": []map[string]string{{"role": "user", "content": "private"}}})
	if rec.Code != 503 || calls.Load() != 0 || prepares.Load() != 1 {
		t.Fatalf("privacy failure retried: status=%d calls=%d prepares=%d", rec.Code, calls.Load(), prepares.Load())
	}
}

func TestConflictingGroupRedactionModelsRejectBeforeCloud(t *testing.T) {
	var calls atomic.Int32
	cloud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); w.WriteHeader(200) }))
	defer cloud.Close()
	h, store := newGroupRoutingEnv(t, ModelOptions{})
	p := saveTestCloudProvider(t, store, "ConflictingPrivacy", cloud.URL+"/v1", "probe")
	for _, g := range []routerconfig.Group{
		{ID: "q4", Name: "Q4", LocalRedaction: true, Members: []string{"local/spark-x2.6-4b", p.ID + "/probe"}},
		{ID: "q8", Name: "Q8", LocalRedaction: true, Members: []string{"local/spark-x2.6-4b-q8_0", p.ID + "/probe"}},
		{ID: "parent", Name: "Parent", Members: []string{"group/q4", "group/q8"}},
	} {
		if _, err := store.SaveGroup(g); err != nil {
			t.Fatal(err)
		}
	}
	saveGroupBoundApp(t, store, []string{"group/parent"})
	rec := doGroupRequest(t, h, "", http.MethodPost, "/app/codex/v1/chat/completions", map[string]any{"model": "group/parent", "messages": []map[string]string{{"role": "user", "content": "private"}}})
	if rec.Code != 409 || calls.Load() != 0 || !strings.Contains(rec.Body.String(), "redaction_policy_conflict") {
		t.Fatalf("conflict dispatched: status=%d calls=%d body=%s", rec.Code, calls.Load(), rec.Body.String())
	}
}

func TestNestedGroupRedactionNeverFallsBackToPlaintext(t *testing.T) {
	for _, scenario := range []string{"nested", "sibling", "duplicate-first", "rule"} {
		t.Run(scenario, func(t *testing.T) {
			var calls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				_, _ = io.Copy(io.Discard, r.Body)
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`)
			}))
			defer upstream.Close()
			h, store := newGroupRoutingEnv(t, ModelOptions{})
			p := saveTestCloudProvider(t, store, "PolicyProbe", upstream.URL+"/v1", "probe")
			cloud := p.ID + "/probe"
			if _, err := store.SaveGroup(routerconfig.Group{ID: "private-child", Name: "Private child", LocalRedaction: true,
				Members: []string{"local/spark-x2.6-4b", cloud}}); err != nil {
				t.Fatal(err)
			}
			parent := routerconfig.Group{ID: "parent", Name: "Parent", Routing: "smart_hybrid", Members: []string{"group/private-child"}}
			switch scenario {
			case "sibling":
				parent.Members = append(parent.Members, cloud)
			case "duplicate-first":
				parent.Members = append([]string{cloud}, parent.Members...)
			case "rule":
				parent.Members = []string{cloud}
				parent.Rules = []routerconfig.Rule{{When: "prompt", Pattern: "PRIVATE_CANARY", Use: "group/private-child"}}
			}
			if _, err := store.SaveGroup(parent); err != nil {
				t.Fatal(err)
			}
			saveGroupBoundApp(t, store, []string{"group/parent"})
			rec := doGroupRequest(t, h, "", http.MethodPost, "/app/codex/v1/chat/completions", map[string]any{
				"model": "group/parent", "messages": []map[string]string{{"role": "user", "content": "PRIVATE_CANARY"}},
			})
			if rec.Code != http.StatusServiceUnavailable || calls.Load() != 0 || !strings.Contains(rec.Body.String(), "redaction_unavailable") {
				t.Fatalf("nested privacy requirement was lost: status=%d cloudCalls=%d body=%s", rec.Code, calls.Load(), rec.Body.String())
			}
		})
	}
}
