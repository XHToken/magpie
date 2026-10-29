package routerservice

import (
	"context"
	"encoding/json"
	"github.com/yetone/magpie/internal/routercloud"
	"github.com/yetone/magpie/internal/routerconfig"
	"github.com/yetone/magpie/internal/routerruntime"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

type userScopeInvoker struct {
	t     *testing.T
	calls int
}

func (v *userScopeInvoker) Invoke(ctx context.Context, purpose routerruntime.Purpose, body []byte, consume func(*http.Response) error) error {
	v.calls++
	if strings.Contains(string(body), "NON_USER_SENTINEL") {
		v.t.Error("non-user context entered local model request")
	}
	return consume(&http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(strictRedactionFixture(body, map[string]string{"Elodie": "private_person"}))))})
}
func TestUserInputModelReceivesOnlyOriginalUserText(t *testing.T) {
	v := &userScopeInvoker{t: t}
	opts := redactionTestOptions(t, ModelOptions{Routing: &RoutingOptions{LocalInvoker: v}})
	req := RedactionRequest{RequestID: "scope", AppID: "codex", SourceProtocol: "chat", TargetProtocol: "chat", Operation: "generation", Body: []byte(`{"model":"fixture","messages":[{"role":"system","content":"NON_USER_SENTINEL Elodie"},{"role":"user","content":"Contact Elodie"},{"role":"assistant","content":"NON_USER_SENTINEL Elodie"}]}`)}
	p, err := opts.Redaction.Prepare(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	defer closePreparedCloud(p)
	if !strings.Contains(string(p.Body), `NON_USER_SENTINEL Elodie`) || strings.Contains(string(p.Body), `Contact Elodie`) {
		t.Fatal("source scope was not preserved", string(p.Body))
	}
}

func TestUserInputRequestSnapshotReusedAcrossTargets(t *testing.T) {
	v := &userScopeInvoker{t: t}
	options := redactionTestOptions(t, ModelOptions{Routing: &RoutingOptions{LocalInvoker: v}})
	work := &requestRedactionWork{plan: requestRedactionPlan{Required: true}}
	ctx := context.WithValue(context.Background(), requestRedactionKey{}, work)
	source := []byte(`{"model":"m","messages":[{"role":"system","content":"NON_USER_SENTINEL Elodie"},{"role":"user","content":"Contact Elodie"}]}`)
	for _, protocol := range []string{"chat", "responses", "anthropic"} {
		wire, e := routercloud.PrepareRequestBody(routercloud.Request{Protocol: "chat", Body: source, Target: routercloud.Target{Model: "m", Provider: routerconfig.ResolvedProvider{Config: routerconfig.CloudProvider{Protocol: protocol, BaseURL: "http://127.0.0.1/v1"}}}}, false)
		if e != nil {
			t.Fatal(e)
		}
		target, e := wire.UserInputTarget()
		if e != nil {
			t.Fatal(e)
		}
		req := RedactionRequest{RequestID: "targets", AppID: "codex", SourceProtocol: "chat", TargetProtocol: protocol, Operation: "generation", Body: wire.Body(), SourceBody: source, UserTarget: target}
		prepared, e := prepareRequiredRedaction(ctx, options, options.Redaction, RoutingVersions{}, req)
		if e != nil {
			t.Fatal(e)
		}
		closePreparedCloud(prepared)
	}
	defer work.userSnapshot.Close()
	if v.calls != 1 || options.Redaction.Stats().Processed != 1 {
		t.Fatal("target fallback repeated detection", v.calls, options.Redaction.Stats())
	}
}
func TestUserInputSessionHandleCannotBeForgedOrShared(t *testing.T) {
	r := newRedactionService(nil)
	defer r.Close()
	first := r.requestSession("app-a", "")
	if !first.Verified {
		t.Fatal("server handle not issued")
	}
	same := r.requestSession("app-a", first.ID)
	if same.ID != first.ID {
		t.Fatal("server identity lost")
	}
	other := r.requestSession("app-b", first.ID)
	if other.ID == first.ID {
		t.Fatal("cross-application session accepted")
	}
	forged := r.requestSession("app-a", "arbitrary-session")
	if forged.ID == "arbitrary-session" {
		t.Fatal("unverified declaration accepted")
	}
	otherService := newRedactionService(nil)
	defer otherService.Close()
	if otherService.requestSession("app-a", first.ID).ID == first.ID {
		t.Fatal("restarted service inherited a handle")
	}
	for _, body := range []string{`{"session_id":"claimed","messages":[{"role":"user","content":"same"}]}`, `{"messages":[{"role":"user","content":"same"},{"role":"assistant","content":"history"}]}`} {
		id, verified, seed := parseRedactionSessionMaterial("app-a", "chat", []byte(body))
		if id != "" || verified || seed != "" {
			t.Fatal("client declaration granted private cache")
		}
	}
}
func TestUserInputLargeToolsDoNotConsumeScanBudget(t *testing.T) {
	v := &userScopeInvoker{t: t}
	options := redactionTestOptions(t, ModelOptions{Routing: &RoutingOptions{LocalInvoker: v}})
	tools := []any{}
	for i := 0; i < 200; i++ {
		tools = append(tools, map[string]any{"type": "function", "function": map[string]any{"name": "tool_" + strings.Repeat("x", i%10), "description": "NON_USER_SENTINEL", "parameters": map[string]any{"type": "object", "properties": map[string]any{"argument": map[string]any{"type": "string", "description": "NON_USER_SENTINEL"}}}}})
	}
	body, _ := json.Marshal(map[string]any{"model": "m", "messages": []any{map[string]any{"role": "system", "content": strings.Repeat("NON_USER_SENTINEL ", 4000)}, map[string]any{"role": "user", "content": "Contact Elodie"}}, "tools": tools})
	prepared, e := options.Redaction.Prepare(context.Background(), RedactionRequest{RequestID: "large-tools", AppID: "codex", SourceProtocol: "chat", TargetProtocol: "chat", Operation: "generation", Body: body})
	if e != nil {
		t.Fatal(e)
	}
	defer closePreparedCloud(prepared)
	if v.calls != 1 || prepared.ObservationMeta.NewUserCharacters != 14 || prepared.ObservationMeta.NewUserChunks != 1 {
		t.Fatal("non-user work counted", v.calls, prepared.ObservationMeta)
	}
}
func TestUserInputNoTextDoesNotInvokeUnavailableModel(t *testing.T) {
	options := redactionTestOptions(t, ModelOptions{Routing: &RoutingOptions{LocalInvoker: &userScopeInvoker{t: t}}})
	p, e := options.Redaction.Prepare(context.Background(), RedactionRequest{RequestID: "empty", AppID: "codex", SourceProtocol: "chat", TargetProtocol: "chat", Operation: "generation", Body: []byte(`{"model":"m","messages":[{"role":"assistant","content":"NON_USER_SENTINEL"}]}`)})
	if e != nil {
		t.Fatal(e)
	}
	defer closePreparedCloud(p)
	if p.ObservationMeta.ScanDisposition != "no_user_input" || p.ObservationMeta.LocalInferenceCalls != 0 {
		t.Fatal(p.ObservationMeta)
	}
}

type unavailableUserInvoker struct{ *userScopeInvoker }

func (v unavailableUserInvoker) Require(routerruntime.Purpose) error { return ErrRedactionUnavailable }
func TestUserInputNoTextBypassesInferenceAdmission(t *testing.T) {
	v := &userScopeInvoker{t: t}
	options := redactionTestOptions(t, ModelOptions{Routing: &RoutingOptions{LocalInvoker: unavailableUserInvoker{v}}})
	req := RedactionRequest{RequestID: "unavailable", AppID: "codex", SourceProtocol: "chat", TargetProtocol: "chat", Operation: "generation", Body: []byte(`{"model":"m","messages":[{"role":"assistant","content":"NON_USER_SENTINEL"}]}`)}
	p, e := options.Redaction.Prepare(context.Background(), req)
	if e != nil {
		t.Fatal(e)
	}
	closePreparedCloud(p)
	req.Body = []byte(`{"model":"m","messages":[{"role":"user","content":"must scan"}]}`)
	if _, e := options.Redaction.Prepare(context.Background(), req); e == nil {
		t.Fatal("unavailable detector allowed new user text")
	}
	if v.calls != 0 {
		t.Fatal("empty user plan invoked model")
	}
}

func TestUserInputExtraBodyConflictHasNoCloudAttempt(t *testing.T) {
	var calls atomic.Int32
	cloud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(500) }))
	defer cloud.Close()
	v := &userScopeInvoker{t: t}
	options := redactionTestOptions(t, ModelOptions{Routing: &RoutingOptions{LocalInvoker: v}})
	h, store := newGroupRoutingEnv(t, options)
	key := "fixture"
	extra := map[string]json.RawMessage{"user": json.RawMessage(`"Contact Elodie"`)}
	provider, e := store.SaveCloudProvider(routerconfig.ProviderEdit{Name: "conflict", Protocol: "chat", BaseURL: cloud.URL + "/v1", APIKey: &key, ExtraBody: &extra, ProxyMode: "direct"})
	if e != nil {
		t.Fatal(e)
	}
	_, e = store.SaveAppWithRedactionCapabilityContext(context.Background(), routerconfig.AppEdit{ID: "codex", Name: "Codex", Cloud: routerconfig.AppCloud{ProviderID: provider.ID, Model: "m"}, CredentialMode: routerconfig.StoredCredential, RedactionEnabled: true}, routerconfig.RedactionAvailable)
	if e != nil {
		t.Fatal(e)
	}
	response := doGroupRequest(t, h, "", http.MethodPost, "/app/codex/v1/chat/completions", map[string]any{"model": "m", "messages": []any{map[string]any{"role": "user", "content": "Contact Elodie"}}})
	if response.Code != 503 || !strings.Contains(response.Body.String(), "redaction_unavailable") || calls.Load() != 0 || v.calls != 0 {
		t.Fatal("scope conflict escaped gate", response.Code, response.Body.String(), calls.Load(), v.calls)
	}
}

