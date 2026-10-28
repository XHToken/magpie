package routerservice

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/yetone/magpie/internal/routerconfig"
)

func TestOfficialModelCataloguesUseTheirOwnPathsAndAuth(t *testing.T) {
	for _, tc := range []struct{ base, path, auth string }{
		{"http://api.moonshot.cn/anthropic", "/v1/models", "bearer"},
		{"http://api.moonshot.ai/anthropic/v1", "/v1/models", "bearer"},
		{"http://api.xiaomimimo.com/anthropic", "/v1/models", "bearer"},
		{"http://qianfan.baidubce.com/anthropic", "/v2/models", "bearer"},
		{"http://api.modelarts-maas.com/anthropic", "/v2/models", "bearer"},
		{"http://open.bigmodel.cn/api/anthropic", "/api/paas/v4/models", "bearer"},
		{"http://api.z.ai/api/anthropic", "/api/paas/v4/models", "bearer"},
		{"http://api.x.ai", "/v1/models", "bearer"},
		{"http://api.stepfun.com", "/v1/models", "bearer"},
		{"http://tokenhub.tencentmaas.com", "/v1/models", "bearer"},
		{"http://openrouter.ai/api", "/api/v1/models", "bearer"},
		{"http://api.minimax.io/anthropic", "/anthropic/v1/models", "anthropic"},
		{"http://api.kimi.com/coding", "/coding/v1/models", "anthropic"},
		{"http://relay.invalid/anthropic", "/anthropic/v1/models", "anthropic"},
		{"http://api.moonshot.cn/anthropic/tokenplan", "/anthropic/tokenplan/v1/models", "anthropic"},
		{"http://api.modelarts-maas.com/plan/anthropic", "/plan/anthropic/v1/models", "anthropic"},
		{"http://qianfan.baidubce.com/anthropic/tokenplan/personal", "/anthropic/tokenplan/personal/v1/models", "anthropic"},
		{"http://coding.dashscope.aliyuncs.com/apps/anthropic", "/apps/anthropic/v1/models", "anthropic"},
		{"http://token-plan.cn-beijing.maas.aliyuncs.com/apps/anthropic", "/apps/anthropic/v1/models", "anthropic"},
	} {
		t.Run(tc.base, func(t *testing.T) {
			proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != tc.path {
					t.Errorf("catalogue path = %s, want %s", r.URL.Path, tc.path)
				}
				if tc.auth == "bearer" {
					if r.Header.Get("Authorization") != "Bearer catalogue-key" || r.Header.Get("x-api-key") != "" || r.Header.Get("anthropic-version") != "" {
						t.Error("catalogue must use Bearer auth without Anthropic headers")
					}
				} else if r.Header.Get("x-api-key") != "catalogue-key" || r.Header.Get("Authorization") != "" {
					t.Error("Anthropic catalogue auth changed")
				}
				_, _ = io.WriteString(w, `{"data":[{"id":"catalogue-model"}]}`)
			}))
			defer proxy.Close()
			p := routerconfig.ResolvedProvider{Config: routerconfig.CloudProvider{BaseURL: tc.base, Protocol: "anthropic", ProxyMode: "custom"}, APIKey: "catalogue-key", ProxyURL: proxy.URL}
			got, err := providerModels(context.Background(), p)
			if err != nil || !reflect.DeepEqual(got, []string{"catalogue-model"}) {
				t.Fatalf("catalogue: %v %v", got, err)
			}
		})
	}
}

