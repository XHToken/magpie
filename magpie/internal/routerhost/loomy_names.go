package routerhost

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/yetone/magpie/internal/edit"
)

// Update generated names in the same transaction as the host and Profile. The
// first baseline distinguishes an existing user label from a generated one;
// later user aliases differ from the journal's last generated name.
func synchronizeLoomyModelNames(o Options, group *portGroup) error {
	if o.Host != "loomy" || len(o.ModelNames) == 0 {
		return nil
	}
	var j journal
	if json.Unmarshal(group.Journal.After, &j) != nil || len(j.Files) == 0 || len(group.Files) == 0 || j.Files[0].Name != "opencode.json" || group.Files[0].Name != "opencode.json" {
		return errors.New("invalid Loomy model label plan")
	}
	var previous journal
	if group.Journal.Existed {
		if json.Unmarshal(group.Journal.Before, &previous) != nil || len(previous.Files) == 0 || previous.Files[0].Name != "opencode.json" {
			return errors.New("invalid previous Loomy model labels")
		}
	} else {
		previous = j
	}
	before, err := readJSON(previous.Files[0].Before)
	if err != nil {
		return err
	}
	managed, err := readJSON(previous.Files[0].After)
	if err != nil {
		return err
	}
	current, err := readJSON(group.Files[0].Before)
	if err != nil {
		return err
	}
	planned, err := readJSON(group.Files[0].After)
	if err != nil {
		return err
	}
	loomyProvider := providerID(current, "provider")
	var patches, managedPatches []edit.KV
	for _, id := range o.EffectiveModels() {
		name := strings.TrimSpace(o.ModelNames[id])
		if name == "" {
			continue
		}
		path := []string{"provider", loomyProvider, "models", id, "name"}
		got, exists := lookup(current, path)
		old, oldExists := lookup(managed, path)
		original, originalExists := lookup(before, path)
		generated := equalValue(got, exists, old, oldExists) && !equalValue(old, oldExists, original, originalExists)
		alias, hasAlias := got.(string)
		hasAlias = hasAlias && strings.TrimSpace(alias) != "" && alias != id && !generated
		if hasAlias {
			name = alias
		} else {
			managedPatches = append(managedPatches, edit.KV{Path: loomyJSONPath(path), Value: name})
		}
		if plannedName, _ := lookup(planned, path); plannedName != name {
			patches = append(patches, edit.KV{Path: loomyJSONPath(path), Value: name})
		}
	}
	if len(patches) == 0 {
		return nil
	}
	group.Files[0].After, err = edit.PatchJSON(group.Files[0].After, patches, nil)
	if err != nil {
		return err
	}
	// Keep the generated label in the journal when carrying a later user alias
	// through a model/primary change. Future renames must still recognize it.
	j.Files[0].After, err = edit.PatchJSON(j.Files[0].After, managedPatches, nil)
	if err != nil {
		return err
	}
	j.Checksum = j.checksum()
	group.Journal.After, err = json.MarshalIndent(j, "", "  ")
	return err
}
