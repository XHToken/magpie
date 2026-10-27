package routerhost

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/yetone/magpie/internal/edit"
	"github.com/yetone/magpie/internal/product"
)

func (e Engine) lock(root string) (func(), error) {
	if err := e.validateState(root); err != nil {
		return nil, err
	}
	for _, dir := range []string{e.StateDir, root} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("cannot create operation directory: %w", err)
		}
	}
	if err := validateRoot(e.StateDir); err != nil {
		return nil, err
	}
	if err := validateRoot(root); err != nil {
		return nil, err
	}
	if err := os.Chmod(e.StateDir, 0o700); err != nil {
		return nil, err
	}
	var held []string
	release := func() {
		for i := len(held) - 1; i >= 0; i-- {
			_ = os.Remove(held[i])
		}
	}
	for _, path := range []string{filepath.Join(e.StateDir, ".operation.lock"), filepath.Join(root, product.HostLock), filepath.Join(root, product.LegacyHostLock)} {
		if err := os.Mkdir(path, 0o700); err != nil {
			release()
			return nil, fmt.Errorf("operation locked or directory not writable: %s; check for a running writer before removing a stale lock", path)
		}
		held = append(held, path)
	}
	return release, nil
}

// Connect applies a saved Profile to WorkBuddy or Loomy, including a model
// change on this same Router, and retires obsolete generated model entries.
func (e Engine) Connect(ctx context.Context, o Options) (*Plan, error) {
	o = NormalizeOptions(o)
	if (o.Host == "loomy" || o.Host == "workbuddy") && len(o.ModelNames) > 0 {
		return e.ConnectAndCommit(ctx, o, nil)
	}
	p, err := e.connect(ctx, o)
	if err != nil {
		return nil, err
	}
	if err := e.retireModels(ctx, o); err != nil {
		return nil, err
	}
	return p, nil
}

