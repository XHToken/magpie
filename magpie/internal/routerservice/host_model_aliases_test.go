package routerservice

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/yetone/magpie/internal/routerconfig"
	"github.com/yetone/magpie/internal/routerhost"
)

func TestWorkBuddyReadableSelectorsRouteExactTargets(t *testing.T) {
	h, store := newGroupRoutingEnv(t, ModelOptions{})
	calls := make(chan string, 8)
	makeProvider := func(name string) routerconfig.CloudProvider {
		t.Helper()
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var body struct{ Model string }
			if json.NewDecoder(r.Body).Decode(&body) != nil || body.Model != "deepseek-chat" {
				t.Error("readable host selector leaked into the upstream model")
			}
			calls <- name
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"id":"reply","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`)
		}))
		t.Cleanup(upstream.Close)
		return saveTestCloudProvider(t, store, name, upstream.URL+"/v1", "deepseek-chat")
	}
	first, second := makeProvider("primary"), makeProvider("secondary")
	models := []string{first.ID + "/deepseek-chat", second.ID + "/deepseek-chat"}
	names, err := hostModelNames(context.Background(), store, models, nil)
	if err != nil {
		t.Fatal(err)
	}
	o, aliases, err := workBuddyModelAliases(routerhost.Options{Models: models, ModelNames: names}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveApp(routerconfig.AppEdit{ID: "workbuddy", Name: "WorkBuddy", Models: &models, ModelAliases: aliases, CredentialMode: routerconfig.StoredCredential}); err != nil {
		t.Fatal(err)
	}
	request := func(model string) *httptest.ResponseRecorder {
		return doGroupRequest(t, h, "", http.MethodPost, "/app/workbuddy/v1/chat/completions", map[string]any{
			"model": model, "messages": []map[string]string{{"role": "user", "content": "hello"}},
		})
	}
	for i, alias := range o.Models {
		if w := request(alias); w.Code != 200 {
			t.Fatalf("readable model %q failed: %d %s", alias, w.Code, w.Body.String())
		}
		want := []string{"primary", "secondary"}[i]
		if got := <-calls; got != want {
			t.Fatalf("same-model providers were mixed up: got %s want %s", got, want)
		}
	}
	// Raw IDs used by existing sessions continue to work after migration.
	if w := request(models[1]); w.Code != 200 {
		t.Fatalf("legacy request failed: %s", w.Body.String())
	}
	if got := <-calls; got != "secondary" {
		t.Fatal("legacy request changed provider")
	}
	app := findSavedApp(context.Background(), store, "workbuddy")
	models = models[:1]
	if _, err := store.SaveApp(routerconfig.AppEdit{ID: app.ID, Name: app.Name, Revision: app.Revision, Models: &models, CredentialMode: app.CredentialMode}); err != nil {
		t.Fatal(err)
	}
	if w := request(o.Models[1]); w.Code != 400 {
		t.Fatalf("retired alias bypassed the saved selection: %d", w.Code)
	}
	if len(calls) != 0 {
		t.Fatal("retired alias reached an upstream")
	}
}

func TestWorkBuddyReadableSelectorCollisionsAndRenames(t *testing.T) {
	models := []string{"group/first", "group/second"}
	names := map[string]string{models[0]: "编程组", models[1]: "编程组"}
	o, aliases, err := workBuddyModelAliases(routerhost.Options{Models: models, ModelNames: names}, nil)
	if err != nil || o.Models[0] == o.Models[1] {
		t.Fatalf("same-name selections collided: %v %v", o.Models, err)
	}
	names[models[0]] = "日常编程"
	renamed, next, err := workBuddyModelAliases(routerhost.Options{Models: []string{models[1], models[0]}, ModelNames: names}, aliases)
	if err != nil || renamed.Models[1] != "日常编程" || next[o.Models[0]] != models[0] || next[o.Models[1]] != models[1] {
		t.Fatalf("rename or primary change lost cached selectors: %v %v", renamed.Models, err)
	}
}
