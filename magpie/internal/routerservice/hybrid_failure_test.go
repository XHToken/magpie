package routerservice

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/yetone/magpie/internal/routerconfig"
)

func TestAuditHybridInvalidCompletionMustBlock(t *testing.T) {
	envelope := func(content, finish string) string {
		raw, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"index": 0, "finish_reason": finish, "message": map[string]any{"role": "assistant", "content": content}}}})
		return string(raw)
	}
	cases := []struct {
		name    string
		raw     string
		blocked bool
	}{
		{"valid_complete_control", envelope(`{"entities":[{"value":"宁祁","label":"NAME"},{"value":"陆遥","label":"NAME"}]}`, "stop"), false},
		{"empty_content", envelope("", "stop"), true},
		{"malformed_json", envelope("not-json", "stop"), true},
		{"truncated_json", envelope(`{"entities":[`, "length"), true},
		{"truncated_partial_entities", envelope(`{"entities":[{"value":"宁祁","label":"NAME"}]}`, "length"), true},
		{"unknown_label", envelope(`{"entities":[{"value":"宁祁","label":"UNRECOGNIZED_KIND"}]}`, "stop"), true},
		{"invalid_outer_envelope", `{"choices":`, true},
		{"empty_choices", `{"choices":[]}`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			var leaked atomic.Bool
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				raw, _ := io.ReadAll(r.Body)
				if bytes.Contains(raw, []byte("宁祁")) || bytes.Contains(raw, []byte("陆遥")) {
					leaked.Store(true)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"id":"audit","object":"chat.completion","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"ok"}}]}`)
			}))
			defer upstream.Close()
			invoker := &scriptedInvoker{response: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(tc.raw))}}
			h, store := newGroupRoutingEnv(t, ModelOptions{Routing: &RoutingOptions{LocalInvoker: invoker}})
			provider := saveTestCloudProvider(t, store, "AuditLocal", upstream.URL+"/v1", "fixture-model")
			_, err := store.SaveGroup(routerconfig.Group{ID: "audit-redaction", Name: "Audit", Routing: "order", LocalRedaction: true, Members: []string{provider.ID + "/fixture-model", "local/spark-x2.5-1.7b"}})
			if err != nil {
				t.Fatal(err)
			}
			saveGroupBoundApp(t, store, []string{"group/audit-redaction"})
			response := doGroupRequest(t, h, "", http.MethodPost, "/app/codex/v1/chat/completions", map[string]any{"model": "group/audit-redaction", "messages": []any{map[string]string{"role": "user", "content": "请转告宁祁和陆遥明天到场，电话13812345678。"}}})
			t.Logf("status=%d upstream_calls=%d plaintext_leaked=%t", response.Code, calls.Load(), leaked.Load())
			if tc.blocked {
				if response.Code < 400 || calls.Load() != 0 {
					t.Errorf("invalid local output must block cloud dispatch: status=%d upstream_calls=%d plaintext_leaked=%t", response.Code, calls.Load(), leaked.Load())
				}
			} else if response.Code != 200 || calls.Load() != 1 || leaked.Load() {
				t.Errorf("valid control failed: status=%d calls=%d leaked=%t", response.Code, calls.Load(), leaked.Load())
			}
		})
	}
}

func TestAuditExtendedInvalidCompletionMustBlock(t *testing.T) {
	envelope := func(content, finish string) string {
		raw, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"index": 0, "finish_reason": finish, "message": map[string]any{"role": "assistant", "content": content}}}})
		return string(raw)
	}
	cases := []struct {
		name    string
		raw     string
		blocked bool
	}{
		{"valid_complete_control", envelope(`{"entities":[{"value":"宁祁","label":"NAME"},{"value":"陆遥","label":"NAME"}]}`, "stop"), false},
		{"explicit_incomplete", envelope(`{"complete":false,"entities":[]}`, "stop"), true},
		{"empty_field_coverage", envelope(`{"complete":false,"fields":[]}`, "stop"), true},
		{"complete_true_empty_field_coverage", envelope(`{"complete":true,"fields":[]}`, "stop"), true},
		{"unalignable_entity", envelope(`{"entities":[{"value":"原文里不存在的姓名","label":"NAME"}]}`, "stop"), true},
		{"duplicate_entities_key", envelope(`{"entities":[{"value":"宁祁","label":"NAME"}],"entities":[]}`, "stop"), true},
		{"multiple_choices", `{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"{\"entities\":[]}"}},{"finish_reason":"length","message":{"role":"assistant","content":""}}]}`, true},
		{"missing_finish_reason", `{"choices":[{"message":{"role":"assistant","content":"{\"entities\":[]}"}}]}`, true},
		{"think_wrapper_rejected", envelope(`<think>推理</think>{"entities":[]}`, "stop"), true},
		{"markdown_fence_rejected", envelope("```json\n{\"entities\":[]}\n```", "stop"), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			var leaked atomic.Bool
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				raw, _ := io.ReadAll(r.Body)
				if bytes.Contains(raw, []byte("宁祁")) || bytes.Contains(raw, []byte("陆遥")) {
					leaked.Store(true)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"id":"audit","object":"chat.completion","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"ok"}}]}`)
			}))
			defer upstream.Close()
			invoker := &scriptedInvoker{response: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(tc.raw))}}
			h, store := newGroupRoutingEnv(t, ModelOptions{Routing: &RoutingOptions{LocalInvoker: invoker}})
			provider := saveTestCloudProvider(t, store, "AuditLocal", upstream.URL+"/v1", "fixture-model")
			_, err := store.SaveGroup(routerconfig.Group{ID: "audit-redaction", Name: "Audit", Routing: "order", LocalRedaction: true, Members: []string{provider.ID + "/fixture-model", "local/spark-x2.5-1.7b"}})
			if err != nil {
				t.Fatal(err)
			}
			saveGroupBoundApp(t, store, []string{"group/audit-redaction"})
			response := doGroupRequest(t, h, "", http.MethodPost, "/app/codex/v1/chat/completions", map[string]any{"model": "group/audit-redaction", "messages": []any{map[string]string{"role": "user", "content": "本段内容是宁祁以及陆遥的内部记录，电话13812345678。"}}})
			t.Logf("status=%d upstream_calls=%d plaintext_leaked=%t", response.Code, calls.Load(), leaked.Load())
			if tc.blocked {
				if response.Code < 400 || calls.Load() != 0 {
					t.Errorf("invalid local output must block cloud dispatch: status=%d upstream_calls=%d plaintext_leaked=%t", response.Code, calls.Load(), leaked.Load())
				}
			} else if response.Code != 200 || calls.Load() != 1 || leaked.Load() {
				t.Errorf("valid control failed: status=%d calls=%d leaked=%t", response.Code, calls.Load(), leaked.Load())
			}
		})
	}
}
