package routerredact

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/yetone/magpie/internal/cloudcodec"
	"github.com/yetone/magpie/internal/routercloud"
)

// HistoryRequired reports only quantities, never source text or private keys.
type HistoryRequired struct{ Messages, Characters int }

func (e *HistoryRequired) Error() string { return "redaction_history_prepare_required" }

func (p *Pipeline) scanReliableUserInput(parent context.Context, in Input) (_ *UserSnapshot, err error) {
	started := time.Now()
	defer func() {
		if err != nil {
			p.mu.Lock()
			defer p.mu.Unlock()
			p.stats.Processed++
			p.stats.Blocked++
			p.stats.DurationMS += time.Since(started).Milliseconds()
			p.stats.Failures[SafeFailureCode(err.Error())]++
		}
	}()
	ctx, cancel := context.WithTimeout(parent, p.options.Budget)
	defer cancel()
	if routercloud.ValidateInput(string(in.Protocol), in.Body) != nil {
		return nil, fail("wire_invalid")
	}
	if in.RequestID == "" || in.AppID == "" || in.AuthenticatedScope == "" || in.RecognitionMaterial == "" {
		return nil, fail("redaction_invalid_input")
	}
	tree, err := wireDecode(in.Body, 8<<20)
	if err != nil {
		return nil, err
	}
	if !userInputMediaSupported(tree) {
		return nil, fail("wire_unsupported")
	}
	view, err := cloudcodec.NewUserHistoryViewForApp(string(in.Protocol), in.Body, in.AppID)
	if err != nil {
		return nil, fail("wire_invalid")
	}
	messages := view.Messages()
	if len(view.Texts()) > 8192 {
		return nil, fail("redaction_input_limit")
	}
	texts := make([][]string, len(messages))
	for i, m := range messages {
		texts[i] = m.Texts
	}
	store := p.options.Results
	results, err := store.Load(ctx, in.AuthenticatedScope, in.RecognitionMaterial, texts)
	if err != nil {
		return nil, err
	}
	missing := &HistoryRequired{}
	newChunks := 0
	for i, m := range messages {
		if results[i] != nil {
			continue
		}
		if !m.Current {
			missing.Messages++
			for _, t := range m.Texts {
				missing.Characters += utf8.RuneCountInString(t)
			}
		} else {
			for _, t := range m.Texts {
				newChunks += len(ChunkText(t, DefaultLLMMaxChunkChars, DefaultLLMChunkOverlap))
			}
		}
	}
	// Inventory all old history before inference, including the newest input.
	if missing.Messages > 0 {
		return nil, missing
	}
	if newChunks > EngineMaxChunks {
		return nil, fail("redaction_input_limit")
	}
	meta := ObservationMeta{ScopeVersion: ScopeVersion, RecognitionMaterial: in.RecognitionMaterial, ScanDisposition: "no_user_input", SecondaryRecognition: "known_values_only", OutboundPolicyVersion: OutboundPolicyVersion}
	for _, m := range messages {
		if !m.Current {
			meta.HistoryUserMessages++
		}
	}
	var all []Entity
	for i, m := range messages {
		result := results[i]
		reused := result != nil
		if result == nil {
			key, _ := store.record(in.AuthenticatedScope, in.RecognitionMaterial, m.Texts)
			release, e := store.acquire(ctx, key)
			if e != nil {
				return nil, e
			}
			result, reused, err = p.recognizeMessage(ctx, in, m.Texts)
			release()
			if err != nil {
				return nil, err
			}
			meta.LocalInferenceCalls += result.LocalInferenceCalls
			meta.NewUserChunks += result.Chunks
			if !reused {
				for _, t := range m.Texts {
					meta.NewUserCharacters += utf8.RuneCountInString(t)
				}
			}
			if meta.NewUserCharacters > 0 {
				meta.ScanDisposition = "scanned"
			}
		}
		if reused {
			if !m.Current {
				meta.HistoryReused++
			}
			meta.ReusedUserFragments += len(m.Texts)
			if meta.ScanDisposition != "scanned" {
				meta.ScanDisposition = "persistent_reuse"
			}
		}
		if m.Current {
			meta.CurrentRecognition = "new"
			if len(result.Entities) == 0 {
				meta.CurrentRecognition = "zero_hit"
			}
			if reused {
				meta.CurrentRecognition = "reused"
				if len(result.Entities) == 0 {
					meta.CurrentRecognition = "reused_zero_hit"
				}
			}
		}
		for _, e := range result.Entities {
			e.Field = m.Fields[e.Field]
			all = append(all, e)
		}
	}
	if len(all) > maxEntities {
		return nil, fail("redaction_mapping_limit")
	}
	all, err = validateEntities(view.Texts(), Result{Complete: true, Entities: all})
	if err != nil {
		return nil, err
	}
	// Recognition records keep their fine categories. Token allocation resolves
	// category disagreements deterministically so one exact original has one
	// token throughout this request, even across different message contexts.
	categoryByValue := map[string]string{}
	for _, entity := range all {
		old := categoryByValue[entity.Text]
		if old == "" || entity.Category < old {
			categoryByValue[entity.Text] = entity.Category
		}
	}
	for i := range all {
		all[i].Category = categoryByValue[all[i].Text]
	}
	reserved := []string{}
	collectLiteralStrings(tree, &reserved)
	if len(in.ReservedBody) > 0 {
		target, e := wireDecode(in.ReservedBody, 8<<20)
		if e != nil {
			return nil, e
		}
		collectLiteralStrings(target, &reserved)
	}
	// A fresh request namespace prevents tokens from another response granting
	// access. Recognition reuse never reuses a restoration table.
	nonce := make([]byte, 16)
	if _, err = rand.Read(nonce); err != nil {
		return nil, fail("redaction_unavailable")
	}
	prefix := "v1" + hex.EncodeToString(nonce)
	fresh, cats := map[string]string{}, map[string]string{}
	byValue, next := map[string]string{}, map[string]int{}
	for _, entity := range all {
		if byValue[entity.Text] != "" {
			continue
		}
		next[entity.Category]++
		token := "<" + entity.Category + ":" + prefix + ":" + strconv.Itoa(next[entity.Category]) + ">"
		byValue[entity.Text] = token
		fresh[token] = strings.Clone(entity.Text)
		cats[token] = strings.Clone(entity.Category)
	}
	for _, literal := range reserved {
		count, e := issuedTokenCount(ctx, literal, fresh, nil)
		if e != nil {
			return nil, e
		}
		if count > 0 {
			return nil, fail("redaction_placeholder_collision")
		}
	}
	processed := view.Texts()
	at := 0
	for field, text := range processed {
		var out strings.Builder
		cursor := 0
		for at < len(all) && all[at].Field == field {
			entity := all[at]
			out.WriteString(text[cursor:entity.Start])
			out.WriteString(byValue[entity.Text])
			cursor = entity.End
			at++
		}
		out.WriteString(text[cursor:])
		processed[field] = out.String()
	}
	if err = contextError(ctx); err != nil {
		return nil, err
	}
	meta.DurationMS = time.Since(started).Milliseconds()
	meta.CacheHit = meta.ReusedUserFragments > 0
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil, fail("redaction_closed")
	}
	p.stats.Processed++
	if meta.ScanDisposition == "no_user_input" {
		p.stats.NoUserInput++
	}
	if meta.CacheHit {
		p.stats.CacheHits++
	}
	p.stats.Entities += uint64(len(all))
	p.stats.DurationMS += meta.DurationMS
	p.stats.NewUserCharacters += uint64(meta.NewUserCharacters)
	p.stats.NewUserChunks += uint64(meta.NewUserChunks)
	p.stats.LocalInferenceCalls += uint64(meta.LocalInferenceCalls)
	p.stats.ReusedUserFragments += uint64(meta.ReusedUserFragments)
	p.mu.Unlock()
	return &UserSnapshot{prepared: &Prepared{Snapshot: &Snapshot{mapping: fresh}, body: []byte(`{}`), categories: cats}, texts: processed, identities: view.Identities(), meta: meta}, nil
}

