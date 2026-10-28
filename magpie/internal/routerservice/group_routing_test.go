package routerservice

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/dirs"
	"github.com/yetone/magpie/internal/routerconfig"
	"github.com/yetone/magpie/internal/routerledger"
	"github.com/yetone/magpie/internal/routermanifest"
	"github.com/yetone/magpie/internal/routerredact"
	"github.com/yetone/magpie/internal/routerruntime"
)

func newGroupRoutingEnv(t *testing.T, opts ModelOptions) (http.Handler, *routerconfig.Store) {
	t.Helper()
	root := t.TempDir()
	layout := dirs.Layout{
		Install: filepath.Join(root, "install"),
		Config:  filepath.Join(root, "config"),
		Data:    filepath.Join(root, "data"),
		Models:  filepath.Join(root, "data", "models"),
		Runtime: filepath.Join(root, "data", "runtime"),
		Cache:   filepath.Join(root, "cache"),
		State:   filepath.Join(root, "state"),
		Logs:    filepath.Join(root, "state", "logs"),
	}
	if err := layout.Ensure(); err != nil {
		t.Fatal(err)
	}
	store := routerconfig.New(layout)
	if _, err := store.Initialize(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		closeServiceGroupRedactors(store, opts.LocalRuntime)
	})
	info := Info{Service: "wefto", Address: "127.0.0.1:13145", PID: 42}
	return handlerWithModelOptions(info, "mgmt-secret-token", store, opts), store
}

func saveTestCloudProvider(t *testing.T, store *routerconfig.Store, name, baseURL string, models ...string) routerconfig.CloudProvider {
	t.Helper()
	key := "sk-provider-" + name
	p, err := store.SaveCloudProvider(routerconfig.ProviderEdit{
		Name: name, Protocol: "chat", BaseURL: baseURL,
		APIKey: &key, DiscoveredModels: &models, ProxyMode: "direct",
	})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func saveGroupBoundApp(t *testing.T, store *routerconfig.Store, models []string) {
	t.Helper()
	if _, err := store.SaveApp(routerconfig.AppEdit{
		ID:             "codex",
		Name:           "Codex",
		Models:         &models,
		CredentialMode: routerconfig.StoredCredential,
	}); err != nil {
		t.Fatal(err)
	}
}

func postAppChat(t *testing.T, h http.Handler, model string) *httptest.ResponseRecorder {
	t.Helper()
	payload := map[string]any{
		"messages": []map[string]string{{"role": "user", "content": "hello"}},
	}
	if model != "" {
		payload["model"] = model
	}
	return doGroupRequest(t, h, "", http.MethodPost, "/app/codex/v1/chat/completions", payload)
}

func TestGroupRoutingSmartSplit(t *testing.T) {
	var cloudCalls atomic.Int32
	cloudSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cloudCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"cloud-1","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"from-cloud"},"finish_reason":"stop"}]}`))
	}))
	defer cloudSrv.Close()

	judge := &scriptedJudge{decision: DecisionLocal}
	invoker := &scriptedInvoker{response: localChatResponse()}
	h, store := newGroupRoutingEnv(t, ModelOptions{
		Routing: &RoutingOptions{Judge: judge, LocalInvoker: invoker},
	})

	p := saveTestCloudProvider(t, store, "CloudOne", cloudSrv.URL+"/v1", "gpt-4.1")
	if _, err := store.SaveGroup(routerconfig.Group{
		ID: "hybrid-group", Name: "Hybrid Group", Routing: "smart_split",
		Members: []string{"local/qwen2.5-coder-7b", p.ID + "/gpt-4.1"},
	}); err != nil {
		t.Fatal(err)
	}
	saveGroupBoundApp(t, store, []string{"group/hybrid-group"})

	// 1. Judged local -> local runtime responds directly without touching cloud.
	rec1 := postAppChat(t, h, "group/hybrid-group")
	if rec1.Code != http.StatusOK || invoker.calls.Load() != 1 || cloudCalls.Load() != 0 {
		t.Fatalf("smart_split local case failed: code=%d local=%d cloud=%d body=%s", rec1.Code, invoker.calls.Load(), cloudCalls.Load(), rec1.Body.String())
	}

	// 2. Judged local, but local fails before committing output -> seamless failover to cloud.
	invoker.response = nil
	invoker.err = errors.New("local inference failed")
	rec2 := postAppChat(t, h, "hybrid-group")
	if rec2.Code != http.StatusOK || invoker.calls.Load() != 2 || cloudCalls.Load() != 1 {
		t.Fatalf("smart_split failover case failed: code=%d local=%d cloud=%d body=%s", rec2.Code, invoker.calls.Load(), cloudCalls.Load(), rec2.Body.String())
	}

	// 3. Judged cloud -> routes directly to cloud member without calling local invoker.
	judge.decision = DecisionCloud
	rec3 := postAppChat(t, h, "")
	if rec3.Code != http.StatusOK || invoker.calls.Load() != 2 || cloudCalls.Load() != 2 {
		t.Fatalf("smart_split cloud case failed: code=%d local=%d cloud=%d body=%s", rec3.Code, invoker.calls.Load(), cloudCalls.Load(), rec3.Body.String())
	}
}

func TestGroupRoutingOrderedFailover(t *testing.T) {
	var calls1, calls3 atomic.Int32
	srv1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls1.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"message":"upstream 500"}}`))
	}))
	defer srv1.Close()

	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv2.Close() // closed immediately to simulate connection failure

	srv3 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls3.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"c3","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok-from-3"},"finish_reason":"stop"}]}`))
	}))
	defer srv3.Close()

	h, store := newGroupRoutingEnv(t, ModelOptions{})
	p1 := saveTestCloudProvider(t, store, "P1", srv1.URL+"/v1", "m1")
	p2 := saveTestCloudProvider(t, store, "P2", srv2.URL+"/v1", "m2")
	p3 := saveTestCloudProvider(t, store, "P3", srv3.URL+"/v1", "m3")

	if _, err := store.SaveGroup(routerconfig.Group{
		ID: "ordered-group", Name: "Ordered Group", Routing: "ordered",
		Members: []string{p1.ID + "/m1", p2.ID + "/m2", p3.ID + "/m3"},
	}); err != nil {
		t.Fatal(err)
	}
	saveGroupBoundApp(t, store, []string{"group/ordered-group"})

	rec := postAppChat(t, h, "group/ordered-group")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 from 3rd member, got %d: %s", rec.Code, rec.Body.String())
	}
	if calls1.Load() == 0 || calls3.Load() != 1 {
		t.Fatalf("unexpected member call counts: calls1=%d calls3=%d", calls1.Load(), calls3.Load())
	}
}

func TestGroupRoutingWeightedAndFailover(t *testing.T) {
	var failFirst atomic.Bool
	var callsA, callsB atomic.Int32
	srvA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callsA.Add(1)
		if failFirst.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"a","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"from-a"},"finish_reason":"stop"}]}`))
	}))
	defer srvA.Close()

	srvB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callsB.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"b","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"from-b"},"finish_reason":"stop"}]}`))
	}))
	defer srvB.Close()

	h, store := newGroupRoutingEnv(t, ModelOptions{})
	pA := saveTestCloudProvider(t, store, "PA", srvA.URL+"/v1", "ma")
	pB := saveTestCloudProvider(t, store, "PB", srvB.URL+"/v1", "mb")
	mA, mB := pA.ID+"/ma", pB.ID+"/mb"

	if _, err := store.SaveGroup(routerconfig.Group{
		ID: "weighted-group", Name: "Weighted Group", Routing: "weighted",
		Members: []string{mA, mB}, Weights: map[string]int{mA: 80, mB: 20},
	}); err != nil {
		t.Fatal(err)
	}
	saveGroupBoundApp(t, store, []string{"group/weighted-group"})

	groupWeightSeq.Store(0)
	for i := 0; i < 5; i++ {
		if rec := postAppChat(t, h, "group/weighted-group"); rec.Code != http.StatusOK {
			t.Fatalf("req %d status=%d body=%s", i, rec.Code, rec.Body.String())
		}
	}
	if callsA.Load() != 4 || callsB.Load() != 1 {
		t.Fatalf("expected 80:20 (4:1) distribution over 5 requests, got A=%d B=%d", callsA.Load(), callsB.Load())
	}

	// When A fails on its turn, weighted routing must failover to B.
	groupWeightSeq.Store(0)
	failFirst.Store(true)
	callsA.Store(0)
	callsB.Store(0)
	rec := postAppChat(t, h, "group/weighted-group")
	if rec.Code != http.StatusOK || callsA.Load() == 0 || callsB.Load() != 1 {
		t.Fatalf("expected weighted failover from A to B: status=%d A=%d B=%d", rec.Code, callsA.Load(), callsB.Load())
	}
}