func TestBailianNativeCatalogueReadsAllPages(t *testing.T) {
	var calls atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Host != "dashscope.aliyuncs.com" || r.URL.Path != "/api/v1/models" || r.Header.Get("Authorization") != "Bearer catalogue-key" || r.URL.Query().Get("capabilities") != "TG" {
			t.Errorf("wrong Bailian catalogue request: %s", r.URL)
		}
		if r.URL.Query().Get("page_no") == "1" {
			_, _ = io.WriteString(w, `{"success":true,"output":{"total":2,"page_no":1,"page_size":1,"models":[{"model":"model-a"}]}}`)
		} else {
			_, _ = io.WriteString(w, `{"success":true,"output":{"total":2,"page_no":2,"page_size":1,"models":[{"model":"model-b"}]}}`)
		}
	}))
	defer proxy.Close()
	p := routerconfig.ResolvedProvider{Config: routerconfig.CloudProvider{BaseURL: "http://dashscope.aliyuncs.com/apps/anthropic", Protocol: "anthropic", ProxyMode: "custom"}, APIKey: "catalogue-key", ProxyURL: proxy.URL}
	got, err := providerModels(context.Background(), p)
	if err != nil || !reflect.DeepEqual(got, []string{"model-a", "model-b"}) || calls.Load() != 2 {
		t.Fatalf("native catalogue pagination: %v %v (%d requests)", got, err, calls.Load())
	}
}

func TestModelCataloguePaginationRejectsForeignTargetsAndRepeatedPages(t *testing.T) {
	for _, foreign := range []bool{true, false} {
		t.Run(map[bool]string{true: "foreign-next", false: "repeated-page"}[foreign], func(t *testing.T) {
			var calls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if foreign {
					_, _ = io.WriteString(w, `{"data":[{"id":"a"}],"links":{"next":"https://foreign.invalid/v1/models"}}`)
				} else {
					_, _ = io.WriteString(w, `{"data":[{"id":"a"}],"has_more":true,"last_id":"a"}`)
				}
			}))
			defer upstream.Close()
			protocol := "chat"
			if !foreign {
				protocol = "anthropic"
			}
			p := routerconfig.ResolvedProvider{Config: routerconfig.CloudProvider{BaseURL: upstream.URL + "/v1", Protocol: protocol, ProxyMode: "direct"}, APIKey: "private-key"}
			models, err := providerModels(context.Background(), p)
			if err == nil || len(models) != 0 {
				t.Fatalf("unsafe/partial catalogue accepted: %v %v", models, err)
			}
			want := int32(1)
			if !foreign {
				want = 2
			}
			if calls.Load() != want {
				t.Fatalf("pagination requests=%d, want %d", calls.Load(), want)
			}
		})
	}
}

func TestPresetCataloguesStayOnConfiguredOrigins(t *testing.T) {
	for _, preset := range loadProviderPresets() {
		for protocol, base := range preset.Endpoints {
			p := routerconfig.CloudProvider{Preset: preset.ID, Protocol: protocol, BaseURL: base}
			before := p
			actual, err := url.Parse(providerCatalogue(p).endpoint(1))
			if err != nil {
				t.Fatal(err)
			}
			configured, err := url.Parse(base)
			if err != nil {
				t.Fatal(err)
			}
			if actual.Scheme != configured.Scheme || actual.Host != configured.Host || !reflect.DeepEqual(p, before) {
				t.Fatalf("%s/%s catalogue changed origin or generation settings: %s", preset.ID, protocol, actual)
			}
		}
	}
}

func TestProviderDiscoveryTimeoutKeepsCacheAndRecordsFailure(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer upstream.Close()
	layout, _ := fixture(t)
	store := routerconfig.New(layout)
	h := handler(Info{Address: "127.0.0.1:13145"}, "management-token", store)
	in := providerInputFor(upstream.URL + "/v1")
	p := providerSaved(t, providerRequest(t, h, http.MethodPost, "/api/cloud-providers", "management-token", in))
	if _, err := store.SaveProviderDiscoveredModels(p.ID, []string{"cached-model"}); err != nil {
		t.Fatal(err)
	}
	response := providerRequest(t, h, http.MethodPost, "/api/cloud-providers/models", "management-token", map[string]any{"id": p.ID, "persist": true, "fallback": true})
	var result providerModelListResult
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &result) != nil || !result.Fallback || result.DiscoveryError != "model_list_timeout" || !reflect.DeepEqual(result.Models, []string{"cached-model"}) {
		t.Fatalf("timeout provenance: %d %s", response.Code, response.Body.String())
	}
	list, err := store.CloudProviders()
	if err != nil {
		t.Fatal(err)
	}
	if list[0].ModelListError != "model_list_timeout" {
		t.Fatalf("timeout status not recorded: %+v", list[0])
	}
}
