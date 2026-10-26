package routerservice

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/tidwall/jsonc"
	"github.com/yetone/magpie/internal/routerconfig"
	"github.com/yetone/magpie/internal/routerhost"
)

// hostAutoAPI manages fast probe-free connection and restoration for WorkBuddy
// and Loomy, supporting multi-select routing groups/models and status inspection.
func hostAutoAPI(w http.ResponseWriter, r *http.Request, info Info, token string, store *routerconfig.Store, stateDir string, runtimes ...*LocalModels) {
	var local *LocalModels
	if len(runtimes) > 0 {
		local = runtimes[0]
	}
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		items := inspectAutoHostConnections(r.Context(), store, info, local)
		if r.Method == http.MethodHead {
			return
		}
		providerJSON(w, http.StatusOK, map[string]any{"items": items, "hosts": items})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 40*time.Second)
	defer cancel()
	deadline, _ := ctx.Deadline()
	_ = http.NewResponseController(w).SetReadDeadline(deadline)
	var in struct {
		Host       string   `json:"host"`
		Action     string   `json:"action"`
		Enabled    *bool    `json:"enabled"`
		Model      string   `json:"model"`
		Models     []string `json:"models"`
		ConfigRoot string   `json:"configRoot"`
		HomeDir    string   `json:"homeDir"`
		AppDataDir string   `json:"appDataDir"`
	}
	if !providerInput(w, r, &in) {
		w.Header().Set("Connection", "close")
		return
	}
	_ = http.NewResponseController(w).SetReadDeadline(time.Time{})
	fail := func(status int, code, message string) {
		providerJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
	}
	if stateDir == "" {
		fail(http.StatusServiceUnavailable, "host_recovery_unavailable", "Host recovery directory unavailable")
		return
	}
	if token != "" && (strings.Contains(in.Model, token) || strings.Contains(in.ConfigRoot, token) || containsSecret(in.Models, token)) {
		fail(http.StatusForbidden, "credential_misuse", "Management credentials cannot be used in host settings")
		return
	}
	if in.Host != "workbuddy" && in.Host != "loomy" {
		fail(http.StatusBadRequest, "unsupported_host", "Automatic connection supports WorkBuddy and Loomy only")
		return
	}
	disconnect := (in.Enabled != nil && !*in.Enabled) || in.Action == "disconnect" || in.Action == "restore"
	root, err := resolveAutoHostRoot(in.Host, in.ConfigRoot, in.HomeDir, in.AppDataDir)
	if err != nil && !disconnect {
		fail(http.StatusConflict, "host_config_unavailable", err.Error())
		return
	}
	if token != "" && strings.Contains(root, token) {
		fail(http.StatusForbidden, "credential_misuse", "Management credentials cannot be used in host settings")
		return
	}
	e := autoHostEngine(in.Host, stateDir, token)
	models := routerhost.Options{Model: in.Model, Models: in.Models}.EffectiveModels()
	if in.Models != nil && len(models) == 0 {
		models = []string{}
	}
	if len(models) == 0 && in.Models == nil && store != nil {
		if saved := findSavedApp(ctx, store, in.Host); saved != nil {
			models = routerhost.Options{Model: saved.Cloud.Model, Models: saved.Models}.EffectiveModels()
		}
	}
	if disconnect {
		persist := func() error { return syncAutoHostStore(ctx, store, in.Host, false, models) }
		restored := false
		if root != "" {
			p, err := e.PreviewRestore(in.Host, root)
			if err != nil {
				if !strings.Contains(err.Error(), "no recovery record") {
					fail(http.StatusConflict, "host_operation_failed", "Host configuration restore failed; recovery record is retained")
					return
				}
			} else if err := e.RestoreAndCommit(ctx, p, persist); err != nil {
				fail(http.StatusConflict, "host_operation_failed", "Host configuration restore failed; recovery record is retained")
				return
			} else {
				restored = true
			}
		}
		if !restored {
			if err := persist(); err != nil {
				fail(http.StatusConflict, "host_profile_sync_failed", "Updating the saved App Profile failed")
				return
			}
		}
		primary := ""
		if len(models) > 0 {
			primary = models[0]
		}
		if models == nil {
			models = []string{}
		}
		providerJSON(w, http.StatusOK, map[string]any{
			"connected": false, "enabled": false, "host": in.Host, "model": primary, "models": models, "configRoot": root,
		})
		return
	}
	o := routerhost.NormalizeOptions(routerhost.Options{Host: in.Host, ConfigRoot: root, RouterURL: "http://" + info.Address, Model: in.Model, Models: models})
	var edit routerconfig.AppEdit
	if store != nil {
		edit, err = autoHostAppEdit(ctx, store, in.Host, true, o.EffectiveModels())
		if err == nil {
			_, err = store.ResolveAppDraftContext(ctx, edit, token)
		}
		if err == nil {
			err = validateAutoHostModels(ctx, store, o.EffectiveModels(), local)
		}
		if err != nil {
			fail(http.StatusConflict, "host_profile_invalid", "Select available routing groups and configured models before connecting; host configuration is unchanged")
			return
		}
	}
	p, err := e.ConnectAndCommit(ctx, o, func() error {
		if store == nil {
			return nil
		}
		_, err := store.SaveAppContext(ctx, edit)
		return err
	})
	if err != nil {
		fail(http.StatusConflict, "host_operation_failed", "Host connection checks or configuration apply failed; the Profile remains saved and the recovery record is retained")
		return
	}
	effective := o.EffectiveModels()
	primary := o.PrimaryModel()
	providerJSON(w, http.StatusOK, map[string]any{
		"connected":  true,
		"enabled":    true,
		"host":       in.Host,
		"model":      primary,
		"models":     effective,
		"configRoot": root,
		"selection":  map[string]string{"workbuddy": "custom-local:" + primary, "loomy": "api_router/" + primary}[in.Host],
		"plan":       p.Summary(),
	})
}

