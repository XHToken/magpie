package routerservice

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/yetone/magpie/internal/routerconfig"
)

func TestGroupRulesRespectNestedActivation(t *testing.T) {
	for _, tc := range []struct {
		name         string
		disabled     bool
		hidden       bool
		childOff     bool
		rootOff      bool
		rootOffGroup bool
		want         string
	}{
		{name: "active control", want: "first"},
		{name: "child member off", childOff: true, want: "second"},
		{name: "parent excludes nested member", rootOff: true, want: "second"},
		{name: "parent excludes descendant group", rootOffGroup: true, want: "second"},
		{name: "disabled rule target", disabled: true, want: "fallback"},
		{name: "hidden rule target", hidden: true, want: "fallback"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			var called []string
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var payload struct {
					Model string `json:"model"`
				}
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Error(err)
				}
				mu.Lock()
				called = append(called, payload.Model)
				mu.Unlock()
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"id":"activation","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
			}))
			defer upstream.Close()
			h, store := newGroupRoutingEnv(t, ModelOptions{})
			provider := saveTestCloudProvider(t, store, "Activation", upstream.URL+"/v1", "first", "second", "fallback")
			first, second := provider.ID+"/first", provider.ID+"/second"
			child := routerconfig.Group{ID: "activation-child", Name: "Child", Routing: "ordered", Members: []string{first, second}, Disabled: tc.disabled, Hidden: tc.hidden}
			if tc.childOff {
				child.Off = []string{first}
			}
			parent := routerconfig.Group{ID: "activation-parent", Name: "Parent", Routing: "ordered", Affinity: "off", Members: []string{provider.ID + "/fallback", first, "group/activation-child"}, Rules: []routerconfig.Rule{{When: "prompt", Pattern: "hello", Use: "group/activation-child"}}}
			if tc.rootOff {
				parent.Off = []string{first}
			}
			groups := []routerconfig.Group{child, parent}
			if tc.rootOffGroup {
				child.Members = []string{"group/activation-leaf", second}
				parent.Members = append(parent.Members, "group/activation-leaf")
				parent.Off = []string{"group/activation-leaf"}
				groups = []routerconfig.Group{{ID: "activation-leaf", Name: "Leaf", Members: []string{first}}, child, parent}
			}
			if _, err := store.SaveGroups(groups); err != nil {
				t.Fatal(err)
			}
			saveGroupBoundApp(t, store, []string{"group/activation-parent"})
			response := postAppChat(t, h, "group/activation-parent")
			mu.Lock()
			defer mu.Unlock()
			if response.Code != http.StatusOK || strings.Join(called, ",") != tc.want {
				t.Fatalf("inactive nested target dispatched: status=%d called=%v want=%s body=%s", response.Code, called, tc.want, response.Body.String())
			}
		})
	}
}

func TestActiveGroupExpansionHonorsEveryAncestor(t *testing.T) {
	leaf := routerconfig.Group{ID: "leaf", Members: []string{"p/off", "p/on"}}
	middle := routerconfig.Group{ID: "middle", Members: []string{"p/off", "group/leaf"}, Off: []string{"p/off"}}
	root := routerconfig.Group{ID: "root", Members: []string{"group/middle"}}
	if got := expandActiveGroupMembers(root, []routerconfig.Group{root, middle, leaf}, nil); strings.Join(got, ",") != "p/on" {
		t.Fatalf("ancestor exclusion lost during nested expansion: %v", got)
	}
}
