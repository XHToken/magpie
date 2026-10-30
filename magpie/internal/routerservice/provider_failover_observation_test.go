package routerservice

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/yetone/magpie/internal/routerconfig"
	"github.com/yetone/magpie/internal/routerledger"
)

func TestProviderFailoverRecordsFinalError(t *testing.T) {
	for _, succeeds := range []bool{true, false} {
		name := "final_failure"
		if succeeds {
			name = "final_success"
		}
		t.Run(name, func(t *testing.T) {
			const model = "qwen-3.8-flash"
			var firstCalls, secondCalls atomic.Int32
			first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				firstCalls.Add(1)
				w.Header().Set("Content-Type", "application/json")
				// A malformed response fails before any client output is committed.
				_, _ = w.Write([]byte(`{"choices":`))
			}))
			defer first.Close()
			second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				secondCalls.Add(1)
				w.Header().Set("Content-Type", "application/json")
				if !succeeds {
					w.WriteHeader(http.StatusUnauthorized)
					_, _ = w.Write([]byte(`{"error":{"message":"invalid key"}}`))
					return
				}
				_, _ = w.Write([]byte(`{"id":"chatcmpl-final","object":"chat.completion","model":"qwen-3.8-flash","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`))
			}))
			defer second.Close()

			ledger, err := routerledger.Open(filepath.Join(t.TempDir(), "requests.sqlite"), routerledger.Options{})
			if err != nil {
				t.Fatal(err)
			}
			defer ledger.Close()
			h, store := newGroupRoutingEnv(t, ModelOptions{Ledger: ledger})
			p1 := saveTestCloudProvider(t, store, "First", first.URL+"/v1", model)
			p2 := saveTestCloudProvider(t, store, "Second", second.URL+"/v1", model)
			if _, err := store.SaveGroup(routerconfig.Group{
				ID: "same-model-failover", Name: "Same model failover", Routing: "order",
				Members: []string{p1.ID + "/" + model, p2.ID + "/" + model},
			}); err != nil {
				t.Fatal(err)
			}
			saveGroupBoundApp(t, store, []string{"group/same-model-failover"})
			response := postAppChat(t, h, "group/same-model-failover")
			wantStatus, wantCode, wantHTTP := "failed", "cloud_auth_error", http.StatusUnauthorized
			if succeeds {
				wantStatus, wantCode, wantHTTP = "succeeded", "", http.StatusOK
			}
			if firstCalls.Load() != 1 || secondCalls.Load() != 1 || response.Code != wantHTTP {
				t.Fatalf("failover: first=%d second=%d HTTP=%d body=%s", firstCalls.Load(), secondCalls.Load(), response.Code, response.Body.String())
			}
			// Read the same management projection used by the request table.
			management := doGroupRequest(t, h, "mgmt-secret-token", http.MethodGet, "/api/requests/"+response.Header().Get("X-Request-ID"), nil)
			var detail struct {
				Request routerledger.Record `json:"request"`
			}
			if management.Code != http.StatusOK || json.Unmarshal(management.Body.Bytes(), &detail) != nil {
				t.Fatalf("request detail: HTTP=%d body=%s", management.Code, management.Body.String())
			}
			record := detail.Request
			if record.Status != wantStatus || record.SafeErrorCode != wantCode || record.ProviderID != p2.ID || record.Attempts != 2 || len(record.AttemptDetails) != 2 {
				t.Fatalf("expected final %s with error %q from second provider: %+v", wantStatus, wantCode, record)
			}
			if record.AttemptDetails[0].ErrorCode != "cloud_response_error" || !record.AttemptDetails[0].Retried || record.AttemptDetails[1].ErrorCode != wantCode {
				t.Fatalf("attempt errors must remain separate from the final error: %+v", record.AttemptDetails)
			}
			if succeeds && (record.FailureStage != "" || record.CapabilityState != "" || record.Usage == nil || record.Usage.TotalTokens == nil || *record.Usage.TotalTokens != 15) {
				t.Fatalf("successful failover retained a failure or lost final usage: %+v", record)
			}
		})
	}
}