func autoHostEngine(host, stateDir, token string) routerhost.Engine {
	var adapter routerhost.Adapter = routerhost.WorkBuddyAdapter{}
	if host == "loomy" {
		adapter = routerhost.LoomyAdapter{}
	}
	return routerhost.Engine{StateDir: stateDir, Adapter: adapter, Checker: routerhost.FastChecker(token)}
}

func findSavedApp(ctx context.Context, store *routerconfig.Store, host string) *routerconfig.AppProfile {
	if store == nil {
		return nil
	}
	apps, err := store.AppsContext(ctx)
	if err != nil {
		return nil
	}
	for i := range apps {
		if apps[i].ID == host {
			return &apps[i]
		}
	}
	return nil
}

func syncAutoHostStore(ctx context.Context, store *routerconfig.Store, host string, enabled bool, models []string) error {
	if store == nil {
		return nil
	}
	edit, err := autoHostAppEdit(ctx, store, host, enabled, models)
	if err != nil {
		return err
	}
	_, err = store.SaveAppContext(ctx, edit)
	return err
}

func autoHostAppEdit(ctx context.Context, store *routerconfig.Store, host string, enabled bool, models []string) (routerconfig.AppEdit, error) {
	apps, err := store.AppsContext(ctx)
	if err != nil {
		return routerconfig.AppEdit{}, err
	}
	var cur *routerconfig.AppProfile
	for i := range apps {
		if apps[i].ID == host {
			cur = &apps[i]
			break
		}
	}
	if cur == nil {
		return routerconfig.AppEdit{}, routerconfig.ErrAppNotFound
	}
	edit := routerconfig.AppEdit{
		ID: host, Revision: cur.Revision, Name: cur.Name, Enabled: &enabled,
		Cloud: cur.Cloud, CredentialMode: cur.CredentialMode,
		RedactionEnabled: cur.RedactionEnabled, ReferencePrice: cur.ReferencePrice,
	}
	if models != nil {
		cp := append([]string{}, models...)
		edit.Models = &cp
		if len(models) == 0 || strings.HasPrefix(models[0], "group/") || strings.HasPrefix(models[0], "local/") || cur.Cloud.ProviderID+"/"+cur.Cloud.Model != models[0] {
			edit.Cloud = routerconfig.AppCloud{}
		}
	} else if len(cur.Models) > 0 {
		cp := append([]string(nil), cur.Models...)
		edit.Models = &cp
	}
	return edit, nil
}

