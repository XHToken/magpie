package routerconfig

import (
	"errors"
	"strings"
)

var ErrGroupExpansion = errors.New("routing group expansion is incomplete")

// GroupPrivacySource retains the requirement even when its local model is missing.
type GroupPrivacySource struct {
	GroupID string `json:"groupId"`
	ModelID string `json:"modelId"`
}

// ActiveGroupExpansion merges privacy before member deduplication or ordering.
type ActiveGroupExpansion struct {
	Members        []string
	PrivacySources []GroupPrivacySource
}

func ResolveActiveGroupExpansion(g Group, all []Group) (ActiveGroupExpansion, error) {
	return resolveGroupExpansion(g, all, true, true)
}

func resolveGroupExpansion(g Group, all []Group, activeOnly, strict bool) (ActiveGroupExpansion, error) {
	byID := make(map[string]Group, len(all)+1)
	for _, item := range all {
		if item.ID != "" {
			byID[item.ID] = item
		}
	}
	if g.ID != "" {
		byID[g.ID] = g
	}
	result := ActiveGroupExpansion{}
	path := map[string]bool{}
	seenMembers := map[string]bool{}
	seenSources := map[GroupPrivacySource]bool{}
	work := 0
	memberBase := func(ref string) string {
		bare, _, _ := SplitMemberEffort(strings.TrimSpace(ref))
		return bare
	}
	var walk func(Group, int, []string) (string, error)
	walk = func(cur Group, depth int, excluded []string) (string, error) {
		if activeOnly && (cur.Disabled || cur.Hidden) {
			return "", nil
		}
		work += 1 + len(excluded) + len(cur.Off)
		if work > 16384 {
			return "", ErrGroupExpansion
		}
		if depth > MaxGroupNest || path[cur.ID] {
			if strict {
				return "", ErrGroupExpansion
			}
			return "", nil
		}
		path[cur.ID] = true
		defer delete(path, cur.ID)
		if activeOnly {
			excluded = append(append([]string(nil), excluded...), cur.Off...)
		}
		excludedMembers := make(map[string]bool, len(excluded))
		for _, off := range excluded {
			excludedMembers[memberBase(off)] = true
		}
		firstLocal := ""
		for _, m := range append(append([]string(nil), cur.Members...), cur.Matched...) {
			work++
			if work > 16384 {
				return "", ErrGroupExpansion
			}
			if activeOnly && excludedMembers[memberBase(m)] {
				continue
			}
			local := ""
			if subID, ok := GroupIDOf(m); ok {
				sub, exists := byID[subID]
				if !exists {
					if strict {
						return "", ErrGroupExpansion
					}
					continue
				}
				if sub.Disabled || sub.Hidden {
					continue
				}
				var err error
				local, err = walk(sub, depth+1, excluded)
				if err != nil {
					return "", err
				}
			} else {
				local, _ = strings.CutPrefix(memberBase(m), "local/")
				if !strings.HasPrefix(memberBase(m), "local/") {
					local = ""
				}
				if !seenMembers[m] {
					seenMembers[m] = true
					result.Members = append(result.Members, m)
				}
			}
			if firstLocal == "" {
				firstLocal = local
			}
		}
		if cur.LocalRedaction {
			source := GroupPrivacySource{GroupID: cur.ID, ModelID: firstLocal}
			if !seenSources[source] {
				seenSources[source] = true
				result.PrivacySources = append(result.PrivacySources, source)
			}
		}
		return firstLocal, nil
	}
	_, err := walk(g, 0, nil)
	return result, err
}
