package routerhost

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/yetone/magpie/internal/product"
)

func workBuddyNameCatalogs(j *journal) (map[string]any, map[string]any, error) {
	if j == nil {
		return nil, nil, nil
	}
	if len(j.Files) == 0 || j.Files[0].Name != "models.json" {
		return nil, nil, errors.New("invalid WorkBuddy model label record")
	}
	before, err := catalogModels("workbuddy", j.Files[0].Before)
	if err != nil {
		return nil, nil, err
	}
	after, err := catalogModels("workbuddy", j.Files[0].After)
	return before, after, err
}

func workBuddyGeneratedName(id string, name any, before, after map[string]any) bool {
	if product.IsProviderName(name) || name == id {
		return true
	}
	b, _ := before[id].(map[string]any)
	a, _ := after[id].(map[string]any)
	old, oldExists := a["name"]
	original, originalExists := b["name"]
	return equalValue(name, name != nil, old, oldExists) && !equalValue(old, oldExists, original, originalExists)
}

// Generated names are tracked by the validated journal, independently of the
// current display text. User aliases must not disconnect the saved model set.
func ManagedWorkBuddyModelIDs(stateDir, root string) ([]string, error) {
	e := Engine{StateDir: stateDir, Adapter: WorkBuddyAdapter{}}
	j, _, err := e.readJournal("workbuddy", root)
	if err != nil || j == nil || j.Status == "restored" {
		return nil, err
	}
	if err := checkJournalContract(j, (WorkBuddyAdapter{}).Contract()); err != nil {
		return nil, err
	}
	before, after, err := workBuddyNameCatalogs(j)
	if err != nil {
		return nil, err
	}
	var ids []string
	for id, value := range after {
		entry, _ := value.(map[string]any)
		if product.IsPlaceholder(entry["apiKey"]) && entry["supportsToolCall"] == true && entry["useCustomProtocol"] == false && workBuddyGeneratedName(id, entry["name"], before, after) {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

// Preserve first backups and aliases while refreshing only generated labels.
// The host file, journal and Profile are committed by the caller together.
func synchronizeWorkBuddyModelNames(o Options, group *portGroup) error {
	if o.Host != "workbuddy" || len(o.ModelNames) == 0 {
		return nil
	}
	var j, previous journal
	if json.Unmarshal(group.Journal.After, &j) != nil || len(group.Files) == 0 || group.Files[0].Name != "models.json" {
		return errors.New("invalid WorkBuddy model label plan")
	}
	if group.Journal.Existed {
		if json.Unmarshal(group.Journal.Before, &previous) != nil {
			return errors.New("invalid previous WorkBuddy model labels")
		}
	} else {
		previous = j
	}
	before, applied, err := workBuddyNameCatalogs(&previous)
	if err != nil {
		return err
	}
	current, err := catalogModels("workbuddy", group.Files[0].Before)
	if err != nil {
		return err
	}
	changed := false
	for _, id := range o.EffectiveModels() {
		name := strings.TrimSpace(o.ModelNames[id])
		if name == "" {
			continue
		}
		old, _ := current[id].(map[string]any)
		alias, exists := old["name"]
		userAlias := exists && alias != "" && !workBuddyGeneratedName(id, alias, before, applied)
		wanted := any(name)
		if userAlias {
			wanted = alias
		}
		planned, err := workBuddyModels(group.Files[0].After)
		if err != nil {
			return err
		}
		entry, index := workBuddyModel(planned, id)
		if entry == nil {
			return errors.New("WorkBuddy label target is missing")
		}
		if !equalValue(entry["name"], entry["name"] != nil, wanted, true) {
			group.Files[0].After, err = workBuddyPatchModel(group.Files[0].After, index, map[string]any{"name": wanted}, nil)
			if err != nil {
				return err
			}
			changed = true
		}
		if !userAlias {
			managed, err := workBuddyModels(j.Files[0].After)
			if err != nil {
				return err
			}
			entry, index := workBuddyModel(managed, id)
			if entry == nil {
				return errors.New("WorkBuddy recovery label target is missing")
			}
			if entry["name"] != name {
				j.Files[0].After, err = workBuddyPatchModel(j.Files[0].After, index, map[string]any{"name": name}, nil)
				if err != nil {
					return err
				}
				changed = true
			}
		}
	}
	if !changed {
		return nil
	}
	c := (WorkBuddyAdapter{}).Contract()
	j.Adapter = &c
	j.Checksum = j.checksum()
	group.Journal.After, err = json.MarshalIndent(j, "", "  ")
	return err
}
