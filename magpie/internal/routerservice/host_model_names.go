package routerservice

import (
	"context"
	"strings"

	"github.com/yetone/magpie/internal/routerconfig"
	"github.com/yetone/magpie/internal/routermanifest"
)

// Host labels come from Router's saved catalog, never client-supplied text or
// credentials. Keep the model first so a long provider name cannot hide it.
func hostModelNames(ctx context.Context, store *routerconfig.Store, models []string, local *LocalModels) (map[string]string, error) {
	var cfg routerconfig.Config
	if store != nil {
		var err error
		cfg, err = store.LoadConfigContext(ctx)
		if err != nil {
			return nil, err
		}
	}
	groups := effectiveGroups(cfg, local)
	providers := map[string]string{}
	for _, provider := range cfg.Providers {
		providers[provider.ID] = strings.TrimSpace(provider.Name)
	}
	catalog := routermanifest.Fixed()
	if local != nil {
		catalog = local.Catalog
	}
	names := make(map[string]string, len(models))
	for _, selector := range models {
		source, model, qualified := strings.Cut(selector, "/")
		if !qualified {
			names[selector] = selector
			continue
		}
		name := model
		switch source {
		case "group":
			if group, ok := findRuntimeGroup(cfg.Groups, groups, model); ok && strings.TrimSpace(group.Name) != "" {
				name = strings.TrimSpace(group.Name)
			}
		case "local":
			for _, entry := range catalog.Models {
				if strings.EqualFold(entry.ID, model) && entry.Tier != "" {
					name = entry.DisplayName()
					break
				}
			}
		default:
			if provider := providers[source]; provider != "" {
				name += " · " + provider
			}
		}
		names[selector] = name
	}
	return names, nil
}
