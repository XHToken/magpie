package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/provider"
	"github.com/yetone/magpie/internal/routerruntime"
)

func setupEdgeCloudGroup(t *testing.T, localHandler, cloudHandler http.Handler) *Server {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	restingUntil.Lock()
	restingUntil.m = map[string]time.Time{}
	restingUntil.Unlock()

	localUp := httptest.NewServer(localHandler)
	t.Cleanup(localUp.Close)
	cloudUp := httptest.NewServer(cloudHandler)
	t.Cleanup(cloudUp.Close)

	if err := provider.Save(provider.Provider{
		ID:     "local",
		Name:   "Local",
		Key:    "local-key",
		Chat:   localUp.URL + "/v1",
		Models: []string{"qwen2.5-3b"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := provider.Save(provider.Provider{
		ID:     "cloud",
		Name:   "Cloud",
		Key:    "cloud-key",
		Chat:   cloudUp.URL + "/v1",
		Models: []string{"gpt-4o"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := provider.SaveGroup(provider.Group{
		ID:      "ec",
		Name:    "EdgeCloud Group",
		Routing: provider.EdgeCloud,
		Members: []string{"cloud/gpt-4o", "local/qwen2.5-3b"},
	}); err != nil {
		t.Fatal(err)
	}
	return New()
}

func TestSaveGroupEdgeCloudValidation(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	findSaved := func(id string) (provider.Group, bool) {
		for _, g := range provider.Groups() {
			if g.ID == id {
				return g, true
			}
		}
		return provider.Group{}, false
	}

	if err := provider.SaveGroup(provider.Group{
		ID:      "with-local",
		Name:    "With Local",
		Routing: provider.EdgeCloud,
		Members: []string{"cloud/gpt-4o", "local/qwen2.5-3b"},
	}); err != nil {
		t.Fatal(err)
	}
	if g, ok := findSaved("with-local"); !ok || g.Routing != provider.EdgeCloud {
		t.Fatalf("expected EdgeCloud routing kept when local member present, got %+v", g)
	}

	if err := provider.SaveGroup(provider.Group{
		ID:      "without-local",
		Name:    "Without Local",
		Routing: provider.EdgeCloud,
		Members: []string{"cloud/gpt-4o", "cloud/claude-3-5-sonnet"},
	}); err != nil {
		t.Fatal(err)
	}
	if g, ok := findSaved("without-local"); !ok || g.Routing != "" {
		t.Fatalf("expected EdgeCloud routing to fall back to empty Smart when no local member present, got %q", g.Routing)
	}
}

func TestEdgeCloudStage1FastFilter(t *testing.T) {
	origStatus := edgeCloudRuntimeStatus
	t.Cleanup(func() { edgeCloudRuntimeStatus = origStatus })

	var localCalls atomic.Int32
	var cloudCalls atomic.Int32

	s := setupEdgeCloudGroup(t,
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			localCalls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"local-reply","choices":[{"message":{"role":"assistant","content":"{\"decision\":\"local\"}"}}]}`))
		}),
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cloudCalls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"cloud-reply","choices":[{"message":{"role":"assistant","content":"from-cloud"}}]}`))
		}),
	)

	send := func(body string) (int, string) {
		localCalls.Store(0)
		cloudCalls.Store(0)
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		s.Handler().ServeHTTP(rec, req)
		return rec.Code, rec.Body.String()
	}

	// 1. Runtime unhealthy -> Cloud directly (0 local calls)
	edgeCloudRuntimeStatus = func() routerruntime.Status {
		return routerruntime.Status{Runtime: routerruntime.Unloaded, Healthy: false}
	}
	if code, out := send(`{"model":"group/ec","messages":[{"role":"user","content":"hello"}]}`); code != 200 || !strings.Contains(out, "from-cloud") || localCalls.Load() != 0 {
		t.Fatalf("unhealthy runtime: code=%d localCalls=%d out=%s", code, localCalls.Load(), out)
	}

	// 2. Runtime busy / queued (Active > 0 || Waiting > 0) -> Cloud directly (0 local calls)
	edgeCloudRuntimeStatus = func() routerruntime.Status {
		return routerruntime.Status{Runtime: routerruntime.Ready, Healthy: true, Active: 1, Waiting: 0}
	}
	if code, out := send(`{"model":"group/ec","messages":[{"role":"user","content":"hello"}]}`); code != 200 || !strings.Contains(out, "from-cloud") || localCalls.Load() != 0 {
		t.Fatalf("busy runtime: code=%d localCalls=%d out=%s", code, localCalls.Load(), out)
	}

	// Set runtime healthy and idle for content fast-filter checks
	edgeCloudRuntimeStatus = func() routerruntime.Status {
		return routerruntime.Status{Runtime: routerruntime.Ready, Healthy: true, Active: 0, Waiting: 0}
	}

	// 3. Request with tools -> Cloud directly (0 local calls)
	if code, out := send(`{"model":"group/ec","messages":[{"role":"user","content":"weather?"}],"tools":[{"type":"function","function":{"name":"get_weather"}}]}`); code != 200 || !strings.Contains(out, "from-cloud") || localCalls.Load() != 0 {
		t.Fatalf("tools request: code=%d localCalls=%d out=%s", code, localCalls.Load(), out)
	}

	// 4. Request with image -> Cloud directly (0 local calls)
	if code, out := send(`{"model":"group/ec","messages":[{"role":"user","content":[{"type":"text","text":"what is this?"},{"type":"image_url","image_url":{"url":"data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII="}}]}]}`); code != 200 || !strings.Contains(out, "from-cloud") || localCalls.Load() != 0 {
		t.Fatalf("image request: code=%d localCalls=%d out=%s", code, localCalls.Load(), out)
	}

	// 5. Request with > 2048 estimated tokens -> Cloud directly (0 local calls)
	longText := strings.Repeat("word ", 2200) // ~11000 bytes > 2048 tokens
	if code, out := send(`{"model":"group/ec","messages":[{"role":"user","content":"` + longText + `"}]}`); code != 200 || !strings.Contains(out, "from-cloud") || localCalls.Load() != 0 {
		t.Fatalf("long context request: code=%d localCalls=%d out=%s", code, localCalls.Load(), out)
	}
}

func TestEdgeCloudStage2AndStage3RoutingAndFallback(t *testing.T) {
	origStatus := edgeCloudRuntimeStatus
	origAssessTimeout := edgeCloudAssessTimeout
	origAttemptTimeout := edgeCloudLocalAttemptTimeout
	origAcquireSlot := edgeCloudAcquireSlot
	t.Cleanup(func() {
		edgeCloudRuntimeStatus = origStatus
		edgeCloudAssessTimeout = origAssessTimeout
		edgeCloudLocalAttemptTimeout = origAttemptTimeout
		edgeCloudAcquireSlot = origAcquireSlot
	})

	edgeCloudRuntimeStatus = func() routerruntime.Status {
		return routerruntime.Status{Runtime: routerruntime.Ready, Healthy: true}
	}

	t.Run("simple task routes to local first and disables thinking in judge", func(t *testing.T) {
		var judgeBody []byte
		var localExecCalls atomic.Int32
		var cloudExecCalls atomic.Int32

		s := setupEdgeCloudGroup(t,
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				b, _ := io.ReadAll(r.Body)
				w.Header().Set("Content-Type", "application/json")
				if bytes.Contains(b, []byte(edgeCloudJudgePrompt[:32])) {
					judgeBody = b
					_, _ = w.Write([]byte(`{"id":"judge","choices":[{"message":{"role":"assistant","content":"{\"decision\":\"local\"}"}}]}`))
					return
				}
				localExecCalls.Add(1)
				_, _ = w.Write([]byte(`{"id":"local-ans","choices":[{"message":{"role":"assistant","content":"answered-by-local"}}]}`))
			}),
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				cloudExecCalls.Add(1)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"id":"cloud-ans","choices":[{"message":{"role":"assistant","content":"answered-by-cloud"}}]}`))
			}),
		)

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"group/ec","messages":[{"role":"user","content":"say hello"}]}`))
		req.Header.Set("Content-Type", "application/json")
		s.Handler().ServeHTTP(rec, req)

		if rec.Code != 200 || !strings.Contains(rec.Body.String(), "answered-by-local") {
			t.Fatalf("expected local answer, got %d %s", rec.Code, rec.Body.String())
		}
		if localExecCalls.Load() != 1 || cloudExecCalls.Load() != 0 {
			t.Fatalf("expected 1 local exec and 0 cloud exec, got local=%d cloud=%d", localExecCalls.Load(), cloudExecCalls.Load())
		}
		var parsedJudge map[string]any
		if err := json.Unmarshal(judgeBody, &parsedJudge); err != nil {
			t.Fatalf("unmarshal judge body: %v", err)
		}
		if parsedJudge["enable_thinking"] != false {
			t.Fatalf("expected enable_thinking=false in judge request, got %v", parsedJudge["enable_thinking"])
		}
		if parsedJudge["max_tokens"] != float64(16) {
			t.Fatalf("expected max_tokens=16 in judge request, got %v", parsedJudge["max_tokens"])
		}
	})

	t.Run("complex task routes to cloud first", func(t *testing.T) {
		var localExecCalls atomic.Int32
		var cloudExecCalls atomic.Int32

		s := setupEdgeCloudGroup(t,
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				b, _ := io.ReadAll(r.Body)
				w.Header().Set("Content-Type", "application/json")
				if bytes.Contains(b, []byte(edgeCloudJudgePrompt[:32])) {
					_, _ = w.Write([]byte(`{"id":"judge","choices":[{"message":{"role":"assistant","content":"{\"decision\":\"cloud\"}"}}]}`))
					return
				}
				localExecCalls.Add(1)
				_, _ = w.Write([]byte(`{"id":"local-ans","choices":[{"message":{"role":"assistant","content":"answered-by-local"}}]}`))
			}),
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				cloudExecCalls.Add(1)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"id":"cloud-ans","choices":[{"message":{"role":"assistant","content":"answered-by-cloud"}}]}`))
			}),
		)

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"group/ec","messages":[{"role":"user","content":"design a distributed consensus protocol"}]}`))
		req.Header.Set("Content-Type", "application/json")
		s.Handler().ServeHTTP(rec, req)

		if rec.Code != 200 || !strings.Contains(rec.Body.String(), "answered-by-cloud") {
			t.Fatalf("expected cloud answer, got %d %s", rec.Code, rec.Body.String())
		}
		if localExecCalls.Load() != 0 || cloudExecCalls.Load() != 1 {
			t.Fatalf("expected 0 local exec and 1 cloud exec, got local=%d cloud=%d", localExecCalls.Load(), cloudExecCalls.Load())
		}
	})

	t.Run("judge timeout routes to cloud first", func(t *testing.T) {
		edgeCloudAssessTimeout = 40 * time.Millisecond
		defer func() { edgeCloudAssessTimeout = origAssessTimeout }()

		s := setupEdgeCloudGroup(t,
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				time.Sleep(150 * time.Millisecond)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"id":"judge","choices":[{"message":{"role":"assistant","content":"{\"decision\":\"local\"}"}}]}`))
			}),
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"id":"cloud-ans","choices":[{"message":{"role":"assistant","content":"cloud-after-judge-timeout"}}]}`))
			}),
		)

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"group/ec","messages":[{"role":"user","content":"quick question"}]}`))
		req.Header.Set("Content-Type", "application/json")
		s.Handler().ServeHTTP(rec, req)

		if rec.Code != 200 || !strings.Contains(rec.Body.String(), "cloud-after-judge-timeout") {
			t.Fatalf("expected cloud answer after judge timeout, got %d %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("local queue timeout or execution failure falls back to cloud", func(t *testing.T) {
		edgeCloudAcquireSlot = func(ctx context.Context) (func(), time.Duration, error) {
			return nil, 25 * time.Millisecond, context.DeadlineExceeded
		}
		defer func() { edgeCloudAcquireSlot = origAcquireSlot }()

		s := setupEdgeCloudGroup(t,
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				b, _ := io.ReadAll(r.Body)
				w.Header().Set("Content-Type", "application/json")
				if bytes.Contains(b, []byte(edgeCloudJudgePrompt[:32])) {
					_, _ = w.Write([]byte(`{"id":"judge","choices":[{"message":{"role":"assistant","content":"{\"decision\":\"local\"}"}}]}`))
					return
				}
				t.Errorf("local execution should have been skipped when queue slot timed out")
			}),
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"id":"cloud-ans","choices":[{"message":{"role":"assistant","content":"cloud-fallback-after-slot-timeout"}}]}`))
			}),
		)

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"group/ec","messages":[{"role":"user","content":"simple hello"}]}`))
		req.Header.Set("Content-Type", "application/json")
		s.Handler().ServeHTTP(rec, req)

		if rec.Code != 200 || !strings.Contains(rec.Body.String(), "cloud-fallback-after-slot-timeout") {
			t.Fatalf("expected cloud fallback after slot timeout, got %d %s", rec.Code, rec.Body.String())
		}
	})
}
