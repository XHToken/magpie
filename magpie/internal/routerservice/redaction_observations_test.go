package routerservice

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/routerledger"
	"github.com/yetone/magpie/internal/routerredact"
)

func TestExtractLatestUserInput(t *testing.T) {
	tests := []struct {
		name     string
		protocol string
		body     string
		want     string
		wantOK   bool
	}{
		{
			name:     "chat latest user message ignores system and earlier history",
			protocol: "chat",
			body:     `{"model":"gpt-4o","messages":[{"role":"system","content":"system secret"},{"role":"user","content":"first turn"},{"role":"assistant","content":"reply"},{"role":"user","content":"我是张三，电话13800138000"}]}`,
			want:     "我是张三，电话13800138000",
			wantOK:   true,
		},
		{
			name:     "chat trailing tool result is skipped",
			protocol: "chat",
			body:     `{"model":"gpt-4o","messages":[{"role":"user","content":"查询张三"},{"role":"assistant","tool_calls":[{"id":"c1"}]},{"role":"tool","content":"result"}]}`,
			want:     "",
			wantOK:   false,
		},
		{
			name:     "anthropic latest user text blocks joined, tool_result skipped",
			protocol: "anthropic",
			body:     `{"model":"claude-3-7-sonnet","system":"sys","messages":[{"role":"user","content":[{"type":"tool_result","content":"tool out"},{"type":"text","text":"请帮李四发送邮件到lisi@example.com"}]}]}`,
			want:     "请帮李四发送邮件到lisi@example.com",
			wantOK:   true,
		},
		{
			name:     "anthropic trailing tool_result only is skipped",
			protocol: "anthropic",
			body:     `{"model":"claude-3-7-sonnet","messages":[{"role":"user","content":"earlier question"},{"role":"assistant","content":[{"type":"tool_use","id":"t1"}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"ok"}]}]}`,
			want:     "",
			wantOK:   false,
		},
		{
			name:     "responses string input",
			protocol: "responses",
			body:     `{"model":"gpt-4.1","instructions":"sys","input":"我的身份证是110101199003072316"}`,
			want:     "我的身份证是110101199003072316",
			wantOK:   true,
		},
		{
			name:     "responses trailing function_call_output is skipped",
			protocol: "responses",
			body:     `{"model":"gpt-4.1","input":[{"role":"user","content":"hi"},{"type":"function_call_output","call_id":"1","output":"done"}]}`,
			want:     "",
			wantOK:   false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := extractLatestUserInput(tc.protocol, []byte(tc.body))
			if ok != tc.wantOK || got != tc.want {
				t.Fatalf("extractLatestUserInput(%q) = (%q, %v), want (%q, %v)", tc.protocol, got, ok, tc.want, tc.wantOK)
			}
		})
	}
}

