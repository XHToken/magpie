package routerhost

import (
	"bytes"
	_ "embed"
	"errors"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/yetone/magpie/internal/edit"
)

const loomyTitleFile = "api-router-title.mjs"

//go:embed loomy_title.mjs
var loomyTitleSource []byte

func loomyV1Contract() AdapterContract {
	return AdapterContract{ID: "loomy", Version: 1, Files: []FileSpec{{
		Name: "opencode.json",
		Fields: []string{"model", "small_model", "provider.api_router.npm", "provider.api_router.name",
			"provider.api_router.options.baseURL", "provider.api_router.options.apiKey",
			"provider.api_router.options.useSessionAuth", "provider.api_router.models"},
	}}}
}

func loomyTitleURL(root string) string {
	p := filepath.ToSlash(filepath.Join(root, loomyTitleFile))
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return (&url.URL{Scheme: "file", Path: p}).String()
}

func loomyTitlePlugins(doc map[string]any, own string) ([]any, error) {
	plugins := []any{}
	if value, exists := doc["plugin"]; exists {
		var ok bool
		plugins, ok = value.([]any)
		if !ok {
			return nil, errors.New("Loomy plugin list must be an array")
		}
	}
	found := false
	for _, entry := range plugins {
		if entry == own {
			if found {
				return nil, errors.New("duplicate API-Router title extension")
			}
			found = true
		}
	}
	if !found {
		plugins = append(plugins, own)
	}
	return plugins, nil
}

func buildLoomyTitle(o Options, files []FilePlan) ([]FilePlan, error) {
	if len(files) != 2 || files[1].Name != loomyTitleFile {
		return nil, errors.New("invalid Loomy title extension input")
	}
	bridge := cloneFile(files[1])
	if bridge.Existed && !bytes.Equal(bridge.Before, loomyTitleSource) {
		return nil, errors.New("Loomy title extension file conflicts; preserve and reconcile it before applying")
	}
	doc, err := readJSON(files[0].After)
	if err != nil {
		return nil, err
	}
	plugins, err := loomyTitlePlugins(doc, loomyTitleURL(o.ConfigRoot))
	if err != nil {
		return nil, err
	}
	if current, exists := doc["plugin"]; !equalValue(current, exists, plugins, true) {
		if files[0].After, err = edit.PatchJSON(files[0].After, []edit.KV{{Path: "plugin", Value: plugins}}, nil); err != nil {
			return nil, err
		}
	}
	bridge.After = bytes.Clone(loomyTitleSource)
	return []FilePlan{files[0], bridge}, nil
}

// Restore only our list entry, retaining later user-installed extensions.
func restoreLoomyTitleList(raw []byte, before, after, now map[string]any) ([]byte, error) {
	b, bok := before["plugin"]
	a, aok := after["plugin"]
	c, cok := now["plugin"]
	if equalValue(b, bok, a, aok) || equalValue(b, bok, c, cok) {
		return raw, nil
	}
	if equalValue(a, aok, c, cok) {
		if bok {
			return edit.PatchJSON(raw, []edit.KV{{Path: "plugin", Value: b}}, nil)
		}
		return deleteJSONField(raw, "plugin")
	}
	old, _ := b.([]any)
	applied, ok1 := a.([]any)
	current, ok2 := c.([]any)
	if !ok1 || !ok2 {
		return nil, errors.New("Loomy title extension list conflict")
	}
	var own string
	for _, item := range applied {
		s, ok := item.(string)
		if ok && strings.HasSuffix(s, "/"+loomyTitleFile) {
			present := false
			for _, original := range old {
				present = present || equalValue(original, true, item, true)
			}
			if !present {
				own = s
			}
		}
	}
	if own == "" {
		return raw, nil
	}
	var keep []any
	for _, item := range current {
		if item != own {
			keep = append(keep, item)
		}
	}
	if len(keep) == 0 && !bok {
		return deleteJSONField(raw, "plugin")
	}
	if keep == nil {
		keep = []any{}
	}
	return edit.PatchJSON(raw, []edit.KV{{Path: "plugin", Value: keep}}, nil)
}
