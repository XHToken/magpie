package routerservice

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/yetone/magpie/internal/dirs"
	"github.com/yetone/magpie/internal/routerconfig"
	"github.com/yetone/magpie/internal/routermanifest"
	"github.com/yetone/magpie/internal/routerredact"
	"github.com/yetone/magpie/internal/routerruntime"
	"github.com/yetone/magpie/internal/testenv"
)

func TestLinuxNativeKeyringHTTPHistory(t *testing.T) {
	if os.Getenv("WEFTO_TEST_PRIVATE_KEYRING") != "1" {
		t.Skip("requires the isolated native Secret Service acceptance script")
	}
	t.Setenv(testenv.Marker, "")
	invoker := &currentPromptInvoker{t: t}
	layout := dirs.Layout{State: t.TempDir()}
	redactor := newRedactionService(&LocalModels{Layout: layout})
	redactor.binding = routerredact.ModelBinding{ModelID: "spark-x2.6-4B", RuntimeID: routerruntime.PlatformRuntime(routermanifest.Fixed(), "linux", "amd64")}
	spec, err := routerredact.NewEngineSpecification(redactor.binding, false)
	if err != nil || redactor.resultsError != nil || redactor.results == nil {
		t.Fatal("native keyring service unavailable", err, redactor.resultsError)
	}
	redactor.invoker = redactionFixtureInvoker{invoker, spec}
	t.Cleanup(redactor.close)
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body bytes.Buffer
		body.ReadFrom(r.Body)
		if bytes.Contains(body.Bytes(), []byte("13800001234")) {
			t.Error("private phone leaked in actual outgoing HTTP request")
		}
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(fixtureEchoTokens(body.Bytes(), `{"choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"<PHONE:1>"}}]}`)))
	}))
	defer upstream.Close()
	h, store := newGroupRoutingEnv(t, ModelOptions{Redaction: redactor, Routing: &RoutingOptions{LocalInvoker: invoker}})
	provider := saveTestCloudProvider(t, store, "native-keyring", upstream.URL+"/v1", "fixture")
	if _, err := store.SaveAppWithRedactionCapabilityContext(context.Background(), routerconfig.AppEdit{ID: "codex", Name: "Codex", CredentialMode: routerconfig.StoredCredential, Cloud: routerconfig.AppCloud{ProviderID: provider.ID, Model: "fixture"}, RedactionEnabled: true}, routerconfig.RedactionAvailable); err != nil {
		t.Fatal(err)
	}
	for turn := 0; turn < 2; turn++ {
		messages := []any{map[string]string{"role": "user", "content": "电话13800001234"}}
		if turn == 1 {
			messages = append(messages, map[string]string{"role": "assistant", "content": "13800001234"}, map[string]string{"role": "user", "content": "继续"})
		}
		response := doGroupRequest(t, h, "", http.MethodPost, "/app/codex/v1/chat/completions", map[string]any{"model": "fixture", "messages": messages})
		if response.Code != 200 || !strings.Contains(response.Body.String(), "13800001234") {
			t.Fatal("native keyring redaction HTTP failed", response.Code, response.Body.String())
		}
	}
	if calls.Load() != 2 || len(invoker.texts) != 2 || invoker.texts[1][0] != "继续" {
		t.Fatal("history was rescanned or cloud calls missing", calls.Load(), len(invoker.texts))
	}
}
