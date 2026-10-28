package routerservice

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/yetone/magpie/internal/routerconfig"
)

func TestInstalledHostFieldsHTTPDirectAndRoutedJSONAndSSE(t *testing.T) {
	raw, err := os.ReadFile("../routercloud/testdata/host-fields/installed-contract.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures struct {
		Cases []struct {
			ID, Host string
			Body     json.RawMessage
		}
	}
	if json.Unmarshal(raw, &fixtures) != nil || len(fixtures.Cases) == 0 {
		t.Fatal("invalid host field contract")
	}
	for _, mode := range []string{"direct", "cloud-judgment", "unavailable-judgment"} {
		t.Run(mode, func(t *testing.T) {
			var expected atomic.Value
			var calls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				wire, _ := io.ReadAll(r.Body)
				var got map[string]any
				if json.Unmarshal(wire, &got) != nil || !reflect.DeepEqual(got, expected.Load()) {
					t.Error("host field changed through product routing/native forwarding")
				}
				w.Header().Set("Content-Type", "application/json")
				io.WriteString(w, cloudHTTPReply("chat", false))
			}))
			defer upstream.Close()
			options := ModelOptions{}
			if mode != "direct" {
				judge := &scriptedJudge{decision: DecisionCloud}
				if mode == "unavailable-judgment" {
					judge.err = ErrJudgmentFailed
				}
				options.Routing = &RoutingOptions{Judge: judge}
			}
			h := cloudHTTPObservedHandler(t, options)
			cloudHTTPBind(t, h, upstream.URL, "chat", "workbuddy", "host-field-provider-fixture")
			draft := providerInputFor(upstream.URL + "/v1")
			key := "loomy-field-provider-fixture"
			draft.Protocol, draft.APIKey = "chat", &key
			provider := providerSaved(t, providerRequest(t, h, "POST", "/api/cloud-providers", cloudHTTPManagement, draft))
			if bound := providerRequest(t, h, "POST", "/api/apps", cloudHTTPManagement, routerconfig.AppEdit{ID: "loomy", Name: "Loomy", CredentialMode: routerconfig.StoredCredential, Cloud: routerconfig.AppCloud{ProviderID: provider.ID, Model: "profile-pinned"}}); bound.Code != 200 {
				t.Fatal("stored Loomy fixture binding failed")
			}
			product := httptest.NewServer(h)
			defer product.Close()
			var count int32
			for _, fixture := range fixtures.Cases {
				apps := []string{"workbuddy"}
				if fixture.Host == "loomy" {
					apps = []string{"loomy"}
				} else if fixture.Host == "both" {
					apps = []string{"workbuddy", "loomy"}
				}
				for _, app := range apps {
					for _, stream := range []bool{false, true} {
						var body map[string]any
						_ = json.Unmarshal(fixture.Body, &body)
						body["model"], body["stream"] = "profile-pinned", stream
						expected.Store(body)
						status, header, reply := workBuddySocketCall(t, product, app, body, "")
						count++
						if status != 200 || header.Get("X-Request-ID") == "" || !bytes.Contains(reply, []byte("hello")) || stream && !bytes.Contains(reply, []byte("[DONE]")) {
							t.Fatalf("%s app=%s stream=%v rejected: status=%d", fixture.ID, app, stream, status)
						}
					}
				}
			}
			if calls.Load() != count {
				t.Fatalf("expected one pinned upstream attempt per host request: calls=%d requests=%d", calls.Load(), count)
			}
		})
	}
}