func inspectAutoHosts(ctx context.Context, store *routerconfig.Store) []map[string]any {
	return inspectAutoHostConnections(ctx, store, Info{}, nil)
}

func inspectAutoHostConnections(ctx context.Context, store *routerconfig.Store, info Info, local *LocalModels) []map[string]any {
	var out []map[string]any
	for _, host := range []string{"loomy", "workbuddy"} {
		root, _ := autoHostConfigRoot(host)
		connected, liveModels := inspectHostLiveFiles(host, root)
		saved := findSavedApp(ctx, store, host)
		hostConfigured := connected
		models, enabled := liveModels, false
		if saved != nil {
			if len(saved.Models) > 0 {
				models = append([]string(nil), saved.Models...)
			} else if len(models) == 0 && saved.Cloud.Model != "" {
				models = []string{saved.Cloud.Model}
			}
			enabled = saved.Configured && saved.Enabled
		}
		connected = connected && enabled && sameHostModelSet(models, liveModels)
		if connected && store != nil {
			connected = validateAutoHostModels(ctx, store, models, local) == nil
		}
		if connected && info.Address != "" {
			connected = hostConnectionMatches(host, root, "http://"+info.Address, liveModels) == nil
		}
		if models == nil {
			models = []string{}
		}
		primary := ""
		if len(models) > 0 {
			primary = models[0]
		}
		out = append(out, map[string]any{
			"host": host, "enabled": connected, "connected": connected, "models": models, "model": primary, "configRoot": root,
			"needsReconnect": (hostConfigured || enabled) && !connected,
		})
	}
	return out
}

func inspectHostLiveFiles(host, root string) (bool, []string) {
	if root == "" {
		return false, nil
	}
	if host == "loomy" {
		raw, err := os.ReadFile(filepath.Join(root, "opencode.json"))
		var doc map[string]any
		if err != nil || json.Unmarshal(jsonc.ToJSON(raw), &doc) != nil {
			return false, nil
		}
		sel, _ := doc["model"].(string)
		primary := strings.TrimPrefix(sel, "api_router/")
		if primary == "" || primary == sel {
			return false, nil
		}
		models := []string{primary}
		if prov, ok := doc["provider"].(map[string]any); ok {
			if ar, ok := prov["api_router"].(map[string]any); ok {
				if mMap, ok := ar["models"].(map[string]any); ok {
					var extra []string
					for k := range mMap {
						if k != primary {
							extra = append(extra, k)
						}
					}
					sort.Strings(extra)
					models = append(models, extra...)
				}
			}
		}
		return true, models
	}
	sRaw, err := os.ReadFile(filepath.Join(root, "settings.json"))
	var sDoc map[string]any
	if err != nil || json.Unmarshal(jsonc.ToJSON(sRaw), &sDoc) != nil {
		return false, nil
	}
	sel, _ := sDoc["model"].(string)
	primary := strings.TrimPrefix(sel, "custom-local:")
	if primary == "" || primary == sel {
		return false, nil
	}
	models := []string{primary}
	if mRaw, err := os.ReadFile(filepath.Join(root, "models.json")); err == nil {
		var entries []map[string]any
		if json.Unmarshal(mRaw, &entries) != nil {
			var mDoc struct {
				Models []map[string]any `json:"models"`
			}
			if json.Unmarshal(mRaw, &mDoc) != nil {
				return false, nil
			}
			entries = mDoc.Models
		}
		for _, item := range entries {
			id, _ := item["id"].(string)
			if id != "" && id != primary && item["name"] == "API Router" {
				models = append(models, id)
			}
		}
	} else {
		return false, nil
	}
	return true, models
}