func TestGroupRoutingManualAndDisabled(t *testing.T) {
	var calls1, calls2 atomic.Int32
	srv1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls1.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"m1","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"m1"},"finish_reason":"stop"}]}`))
	}))
	defer srv1.Close()

	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls2.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv2.Close()

	h, store := newGroupRoutingEnv(t, ModelOptions{})
	p1 := saveTestCloudProvider(t, store, "M1", srv1.URL+"/v1", "m1")
	p2 := saveTestCloudProvider(t, store, "M2", srv2.URL+"/v1", "m2")

	// 1. Manual strategy only invokes Pick member and does not failover when Pick fails.
	if _, err := store.SaveGroup(routerconfig.Group{
		ID: "manual-group", Name: "Manual Group", Routing: "manual",
		Members: []string{p1.ID + "/m1", p2.ID + "/m2"}, Pick: p2.ID + "/m2",
	}); err != nil {
		t.Fatal(err)
	}
	// 2. Disabled group returns 409 group_disabled.
	if _, err := store.SaveGroup(routerconfig.Group{
		ID: "off-group", Name: "Off Group", Routing: "ordered", Disabled: true,
		Members: []string{p1.ID + "/m1"},
	}); err != nil {
		t.Fatal(err)
	}
	saveGroupBoundApp(t, store, []string{"group/manual-group", "group/off-group"})

	recManual := postAppChat(t, h, "group/manual-group")
	if recManual.Code == http.StatusOK || calls2.Load() == 0 || calls1.Load() != 0 {
		t.Fatalf("manual group should only call pick and not failover: status=%d calls1=%d calls2=%d", recManual.Code, calls1.Load(), calls2.Load())
	}

	recOff := postAppChat(t, h, "group/off-group")
	if recOff.Code != http.StatusConflict {
		t.Fatalf("disabled group expected 409, got %d: %s", recOff.Code, recOff.Body.String())
	}
	var errEnv struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal(recOff.Body.Bytes(), &errEnv)
	if errEnv.Error.Code != "group_disabled" {
		t.Fatalf("expected error.code=group_disabled, got %q (%s)", errEnv.Error.Code, recOff.Body.String())
	}
}

func postAppChatWithPayload(t *testing.T, h http.Handler, payload map[string]any, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:13145/app/codex/v1/chat/completions", bytes.NewReader(b))
	req.RemoteAddr = "127.0.0.1:54321"
	req.Host = "127.0.0.1:13145"
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestGroupRoutingStrategiesRotateUsageAndSink(t *testing.T) {
	var rateLimitA atomic.Bool
	var callsA, callsB atomic.Int32
	srvA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callsA.Add(1)
		if rateLimitA.Load() {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"message":"rate limited"}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"a","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"from-a"},"finish_reason":"stop"}]}`))
	}))
	defer srvA.Close()

	srvB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callsB.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"b","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"from-b"},"finish_reason":"stop"}]}`))
	}))
	defer srvB.Close()

	h, store := newGroupRoutingEnv(t, ModelOptions{})
	pA := saveTestCloudProvider(t, store, "PA", srvA.URL+"/v1", "ma")
	pB := saveTestCloudProvider(t, store, "PB", srvB.URL+"/v1", "mb")
	mA, mB := pA.ID+"/ma", pB.ID+"/mb"

	// 1. Round-robin (rotate) alternates across requests when affinity is off.
	if _, err := store.SaveGroup(routerconfig.Group{
		ID: "rr-group", Name: "RR Group", Routing: "round-robin", Affinity: "off",
		Members: []string{mA, mB},
	}); err != nil {
		t.Fatal(err)
	}
	// 2. Least-used (usage) prefers member with lower recorded token usage.
	if _, err := store.SaveGroup(routerconfig.Group{
		ID: "usage-group", Name: "Usage Group", Routing: "least-used", Affinity: "off",
		Members: []string{mA, mB},
	}); err != nil {
		t.Fatal(err)
	}
	// 3. Sink (cooldownMove) moves 429-failing member to the end for subsequent requests.
	if _, err := store.SaveGroup(routerconfig.Group{
		ID: "sink-group", Name: "Sink Group", Routing: "ordered", Affinity: "off", Sink: true,
		Members: []string{mA, mB},
	}); err != nil {
		t.Fatal(err)
	}
	saveGroupBoundApp(t, store, []string{"group/rr-group", "group/usage-group", "group/sink-group"})

	for i := 0; i < 4; i++ {
		if rec := postAppChat(t, h, "group/rr-group"); rec.Code != http.StatusOK {
			t.Fatalf("rr req %d failed: %d %s", i, rec.Code, rec.Body.String())
		}
	}
	if callsA.Load() != 2 || callsB.Load() != 2 {
		t.Fatalf("expected round-robin 2:2 calls, got A=%d B=%d", callsA.Load(), callsB.Load())
	}

	// Record heavy token usage on mA so least-used picks mB.
	now := time.Now()
	recordMemberSuccess(mA, 50000, now)
	recordMemberSuccess(mB, 10, now)
	callsA.Store(0)
	callsB.Store(0)
	if rec := postAppChat(t, h, "group/usage-group"); rec.Code != http.StatusOK || callsB.Load() != 1 || callsA.Load() != 0 {
		t.Fatalf("expected least-used to pick mB, got A=%d B=%d", callsA.Load(), callsB.Load())
	}

	// Sink test: mA returns 429 on request 1 -> fails over to mB and sinks mA.
	// On request 2 (even with rateLimitA reset), mB is tried first without hitting mA.
	callsA.Store(0)
	callsB.Store(0)
	rateLimitA.Store(true)
	if rec := postAppChat(t, h, "group/sink-group"); rec.Code != http.StatusOK {
		t.Fatalf("sink req 1 failed: %d %s", rec.Code, rec.Body.String())
	}
	if callsA.Load() == 0 || callsB.Load() != 1 {
		t.Fatalf("sink req 1 expected A tried then B=1, got A=%d B=%d", callsA.Load(), callsB.Load())
	}
	rateLimitA.Store(false)
	callsA.Store(0)
	callsB.Store(0)
	if rec := postAppChat(t, h, "group/sink-group"); rec.Code != http.StatusOK {
		t.Fatalf("sink req 2 failed: %d %s", rec.Code, rec.Body.String())
	}
	if callsA.Load() != 0 || callsB.Load() != 1 {
		t.Fatalf("after sink, expected mB tried first (A=0, B=1), got A=%d B=%d", callsA.Load(), callsB.Load())
	}
}