func TestWorkBuddyHTTPTenCompletedTurnsRetainGrowingHistory(t *testing.T) {
	raw, err := os.ReadFile("../routercloud/testdata/host-fields/installed-contract.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures struct {
		Cases []struct {
			ID   string
			Body struct{ Messages []json.RawMessage }
		}
	}
	if err := json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatal(err)
	}
	var assistantTemplate json.RawMessage
	for _, fixture := range fixtures.Cases {
		if fixture.ID == "workbuddy-5.8.3-second-turn-step-seq" {
			assistantTemplate = fixture.Body.Messages[1]
		}
	}
	if len(assistantTemplate) == 0 {
		t.Fatal("missing installed completed-turn metadata fixture")
	}
	for _, mode := range []string{"direct", "cloud-judgment", "unavailable-judgment"} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%v", mode, stream), func(t *testing.T) {
				var expected atomic.Value
				var calls atomic.Int32
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					turn := calls.Add(1)
					var got map[string]any
					if json.NewDecoder(r.Body).Decode(&got) != nil || !reflect.DeepEqual(got, expected.Load()) {
						t.Errorf("turn %d changed accumulated history or host metadata", turn)
					}
					marker := fmt.Sprintf("fixture-turn-%d", turn)
					if stream {
						w.Header().Set("Content-Type", "text/event-stream")
						fmt.Fprintf(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":%q}}]}\n\ndata: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":2,\"total_tokens\":5}}\n\ndata: [DONE]\n\n", marker)
					} else {
						w.Header().Set("Content-Type", "application/json")
						fmt.Fprintf(w, "{\"choices\":[{\"index\":0,\"message\":{\"role\":\"assistant\",\"content\":%q},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":2,\"total_tokens\":5}}", marker)
					}
				}))
				defer upstream.Close()
				options := ModelOptions{}
				if mode != "direct" {
					judge := &scriptedJudge{decision: DecisionCloud}
					if mode == "unavailable-judgment" {
						judge.err = ErrJudgmentFailed
					}
					options.Routing = &RoutingOptions{Judge: judge}
				}
				h := cloudHTTPObservedHandler(t, options)
				cloudHTTPBind(t, h, upstream.URL, "chat", "workbuddy", "ten-turn-provider-fixture")
				product := httptest.NewServer(h)
				defer product.Close()
				var history []any
				for turn := 1; turn <= 10; turn++ {
					history = append(history, map[string]any{"role": "user", "content": fmt.Sprintf("fixture-user-%d", turn)})
					body := map[string]any{"model": "profile-pinned", "stream": stream, "messages": history}
					encoded, err := json.Marshal(body)
					if err != nil {
						t.Fatal(err)
					}
					var want map[string]any
					if err := json.Unmarshal(encoded, &want); err != nil {
						t.Fatal(err)
					}
					expected.Store(want)
					status, header, reply := workBuddySocketCall(t, product, "workbuddy", body, "")
					if status != 200 || header.Get("X-Request-ID") == "" || stream && !bytes.Contains(reply, []byte("[DONE]")) {
						t.Fatalf("turn %d with %d historical assistant messages failed: HTTP %d", turn, turn-1, status)
					}
					parts := [][]byte{reply}
					if stream {
						parts = bytes.Split(reply, []byte("\n"))
					}
					var text string
					for _, part := range parts {
						if stream {
							if !bytes.HasPrefix(part, []byte("data: ")) || bytes.Equal(part, []byte("data: [DONE]")) {
								continue
							}
							part = bytes.TrimPrefix(part, []byte("data: "))
						}
						var output struct {
							Choices []struct {
								Message, Delta struct{ Content string }
							}
						}
						if err := json.Unmarshal(part, &output); err != nil {
							t.Fatal(err)
						}
						for _, choice := range output.Choices {
							text += choice.Message.Content + choice.Delta.Content
						}
					}
					if text != fmt.Sprintf("fixture-turn-%d", turn) {
						t.Fatalf("turn %d returned wrong response content", turn)
					}
					var assistant map[string]any
					if err := json.Unmarshal(assistantTemplate, &assistant); err != nil {
						t.Fatal(err)
					}
					assistant["content"] = []any{map[string]any{"type": "text", "text": text, "annotations": []any{}}}
					assistant["stepSeq"] = turn
					assistant["messageId"] = fmt.Sprintf("fixture-message-%d", turn)
					history = append(history, assistant)
				}
				if calls.Load() != 10 {
					t.Fatalf("expected ten upstream calls, got %d", calls.Load())
				}
			})
		}
	}
}