// Caller holds only this message's coalescing gate, never a database transaction
// or service lock. A committed concurrent result takes precedence over scanning.
func (p *Pipeline) recognizeMessage(ctx context.Context, in Input, texts []string) (*Result, bool, error) {
	store := p.options.Results
	cached, err := store.Load(ctx, in.AuthenticatedScope, in.RecognitionMaterial, [][]string{texts})
	if err != nil {
		return nil, false, err
	}
	if cached[0] != nil {
		return cached[0], true, nil
	}
	result := Result{Complete: true}
	nonempty := false
	for _, text := range texts {
		if text != "" {
			nonempty = true
			break
		}
	}
	if nonempty {
		if p.options.Extractor == nil {
			return nil, false, fail("redaction_unavailable")
		}
		result, err = p.options.Extractor.Extract(ctx, Extraction{Versions: in.Versions, Texts: texts})
		if err != nil {
			return nil, false, err
		}
	}
	if err = contextError(ctx); err != nil {
		return nil, false, err
	}
	if _, err = validateEntities(texts, result); err != nil {
		return nil, false, err
	}
	if err = store.Save(ctx, in.AuthenticatedScope, in.RecognitionMaterial, texts, result); err != nil {
		return nil, false, err
	}
	persisted, err := store.Load(ctx, in.AuthenticatedScope, in.RecognitionMaterial, [][]string{texts})
	if err != nil {
		return nil, false, err
	}
	if persisted[0] == nil {
		return nil, false, fail("redaction_store_commit_failed")
	}
	persisted[0].LocalInferenceCalls = result.LocalInferenceCalls
	persisted[0].Chunks = result.Chunks
	return persisted[0], false, nil
}

const OutboundPolicyVersion = "full-known-values-v2"