func TestGroupRoutingModelsUnlistsContextAndFamily(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"ok","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"by-alias"},"finish_reason":"stop"}]}`))
	}))
	defer srv.Close()

	h, store := newGroupRoutingEnv(t, ModelOptions{})
	p1 := saveTestCloudProvider(t, store, "P1", srv.URL+"/v1", "claude-sonnet-4-20250514", "gpt-4o-mini")
	mSonnet := p1.ID + "/claude-sonnet-4-20250514"
	mMini := p1.ID + "/gpt-4o-mini"

	if _, err := store.SaveGroup(routerconfig.Group{
		ID:      "coding-pool",
		Name:    "Coding Pool",
		Family:  "sonnet-unified",
		Routing: "ordered",
		Unlists: true,
		Context: -1, // min of 200k and 128k -> 128000
		Members: []string{mSonnet, mMini},
	}); err != nil {
		t.Fatal(err)
	}
	saveGroupBoundApp(t, store, []string{"group/coding-pool", mSonnet, mMini})

	// 1. GET /app/codex/v1/models should unlist mSonnet & mMini and report context_window=128000 on group/coding-pool.
	recModels := doGroupRequest(t, h, "", http.MethodGet, "/app/codex/v1/models", nil)
	if recModels.Code != http.StatusOK {
		t.Fatalf("GET /app/codex/v1/models status=%d body=%s", recModels.Code, recModels.Body.String())
	}
	var listOut struct {
		Data []struct {
			ID            string `json:"id"`
			ContextWindow int    `json:"context_window"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recModels.Body.Bytes(), &listOut); err != nil {
		t.Fatal(err)
	}
	if len(listOut.Data) != 1 || listOut.Data[0].ID != "group/coding-pool" || listOut.Data[0].ContextWindow != 128000 {
		t.Fatalf("expected only group/coding-pool with context_window=128000, got %+v", listOut.Data)
	}

	// 2. Request using Family alias "sonnet-unified" routes to the group.
	recChat := postAppChat(t, h, "sonnet-unified")
	if recChat.Code != http.StatusOK || !strings.Contains(recChat.Body.String(), "by-alias") {
		t.Fatalf("expected chat via family alias sonnet-unified to succeed, got %d: %s", recChat.Code, recChat.Body.String())
	}
}

func TestGroupRoutingEffortLevelsAndFast(t *testing.T) {
	var mu sync.Mutex
	var gotPayloads []map[string]any
	var failFirst atomic.Bool

	srv1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if failFirst.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var m map[string]any
		_ = json.Unmarshal(raw, &m)
		mu.Lock()
		gotPayloads = append(gotPayloads, m)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"1","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok1"},"finish_reason":"stop"}]}`))
	}))
	defer srv1.Close()

	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var m map[string]any
		_ = json.Unmarshal(raw, &m)
		mu.Lock()
		gotPayloads = append(gotPayloads, m)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"2","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok2"},"finish_reason":"stop"}]}`))
	}))
	defer srv2.Close()

	h, store := newGroupRoutingEnv(t, ModelOptions{})
	p1 := saveTestCloudProvider(t, store, "P1", srv1.URL+"/v1", "o3-mini")
	p2 := saveTestCloudProvider(t, store, "P2", srv2.URL+"/v1", "o3-mini-2")
	m1, m2 := p1.ID+"/o3-mini", p2.ID+"/o3-mini-2"

	if _, err := store.SaveGroup(routerconfig.Group{
		ID:       "effort-group",
		Name:     "Effort Group",
		Routing:  "ordered",
		Affinity: "off",
		Effort:   "low",
		Members:  []string{m1, m2 + ":xhigh"},
		Levels:   []string{"low", "medium"},
		Fast:     []string{m1},
	}); err != nil {
		t.Fatal(err)
	}
	saveGroupBoundApp(t, store, []string{"group/effort-group"})

	// Request 1 hits m1 -> gets group Effort="low" and Fast service_tier="priority".
	if rec := postAppChat(t, h, "group/effort-group"); rec.Code != http.StatusOK {
		t.Fatalf("req 1 failed: %d %s", rec.Code, rec.Body.String())
	}
	// Request 2 fails m1 -> fails over to m2 which has memberEffort="xhigh" clamped by Levels to "medium" and no Fast.
	failFirst.Store(true)
	if rec := postAppChat(t, h, "group/effort-group"); rec.Code != http.StatusOK {
		t.Fatalf("req 2 failed: %d %s", rec.Code, rec.Body.String())
	}

	mu.Lock()
	defer mu.Unlock()
	if len(gotPayloads) != 2 {
		t.Fatalf("expected 2 captured upstream payloads, got %d", len(gotPayloads))
	}
	if gotPayloads[0]["reasoning_effort"] != "low" || gotPayloads[0]["service_tier"] != "priority" {
		t.Fatalf("m1 expected reasoning_effort=low & service_tier=priority, got %+v", gotPayloads[0])
	}
	if gotPayloads[1]["reasoning_effort"] != "medium" || gotPayloads[1]["service_tier"] != nil {
		t.Fatalf("m2 expected reasoning_effort=medium (clamped from xhigh) & no service_tier, got %+v", gotPayloads[1])
	}
}

func TestGroupRoutingFirstTokenTimeoutFailover(t *testing.T) {
	srvSlow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
			return
		case <-time.After(1500 * time.Millisecond):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"slow","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"slow"},"finish_reason":"stop"}]}`))
		}
	}))
	defer srvSlow.Close()

	srvFast := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"fast","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"fast-fallback"},"finish_reason":"stop"}]}`))
	}))
	defer srvFast.Close()

	h, store := newGroupRoutingEnv(t, ModelOptions{})
	pSlow := saveTestCloudProvider(t, store, "PSlow", srvSlow.URL+"/v1", "mslow")
	pFast := saveTestCloudProvider(t, store, "PFast", srvFast.URL+"/v1", "mfast")

	if _, err := store.SaveGroup(routerconfig.Group{
		ID:         "ft-group",
		Name:       "FirstToken Group",
		Routing:    "ordered",
		Affinity:   "off",
		FirstToken: 1, // 1 second first-token timeout
		Members:    []string{pSlow.ID + "/mslow", pFast.ID + "/mfast"},
	}); err != nil {
		t.Fatal(err)
	}
	saveGroupBoundApp(t, store, []string{"group/ft-group"})

	rec := postAppChat(t, h, "group/ft-group")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "fast-fallback") {
		t.Fatalf("expected firstToken timeout to failover to fast member, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGroupRoutingPatienceRetry(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := attempts.Add(1)
		if n <= 3 {
			// Initial round (up to 3 cloud retry attempts) returns 429; next patience round succeeds.
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"message":"rate limit"}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"recovered","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"patience-ok"},"finish_reason":"stop"}]}`))
	}))
	defer srv.Close()

	h, store := newGroupRoutingEnv(t, ModelOptions{})
	p := saveTestCloudProvider(t, store, "PPatience", srv.URL+"/v1", "mp")

	if _, err := store.SaveGroup(routerconfig.Group{
		ID:       "patience-group",
		Name:     "Patience Group",
		Routing:  "ordered",
		Affinity: "off",
		Patience: 5,
		Members:  []string{p.ID + "/mp"},
	}); err != nil {
		t.Fatal(err)
	}
	saveGroupBoundApp(t, store, []string{"group/patience-group"})

	rec := postAppChat(t, h, "group/patience-group")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "patience-ok") {
		t.Fatalf("expected patience retry to recover and return 200, got %d (attempts=%d): %s", rec.Code, attempts.Load(), rec.Body.String())
	}
}

func TestGroupRoutingAffinityModes(t *testing.T) {
	var failA atomic.Bool
	var callsA, callsB atomic.Int32
	srvA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callsA.Add(1)
		if failA.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"a","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"from-a"},"finish_reason":"stop"}]}`))
	}))
	defer srvA.Close()

	srvB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callsB.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"b","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"from-b"},"finish_reason":"stop"}]}`))
	}))
	defer srvB.Close()

	h, store := newGroupRoutingEnv(t, ModelOptions{})
	pA := saveTestCloudProvider(t, store, "PA", srvA.URL+"/v1", "ma")
	pB := saveTestCloudProvider(t, store, "PB", srvB.URL+"/v1", "mb")
	mA, mB := pA.ID+"/ma", pB.ID+"/mb"

	if _, err := store.SaveGroup(routerconfig.Group{
		ID: "sess-group", Name: "Session Group", Routing: "round-robin",
		Affinity: "session", Stickiness: 10,
		Members: []string{mA, mB},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveGroup(routerconfig.Group{
		ID: "pin-group", Name: "Pin Group", Routing: "ordered",
		Affinity: "pin",
		Members:  []string{mA, mB},
	}); err != nil {
		t.Fatal(err)
	}
	saveGroupBoundApp(t, store, []string{"group/sess-group", "group/pin-group"})

	// 1. Session affinity sticks to the first chosen member across multiple requests with same X-Session-Id,
	// even though strategy is round-robin.
	payload := map[string]any{
		"model":    "group/sess-group",
		"messages": []map[string]string{{"role": "user", "content": "hi"}},
	}
	for i := 0; i < 3; i++ {
		rec := postAppChatWithPayload(t, h, payload, map[string]string{"X-Session-Id": "sess-123"})
		if rec.Code != http.StatusOK {
			t.Fatalf("sess req %d failed: %d", i, rec.Code)
		}
	}
	if (callsA.Load() == 3 && callsB.Load() == 0) || (callsA.Load() == 0 && callsB.Load() == 3) {
		// expected: all 3 calls went to the same sticky member
	} else {
		t.Fatalf("expected session affinity to stick to one member across 3 calls, got A=%d B=%d", callsA.Load(), callsB.Load())
	}

	// 2. Pin affinity: once pinned to mA on turn 1, if mA fails on turn 2, it does NOT failover to mB.
	callsA.Store(0)
	callsB.Store(0)
	pinPayload := map[string]any{
		"model":    "group/pin-group",
		"messages": []map[string]string{{"role": "user", "content": "pin me"}},
	}
	rec1 := postAppChatWithPayload(t, h, pinPayload, map[string]string{"X-Session-Id": "pin-sess"})
	if rec1.Code != http.StatusOK || callsA.Load() != 1 {
		t.Fatalf("pin turn 1 failed: code=%d A=%d", rec1.Code, callsA.Load())
	}
	failA.Store(true)
	callsA.Store(0)
	callsB.Store(0)
	rec2 := postAppChatWithPayload(t, h, pinPayload, map[string]string{"X-Session-Id": "pin-sess"})
	if rec2.Code == http.StatusOK || callsB.Load() != 0 {
		t.Fatalf("pin turn 2 expected failure without failover to mB, got code=%d A=%d B=%d", rec2.Code, callsA.Load(), callsB.Load())
	}
}

func TestGroupRoutingRulesWhenDimensions(t *testing.T) {
	var callsA, callsB atomic.Int32
	srvA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callsA.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"a","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"from-a"},"finish_reason":"stop"}]}`))
	}))
	defer srvA.Close()

	srvB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callsB.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"b","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"from-b"},"finish_reason":"stop"}]}`))
	}))
	defer srvB.Close()

	h, store := newGroupRoutingEnv(t, ModelOptions{})
	pA := saveTestCloudProvider(t, store, "PA", srvA.URL+"/v1", "ma")
	pB := saveTestCloudProvider(t, store, "PB", srvB.URL+"/v1", "mb")
	mA, mB := pA.ID+"/ma", pB.ID+"/mb"

	if _, err := store.SaveGroup(routerconfig.Group{
		ID:       "rules-group",
		Name:     "Rules Group",
		Family:   "custom-alias-pro",
		Routing:  "ordered",
		Affinity: "off",
		Members:  []string{mA, mB},
		Rules: []routerconfig.Rule{
			{When: "vision", Use: mB},
			{When: "tools", Pattern: "web_search", Use: mB},
			{When: "system", Pattern: "architect", Use: mB},
			{When: "prompt", Pattern: "/deploy\\s+prod/i", Use: mB},
			{When: "tokens", Pattern: ">50", Use: mB},
			{When: "model", Pattern: "alias-pro", Use: mB},
		},
	}); err != nil {
		t.Fatal(err)
	}
	saveGroupBoundApp(t, store, []string{"group/rules-group"})

	// Default short prompt without matching rule -> mA (first in ordered).
	recDef := postAppChat(t, h, "group/rules-group")
	if recDef.Code != http.StatusOK || callsA.Load() != 1 || callsB.Load() != 0 {
		t.Fatalf("default expected mA, got A=%d B=%d", callsA.Load(), callsB.Load())
	}

	for _, tc := range []struct {
		name    string
		payload map[string]any
	}{
		{
			name: "tools",
			payload: map[string]any{
				"model":    "group/rules-group",
				"messages": []map[string]string{{"role": "user", "content": "hi"}},
				"tools": []map[string]any{
					{"type": "function", "function": map[string]any{"name": "web_search"}},
				},
			},
		},
		{
			name: "system",
			payload: map[string]any{
				"model": "group/rules-group",
				"messages": []map[string]string{
					{"role": "system", "content": "You are a principal cloud architect."},
					{"role": "user", "content": "hi"},
				},
			},
		},
		{
			name: "prompt_regex",
			payload: map[string]any{
				"model":    "group/rules-group",
				"messages": []map[string]string{{"role": "user", "content": "Please DEPLOY PROD now"}},
			},
		},
		{
			name: "tokens",
			payload: map[string]any{
				"model":    "group/rules-group",
				"messages": []map[string]string{{"role": "user", "content": strings.Repeat("long token text ", 30)}},
			},
		},
		{
			name: "model_when",
			payload: map[string]any{
				"model":    "custom-alias-pro",
				"messages": []map[string]string{{"role": "user", "content": "hi"}},
			},
		},
	} {
		callsA.Store(0)
		callsB.Store(0)
		rec := postAppChatWithPayload(t, h, tc.payload, nil)
		if rec.Code != http.StatusOK || callsB.Load() != 1 || callsA.Load() != 0 {
			t.Fatalf("rule case %s expected mB (A=0, B=1), got status=%d A=%d B=%d body=%s", tc.name, rec.Code, callsA.Load(), callsB.Load(), rec.Body.String())
		}
	}

	// Verify vision rule evaluation across OpenAI image_url, Responses input_image, and Anthropic image blocks.
	cfg, err := store.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	groups := effectiveGroups(cfg, nil)
	var rulesGroup routerconfig.Group
	for _, g := range groups {
		if g.ID == "rules-group" {
			rulesGroup = g
			break
		}
	}
	for _, visionBody := range []string{
		`{"model":"group/rules-group","messages":[{"role":"user","content":[{"type":"text","text":"look"},{"type":"image_url","image_url":{"url":"https://example.com/a.png"}}]}]}`,
		`{"model":"group/rules-group","input":[{"role":"user","content":[{"type":"input_image","image_url":"https://example.com/a.png"}]}]}`,
		`{"model":"group/rules-group","messages":[{"role":"user","content":[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"AAAA"}}]}]}`,
	} {
		promoted, matchedRule := evaluateGroupRules(rulesGroup, groups, "chat", []byte(visionBody), rulesGroup.Members)
		if matchedRule == nil || len(promoted) == 0 || promoted[0] != mB {
			t.Fatalf("expected vision rule to match %s for body %s, got rule=%v promoted=%v", mB, visionBody, matchedRule, promoted)
		}
	}
}

