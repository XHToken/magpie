package routerservice

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/tidwall/jsonc"
	"github.com/yetone/magpie/internal/edit"
	"github.com/yetone/magpie/internal/routerconfig"
	"github.com/yetone/magpie/internal/routerhost"
)

func validateAutoHostModels(ctx context.Context, store *routerconfig.Store, models []string, local *LocalModels) error {
	if len(models) == 0 {
		return routerconfig.ErrAppUnconfigured
	}
	cfg, err := store.LoadConfigContext(ctx)
	if err != nil {
		return err
	}
	groups := effectiveGroups(cfg, local)
	for _, model := range models {
		if id, ok := routerconfig.GroupIDOf(model); ok {
			g, found := findRuntimeGroup(cfg.Groups, groups, id)
			if !found || g.Hidden || g.Disabled || len(expandActiveGroupMembers(g, groups, nil)) == 0 {
				return errors.New("selected routing group is unavailable")
			}
			continue
		}
		if strings.HasPrefix(model, "local/") {
			if local != nil {
				id := strings.TrimPrefix(model, "local/")
				found := false
				for _, m := range local.Catalog.Models {
					found = found || strings.EqualFold(m.ID, id)
				}
				if !found {
					return errors.New("selected local model is unavailable")
				}
			}
			continue
		}
		providerID, modelID, ok := strings.Cut(model, "/")
		if !ok || modelID == "" {
			return errors.New("selected model requires a provider")
		}
		p := routerconfig.AppProfile{CredentialMode: routerconfig.StoredCredential}
		if _, err := store.ResolveAppProviderTargetContext(ctx, p, providerID, modelID, "", ""); err != nil {
			return err
		}
	}
	return nil
}

func sameHostModelSet(saved, live []string) bool {
	if len(saved) == 0 || len(saved) != len(live) {
		return false
	}
	set := map[string]bool{}
	for _, m := range saved {
		set[m] = true
	}
	for _, m := range live {
		if !set[m] {
			return false
		}
	}
	return true
}

func hostConnectionMatches(host, root, origin string, models []string) error {
	var adapter routerhost.Adapter = routerhost.WorkBuddyAdapter{}
	if host == "loomy" {
		adapter = routerhost.LoomyAdapter{}
	}
	var files []routerhost.FilePlan
	for _, f := range adapter.Contract().Files {
		raw, err := os.ReadFile(filepath.Join(root, f.Name))
		if err != nil {
			return err
		}
		files = append(files, routerhost.FilePlan{Name: f.Name, Before: raw, Existed: true})
	}
	// Loomy can switch the primary model without changing its independent
	// small model. Either selection is valid while it stays in the bound set.
	if host == "loomy" && len(files) > 0 && len(models) > 0 {
		var doc map[string]any
		if err := json.Unmarshal(jsonc.ToJSON(files[0].Before), &doc); err != nil {
			return err
		}
		small, _ := doc["small_model"].(string)
		if id, managed := strings.CutPrefix(small, "api_router/"); managed {
			bound := false
			for _, model := range models {
				bound = bound || model == id
			}
			if !bound {
				return errors.New("host small model is not bound to its Router Profile")
			}
			var err error
			files[0].Before, err = edit.PatchJSON(files[0].Before, []edit.KV{{Path: "small_model", Value: "api_router/" + models[0]}}, nil)
			if err != nil {
				return err
			}
		}
	}
	got, err := adapter.Build(routerhost.Options{Host: host, ConfigRoot: root, RouterURL: origin, Models: models}, files)
	if err != nil {
		return err
	}
	for _, f := range got {
		if !bytes.Equal(f.Before, f.After) {
			return errors.New("host configuration differs from its Router Profile")
		}
	}
	return nil
}