func TestRedactionObservationsAPIAndRecording(t *testing.T) {
	store, err := routerledger.OpenRedactionObservations(filepath.Join(t.TempDir(), "redaction_observations.sqlite"))
	if err != nil {
		t.Fatalf("OpenRedactionObservations: %v", err)
	}
	defer store.Close()

	token := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	info := Info{Address: "127.0.0.1:28080", Version: "0.1.0"}
	h := handlerWithModelOptions(info, token, nil, ModelOptions{
		RedactionObservations: store,
	})

	rawBody := []byte(`{"model":"deepseek-chat","messages":[{"role":"system","content":"system prompt"},{"role":"user","content":"我是张三，手机号13800138000，住在北京市海淀区中关村南大街1号"}]}`)
	redactedBody := []byte(`{"model":"deepseek-chat","messages":[{"role":"system","content":"system prompt"},{"role":"user","content":"我是<NAME:1>，手机号<PHONE:1>，住在<ADDRESS:1>"}]}`)

	recordRedactionObservation(
		store,
		"req_obs_1",
		"workbuddy",
		"WorkBuddy",
		"cloud",
		"deepseek-chat",
		"chat",
		rawBody,
		redactedBody,
		&routerredact.ObservationMeta{
			CacheHit:   false,
			DurationMS: 25,
			Replacements: []routerredact.ReplacementMeta{
				{Placeholder: "<NAME:1>", Category: "NAME", Original: "张三"},
				{Placeholder: "<PHONE:1>", Category: "PHONE", Original: "13800138000"},
				{Placeholder: "<ADDRESS:1>", Category: "ADDRESS", Original: "北京市海淀区中关村南大街1号"},
			},
		},
	)

	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:28080/api/redaction/observations?hitOnly=1", nil)
	req.RemoteAddr = "127.0.0.1:54321"
	req.Host = "127.0.0.1:28080"
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/redaction/observations status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var resp routerledger.RedactionObservationListResult
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Summary.Total != 1 || resp.Summary.Hits != 1 || len(resp.Observations) != 1 {
		t.Fatalf("unexpected response summary/observations: %+v", resp)
	}
	obs := resp.Observations[0]
	if obs.RawInput != "我是张三，手机号13800138000，住在北京市海淀区中关村南大街1号" {
		t.Fatalf("unexpected RawInput: %q", obs.RawInput)
	}
	if obs.RedactedInput != "我是<NAME:1>，手机号<PHONE:1>，住在<ADDRESS:1>" {
		t.Fatalf("unexpected RedactedInput: %q", obs.RedactedInput)
	}
	if len(obs.Replacements) != 3 {
		t.Fatalf("expected 3 replacements, got %+v", obs.Replacements)
	}

	delReq := httptest.NewRequest(http.MethodDelete, "http://127.0.0.1:28080/api/redaction/observations", nil)
	delReq.RemoteAddr = "127.0.0.1:54321"
	delReq.Host = "127.0.0.1:28080"
	delReq.Header.Set("Authorization", "Bearer "+token)
	delRec := httptest.NewRecorder()
	h.ServeHTTP(delRec, delReq)
	if delRec.Code != http.StatusOK {
		t.Fatalf("DELETE /api/redaction/observations status = %d", delRec.Code)
	}
}

func TestRedactionObservationStorePhysicallyPrunesExpiredPlaintextWithoutNewWrites(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "redaction_observations.sqlite")
	store, err := routerledger.OpenRedactionObservations(dbPath, routerledger.RedactionObservationOptions{
		MaxEntries:    100,
		MaxAge:        80 * time.Millisecond,
		PruneInterval: 25 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("open observations store: %v", err)
	}
	defer store.Close()

	const secretName = "绝密测试姓名司马长风"
	const secretPhone = "13999887766"
	if err := store.Record(routerledger.RedactionObservation{
		RequestID:     "req_expire_1",
		AppID:         "workbuddy",
		AppName:       "WorkBuddy",
		Destination:   "cloud",
		Protocol:      "chat",
		Model:         "gpt-4o",
		StartedAt:     time.Now().Add(-50 * time.Millisecond).UTC().Format(time.RFC3339Nano),
		Hit:           true,
		EntityCount:   2,
		Categories:    []string{"NAME", "PHONE"},
		RawInput:      "联系人" + secretName + "，电话" + secretPhone,
		RedactedInput: "联系人<NAME:1>，电话<PHONE:1>",
		Replacements: []routerledger.RedactionReplacementItem{
			{Placeholder: "<NAME:1>", Category: "NAME", Raw: secretName, Count: 1},
			{Placeholder: "<PHONE:1>", Category: "PHONE", Raw: secretPhone, Count: 1},
		},
	}); err != nil {
		t.Fatalf("record observation: %v", err)
	}

	// Wait for the observation to expire and for the periodic janitor to run without any new Record() writes.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		rawBytes, readErr := os.ReadFile(dbPath)
		if readErr != nil {
			t.Fatalf("read sqlite file: %v", readErr)
		}
		if !strings.Contains(string(rawBytes), secretName) && !strings.Contains(string(rawBytes), secretPhone) {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}

	page, err := store.Query(routerledger.RedactionObservationFilter{Limit: 10})
	if err != nil {
		t.Fatalf("query after expiry: %v", err)
	}
	if page.Summary.Total != 0 || len(page.Observations) != 0 {
		t.Fatalf("expected 0 observations after expiry, got %+v", page)
	}

	rawBytes, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatalf("read sqlite file: %v", err)
	}
	if strings.Contains(string(rawBytes), secretName) || strings.Contains(string(rawBytes), secretPhone) {
		t.Fatalf("expected expired plaintext (%q / %q) to be physically pruned from SQLite file with secure_delete=ON", secretName, secretPhone)
	}
}