func TestGroupRoutingEdgeCloudAndSaveValidation(t *testing.T) {
	var cloudCalls atomic.Int32
	cloudSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cloudCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"cloud-ec","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"from-cloud"},"finish_reason":"stop"}]}`))
	}))
	defer cloudSrv.Close()

	judge := &scriptedJudge{decision: DecisionLocal}
	invoker := &scriptedInvoker{response: localChatResponse()}
	h, store := newGroupRoutingEnv(t, ModelOptions{
		Routing: &RoutingOptions{Judge: judge, LocalInvoker: invoker},
	})
	p := saveTestCloudProvider(t, store, "CloudEC", cloudSrv.URL+"/v1", "gpt-4.1")

	// Saving edge-cloud without local/... member falls back to ""
	rec := doGroupRequest(t, h, "mgmt-secret-token", http.MethodPost, "/api/routing-groups", map[string]any{
		"name":    "Cloud Only EC",
		"routing": "edge-cloud",
		"members": []string{p.ID + "/gpt-4.1"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 saving cloud-only group, got %d: %s", rec.Code, rec.Body.String())
	}
	var savedCloudOnly struct {
		Group routerconfig.Group `json:"group"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &savedCloudOnly); err != nil {
		t.Fatal(err)
	}
	if savedCloudOnly.Group.Routing != "" {
		t.Fatalf("expected cloud-only group routing to fall back to empty string, got %q", savedCloudOnly.Group.Routing)
	}

	// Saving edge-cloud with local/... member preserves edge-cloud
	rec = doGroupRequest(t, h, "mgmt-secret-token", http.MethodPost, "/api/routing-groups", map[string]any{
		"id":      "ec-group",
		"name":    "Edge Cloud Group",
		"routing": "edge-cloud",
		"members": []string{"local/qwen2.5-coder-7b", p.ID + "/gpt-4.1"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 saving edge-cloud group, got %d: %s", rec.Code, rec.Body.String())
	}
	var savedEC struct {
		Group routerconfig.Group `json:"group"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &savedEC); err != nil {
		t.Fatal(err)
	}
	if savedEC.Group.Routing != "edge-cloud" {
		t.Fatalf("expected edge-cloud group routing to be preserved, got %q", savedEC.Group.Routing)
	}

	saveGroupBoundApp(t, store, []string{"group/ec-group"})

	// Simple task -> local first
	judge.decision = DecisionLocal
	chatRec := postAppChat(t, h, "group/ec-group")
	if chatRec.Code != http.StatusOK || invoker.calls.Load() != 1 || cloudCalls.Load() != 0 {
		t.Fatalf("expected local execution on simple task, got status=%d local=%d cloud=%d", chatRec.Code, invoker.calls.Load(), cloudCalls.Load())
	}

	// Complex task -> cloud first
	judge.decision = DecisionCloud
	chatRec = postAppChat(t, h, "group/ec-group")
	if chatRec.Code != http.StatusOK || cloudCalls.Load() != 1 {
		t.Fatalf("expected cloud execution on complex task, got status=%d cloud=%d", chatRec.Code, cloudCalls.Load())
	}
}

func TestGroupLocalRedactionExecution(t *testing.T) {
	var upstreamBodies []string
	var mu sync.Mutex
	cloudSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		upstreamBodies = append(upstreamBodies, string(raw))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl-red","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"已收到<NAME:1>的电话<PHONE:1>"},"finish_reason":"stop"}]}`))
	}))
	defer cloudSrv.Close()

	llmInvoker := &scriptedInvoker{
		response: &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"{\"entities\":[{\"value\":\"张三\",\"label\":\"NAME\"}]}"}}]}`)),
		},
	}
	h, store := newGroupRoutingEnv(t, ModelOptions{
		Routing: &RoutingOptions{LocalInvoker: llmInvoker},
	})
	p := saveTestCloudProvider(t, store, "CloudRedact", cloudSrv.URL+"/v1", "gpt-4.1")

	// 1. Cloud-only group cannot enable localRedaction
	rec := doGroupRequest(t, h, "mgmt-secret-token", http.MethodPost, "/api/routing-groups", map[string]any{
		"id":             "cloud-only-redact",
		"name":           "Cloud Only Redact",
		"routing":        "order",
		"localRedaction": true,
		"members":        []string{p.ID + "/gpt-4.1"},
	})
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "redaction_model_missing") {
		t.Fatalf("expected explicit rejection instead of clearing redaction intent, got %d: %s", rec.Code, rec.Body.String())
	}

	// 2. Group with local/* model and localRedaction=true redacts before routing and restores response
	rec = doGroupRequest(t, h, "mgmt-secret-token", http.MethodPost, "/api/routing-groups", map[string]any{
		"id":             "redact-group",
		"name":           "Redact Group",
		"routing":        "order",
		"localRedaction": true,
		"members":        []string{"local/qwen3-4b-instruct-2507-q4_k_m", p.ID + "/gpt-4.1"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 saving redact group, got %d: %s", rec.Code, rec.Body.String())
	}
	var savedRedact struct {
		Group routerconfig.Group `json:"group"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &savedRedact); err != nil {
		t.Fatal(err)
	}
	if !savedRedact.Group.LocalRedaction {
		t.Fatalf("expected group localRedaction to be true")
	}

	saveGroupBoundApp(t, store, []string{"group/redact-group"})

	chatPayload := map[string]any{
		"model": "group/redact-group",
		"messages": []map[string]any{
			{"role": "user", "content": "请联系张三，手机号 13812345678，合同金额86.4万元"},
		},
	}
	chatRec := doGroupRequest(t, h, "", http.MethodPost, "/app/codex/v1/chat/completions", chatPayload)
	if chatRec.Code != http.StatusOK {
		t.Fatalf("expected 200 on redacted group chat, got %d: %s", chatRec.Code, chatRec.Body.String())
	}

	mu.Lock()
	if len(upstreamBodies) != 1 {
		mu.Unlock()
		t.Fatalf("expected 1 upstream cloud call, got %d", len(upstreamBodies))
	}
	sentUpstream := upstreamBodies[0]
	mu.Unlock()

	if strings.Contains(sentUpstream, "张三") || strings.Contains(sentUpstream, "13812345678") || strings.Contains(sentUpstream, "86.4万元") {
		t.Fatalf("expected upstream body to be redacted and amount generalized, got: %s", sentUpstream)
	}
	if !strings.Contains(sentUpstream, "<NAME:1>") || !strings.Contains(sentUpstream, "<PHONE:1>") || !strings.Contains(sentUpstream, "约85万元") {
		t.Fatalf("expected placeholders and generalized amount in upstream body, got: %s", sentUpstream)
	}
	if !strings.Contains(chatRec.Body.String(), "已收到张三的电话13812345678") {
		t.Fatalf("expected placeholders to be restored in client response, got: %s", chatRec.Body.String())
	}

	// 3. When local runtime is unavailable, a localRedaction group must fail closed and never forward raw text upstream.
	hNoRuntime, storeNoRuntime := newGroupRoutingEnv(t, ModelOptions{})
	pNoRuntime := saveTestCloudProvider(t, storeNoRuntime, "CloudRedactFailClosed", cloudSrv.URL+"/v1", "gpt-4.1")
	if _, err := storeNoRuntime.SaveGroup(routerconfig.Group{
		ID:             "redact-fail-closed",
		Name:           "Redact Fail Closed",
		Routing:        "order",
		LocalRedaction: true,
		Members:        []string{"local/qwen3-4b-instruct-2507-q4_k_m", pNoRuntime.ID + "/gpt-4.1"},
	}); err != nil {
		t.Fatal(err)
	}
	saveGroupBoundApp(t, storeNoRuntime, []string{"group/redact-fail-closed"})
	mu.Lock()
	upstreamBodies = nil
	mu.Unlock()
	failClosedRec := doGroupRequest(t, hNoRuntime, "", http.MethodPost, "/app/codex/v1/chat/completions", map[string]any{
		"model": "group/redact-fail-closed",
		"messages": []map[string]any{
			{"role": "user", "content": "请联系张三，内部项目代号青鸾，预算八十六万四千。"},
		},
	})
	mu.Lock()
	leakedCalls := len(upstreamBodies)
	mu.Unlock()
	if failClosedRec.Code < 400 || leakedCalls != 0 {
		t.Fatalf("expected group local redaction failure to abort before upstream dispatch, got status=%d upstreamCalls=%d body=%s", failClosedRec.Code, leakedCalls, failClosedRec.Body.String())
	}
}

type dynamicPayloadInvoker struct {
	mu      sync.Mutex
	payload string
	status  int
}

func (f *dynamicPayloadInvoker) set(status int, payload string) {
	f.mu.Lock()
	f.status = status
	f.payload = payload
	f.mu.Unlock()
}

func (f *dynamicPayloadInvoker) Invoke(_ context.Context, _ routerruntime.Purpose, _ []byte, fn func(*http.Response) error) error {
	f.mu.Lock()
	status := f.status
	payload := f.payload
	f.mu.Unlock()
	if status == 0 {
		status = http.StatusOK
	}
	resp := &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(payload)),
	}
	defer resp.Body.Close()
	return fn(resp)
}

func TestGroupLocalRedactionInvalidCompletionMustBlock(t *testing.T) {
	var upstreamBodies []string
	var mu sync.Mutex
	cloudSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		upstreamBodies = append(upstreamBodies, string(raw))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl-red","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"已收到<NAME:1>"},"finish_reason":"stop"}]}`))
	}))
	defer cloudSrv.Close()

	llmInvoker := &dynamicPayloadInvoker{}
	h, store := newGroupRoutingEnv(t, ModelOptions{
		Routing: &RoutingOptions{LocalInvoker: llmInvoker},
	})
	p := saveTestCloudProvider(t, store, "CloudRedactAudit", cloudSrv.URL+"/v1", "gpt-4.1")
	if _, err := store.SaveGroup(routerconfig.Group{
		ID:             "audit-redact-group",
		Name:           "Audit Redact Group",
		Routing:        "order",
		LocalRedaction: true,
		Members:        []string{"local/qwen3-1.7b-q4_k_m", p.ID + "/gpt-4.1"},
	}); err != nil {
		t.Fatal(err)
	}
	saveGroupBoundApp(t, store, []string{"group/audit-redact-group"})

	badPayloads := []struct {
		name    string
		payload string
	}{
		{name: "empty_content", payload: `{"choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":""}}]}`},
		{name: "malformed_json", payload: `{"choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"not-json"}}]}`},
		{name: "truncated_json", payload: `{"choices":[{"index":0,"finish_reason":"length","message":{"role":"assistant","content":"{\"entities\":["}}]}`},
		{name: "truncated_partial_entities", payload: `{"choices":[{"index":0,"finish_reason":"length","message":{"role":"assistant","content":"{\"entities\":[{\"value\":\"宁祁\",\"label\":\"NAME\"}]}"}}]}`},
		{name: "unknown_label", payload: `{"choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"{\"entities\":[{\"value\":\"宁祁\",\"label\":\"UNRECOGNIZED_KIND\"}]}"}}]}`},
		{name: "invalid_outer_envelope", payload: `{"choices":`},
		{name: "empty_choices", payload: `{"choices":[]}`},
		{name: "truncated_flag", payload: `{"truncated":true,"choices":[{"finish_reason":"stop","message":{"content":"{\"entities\":[]}"}}]}`},
	}

	for i, tc := range badPayloads {
		t.Run(tc.name, func(t *testing.T) {
			llmInvoker.set(http.StatusOK, tc.payload)
			mu.Lock()
			upstreamBodies = nil
			mu.Unlock()

			rec := doGroupRequest(t, h, "", http.MethodPost, "/app/codex/v1/chat/completions", map[string]any{
				"model": "group/audit-redact-group",
				"messages": []map[string]any{
					{"role": "user", "content": "请转告宁祁明天到场，第" + string(rune('A'+i)) + "次确认。"},
				},
			})
			mu.Lock()
			leaked := append([]string(nil), upstreamBodies...)
			mu.Unlock()
			if rec.Code < 400 || len(leaked) > 0 {
				t.Fatalf("%s: expected fail-closed block with 0 upstream calls; got status=%d upstreamCalls=%d upstream=%v resp=%s",
					tc.name, rec.Code, len(leaked), leaked, rec.Body.String())
			}
		})
	}

	// Control case: valid LLM output succeeds, redacts "宁祁" -> "<NAME:1>", and restores it in the response.
	llmInvoker.set(http.StatusOK, `{"choices":[{"finish_reason":"stop","message":{"content":"{\"entities\":[{\"label\":\"NAME\",\"value\":\"宁祁\"}]}"}}]}`)
	mu.Lock()
	upstreamBodies = nil
	mu.Unlock()
	okRec := doGroupRequest(t, h, "", http.MethodPost, "/app/codex/v1/chat/completions", map[string]any{
		"model": "group/audit-redact-group",
		"messages": []map[string]any{
			{"role": "user", "content": "请转告宁祁明天到场，最终确认。"},
		},
	})
	mu.Lock()
	sent := append([]string(nil), upstreamBodies...)
	mu.Unlock()
	if okRec.Code != http.StatusOK || len(sent) != 1 || strings.Contains(sent[0], "宁祁") || !strings.Contains(sent[0], "<NAME:1>") {
		t.Fatalf("control valid payload failed: status=%d upstream=%v resp=%s", okRec.Code, sent, okRec.Body.String())
	}
	if !strings.Contains(okRec.Body.String(), "已收到宁祁") {
		t.Fatalf("expected placeholder restoration in response, got: %s", okRec.Body.String())
	}
}

