package routerledger

import "strings"

// ObservedModel identifies a local model that actually started inference for a
// business request. It does not identify the final generation destination.
type ObservedModel struct {
	Purpose string `json:"purpose"`
	ModelID string `json:"modelId"`
}

// ObservedModels projects retained terminal calls for one bounded request page.
// Configured models, rejected calls and management checks are not executions.
func (s *Store) ObservedModels(requestIDs []string) (map[string][]ObservedModel, error) {
	if len(requestIDs) > 100 {
		return nil, ErrInvalid
	}
	args := make([]any, len(requestIDs))
	for i, id := range requestIDs {
		if !identifier(id, 128, false) {
			return nil, ErrInvalid
		}
		args[i] = id
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(); err != nil {
		return nil, err
	}
	out := map[string][]ObservedModel{}
	if len(args) == 0 {
		return out, nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(args)), ",")
	calls, err := s.readCallsLocked("SELECT data FROM calls WHERE scope='business' AND request_id IN ("+placeholders+") ORDER BY started_ns,call_id", args...)
	if err != nil {
		return nil, err
	}
	for _, call := range calls {
		if !call.InferenceStarted || call.ModelID == "" {
			continue
		}
		model := ObservedModel{Purpose: call.Purpose, ModelID: call.ModelID}
		duplicate := false
		for _, previous := range out[call.RequestID] {
			if previous == model {
				duplicate = true
				break
			}
		}
		if !duplicate {
			out[call.RequestID] = append(out[call.RequestID], model)
		}
	}
	return out, nil
}