func TestUserInputNativeProtocolsExcludeToolContext(t *testing.T) {
	cases := []struct{ protocol, body string }{
		{"anthropic", `{"model":"m","max_tokens":32,"system":"NON_USER_SENTINEL Elodie","tools":[{"name":"f","description":"NON_USER_SENTINEL","input_schema":{"type":"object"}}],"messages":[{"role":"user","content":"Contact Elodie"},{"role":"assistant","content":[{"type":"tool_use","id":"call","name":"f","input":{"x":"NON_USER_SENTINEL"}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"call","content":"NON_USER_SENTINEL Elodie"},{"type":"text","text":"Continue Elodie"}]}]}`},
		{"responses", `{"model":"m","instructions":"NON_USER_SENTINEL Elodie","tools":[{"type":"function","name":"f","description":"NON_USER_SENTINEL","parameters":{"type":"object"}}],"input":[{"role":"user","content":[{"type":"input_text","text":"Contact Elodie"}]},{"type":"function_call","id":"fc","call_id":"call","name":"f","arguments":"{\"x\":\"NON_USER_SENTINEL\"}"},{"type":"function_call_output","call_id":"call","output":"NON_USER_SENTINEL Elodie"}]}`},
	}
	for _, test := range cases {
		t.Run(test.protocol, func(t *testing.T) {
			v := &userScopeInvoker{t: t}
			options := redactionTestOptions(t, ModelOptions{Routing: &RoutingOptions{LocalInvoker: v}})
			p, e := options.Redaction.Prepare(context.Background(), RedactionRequest{RequestID: "protocol-scope", AppID: "codex", SourceProtocol: test.protocol, TargetProtocol: test.protocol, Operation: "generation", Body: []byte(test.body)})
			if e != nil {
				t.Fatal(e)
			}
			defer closePreparedCloud(p)
			if v.calls != 1 || strings.Contains(string(p.Body), "Contact Elodie") || !strings.Contains(string(p.Body), "NON_USER_SENTINEL Elodie") {
				t.Fatal("original protocol scope lost", v.calls, string(p.Body))
			}
		})
	}
}