func TestGroupOrchestratorRedactorEnforcesManagerBindingAndFormalAcceptance(t *testing.T) {
	layout, _ := fixture(t)
	baseStore := routerconfig.New(layout)
	const modelID = "spark-x2.6-4B"
	localSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"{\"entities\":[{\"label\":\"NAME\",\"value\":\"宁祁\"}]}"}}]}`))
	}))
	defer localSrv.Close()

	catalog := routermanifest.Fixed()
	runtimeID := routerruntime.PlatformRuntime(catalog, runtime.GOOS, runtime.GOARCH)
	mgr, err := routerruntime.New(context.Background(), routerruntime.Options{
		Catalog:               catalog,
		RuntimeID:             runtimeID,
		Store:                 routerruntime.ConfigIntentStore{Store: baseStore},
		Installer:             integrationInstaller{},
		Factory:               integrationFactory{endpoint: localSrv.URL},
		ExperimentalRedaction: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.Close(context.Background())
	if err := mgr.Select(context.Background(), modelID); err != nil {
		t.Fatal(err)
	}
	if err := mgr.Enable(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, check := range []routerruntime.Check{
		{Purpose: routerruntime.Judgment, Suite: "judgment-engineering-v1", Run: func(context.Context, string) (string, error) { return "judgment-fixture", nil }},
		{Purpose: routerruntime.Execution, Suite: "execution-engineering-v1", Run: func(context.Context, string) (string, error) { return "execution-fixture", nil }},
	} {
		if err := mgr.CheckCapability(context.Background(), check); err != nil {
			t.Fatal(err)
		}
	}

	var upstreamCalls atomic.Int32
	cloudSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl-1","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	}))
	defer cloudSrv.Close()

	// 1. Formal mode (ExperimentalRedaction=false) without formal Redaction capability acceptance MUST fail closed,
	// even if the text only contains rule-detectable PII (13800138000) or LLM-detectable PII (宁祁).
	hFormal, storeFormal := newGroupRoutingEnv(t, ModelOptions{
		LocalRuntime:          &LocalModels{Manager: mgr, Layout: layout, Catalog: catalog},
		ExperimentalRedaction: false,
	})
	pFormal := saveTestCloudProvider(t, storeFormal, "CloudFormal", cloudSrv.URL+"/v1", "gpt-4.1")
	if _, err := storeFormal.SaveGroup(routerconfig.Group{
		ID:             "formal-redact-group",
		Name:           "Formal Redact Group",
		Routing:        "order",
		LocalRedaction: true,
		Members:        []string{"local/" + modelID, pFormal.ID + "/gpt-4.1"},
	}); err != nil {
		t.Fatal(err)
	}
	saveGroupBoundApp(t, storeFormal, []string{"group/formal-redact-group"})

	recFormal := doGroupRequest(t, hFormal, "", http.MethodPost, "/app/codex/v1/chat/completions", map[string]any{
		"model": "group/formal-redact-group",
		"messages": []map[string]any{
			{"role": "user", "content": "请转告宁祁（手机 13800138000）明天参会。"},
		},
	})
	if recFormal.Code < 400 || upstreamCalls.Load() != 0 {
		t.Fatalf("expected formal group redaction without capability acceptance to fail closed; got status=%d upstreamCalls=%d body=%s",
			recFormal.Code, upstreamCalls.Load(), recFormal.Body.String())
	}

	// 2. Experimental mode (ExperimentalRedaction=true) succeeds when bound model matches and uses ExperimentalRedactionInvoker,
	// but fails closed if the group's local/* model does not match the Manager's active model.
	hExp, storeExp := newGroupRoutingEnv(t, ModelOptions{
		LocalRuntime:          &LocalModels{Manager: mgr, Layout: layout, Catalog: catalog},
		ExperimentalRedaction: true,
	})
	pExp := saveTestCloudProvider(t, storeExp, "CloudExp", cloudSrv.URL+"/v1", "gpt-4.1")
	if _, err := storeExp.SaveGroup(routerconfig.Group{
		ID:             "exp-mismatch-group",
		Name:           "Exp Mismatch Group",
		Routing:        "order",
		LocalRedaction: true,
		Members:        []string{"local/spark-x2.6-4B-Q8_0", pExp.ID + "/gpt-4.1"},
	}); err != nil {
		t.Fatal(err)
	}
	saveGroupBoundApp(t, storeExp, []string{"group/exp-mismatch-group"})

	recMismatch := doGroupRequest(t, hExp, "", http.MethodPost, "/app/codex/v1/chat/completions", map[string]any{
		"model": "group/exp-mismatch-group",
		"messages": []map[string]any{
			{"role": "user", "content": "请转告宁祁明天参会。"},
		},
	})
	if recMismatch.Code < 400 || upstreamCalls.Load() != 0 {
		t.Fatalf("expected group with mismatched local/* model to fail closed; got status=%d upstreamCalls=%d body=%s",
			recMismatch.Code, upstreamCalls.Load(), recMismatch.Body.String())
	}

	// 3. After formal Redaction capability acceptance on the bound model+suite, formal mode succeeds.
	id, settings := mgr.SnapshotBinding(context.Background())
	spec, err := routerredact.NewPrivacySpecification(routerredact.ModelBinding{
		ModelID:       id.ModelID,
		RuntimeID:     id.RuntimeID,
		ContextTokens: settings.Effective().ContextTokens,
		ExecutionPlan: id.ExecutionPlan,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.CheckCapability(context.Background(), routerruntime.Check{
		Purpose: routerruntime.Redaction,
		Suite:   spec.CapabilitySuite(),
		Run:     func(context.Context, string) (string, error) { return "redaction-fixture", nil },
	}); err != nil {
		t.Fatal(err)
	}

	recAccepted := doGroupRequest(t, hFormal, "", http.MethodPost, "/app/codex/v1/chat/completions", map[string]any{
		"model": "group/formal-redact-group",
		"messages": []map[string]any{
			{"role": "user", "content": "请转告宁祁明天参会。"},
		},
	})
	if recAccepted.Code != http.StatusOK || upstreamCalls.Load() != 1 {
		t.Fatalf("expected formal group redaction after capability acceptance to succeed; got status=%d upstreamCalls=%d body=%s",
			recAccepted.Code, upstreamCalls.Load(), recAccepted.Body.String())
	}
}

func TestGroupMultiCandidateRetryPersistsLedgerRecordWithNormalizedAttempts(t *testing.T) {
	cand1Srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":{"message":"upstream busy"}}`))
	}))
	defer cand1Srv.Close()

	cand2Srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl-ok","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok-from-cand2"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`))
	}))
	defer cand2Srv.Close()

	ledgerPath := filepath.Join(t.TempDir(), "requests.sqlite")
	ledgerStore, err := routerledger.Open(ledgerPath, routerledger.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer ledgerStore.Close()

	h, store := newGroupRoutingEnv(t, ModelOptions{
		Ledger: ledgerStore,
	})
	p1 := saveTestCloudProvider(t, store, "Cand1", cand1Srv.URL+"/v1", "m1")
	p2 := saveTestCloudProvider(t, store, "Cand2", cand2Srv.URL+"/v1", "m2")
	if _, err := store.SaveGroup(routerconfig.Group{
		ID:      "multi-attempt-group",
		Name:    "Multi Attempt Group",
		Routing: "order",
		Members: []string{p1.ID + "/m1", p2.ID + "/m2"},
	}); err != nil {
		t.Fatal(err)
	}
	saveGroupBoundApp(t, store, []string{"group/multi-attempt-group"})

	rec := doGroupRequest(t, h, "", http.MethodPost, "/app/codex/v1/chat/completions", map[string]any{
		"model": "group/multi-attempt-group",
		"messages": []map[string]any{
			{"role": "user", "content": "hello"},
		},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200 after fallback to candidate 2, got %d: %s", rec.Code, rec.Body.String())
	}

	st := ledgerStore.Status()
	if st.FailureCount != 0 || st.ObservationFailures != 0 {
		t.Fatalf("expected 0 ledger failures, got FailureCount=%d LastErrorCode=%q ObservationFailures=%d",
			st.FailureCount, st.LastErrorCode, st.ObservationFailures)
	}
	page, err := ledgerStore.List(routerledger.Query{Limit: 10})
	if err != nil || len(page.Records) != 1 {
		t.Fatalf("expected 1 ledger record, got len=%d err=%v", len(page.Records), err)
	}
	r := page.Records[0]
	if r.Status != "succeeded" || r.Attempts != 3 || len(r.AttemptDetails) != 3 {
		t.Fatalf("expected succeeded record with 3 attempts (2 on cand1 + 1 on cand2), got status=%q attempts=%d details=%+v",
			r.Status, r.Attempts, r.AttemptDetails)
	}
	for i, a := range r.AttemptDetails {
		if a.Number != i+1 {
			t.Fatalf("attempt[%d].Number = %d, want %d", i, a.Number, i+1)
		}
		wantRetried := i+1 < len(r.AttemptDetails)
		if a.Retried != wantRetried {
			t.Fatalf("attempt[%d].Retried = %v, want %v", i, a.Retried, wantRetried)
		}
	}
}

type purposeSwitchInvoker struct {
	mu          sync.Mutex
	redactResp  string
	execResp    string
	execHeaders http.Header
	execStatus  int
	execBodies  []string
}

func (p *purposeSwitchInvoker) Invoke(_ context.Context, purpose routerruntime.Purpose, body []byte, fn func(*http.Response) error) error {
	p.mu.Lock()
	var payload string
	status := http.StatusOK
	headers := http.Header{"Content-Type": []string{"application/json"}}
	if purpose == routerruntime.Redaction {
		payload = p.redactResp
	} else {
		p.execBodies = append(p.execBodies, string(body))
		payload = p.execResp
		if p.execStatus != 0 {
			status = p.execStatus
		}
		if p.execHeaders != nil {
			headers = p.execHeaders.Clone()
		}
	}
	p.mu.Unlock()
	resp := &http.Response{
		StatusCode: status,
		Header:     headers,
		Body:       io.NopCloser(strings.NewReader(payload)),
	}
	defer resp.Body.Close()
	return fn(resp)
}

func (p *purposeSwitchInvoker) takeExecBodies() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := append([]string(nil), p.execBodies...)
	p.execBodies = nil
	return out
}

