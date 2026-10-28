package routerservice

import (
	"encoding/json"
	"net/url"
	"strconv"
	"strings"

	"github.com/yetone/magpie/internal/routerconfig"
)

type modelCatalogue struct {
	base          string
	auth          string
	nativeBailian bool
}

// Catalogue rules are independent of generation. They match official origins
// and exact product paths, never the preset name or an arbitrary host suffix.
func providerCatalogue(p routerconfig.CloudProvider) modelCatalogue {
	base := strings.TrimRight(p.BaseURL, "/")
	c := modelCatalogue{base: base, auth: p.Protocol}
	u, err := url.Parse(base)
	if err != nil {
		return c
	}
	path := u.EscapedPath()
	if (u.Scheme == "http" || u.Scheme == "https") && u.User == nil && u.Host == u.Hostname() && u.RawQuery == "" && !u.ForceQuery && u.Fragment == "" {
		host := strings.ToLower(u.Hostname())
		anthropicPath := path == "/anthropic" || path == "/anthropic/v1"
		cataloguePath := ""
		switch host {
		case "api.deepseek.com", "api.moonshot.cn", "api.moonshot.ai", "api.xiaomimimo.com":
			if p.Protocol == "anthropic" && anthropicPath {
				cataloguePath = "/v1"
			}
		case "api.modelarts-maas.com", "api-ap-southeast-1.modelarts-maas.com":
			if anthropicPath || path == "/openai/v1" || path == "/v2" {
				cataloguePath = "/v2"
			}
		case "qianfan.baidubce.com":
			if p.Protocol == "anthropic" && anthropicPath {
				cataloguePath = "/v2"
			}
		case "open.bigmodel.cn", "api.z.ai":
			if path == "/api/anthropic" || path == "/api/anthropic/v1" {
				cataloguePath = "/api/paas/v4"
			}
		case "api.x.ai", "api.stepfun.com", "api.stepfun.ai", "tokenhub.tencentmaas.com", "tokenhub.tencentcloudmaas.com", "tokenhub-intl.tencentcloudmaas.com", "tokenhub-us.tencentcloudmaas.com", "ollama.com":
			if path == "" || path == "/v1" {
				cataloguePath = "/v1"
			}
		case "openrouter.ai":
			if path == "/api" || path == "/api/v1" {
				cataloguePath = "/api/v1"
			}
		}
		if bailianCatalogueHost(host) && (path == "/apps/anthropic" || path == "/apps/anthropic/v1" || path == "/compatible-mode/v1") {
			cataloguePath, c.nativeBailian = "/api/v1", true
		}
		if cataloguePath != "" {
			u.Path, u.RawPath = cataloguePath, ""
			c.base, c.auth = u.String(), "chat"
		}
	}
	if c.auth == "anthropic" && !strings.HasSuffix(c.base, "/v1") {
		c.base += "/v1"
	}
	return c
}

func bailianCatalogueHost(host string) bool {
	switch host {
	case "dashscope.aliyuncs.com", "dashscope-intl.aliyuncs.com", "dashscope-us.aliyuncs.com", "cn-hongkong.dashscope.aliyuncs.com":
		return true
	}
	for _, region := range []string{"cn-beijing", "ap-southeast-1", "cn-hongkong", "eu-central-1", "ap-northeast-1", "us-east-1"} {
		suffix := "." + region + ".maas.aliyuncs.com"
		workspace := strings.TrimSuffix(host, suffix)
		if workspace != host && workspace != "" && !strings.Contains(workspace, ".") && workspace != "token-plan" && workspace != "coding" {
			return true
		}
	}
	return false
}

func (c modelCatalogue) endpoint(page int) string {
	u, err := url.Parse(c.base + "/models")
	if err != nil {
		return c.base + "/models"
	}
	if c.nativeBailian {
		q := u.Query()
		q.Set("page_no", strconv.Itoa(page))
		q.Set("page_size", "100")
		q.Set("capabilities", "TG")
		u.RawQuery = q.Encode()
	}
	return u.String()
}

type cataloguePage struct {
	ids   []string
	next  string
	more  bool
	total int
}

func (c modelCatalogue) decode(data []byte, page int) (cataloguePage, error) {
	invalid := func() (cataloguePage, error) {
		return cataloguePage{}, modelListFailure("model_list_invalid_response", "upstream returned an unsupported model list; enter a model ID manually")
	}
	var out cataloguePage
	if c.nativeBailian {
		var reply struct {
			Success *bool `json:"success"`
			Output  *struct {
				Total  *int `json:"total"`
				Models []struct {
					Model string `json:"model"`
				} `json:"models"`
			} `json:"output"`
		}
		if json.Unmarshal(data, &reply) != nil || reply.Output == nil || reply.Output.Models == nil || reply.Output.Total == nil || *reply.Output.Total < len(reply.Output.Models) || (reply.Success != nil && !*reply.Success) {
			return invalid()
		}
		for _, m := range reply.Output.Models {
			out.ids = append(out.ids, m.Model)
		}
		// The caller uses unique-count progress as well to reject repeated pages.
		out.more, out.total = *reply.Output.Total > 0, *reply.Output.Total
		out.next = c.endpoint(page + 1)
		return out, nil
	}
	var reply struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
		HasMore bool   `json:"has_more"`
		LastID  string `json:"last_id"`
		Links   struct {
			Next string `json:"next"`
		} `json:"links"`
	}
	if json.Unmarshal(data, &reply) != nil || reply.Data == nil {
		return invalid()
	}
	for _, m := range reply.Data {
		out.ids = append(out.ids, m.ID)
	}
	if reply.HasMore {
		if c.auth != "anthropic" || reply.LastID == "" {
			return invalid()
		}
		out.more = true
		q := url.Values{"after_id": {reply.LastID}, "limit": {"100"}}
		out.next = c.base + "/models?" + q.Encode()
	} else if reply.Links.Next != "" {
		base, _ := url.Parse(c.base + "/models")
		next, err := base.Parse(reply.Links.Next)
		if err != nil || next.User != nil || next.Scheme != base.Scheme || next.Host != base.Host || next.EscapedPath() != base.EscapedPath() || next.Fragment != "" {
			return invalid()
		}
		out.more, out.next = true, next.String()
	}
	return out, nil
}
