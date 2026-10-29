package routerservice

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

const titleMemoryFixture = "## 可用记忆条目\n\n- [古籍修复偏好](memory/book.md) SECRET_MEMORY_BOOK_RESTORATION\n\n只在条目与当前请求明确相关时读取对应文件。不要向用户提及这些条目、路径或读取过程。"
const nativeTitleFixture = "You are a title generator. You output ONLY a thread title. Nothing else.\nGenerate a title."

func TestLoomyTitleLatestContextDoesNotFallBackToHistory(t *testing.T) {
	for _, latest := range []string{"", titleMemoryFixture, "<system-reminder>unclosed"} {
		envelope, _ := json.Marshal(map[string]any{"api_router_title": 1, "user_messages": []string{"OLD_USER", latest}})
		body, _ := json.Marshal(map[string]any{"messages": []map[string]string{{"role": "user", "content": string(envelope)}}})
		var input loomyTitleInput
		if err := json.Unmarshal(body, &input); err != nil {
			t.Fatal(err)
		}
		texts, err := input.sources()
		if err == nil && len(texts) != 0 {
			t.Fatal("title fell back to history or scanned host-only text", texts)
		}
	}
}

func TestLoomyTitleUsesModelWithVisibleConversation(t *testing.T) {
	var mu sync.Mutex
	var upstreamBodies []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		upstreamBodies = append(upstreamBodies, string(raw))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"title-model","object":"chat.completion","model":"profile-pinned","choices":[{"index":0,"message":{"role":"assistant","content":"标题：模型动态生成结果"},"finish_reason":"stop"}],"usage":{"prompt_tokens":20,"completion_tokens":8}}`)
	}))
	defer upstream.Close()
	h := cloudHTTPHandler(t)
	cloudHTTPBind(t, h, upstream.URL, "chat", "loomy", "test-key")
	for _, stream := range []bool{false, true} {
		for _, envelope := range []bool{false, true} {
			user := titleMemoryFixture + "\n\n<system-reminder>SECRET_SYSTEM_NOTE</system-reminder>\n\n你好"
			if envelope {
				data, _ := json.Marshal(map[string]any{"api_router_title": 1, "user_messages": []string{user, "请整理会议纪要"}})
				user = string(data)
			}
			body, _ := json.Marshal(map[string]any{"model": "profile-pinned", "stream": stream, "messages": []map[string]string{
				{"role": "system", "content": nativeTitleFixture},
				{"role": "user", "content": "Generate a title for this conversation:\n"},
				{"role": "user", "content": user},
				{"role": "assistant", "content": "SECRET_ASSISTANT_SUGGESTION"},
			}})
			r := httptest.NewRequest("POST", "http://127.0.0.1:13145/app/loomy/v1/chat/completions", strings.NewReader(string(body)))
			r.RemoteAddr = "127.0.0.1:1234"
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Authorization", "Bearer host-session")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			mu.Lock()
			last := ""
			if len(upstreamBodies) > 0 {
				last = upstreamBodies[len(upstreamBodies)-1]
			}
			mu.Unlock()
			if strings.Contains(last, "SECRET_") || strings.Contains(last, "古籍修复偏好") {
				t.Fatal("title model received hidden memory or system context")
			}
			if w.Code != 200 || w.Header().Get("X-Api-Router-Title-Policy") != "v1" || !strings.Contains(w.Body.String(), "模型动态生成结果") {
				t.Fatalf("title did not come from model (stream=%v, envelope=%v): %d %s", stream, envelope, w.Code, w.Body.String())
			}
			if stream && !strings.Contains(w.Body.String(), "data: [DONE]") {
				t.Fatal("incomplete title stream")
			}
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(upstreamBodies) != 4 {
		t.Fatalf("even a greeting must invoke the configured model: %d calls", len(upstreamBodies))
	}
	for _, body := range upstreamBodies {
		if strings.Contains(body, "SECRET_") || strings.Contains(body, "古籍修复偏好") || strings.Contains(body, "memory/book.md") {
			t.Fatal("hidden context influenced title request:", body)
		}
		if (!strings.Contains(body, "你好") && !strings.Contains(body, "请整理会议纪要")) || !strings.Contains(body, `"stream":false`) {
			t.Fatal("visible user content missing from canonical request:", body)
		}
		if strings.Contains(body, "api_router_title") || strings.Contains(body, "user_messages") || strings.Contains(body, "你好") && strings.Contains(body, "请整理会议纪要") {
			t.Fatal("title request included its JSON envelope or earlier user prompt:", body)
		}
	}
}

func TestLoomyTitleFailureAndNormalChatBoundaries(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"title-empty","object":"chat.completion","model":"profile-pinned","choices":[{"index":0,"message":{"role":"assistant","content":""},"finish_reason":"stop"}]}`)
	}))
	defer upstream.Close()
	h := cloudHTTPHandler(t)
	cloudHTTPBind(t, h, upstream.URL, "chat", "loomy", "test-key")
	for _, tc := range []struct {
		name, body, purpose string
		status              int
		call                bool
	}{
		{"empty-title", `{"model":"profile-pinned","messages":[{"role":"user","content":"你好"}]}`, "chat.title", 502, true},
		{"normal-chat", `{"model":"profile-pinned","messages":[{"role":"user","content":"你好"}]}`, "", 200, true},
		{"user-mentions-title", `{"model":"profile-pinned","messages":[{"role":"user","content":"You are a title generator. You output ONLY a thread title. Nothing else."}]}`, "", 200, true},
		{"unbound-model", `{"model":"not-bound","messages":[{"role":"user","content":"你好"}]}`, "chat.title", 400, false},
		{"duplicate-key", `{"model":"profile-pinned","model":"not-bound","messages":[{"role":"user","content":"你好"}]}`, "chat.title", 400, false},
		{"unknown-field", `{"model":"profile-pinned","surprise":true,"messages":[{"role":"user","content":"你好"}]}`, "chat.title", 400, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mu.Lock()
			before := calls
			mu.Unlock()
			r := httptest.NewRequest("POST", "http://127.0.0.1:13145/app/loomy/v1/chat/completions", strings.NewReader(tc.body))
			r.RemoteAddr = "127.0.0.1:1234"
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Authorization", "Bearer host-session")
			r.Header.Set("X-Api-Router-Purpose", tc.purpose)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
			mu.Lock()
			after := calls
			mu.Unlock()
			want := 0
			if tc.call {
				want = 1
			}
			if after-before != want {
				t.Fatal("request validation or model invocation was bypassed")
			}
			if tc.status != 200 && strings.Contains(w.Body.String(), "chat.completion.chunk") {
				t.Fatal("failed title committed partial text")
			}
		})
	}
}