func (e Engine) connect(ctx context.Context, o Options) (*Plan, error) {
	if o.Host != "workbuddy" && o.Host != "loomy" {
		return nil, errors.New("automatic connection supports WorkBuddy and Loomy only")
	}
	if err := validateOptions(o); err != nil {
		return nil, err
	}
	if err := e.validateState(o.ConfigRoot); err != nil {
		return nil, err
	}
	if pending, err := e.readSnapshot(e.StateDir, FilePlan{Name: portTransactionFile}); err != nil || pending.Existed {
		if err != nil {
			return nil, err
		}
		return nil, errors.New("an interrupted host transaction needs recovery before another connection change")
	}
	if p, err := e.Preview(o); err == nil {
		return p, e.Apply(ctx, p)
	}
	group, contract, err := e.prepareConnection(o)
	if err != nil {
		return nil, err
	}
	if e.Checker == nil {
		return nil, errors.New("connection checker is required")
	}
	if err := e.Checker.Check(ctx, o); err != nil {
		return nil, errors.New("connection checks failed; host configuration unchanged")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := e.commitHostGroups(ctx, []portGroup{*group}, func() error { return nil }); err != nil {
		return nil, err
	}
	p := &Plan{Action: "apply", Host: o.Host, options: o, files: group.Files, stateDir: e.StateDir, contract: contract}
	p.Digest = planDigest(p)
	return p, nil
}

func (e Engine) prepareConnection(o Options) (*portGroup, AdapterContract, error) {
	o = NormalizeOptions(o)
	a, contract, err := e.adapter(o.Host)
	if err != nil {
		return nil, contract, err
	}
	if err := validateOptions(o); err != nil {
		return nil, contract, err
	}
	if err := e.validateState(o.ConfigRoot); err != nil {
		return nil, contract, err
	}
	var files []snapshot
	var input []FilePlan
	for _, spec := range contract.Files {
		s, err := e.readSnapshot(o.ConfigRoot, FilePlan{Name: spec.Name})
		if err != nil {
			return nil, contract, err
		}
		files = append(files, s)
		input = append(input, cloneFile(s.FilePlan))
	}
	j, recordHash, err := e.readJournal(o.Host, o.ConfigRoot)
	if err != nil {
		return nil, contract, err
	}
	if err := checkJournalContract(j, contract); err != nil {
		return nil, contract, err
	}
	if j != nil && j.Status != "applied" && j.Status != "restored" {
		return nil, contract, errors.New("an incomplete host operation needs recovery")
	}
	if err := checkConnectedFiles(a, o, input); err != nil && !originalConnectionFiles(j, input) {
		return nil, contract, err
	}
	var baseline []FilePlan
	if j != nil && j.Status == "applied" {
		for i, f := range j.Files {
			if i >= len(input) || f.Name != input[i].Name {
				return nil, contract, errors.New("host recovery file set changed")
			}
			curBytes := input[i].Before
			if j.Host == "loomy" && f.Name == "opencode.json" {
				// Explicit reconnect replaces a native primary selection after the
				// existing Router provider has passed checkConnectedFiles above.
				// Normalize only the recovery input; keep the actual file snapshot
				// for stale-write checks and transaction rollback.
				if nowDoc, err := readJSON(curBytes); err == nil && loomyPreservedSelector(nowDoc["model"]) {
					if afterDoc, err := readJSON(f.After); err == nil {
						if afterModel, ok := afterDoc["model"].(string); ok && loomyRouterSelection(afterModel) {
							if patched, err := edit.PatchJSON(curBytes, []edit.KV{{Path: "model", Value: afterModel}}, nil); err == nil {
								curBytes = patched
							}
						}
					}
				}
			}
			before, err := restoreConnectionContent(a, j, f.FilePlan, curBytes)
			if err != nil {
				return nil, contract, err
			}
			input[i].Before, input[i].Existed = before, before != nil
			baseline = append(baseline, cloneFile(f.FilePlan))
		}
		if len(baseline) != len(input) {
			return nil, contract, errors.New("host recovery file set changed")
		}
	} else {
		for _, f := range input {
			baseline = append(baseline, cloneFile(f))
		}
		j = &journal{Version: 1, Host: o.Host, Root: o.ConfigRoot, Adapter: &contract}
	}
	replacements, err := buildConnection(a, o, input)
	if err != nil {
		return nil, contract, err
	}
	managed, err := buildConnection(a, o, baseline)
	if err != nil {
		return nil, contract, err
	}
	for i := range files {
		files[i].After = replacements[i].After
	}
	if len(j.Files) == 0 || j.Status == "restored" {
		j.Files = make([]snapshot, len(files))
		copy(j.Files, files)
	}
	for i := range j.Files {
		j.Files[i].After = managed[i].After
	}
	j.Adapter = &contract
	j.Status = "applied"
	keep := o.effectiveModelSet()
	var retired []string
	for _, id := range j.RetiredModels {
		if !keep[id] {
			retired = append(retired, id)
		}
	}
	j.RetiredModels = retired
	j.Checksum = j.checksum()
	raw, err := json.MarshalIndent(j, "", "  ")
	if err != nil {
		return nil, contract, err
	}
	record, err := e.readSnapshot(e.StateDir, FilePlan{Name: filepath.Base(e.recordPath(o.Host, o.ConfigRoot)), After: raw})
	if err != nil {
		return nil, contract, err
	}
	if (record.Existed && sum(record.Before) != recordHash) || (!record.Existed && recordHash != "") {
		return nil, contract, errors.New("host recovery record changed during connection planning")
	}
	return &portGroup{Host: o.Host, Root: o.ConfigRoot, Files: files, Journal: record}, contract, nil
}

// A host can restore its exact pre-connection bytes independently. Its validated
// journal still identifies that baseline, so an explicit reconnect can reuse it.
func originalConnectionFiles(j *journal, input []FilePlan) bool {
	if j == nil || j.Status != "applied" || len(j.Files) != len(input) {
		return false
	}
	for i, f := range j.Files {
		if f.Name != input[i].Name || f.Existed != input[i].Existed || !bytes.Equal(f.Before, input[i].Before) {
			return false
		}
	}
	return true
}

func checkConnectedFiles(a Adapter, o Options, input []FilePlan) error {
	i, prefix := 0, loomyProvider+"/"
	if o.Host == "workbuddy" {
		i, prefix = 1, "custom-local:"
	}
	if i >= len(input) {
		return errors.New("host connection files unavailable")
	}
	doc, err := readJSON(input[i].Before)
	if err != nil {
		return err
	}
	loomyProvider := providerID(doc, "provider")
	if o.Host == "loomy" {
		prefix = loomyProvider + "/"
	}
	selection, ok := doc["model"].(string)
	if !ok || !strings.HasPrefix(selection, prefix) {
		if o.Host != "loomy" || !loomyPreservedSelector(selection) || o.PrimaryModel() == "" {
			return errors.New("host is not selecting its Router connection")
		}
		selection = prefix + o.PrimaryModel()
	}
	o.Model = strings.TrimPrefix(selection, prefix)
	o.Models = nil
	checkInput := input
	if o.Host == "loomy" {
		var patches []edit.KV
		if cur, _ := doc["model"].(string); cur != selection {
			patches = append(patches, edit.KV{Path: "model", Value: selection})
		}
		if small, ok := doc["small_model"].(string); ok && loomyRouterSelection(small) && small != selection {
			patches = append(patches, edit.KV{Path: "small_model", Value: selection})
		}
		if prov, ok := lookup(doc, []string{"provider", loomyProvider}); ok && !loomyDefaultsOnlyProvider(prov) {
			if pm, ok := prov.(map[string]any); ok {
				if models, ok := pm["models"].(map[string]any); ok && loomyModelKey(o.Model) {
					if _, exists := models[o.Model]; !exists {
						patches = append(patches, edit.KV{
							Path:  loomyJSONPath([]string{"provider", loomyProvider, "models", o.Model}),
							Value: map[string]any{"name": o.Model, "tool_call": true},
						})
					}
				}
			}
		}
		if len(patches) > 0 {
			if patched, err := edit.PatchJSON(input[i].Before, patches, nil); err == nil {
				checkInput = append([]FilePlan(nil), input...)
				checkInput[i] = cloneFile(input[i])
				checkInput[i].Before = patched
			}
		}
	}
	var got []FilePlan
	if o.Host == "loomy" {
		got, err = buildLoomyModels(o, checkInput[:1])
	} else {
		got, err = a.Build(o, checkInput)
	}
	if err != nil {
		return errors.New("host Router connection fields conflict")
	}
	for _, f := range got {
		if !sameConnection(o.Host, f.Before, f.After) {
			return errors.New("host Router connection belongs to another target")
		}
	}
	return nil
}

func buildConnection(a Adapter, o Options, input []FilePlan) ([]FilePlan, error) {
	got, err := a.Build(o, input)
	if err == nil || o.Host != "loomy" || len(input) != 2 {
		return got, err
	}
	doc, err := readJSON(input[0].Before)
	if err != nil {
		return nil, err
	}
	checkInput := input
	loomyProvider := providerID(doc, "provider")
	var patches []edit.KV
	if key, _ := lookup(doc, []string{"provider", loomyProvider, "options", "apiKey"}); product.IsPlaceholder(key) {
		if base, ok := lookup(doc, []string{"provider", loomyProvider, "options", "baseURL"}); ok {
			if baseStr, ok := base.(string); ok {
				origin := strings.TrimSuffix(strings.TrimRight(baseStr, "/"), "/app/loomy/v1")
				targetBase := strings.TrimRight(o.RouterURL, "/") + "/app/loomy/v1"
				if origin != strings.TrimRight(baseStr, "/") && validPortOrigin(origin) && strings.TrimRight(baseStr, "/") != targetBase {
					patches = append(patches, edit.KV{Path: "provider." + loomyProvider + ".options.baseURL", Value: targetBase})
					if patched, err := edit.PatchJSON(input[0].Before, patches, nil); err == nil {
						f := cloneFile(input[0])
						f.Before = patched
						checkInput = append([]FilePlan{f}, input[1:]...)
					}
				}
			}
		}
	}
	if err := checkConnectedFiles(a, o, checkInput); err != nil {
		return nil, err
	}
	for _, m := range o.EffectiveModels() {
		if !loomyModelKey(m) {
			return nil, errors.New("invalid Loomy model key")
		}
		want := map[string]any{"name": m, "tool_call": true}
		if existing, exists := lookup(doc, []string{"provider", loomyProvider, "models", m}); exists {
			if !loomyManagedModelEntry(existing) {
				return nil, errors.New("Loomy target model conflicts")
			}
			continue
		}
		patches = append(patches, edit.KV{Path: loomyJSONPath([]string{"provider", loomyProvider, "models", m}), Value: want})
	}
	raw, err := edit.PatchJSON(input[0].Before, patches, nil)
	if err != nil {
		return nil, err
	}
	f := cloneFile(input[0])
	f.Before = raw
	got, err = a.Build(o, append([]FilePlan{f}, input[1:]...))
	if err == nil {
		got[0].Before = bytes.Clone(input[0].Before)
	}
	return got, err
}
