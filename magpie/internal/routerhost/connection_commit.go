package routerhost

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
)

// ConnectAndCommit includes the saved application Profile in the host write
// transaction. A failed Profile save rolls back the host and recovery record
// before Loomy is asked to reload its configuration.
func (e Engine) ConnectAndCommit(ctx context.Context, o Options, persist func() error) (*Plan, error) {
	o = NormalizeOptions(o)
	if o.Host != "loomy" && o.Host != "workbuddy" {
		return nil, errors.New("automatic connection supports WorkBuddy and Loomy only")
	}
	if err := validateOptions(o); err != nil {
		return nil, err
	}
	if err := e.validateState(o.ConfigRoot); err != nil {
		return nil, err
	}
	if pending, err := e.readSnapshot(e.StateDir, FilePlan{Name: portTransactionFile}); err != nil || pending.Existed {
		return nil, errors.New("an interrupted host transaction needs recovery before another connection change")
	}
	var group *portGroup
	var contract AdapterContract
	if p, err := e.Preview(o); err == nil {
		contract = p.contract
		group, err = e.connectionGroup(p)
		if err != nil {
			return nil, err
		}
	} else {
		var err error
		group, contract, err = e.prepareConnection(o)
		if err != nil {
			return nil, err
		}
	}
	if err := retireConnectionModels(o, group); err != nil {
		return nil, err
	}
	if e.Checker == nil {
		return nil, errors.New("connection checker is required")
	}
	if err := e.Checker.Check(ctx, o); err != nil {
		return nil, errors.New("connection checks failed; host configuration unchanged")
	}
	if persist == nil {
		persist = func() error { return nil }
	}
	if err := e.commitHostGroups(ctx, []portGroup{*group}, persist); err != nil {
		return nil, err
	}
	p := &Plan{Action: "apply", Host: o.Host, options: o, files: group.Files, stateDir: e.StateDir, contract: contract}
	p.Digest = planDigest(p)
	return p, nil
}

func (e Engine) connectionGroup(p *Plan) (*portGroup, error) {
	j, hash, err := e.readJournal(p.Host, p.options.ConfigRoot)
	if err != nil || hash != p.recordHash {
		return nil, errors.New("host recovery record changed during connection planning")
	}
	if p.Action == "apply" && (j == nil || j.Status == "restored") {
		j = &journal{Version: 1, Host: p.Host, Root: p.options.ConfigRoot, Files: p.files, Adapter: &p.contract}
	}
	if j == nil {
		return nil, errors.New("host recovery record is missing")
	}
	if p.Action == "apply" {
		j.Status = "applied"
	}
	j.Checksum = j.checksum()
	raw, _ := json.MarshalIndent(j, "", "  ")
	record, err := e.readSnapshot(e.StateDir, FilePlan{Name: filepath.Base(e.recordPath(p.Host, p.options.ConfigRoot)), After: raw})
	if err != nil {
		return nil, err
	}
	if record.Existed && sum(record.Before) != hash || !record.Existed && hash != "" {
		return nil, errors.New("host recovery record changed during connection planning")
	}
	return &portGroup{Host: p.Host, Root: p.options.ConfigRoot, Files: p.files, Journal: record}, nil
}

func retireConnectionModels(o Options, group *portGroup) error {
	var j journal
	if json.Unmarshal(group.Journal.After, &j) != nil || len(group.Files) == 0 {
		return errors.New("invalid host connection plan")
	}
	before := group.Files[0].After
	var after []byte
	var err error
	if o.Host == "loomy" {
		after, err = retireLoomyModels(o, before)
	} else {
		after, err = retireWorkBuddyModels(o, before, &j)
	}
	if err != nil || equal(before, after) {
		return err
	}
	ids, err := removedModelIDs(o.Host, before, after)
	if err != nil {
		return err
	}
	known := map[string]bool{}
	for _, id := range append(j.RetiredModels, ids...) {
		known[id] = true
	}
	if len(known) > 1024 {
		return errors.New("too many retired model identities; preserve the current catalog")
	}
	j.RetiredModels = nil
	for id := range known {
		j.RetiredModels = append(j.RetiredModels, id)
	}
	sort.Strings(j.RetiredModels)
	j.Version = 2
	j.Checksum = j.checksum()
	group.Journal.After, err = json.MarshalIndent(j, "", "  ")
	group.Files[0].After = after
	return err
}

// RestoreAndCommit keeps a failed Profile update from leaving the host restored
// while Router still advertises its previous connection.
func (e Engine) RestoreAndCommit(ctx context.Context, p *Plan, persist func() error) error {
	if err := e.checkPlan(p, "restore"); err != nil {
		return err
	}
	group, err := e.connectionGroup(p)
	if err != nil {
		return err
	}
	var j journal
	if json.Unmarshal(group.Journal.After, &j) != nil {
		return errors.New("invalid host recovery record")
	}
	j.Status = "restored"
	j.Checksum = j.checksum()
	group.Journal.After, err = json.MarshalIndent(j, "", "  ")
	if err != nil {
		return err
	}
	if persist == nil {
		persist = func() error { return nil }
	}
	if err := e.commitHostGroups(ctx, []portGroup{*group}, persist); err != nil {
		return err
	}
	for _, f := range group.Files {
		for _, original := range j.Files {
			if f.Name == original.Name && original.Existed && equal(f.After, original.Before) {
				if err := os.Chmod(hostPath(p.options.ConfigRoot, f.Name), os.FileMode(original.Mode)); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
