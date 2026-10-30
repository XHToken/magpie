package routerservice

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/yetone/magpie/internal/routerconfig"
)

func TestHistoryPreparationAPIRequiresExplicitSelectionAndNoCloud(t *testing.T) {
	invoker := &userScopeInvoker{t: t}
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		raw, _ := io.ReadAll(r.Body)
		if strings.Contains(string(raw), "Elodie") {
			t.Error("history leaked upstream")
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, fixtureEchoTokens(raw, `{"choices":[{"message":{"role":"assistant","content":"<NAME:1>"},"finish_reason":"stop"}]}`))
	}))
	defer upstream.Close()
	h, store := newGroupRoutingEnv(t, ModelOptions{Routing: &RoutingOptions{LocalInvoker: invoker}})
	provider := saveTestCloudProvider(t, store, "history", upstream.URL+"/v1", "fixture")
	if _, err := store.SaveAppWithRedactionCapabilityContext(context.Background(), routerconfig.AppEdit{ID: "codex", Name: "Codex", CredentialMode: routerconfig.StoredCredential, Cloud: routerconfig.AppCloud{ProviderID: provider.ID, Model: "fixture"}, RedactionEnabled: true}, routerconfig.RedactionAvailable); err != nil {
		t.Fatal(err)
	}
	body := map[string]any{"model": "fixture", "messages": []any{map[string]string{"role": "user", "content": "Contact Elodie"}, map[string]string{"role": "assistant", "content": "Elodie"}, map[string]string{"role": "user", "content": "continue"}}}
	blocked := doGroupRequest(t, h, "", http.MethodPost, "/app/codex/v1/chat/completions", body)
	if blocked.Code != 409 || !strings.Contains(blocked.Body.String(), "redaction_history_prepare_required") || calls.Load() != 0 || invoker.calls != 0 {
		t.Fatal("unknown history auto-scanned or sent", blocked.Code, blocked.Body.String())
	}
	payload := map[string]any{"action": "create", "appId": "codex", "protocol": "chat", "body": body, "selected": []int{0}}
	denied := doGroupRequest(t, h, "", http.MethodPost, "/api/redaction/history", payload)
	if denied.Code != 401 {
		t.Fatal("management authorization bypassed")
	}
	created := doGroupRequest(t, h, "mgmt-secret-token", http.MethodPost, "/api/redaction/history", payload)
	var task struct {
		ID            string
		CloudAttempts int
		Progress      struct {
			CompletedMessages, TotalMessages int
			Done                             bool
		}
	}
	if json.Unmarshal(created.Body.Bytes(), &task) != nil || created.Code != 200 || task.ID == "" || task.Progress.TotalMessages != 1 || invoker.calls != 0 {
		t.Fatal("creation performed inference or lost selection", created.Code, created.Body.String())
	}
	step := doGroupRequest(t, h, "mgmt-secret-token", http.MethodPost, "/api/redaction/history", map[string]any{"action": "step", "appId": "codex", "id": task.ID})
	if step.Code != 200 || invoker.calls != 1 || calls.Load() != 0 {
		t.Fatal("local preparation escaped bounds", step.Code, step.Body.String())
	}
	accepted := doGroupRequest(t, h, "", http.MethodPost, "/app/codex/v1/chat/completions", body)
	if accepted.Code != 200 || invoker.calls != 2 || calls.Load() != 1 || !strings.Contains(accepted.Body.String(), "Elodie") {
		t.Fatal("prepared history was rescanned or failed restoration", accepted.Code, accepted.Body.String(), invoker.calls)
	}
}
