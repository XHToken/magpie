package routerservice

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/yetone/magpie/internal/routerconfig"
)

func TestDeepSeekAnthropicDiscoversAndCachesOfficialModels(t *testing.T) {
	const token = "management-token"
	want := []string{"deepseek-flash", "deepseek-v4-pro"}
	for _, base := range []string{"http://api.deepseek.com/anthropic", "http://api.deepseek.com/anthropic/v1/"} {
		t.Run(base, func(t *testing.T) {
			// The configured proxy serves the official host without external traffic.
			var requests atomic.Int32
			proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if r.URL.Host != "api.deepseek.com" || r.URL.Path != "/v1/models" {
					http.NotFound(w, r)
					return
				}
				if r.Header.Get("Authorization") != "Bearer test-cloud-api-key" || r.Header.Get("x-api-key") != "" || r.Header.Get("anthropic-version") != "" {
					t.Error("model catalogue must use Bearer authentication")
				}
				_, _ = io.WriteString(w, `{"data":[{"id":"deepseek-flash"},{"id":"deepseek-v4-pro"}]}`)
			}))
			defer proxy.Close()
			layout, _ := fixture(t)
			store := routerconfig.New(layout)
			h := handler(Info{Address: "127.0.0.1:13145"}, token, store)
			in := providerInputFor(base)
			in.Protocol, in.Preset, in.ProxyMode = "anthropic", "deepseek", "custom"
			in.ProxyURL = &proxy.URL
			saved := providerSaved(t, providerRequest(t, h, http.MethodPost, "/api/cloud-providers", token, in))
			response := providerRequest(t, h, http.MethodPost, "/api/cloud-providers/models", token, map[string]any{"id": saved.ID, "persist": true, "fallback": true})
			var result providerModelListResult
			if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &result) != nil {
				t.Fatalf("model discovery: %d %s", response.Code, response.Body.String())
			}
			if result.Fallback || result.Source != "discovered" || !reflect.DeepEqual(result.Models, want) {
				t.Fatalf("expected discovered official models, got %+v", result)
			}
			providers, err := store.CloudProviders()
			if err != nil || len(providers) != 1 {
				t.Fatalf("providers: %v %v", providers, err)
			}
			p := providers[0]
			if p.Protocol != "anthropic" || p.BaseURL != saved.BaseURL || !reflect.DeepEqual(p.DiscoveredModels, want) || !reflect.DeepEqual(effectiveProviderModels(p), want) {
				t.Fatalf("catalogue discovery changed generation settings or failed to cache: %+v", p)
			}
			if requests.Load() != 1 {
				t.Fatalf("expected one catalogue request, got %d", requests.Load())
			}
		})
	}
}

func TestDeepSeekPresetUsesCurrentModelsAcrossProtocols(t *testing.T) {
	for _, protocol := range []string{"chat", "responses", "anthropic"} {
		p := routerconfig.CloudProvider{Preset: "deepseek", Protocol: protocol, BaseURL: "https://api.deepseek.com/anthropic"}
		if got := presetModelsForProvider(p); !reflect.DeepEqual(got, []string{"deepseek-flash", "deepseek-v4-pro"}) {
			t.Errorf("%s fallback contains obsolete models: %v", protocol, got)
		}
	}
}

func TestDeepSeekPresetDoesNotRedirectCustomModelCatalogues(t *testing.T) {
	for _, base := range []string{
		"http://relay.invalid/anthropic",
		"http://api.deepseek.com.invalid/anthropic",
		"http://api.deepseek.com:8080/anthropic",
		"http://api.deepseek.com/custom/anthropic",
		"http://api.deepseek.com/%61nthropic",
	} {
		t.Run(base, func(t *testing.T) {
			proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.String() != base+"/v1/models" {
					t.Errorf("custom catalogue address changed: %s", r.URL)
				}
				if r.Header.Get("x-api-key") != "custom-key" || r.Header.Get("Authorization") != "" || r.Header.Get("anthropic-version") == "" {
					t.Error("custom Anthropic authentication changed")
				}
				_, _ = io.WriteString(w, `{"data":[{"id":"custom-model"}]}`)
			}))
			defer proxy.Close()
			p := routerconfig.ResolvedProvider{
				Config: routerconfig.CloudProvider{BaseURL: base, Protocol: "anthropic", Preset: "deepseek", ProxyMode: "custom"},
				APIKey: "custom-key", ProxyURL: proxy.URL,
			}
			models, err := providerModels(context.Background(), p)
			if err != nil || strings.Join(models, ",") != "custom-model" {
				t.Fatalf("custom catalogue: %v %v", models, err)
			}
		})
	}
}
