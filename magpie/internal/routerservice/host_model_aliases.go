package routerservice

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/yetone/magpie/internal/routerconfig"
	"github.com/yetone/magpie/internal/routerhost"
)

// WorkBuddy displays name:ID unless both values agree. Publish readable IDs and
// keep their exact routing targets in the Profile committed with the host files.
func workBuddyModelAliases(o routerhost.Options, previous map[string]string) (routerhost.Options, map[string]string, error) {
	targets := o.EffectiveModels()
	aliases := map[string]string{}
	for alias, target := range previous {
		for _, selected := range targets {
			if selected == target {
				aliases[alias] = target
			}
		}
	}
	// Allocate in target order independent of the primary selection.
	ordered := append([]string(nil), targets...)
	sort.Strings(ordered)
	byTarget := map[string]string{}
	for _, target := range ordered {
		base := strings.TrimSpace(o.ModelNames[target])
		if base == "" {
			return o, nil, errors.New("model display name is unavailable")
		}
		// These installed WorkBuddy IDs have built-in display overrides. Avoid
		// those special IDs so the host renders the configured name verbatim.
		switch base {
		case "deepseek-chat", "deepseek-reasoner", "default-model", "primary-model", "fast-model", "balanced-model", "deep-model":
			base += " · 端云协同"
		}
		alias := base
		for n := 2; aliases[alias] != "" && aliases[alias] != target; n++ {
			alias = fmt.Sprintf("%s (%d)", base, n)
		}
		if len(alias) > 256 || strings.ContainsAny(alias, "\r\n\x00") || strings.HasPrefix(alias, "custom-local:") {
			return o, nil, errors.New("model display name cannot be used as a host selector")
		}
		aliases[alias], byTarget[target] = target, alias
	}
	o.Models = make([]string, 0, len(targets))
	o.ModelNames = map[string]string{}
	for _, target := range targets {
		alias := byTarget[target]
		o.Models = append(o.Models, alias)
		o.ModelNames[alias] = alias
	}
	o.Model = o.Models[0]
	return o, aliases, nil
}

func hostRoutingModels(app *routerconfig.AppProfile, models []string) []string {
	out := append([]string(nil), models...)
	for i, model := range out {
		out[i] = hostRoutingModel(app, model)
	}
	return out
}

func hostRoutingModel(app *routerconfig.AppProfile, model string) string {
	if app != nil && app.ID == "workbuddy" {
		if target := app.ModelAliases[model]; target != "" {
			return target
		}
	}
	return model
}