func TestGroupLocalCandidateRestoresRedactedResponsesAcrossProtocols(t *testing.T) {
	invoker := &purposeSwitchInvoker{
		redactResp: `{"choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"{\"entities\":[{\"label\":\"NAME\",\"value\":\"宁祁\"}]}"}}]}`,
		execResp:   `{"id":"local-chat-1","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"已为<NAME:1>登记手机号<PHONE:1>"},"finish_reason":"stop"}],"usage":{"prompt_tokens":12,"completion_tokens":8,"total_tokens":20}}`,
	}
	h, store := newGroupRoutingEnv(t, ModelOptions{
		Routing: &RoutingOptions{LocalInvoker: invoker},
	})
	if _, err := store.SaveGroup(routerconfig.Group{
		ID:             "local-restore-group",
		Name:           "Local Restore Group",
		Routing:        "order",
		LocalRedaction: true,
		Members:        []string{"local/qwen3-1.7b-q4_k_m"},
	}); err != nil {
		t.Fatal(err)
	}
	saveGroupBoundApp(t, store, []string{"group/local-restore-group"})

	cases := []struct {
		name    string
		path    string
		payload map[string]any
	}{
		{
			name: "chat_completions",
			path: "/app/codex/v1/chat/completions",
			payload: map[string]any{
				"model": "group/local-restore-group",
				"messages": []map[string]any{
					{"role": "user", "content": "请帮宁祁登记手机号 13800138000，协议 chat"},
				},
			},
		},
		{
			name: "anthropic_messages",
			path: "/app/codex/v1/messages",
			payload: map[string]any{
				"model":      "group/local-restore-group",
				"max_tokens": 128,
				"messages": []map[string]any{
					{"role": "user", "content": "请帮宁祁登记手机号 13800138000，协议 anthropic"},
				},
			},
		},
		{
			name: "openai_responses",
			path: "/app/codex/v1/responses",
			payload: map[string]any{
				"model": "group/local-restore-group",
				"input": "请帮宁祁登记手机号 13800138000，协议 responses",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := doGroupRequest(t, h, "", http.MethodPost, tc.path, tc.payload)
			execBodies := invoker.takeExecBodies()
			if rec.Code != http.StatusOK || len(execBodies) != 1 {
				t.Fatalf("expected HTTP 200 and 1 local execution call, got status=%d execCalls=%d body=%s",
					rec.Code, len(execBodies), rec.Body.String())
			}
			if strings.Contains(execBodies[0], "宁祁") || strings.Contains(execBodies[0], "13800138000") {
				t.Fatalf("expected local execution input to be redacted, got: %s", execBodies[0])
			}
			if (!strings.Contains(execBodies[0], "<NAME:1>") && !strings.Contains(execBodies[0], `\u003cNAME:1\u003e`)) ||
				(!strings.Contains(execBodies[0], "<PHONE:1>") && !strings.Contains(execBodies[0], `\u003cPHONE:1\u003e`)) {
				t.Fatalf("expected placeholders in local execution input, got: %s", execBodies[0])
			}
			respText := rec.Body.String()
			if !strings.Contains(respText, "已为宁祁登记手机号13800138000") {
				t.Fatalf("expected restored sensitive values in client response, got: %s", respText)
			}
			if strings.Contains(respText, "<NAME:") || strings.Contains(respText, "<PHONE:") {
				t.Fatalf("expected zero residual placeholders in client response, got: %s", respText)
			}
		})
	}
}

