package routerconfig

import (
	"context"
	"encoding/json"
)

// SaveProviderModelDiscoveryContext records a result only for the connection
// that was queried. A late response cannot repopulate a newly edited target.
func (s *Store) SaveProviderModelDiscoveryContext(ctx context.Context, expected ResolvedProvider, models []string, failureCode string) error {
	switch failureCode {
	case "", "model_key_required", "model_list_auth_failed", "model_list_forbidden", "model_list_unavailable", "model_list_rate_limited", "model_list_upstream_failed", "model_list_timeout", "model_list_connection_failed", "model_list_invalid_response", "model_list_failed", "model_list_empty":
	default:
		return invalidProvider("invalid model discovery status")
	}
	normalized, err := normalizeDiscoveredModels(models)
	if err != nil {
		return err
	}
	ctx, unlock, err := s.lockContext(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	f, err := s.providerFilesContext(ctx)
	if err != nil {
		return err
	}
	current, fields, err := f.resolved(expected.Config.ID)
	if err != nil {
		return err
	}
	if current.Config.Revision != expected.Config.Revision || !sameProviderConnection(current, expected) {
		return ErrProviderConflict
	}
	if failureCode == "" {
		fields["discoveredModels"], _ = json.Marshal(normalized)
		delete(fields, "modelListError")
	} else {
		fields["modelListError"], _ = json.Marshal(failureCode)
	}
	f.providers[current.Config.ID], err = json.Marshal(fields)
	if err != nil {
		return err
	}
	return s.writeProviders(ctx, f, false)
}
