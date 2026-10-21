package routerhost

import (
	"bytes"
	"reflect"
	"strings"

	"github.com/yetone/magpie/internal/product"
)

// providerID keeps an existing connection's key until the normal backed-up
// reconnect replaces it. New connections use Wefto's key.
func providerID(doc map[string]any, container string) string {
	selection, _ := doc["model"].(string)
	id, _, _ := strings.Cut(selection, "/")
	if container == "model_providers" {
		id, _ = doc["model_provider"].(string)
	}
	if product.IsProvider(id) {
		return id
	}
	providers, _ := doc[container].(map[string]any)
	if _, ok := providers[product.ID]; ok {
		return product.ID
	}
	if _, ok := providers[product.LegacyProviderID]; ok {
		return product.LegacyProviderID
	}
	return product.ID
}

// sameConnection permits only known branding aliases in a preflight compare.
// All endpoints, models, options and user-owned fields still have to match.
func sameConnection(host string, before, after []byte) bool {
	if bytes.Equal(before, after) {
		return true
	}
	read := readJSON
	if host == "workbuddy" {
		read = workBuddyModels
	}
	a, errA := read(before)
	b, errB := read(after)
	if errA != nil || errB != nil {
		return false
	}
	normalize := func(doc map[string]any) {
		entries := []map[string]any{}
		if host == "workbuddy" {
			entries = workBuddyModelEntries(doc)
		} else {
			for _, id := range []string{product.ID, product.LegacyProviderID} {
				if value, ok := lookup(doc, []string{"provider", id}); ok {
					if p, ok := value.(map[string]any); ok {
						entries = append(entries, p)
					}
				}
			}
		}
		for _, entry := range entries {
			options := entry
			if host == "loomy" {
				options, _ = entry["options"].(map[string]any)
			}
			if !product.IsPlaceholder(options["apiKey"]) {
				continue
			}
			options["apiKey"] = product.PlaceholderKey
			if product.IsProviderName(entry["name"]) {
				entry["name"] = product.Name
			}
		}
	}
	normalize(a)
	normalize(b)
	return reflect.DeepEqual(a, b)
}

func expandedContract(legacy AdapterContract) AdapterContract {
	c := AdapterContract{ID: legacy.ID, Version: 2}
	for _, f := range legacy.Files {
		fields := append([]string(nil), f.Fields...)
		for _, field := range f.Fields {
			if renamed := strings.ReplaceAll(field, product.LegacyProviderID, product.ID); renamed != field {
				fields = append(fields, renamed)
			}
		}
		c.Files = append(c.Files, FileSpec{Name: f.Name, Fields: fields})
	}
	return c
}