func TestGroupLocalCandidateRestoresStreamingSSEResponse(t *testing.T) {
	ssePayload := strings.Join([]string{
		`data: {"id":"chatcmpl-sse","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"role":"assistant","content":"你好，"}}]}`,
		"",
		`data: {"id":"chatcmpl-sse","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"<NA"}}]}`,
		"",
		`data: {"id":"chatcmpl-sse","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"ME:1>，手机 "}}]}`,
		"",
		`data: {"id":"chatcmpl-sse","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"<PHO"}}]}`,
		"",
		`data: {"id":"chatcmpl-sse","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"NE:1> 已确认"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":6,"total_tokens":16}}`,
		"",
		`data: [DONE]`,
		"",
		"",
	}, "\n")
	invoker := &purposeSwitchInvoker{
		redactResp:  `{"choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"{\"entities\":[{\"label\":\"NAME\",\"value\":\"宁祁\"}]}"}}]}`,
		execResp:    ssePayload,
		execHeaders: http.Header{"Content-Type": []string{"text/event-stream"}},
	}
	h, store := newGroupRoutingEnv(t, ModelOptions{
		Routing: &RoutingOptions{LocalInvoker: invoker},
	})
	if _, err := store.SaveGroup(routerconfig.Group{
		ID:             "local-sse-restore-group",
		Name:           "Local SSE Restore Group",
		Routing:        "order",
		LocalRedaction: true,
		Members:        []string{"local/qwen3-1.7b-q4_k_m"},
	}); err != nil {
		t.Fatal(err)
	}
	saveGroupBoundApp(t, store, []string{"group/local-sse-restore-group"})

	rec := doGroupRequest(t, h, "", http.MethodPost, "/app/codex/v1/chat/completions", map[string]any{
		"model":  "group/local-sse-restore-group",
		"stream": true,
		"messages": []map[string]any{
			{"role": "user", "content": "请通知宁祁（13800138000）流式确认"},
		},
	})
	execBodies := invoker.takeExecBodies()
	if rec.Code != http.StatusOK || len(execBodies) != 1 {
		t.Fatalf("expected HTTP 200 and 1 local execution call, got status=%d execCalls=%d body=%s",
			rec.Code, len(execBodies), rec.Body.String())
	}
	if strings.Contains(execBodies[0], "宁祁") || strings.Contains(execBodies[0], "13800138000") {
		t.Fatalf("expected local execution input to be redacted, got: %s", execBodies[0])
	}
	respSSE := rec.Body.String()
	if !strings.Contains(respSSE, "宁祁") || !strings.Contains(respSSE, "13800138000") {
		t.Fatalf("expected restored entity values in SSE stream, got:\n%s", respSSE)
	}
	if strings.Contains(respSSE, "<NAME:") || strings.Contains(respSSE, "<PHONE:") || strings.Contains(respSSE, "<NA") || strings.Contains(respSSE, "<PHO") {
		t.Fatalf("expected zero residual placeholder fragments in SSE stream, got:\n%s", respSSE)
	}
}

func TestGroupLocalCandidateRestoreFailureFailsClosedWithoutCommittingOutput(t *testing.T) {
	invoker := &purposeSwitchInvoker{
		redactResp: `{"choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"{\"entities\":[{\"label\":\"NAME\",\"value\":\"宁祁\"}]}"}}]}`,
		// Malformed JSON causes chat->anthropic response translation / restore to fail before output commit.
		execResp: `{"id":"broken-json","choices":[{"message":{"content":"<NAME:1>`,
	}
	var cloudCalls atomic.Int32
	cloudSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cloudCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"cloud-fallback","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"来自云端恢复<NAME:1>"},"finish_reason":"stop"}]}`))
	}))
	defer cloudSrv.Close()

	h, store := newGroupRoutingEnv(t, ModelOptions{
		Routing: &RoutingOptions{LocalInvoker: invoker},
	})
	p := saveTestCloudProvider(t, store, "CloudFallback", cloudSrv.URL+"/v1", "gpt-4.1")
	if _, err := store.SaveGroup(routerconfig.Group{
		ID:             "local-restore-failclosed-only",
		Name:           "Local Restore FailClosed Only",
		Routing:        "order",
		LocalRedaction: true,
		Members:        []string{"local/qwen3-1.7b-q4_k_m"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveGroup(routerconfig.Group{
		ID:             "local-restore-fallback-group",
		Name:           "Local Restore Fallback Group",
		Routing:        "order",
		LocalRedaction: true,
		Members:        []string{"local/qwen3-1.7b-q4_k_m", p.ID + "/gpt-4.1"},
	}); err != nil {
		t.Fatal(err)
	}
	saveGroupBoundApp(t, store, []string{"group/local-restore-failclosed-only", "group/local-restore-fallback-group"})

	// 1. Single local candidate: fails closed with non-200 error and never leaks unrestored placeholder text.
	recSingle := doGroupRequest(t, h, "", http.MethodPost, "/app/codex/v1/chat/completions", map[string]any{
		"model": "group/local-restore-failclosed-only",
		"messages": []map[string]any{
			{"role": "user", "content": "请通知宁祁单节点失败闭合测试"},
		},
	})
	if recSingle.Code < 400 || strings.Contains(recSingle.Body.String(), "<NAME:1>") || strings.Contains(recSingle.Body.String(), "broken-json") {
		t.Fatalf("expected fail-closed error response without partial/unrestored body, got status=%d body=%s",
			recSingle.Code, recSingle.Body.String())
	}

	// 2. Multi-candidate group: because OutputCommitted remained false when local restore failed, fallback to cloud succeeds and restores.
	recFallback := doGroupRequest(t, h, "", http.MethodPost, "/app/codex/v1/chat/completions", map[string]any{
		"model": "group/local-restore-fallback-group",
		"messages": []map[string]any{
			{"role": "user", "content": "请通知宁祁多节点回退恢复测试"},
		},
	})
	if recFallback.Code != http.StatusOK || cloudCalls.Load() != 1 || !strings.Contains(recFallback.Body.String(), "来自云端恢复宁祁") {
		t.Fatalf("expected fallback to cloud candidate with restored response, got status=%d cloudCalls=%d body=%s",
			recFallback.Code, cloudCalls.Load(), recFallback.Body.String())
	}
}

func waitPipelineDone(t *testing.T, p *routerredact.Pipeline, label string) {
	t.Helper()
	if p == nil {
		t.Fatalf("%s: expected non-nil pipeline", label)
	}
	select {
	case <-p.Done():
	case <-time.After(2 * time.Second):
		t.Fatalf("%s: timed out waiting for pipeline expiryLoop to stop", label)
	}
}

func TestGroupOrchestratorRedactorLifecycleAndLeakPrevention(t *testing.T) {
	t.Run("ephemeral_non_cacheable_closes_pipeline", func(t *testing.T) {
		invoker := &dynamicPayloadInvoker{}
		invoker.set(http.StatusOK, `{"choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"{\"entities\":[{\"label\":\"NAME\",\"value\":\"宁祁\"}]}"}}]}`)
		opts := ModelOptions{
			Routing: &RoutingOptions{LocalInvoker: invoker},
		}
		redactor := getGroupOrchestratorRedactor(opts, "qwen3-1.7b-q4_k_m")
		if !redactor.ephemeral {
			t.Fatalf("expected non-cacheable redactor to be marked ephemeral")
		}
		// RoutingVersions() must not allocate an active pipeline before Prepare.
		_ = redactor.RoutingVersions()
		redactor.mu.Lock()
		preActive := redactor.active
		redactor.mu.Unlock()
		if preActive != nil {
			t.Fatalf("expected RoutingVersions() not to pre-create a pipeline on ephemeral redactor")
		}

		prep, err := redactor.Prepare(context.Background(), RedactionRequest{
			RequestID:      "req-eph-1",
			AppID:          "codex",
			SourceProtocol: "chat",
			TargetProtocol: "chat",
			Operation:      "generation",
			Body:           []byte(`{"model":"m","messages":[{"role":"user","content":"请联系宁祁 13800138000"}]}`),
		})
		if err != nil {
			t.Fatalf("Prepare failed: %v", err)
		}
		redactor.mu.Lock()
		p1 := redactor.active
		redactor.mu.Unlock()
		if p1 == nil {
			t.Fatalf("expected active pipeline during in-flight Prepare")
		}
		_ = prep.Close()
		waitPipelineDone(t, p1, "ephemeral pipeline after prep.Close")
		if stats := redactor.Stats(); stats.Processed != 1 {
			t.Fatalf("expected retired stats to preserve Processed=1 after ephemeral close, got %+v", stats)
		}
	})

	t.Run("concurrent_cold_cache_single_pipeline", func(t *testing.T) {
		_, store := newGroupRoutingEnv(t, ModelOptions{})
		opts := ModelOptions{store: store}
		const workers = 32
		start := make(chan struct{})
		var wg sync.WaitGroup
		redactors := make([]*groupOrchestratorRedactor, workers)
		pipelines := make([]*routerredact.Pipeline, workers)
		for i := 0; i < workers; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				<-start
				r := getGroupOrchestratorRedactor(opts, "spark-x2.6-4B")
				p, _, _ := r.current(context.Background())
				redactors[idx] = r
				pipelines[idx] = p
			}(i)
		}
		close(start)
		wg.Wait()
		for i := 1; i < workers; i++ {
			if redactors[i] != redactors[0] {
				t.Fatalf("worker %d got different redactor instance %p != %p", i, redactors[i], redactors[0])
			}
			if pipelines[i] != pipelines[0] {
				t.Fatalf("worker %d got different pipeline instance %p != %p", i, pipelines[i], pipelines[0])
			}
		}
		closeGroupOrchestratorRedactors(store, nil)
		waitPipelineDone(t, pipelines[0], "cached pipeline after closeGroupOrchestratorRedactors")
	})

	t.Run("model_switch_closes_idle_and_inflight_retired_pipelines", func(t *testing.T) {
		layout, _ := fixture(t)
		baseStore := routerconfig.New(layout)
		localSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"{\"entities\":[{\"label\":\"NAME\",\"value\":\"宁祁\"}]}"}}]}`))
		}))
		defer localSrv.Close()

		catalog := routermanifest.Fixed()
		runtimeID := routerruntime.PlatformRuntime(catalog, runtime.GOOS, runtime.GOARCH)
		mgr, err := routerruntime.New(context.Background(), routerruntime.Options{
			Catalog:               catalog,
			RuntimeID:             runtimeID,
			Store:                 routerruntime.ConfigIntentStore{Store: baseStore},
			Installer:             integrationInstaller{},
			Factory:               integrationFactory{endpoint: localSrv.URL},
			ExperimentalRedaction: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		defer mgr.Close(context.Background())
		if err := mgr.Select(context.Background(), "spark-x2.6-4B"); err != nil {
			t.Fatal(err)
		}
		if err := mgr.Enable(context.Background()); err != nil {
			t.Fatal(err)
		}

		opts := ModelOptions{
			store:                 baseStore,
			LocalRuntime:          &LocalModels{Manager: mgr, Layout: layout, Catalog: catalog},
			ExperimentalRedaction: true,
		}
		defer closeServiceGroupRedactors(baseStore, opts.LocalRuntime)

		redactor := getGroupOrchestratorRedactor(opts, "local-redactor")
		p1, _, _ := redactor.current(context.Background())
		if p1 == nil {
			t.Fatal("expected initial pipeline p1")
		}

		// 1. Idle model switch: p1 has no in-flight requests, so switching to spark-x2.6-4B-Q8_0 closes p1 immediately.
		if err := mgr.Disable(context.Background()); err != nil {
			t.Fatal(err)
		}
		if err := mgr.Select(context.Background(), "spark-x2.6-4B-Q8_0"); err != nil {
			t.Fatal(err)
		}
		if err := mgr.Enable(context.Background()); err != nil {
			t.Fatal(err)
		}
		p2, _, _ := redactor.current(context.Background())
		if p2 == nil || p2 == p1 {
			t.Fatalf("expected new pipeline p2 after model switch, got p1=%p p2=%p", p1, p2)
		}
		waitPipelineDone(t, p1, "idle pipeline p1 after model switch")
		redactor.mu.Lock()
		retiringLenIdle := len(redactor.retiring)
		redactor.mu.Unlock()
		if retiringLenIdle != 0 {
			t.Fatalf("expected 0 retiring pipelines after idle switch, got %d", retiringLenIdle)
		}

		// 2. In-flight model switch: acquire PreparedCloudRequest on p2, switch model back to 1.7B while prep is open,
		// verify prep.Restore still works, and verify p2 closes as soon as prep.Close() runs.
		prep, err := redactor.Prepare(context.Background(), RedactionRequest{
			RequestID:      "req-inflight-switch",
			AppID:          "codex",
			SourceProtocol: "chat",
			TargetProtocol: "chat",
			Operation:      "generation",
			Body:           []byte(`{"model":"m","messages":[{"role":"user","content":"请联系宁祁 13800138000"}]}`),
		})
		if err != nil {
			t.Fatalf("Prepare on p2 failed: %v", err)
		}
		if err := mgr.Disable(context.Background()); err != nil {
			t.Fatal(err)
		}
		if err := mgr.Select(context.Background(), "spark-x2.6-4B"); err != nil {
			t.Fatal(err)
		}
		if err := mgr.Enable(context.Background()); err != nil {
			t.Fatal(err)
		}
		p3, _, _ := redactor.current(context.Background())
		if p3 == nil || p3 == p2 {
			t.Fatalf("expected new pipeline p3 after second model switch, got p2=%p p3=%p", p2, p3)
		}
		redactor.mu.Lock()
		retiringDuringInFlight := len(redactor.retiring)
		redactor.mu.Unlock()
		if retiringDuringInFlight != 1 {
			t.Fatalf("expected 1 retiring pipeline while prep is in-flight, got %d", retiringDuringInFlight)
		}
		restored, err := prep.Restore(context.Background(), "chat", "chat.completions", []byte(`{"id":"1","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"收到<NAME:1> <PHONE:1>"},"finish_reason":"stop"}]}`))
		if err != nil || !strings.Contains(string(restored), "收到宁祁 13800138000") {
			t.Fatalf("expected in-flight snapshot restore to succeed across model switch, got err=%v body=%s", err, string(restored))
		}
		_ = prep.Close()
		waitPipelineDone(t, p2, "retiring pipeline p2 after in-flight prep.Close")
		redactor.mu.Lock()
		retiringAfterClose := len(redactor.retiring)
		redactor.mu.Unlock()
		if retiringAfterClose != 0 {
			t.Fatalf("expected 0 retiring pipelines after in-flight prep.Close, got %d", retiringAfterClose)
		}
		if stats := redactor.Stats(); stats.Processed != 1 {
			t.Fatalf("expected Stats() to retain Processed=1 from retired pipeline p2, got %+v", stats)
		}

		// 3. Shutdown via closeServiceGroupRedactors closes active pipeline p3, clears references, and prevents recreation.
		closeServiceGroupRedactors(baseStore, opts.LocalRuntime)
		waitPipelineDone(t, p3, "active pipeline p3 after closeServiceGroupRedactors")
		redactor.mu.Lock()
		closedFlag := redactor.closed
		mgrRef := redactor.manager
		invRef := redactor.invoker
		activeRef := redactor.active
		redactor.mu.Unlock()
		if !closedFlag || mgrRef != nil || invRef != nil || activeRef != nil {
			t.Fatalf("expected closed redactor to clear active/manager/invoker references, got closed=%v active=%v mgr=%v invoker=%v",
				closedFlag, activeRef, mgrRef, invRef)
		}
		if _, err := redactor.Prepare(context.Background(), RedactionRequest{
			RequestID:      "req-after-close",
			AppID:          "codex",
			SourceProtocol: "chat",
			TargetProtocol: "chat",
			Operation:      "chat.completions",
			Body:           []byte(`{"model":"m","messages":[{"role":"user","content":"hello"}]}`),
		}); !errors.Is(err, ErrRedactionUnavailable) {
			t.Fatalf("expected ErrRedactionUnavailable on closed redactor, got %v", err)
		}
	})
}
