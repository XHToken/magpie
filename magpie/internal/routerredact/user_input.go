package routerredact

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/yetone/magpie/internal/cloudcodec"
	"github.com/yetone/magpie/internal/routercloud"
)

const ScopeVersion = cloudcodec.UserInputScope
const KnownValueBoundaryVersion = "exact-longest-boundary-v1"

// UserSnapshot holds only processed original user fragments and private mapping.
// It can prepare several targets without retaining system prompts or schemas.
type UserSnapshot struct {
	mu         sync.RWMutex
	prepared   *Prepared
	texts      []string
	identities []string
	meta       ObservationMeta
}

func (s *UserSnapshot) Close() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.prepared != nil {
		s.prepared.Close()
	}
	s.prepared = nil
	s.texts = nil
	s.identities = nil
}

func (p *Pipeline) ScanUserInput(ctx context.Context, in Input) (*UserSnapshot, error) {
	if routercloud.ValidateInput(string(in.Protocol), in.Body) != nil {
		return nil, fail("wire_invalid")
	}
	if in.RequestID == "" || len(in.RequestID) > 128 || in.AppID == "" || len(in.AppID) > 48 || len(in.SessionID) > 256 || in.Versions.Model == "" || in.Versions.Prompt == "" || in.Versions.Config == "" {
		return nil, fail("redaction_invalid_input")
	}
	tree, err := wireDecode(in.Body, 8<<20)
	if err != nil {
		return nil, err
	}
	if !userInputMediaSupported(tree) {
		return nil, fail("wire_unsupported")
	}
	view, err := cloudcodec.NewUserTextViewForApp(string(in.Protocol), in.Body, in.AppID)
	if err != nil {
		return nil, fail("wire_invalid")
	}
	texts := view.Texts()
	if len(texts) > 8192 {
		return nil, fail("redaction_input_limit")
	}
	nonempty := false
	for _, text := range texts {
		if text != "" {
			nonempty = true
			break
		}
	}
	if !nonempty {
		snapshot, e := p.noUserInput(ctx, in, tree)
		if e != nil {
			return nil, e
		}
		snapshot.texts = texts
		snapshot.identities = view.Identities()
		return snapshot, nil
	}
	messages := make([]any, len(texts))
	for i, text := range texts {
		messages[i] = map[string]any{"role": "user", "content": text}
	}
	canonical, err := wireMarshal(map[string]any{"model": "user-input-scan", "messages": messages})
	if err != nil {
		return nil, fail("wire_invalid")
	}
	reserved := []string{}
	collectLiteralStrings(tree, &reserved)
	if len(in.ReservedBody) > 0 {
		targetTree, e := wireDecode(in.ReservedBody, 8<<20)
		if e != nil {
			return nil, e
		}
		collectLiteralStrings(targetTree, &reserved)
	}
	in.Protocol = Chat
	in.Body = canonical
	in.CacheScope = ScopeVersion
	in.Operation = "generation"
	in.TextIdentities = view.Identities()
	in.Reserved = append(in.Reserved, reserved...)
	in.SeedSessionID = ""
	prepared, err := p.Prepare(ctx, in)
	if err != nil {
		return nil, err
	}
	body, err := prepared.Body()
	if err != nil {
		prepared.Close()
		return nil, err
	}
	doc, err := parseRequest(Chat, body)
	if err != nil {
		prepared.Close()
		return nil, err
	}
	meta, err := prepared.ObservationMeta()
	if err != nil {
		prepared.Close()
		return nil, err
	}
	meta.ScopeVersion = ScopeVersion
	meta.ReusedUserFragments = 0
	if prepared.cacheHit {
		meta.ReusedUserFragments = len(texts)
	}
	meta.ScanDisposition = "scanned"
	if len(texts) == 0 {
		meta.ScanDisposition = "no_user_input"
	} else if prepared.cacheHit {
		meta.ScanDisposition = "cache_hit"
	}
	// Exact model call and chunk counts are populated by the extraction result.
	meta.NewUserCharacters = prepared.newUserCharacters
	meta.ReusedUserFragments = prepared.reusedFragments
	meta.LocalInferenceCalls = prepared.localInferenceCalls
	meta.NewUserChunks = prepared.newUserChunks
	if meta.NewUserCharacters > 0 {
		meta.ScanDisposition = "scanned"
	}
	p.mu.Lock()
	p.stats.NewUserCharacters += uint64(meta.NewUserCharacters)
	p.stats.NewUserChunks += uint64(meta.NewUserChunks)
	p.stats.ReusedUserFragments += uint64(meta.ReusedUserFragments)
	p.stats.LocalInferenceCalls += uint64(meta.LocalInferenceCalls)
	p.mu.Unlock()
	return &UserSnapshot{prepared: prepared, texts: append([]string(nil), doc.texts...), identities: view.Identities(), meta: meta}, nil
}
func collectLiteralStrings(v any, out *[]string) {
	switch x := v.(type) {
	case string:
		*out = append(*out, x)
	case []any:
		for _, a := range x {
			collectLiteralStrings(a, out)
		}
	case map[string]any:
		for _, a := range x {
			collectLiteralStrings(a, out)
		}
	}
}

func (s *UserSnapshot) PrepareTarget(protocol Protocol, body []byte, target *cloudcodec.UserTextTarget) (*Prepared, error) {
	return s.PrepareTargetContext(context.Background(), protocol, body, target)
}
func (s *UserSnapshot) PrepareTargetContext(ctx context.Context, protocol Protocol, body []byte, target *cloudcodec.UserTextTarget) (*Prepared, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.prepared == nil || target == nil {
		return nil, fail("redaction_released")
	}
	mapping, err := s.prepared.Snapshot.mappingCopy()
	if err != nil {
		return nil, err
	}
	for token := range s.prepared.categories {
		if _, ok := mapping[token]; !ok {
			for _, text := range s.texts {
				if strings.Contains(text, token) {
					return nil, fail("redaction_invalid_result")
				}
			}
		}
	}
	rawDoc, e := parseKnownValues(protocol, body)
	if e != nil {
		return nil, e
	}
	for i, text := range rawDoc.texts {
		if rawDoc.identities[i] == "excluded-key" {
			continue
		}
		for token := range mapping {
			if strings.Contains(text, token) {
				return nil, fail("redaction_unavailable")
			}
		}
	}
	matcher := newKnownMatcher(mapping)
	processed := append([]string(nil), s.texts...)
	for i, text := range processed {
		processed[i], err = matcher.replace(ctx, text, map[string]string{})
		if err != nil {
			return nil, err
		}
	}
	rendered, err := target.Render(processed)
	if err != nil {
		return nil, fail("redaction_unavailable")
	}
	// Validate final protocol independently of privacy recognition.
	if routercloud.ValidateInput(string(protocol), rendered) != nil {
		return nil, fail("wire_invalid")
	}

	doc, err := parseKnownValues(protocol, rendered)
	if err != nil {
		return nil, err
	}
	used := map[string]string{}
	replacements := make([]string, len(doc.texts))
	for i, text := range doc.texts {
		if doc.identities[i] == "excluded-key" {
			replacements[i] = text
			continue
		}
		replacements[i], err = matcher.replace(ctx, text, used)
		if err != nil {
			return nil, err
		}
	}
	rendered, err = doc.render(replacements)
	if err != nil {
		return nil, err
	}
	// Only tokens present in allowed text values of this actual target may restore.
	userView, err := cloudcodec.NewUserTextView(string(protocol), rendered)
	if err != nil {
		return nil, fail("wire_invalid")
	}
	allowed := userView.Texts()
	for i, text := range replacements {
		if doc.identities[i] != "excluded-key" {
			allowed = append(allowed, text)
		}
	}
	for _, text := range allowed {
		for token, original := range mapping {
			if strings.Contains(text, token) {
				used[token] = original
			}
		}
	}
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	return &Prepared{Snapshot: &Snapshot{mapping: used}, body: rendered, cacheHit: s.meta.CacheHit, categories: cloneStrings(s.prepared.categories), durationMS: s.meta.DurationMS, scopeMeta: s.meta}, nil
}

// Boundary v1: Unicode word boundaries apply to short (<4 code points) values
// and to adjacent ASCII word characters at ASCII edges for longer values.
// Matching is exact and single pass.
func knownBoundary(text, value string, start, end int) bool {
	word := func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' }
	short := utf8.RuneCountInString(value) < 4
	first, _ := utf8.DecodeRuneInString(value)
	last, _ := utf8.DecodeLastRuneInString(value)
	if start > 0 && (short || first < 128 && word(first)) {
		r, _ := utf8.DecodeLastRuneInString(text[:start])
		if word(r) && (short || r < 128) {
			return false
		}
	}
	if end < len(text) && (short || last < 128 && word(last)) {
		r, _ := utf8.DecodeRuneInString(text[end:])
		if word(r) && (short || r < 128) {
			return false
		}
	}
	return true
}

type knownNode struct {
	next         map[byte]*knownNode
	token, value string
}

func newKnownMatcher(mapping map[string]string) *knownNode {
	root := &knownNode{}
	tokens := make([]string, 0, len(mapping))
	for token := range mapping {
		tokens = append(tokens, token)
	}
	sort.Strings(tokens)
	for _, token := range tokens {
		value := mapping[token]
		if value == "" {
			continue
		}
		node := root
		for i := 0; i < len(value); i++ {
			if node.next == nil {
				node.next = map[byte]*knownNode{}
			}
			if node.next[value[i]] == nil {
				node.next[value[i]] = &knownNode{}
			}
			node = node.next[value[i]]
		}
		if node.token == "" {
			node.token, node.value = token, value
		}
	}
	return root
}
func replaceKnownValues(text string, mapping, used map[string]string) string {
	out, _ := newKnownMatcher(mapping).replace(context.Background(), text, used)
	return out
}
func (root *knownNode) replace(ctx context.Context, text string, used map[string]string) (string, error) {
	if len(root.next) == 0 {
		return text, contextError(ctx)
	}
	var out strings.Builder
	for at := 0; at < len(text); {
		if err := contextError(ctx); err != nil {
			return "", err
		}
		// A placeholder, whether literal or inserted, is never reinterpreted.
		if text[at] == '<' {
			if end := strings.IndexByte(text[at:min(len(text), at+maxPlaceholderBytes)], '>'); end >= 0 {
				out.WriteString(text[at : at+end+1])
				at += end + 1
				continue
			}
		}
		node := root
		var best *knownNode
		end := at
		for i := at; i < len(text); i++ {
			node = node.next[text[i]]
			if node == nil {
				break
			}
			if node.token != "" && knownBoundary(text, node.value, at, i+1) {
				best = node
				end = i + 1
			}
		}
		if best != nil {
			out.WriteString(best.token)
			used[best.token] = best.value
			at = end
		} else {
			r, n := utf8.DecodeRuneInString(text[at:])
			out.WriteRune(r)
			at += n
		}
		if out.Len() > 32<<20 {
			return "", fail("redaction_input_limit")
		}
	}
	return out.String(), nil
}

// Secondary values never enter a detector. Protocol keys, system/developer,
// schema, tool names, IDs and non-string scalars remain outside this view.
func parseKnownValues(protocol Protocol, body []byte) (*textDocument, error) {
	root, err := wireDecode(body, 8<<20)
	if err != nil {
		return nil, err
	}
	o, ok := root.(map[string]any)
	if !ok {
		return nil, fail("wire_invalid")
	}
	d := &textDocument{root: o}
	args := func(o map[string]any, key string) error { return d.arguments(o, key) }
	switch protocol {
	case Chat:
		list, _ := o["messages"].([]any)
		for _, raw := range list {
			m, _ := raw.(map[string]any)
			role := m["role"]
			if role == "assistant" || role == "tool" {
				if err := d.content(m, "content", protocol); err != nil {
					return nil, err
				}
				if calls, ok := m["tool_calls"].([]any); ok {
					for _, raw := range calls {
						call, _ := raw.(map[string]any)
						fn, _ := call["function"].(map[string]any)
						if err := args(fn, "arguments"); err != nil {
							return nil, err
						}
					}
				}
			}
		}
	case Anthropic:
		list, _ := o["messages"].([]any)
		for _, raw := range list {
			m, _ := raw.(map[string]any)
			if m["role"] == "assistant" {
				if err := d.content(m, "content", protocol); err != nil {
					return nil, err
				}
				continue
			}
			if blocks, ok := m["content"].([]any); ok {
				for _, raw := range blocks {
					b, _ := raw.(map[string]any)
					if b["type"] == "tool_result" {
						if err := d.content(b, "content", protocol); err != nil {
							return nil, err
						}
					}
				}
			}
		}
	case Responses:
		list, _ := o["input"].([]any)
		for _, raw := range list {
			item, _ := raw.(map[string]any)
			switch item["type"] {
			case "function_call":
				if err := args(item, "arguments"); err != nil {
					return nil, err
				}
			case "custom_tool_call":
				if err := d.field(item, "input"); err != nil {
					return nil, err
				}
			case "function_call_output", "custom_tool_call_output":
				if err := d.content(item, "output", protocol); err != nil {
					return nil, err
				}
			case "message", nil:
				if item["role"] == "assistant" {
					if err := d.content(item, "content", protocol); err != nil {
						return nil, err
					}
				}
			}
		}
	default:
		return nil, fail("wire_unsupported")
	}
	// Keys are never rewritten or detected; d.values formerly marked them as
	// protected detection fields. Drop those leaves from the mapping application.
	for _, i := range d.protected {
		d.identities[i] = "excluded-key"
	}
	return d, d.err
}

func (p *Pipeline) noUserInput(ctx context.Context, in Input, tree any) (*UserSnapshot, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	mapping, categories := map[string]string{}, map[string]string{}
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil, fail("redaction_closed")
	}
	now := p.options.Now()
	p.expireLocked(now)
	key := cacheKey{in.AppID, in.SessionID, ScopeVersion, Chat, "generation", in.Versions}
	if cached, ok := p.cache[key]; ok && in.SessionVerified && in.SessionID != "" {
		mapping = cloneStrings(cached.mapping)
		categories = cloneStrings(cached.categories)
		cached.used = now
		p.cache[key] = cached
	}
	p.stats.Processed++
	p.mu.Unlock()
	// A literal old token cannot authorize access to its prior private value.
	literals := []string{}
	collectLiteralStrings(tree, &literals)
	tokens := make([]string, 0, len(mapping))
	for token := range mapping {
		tokens = append(tokens, token)
	}
	sort.Strings(tokens)
	for _, token := range tokens {
		if err := contextError(ctx); err != nil {
			return nil, err
		}
		for _, literal := range literals {
			if strings.Contains(literal, token) {
				value, category := mapping[token], categories[token]
				if !categoryValid(category) {
					return nil, fail("redaction_invalid_result")
				}
				assigned := false
				delete(mapping, token)
				delete(categories, token)
				for n := 1; n <= maxEntities*4; n++ {
					if err := contextError(ctx); err != nil {
						return nil, err
					}
					candidate := "<" + category + ":" + strconv.Itoa(n) + ">"
					if _, exists := mapping[candidate]; exists {
						continue
					}
					collision := false
					for _, text := range literals {
						if strings.Contains(text, candidate) {
							collision = true
							break
						}
					}
					if !collision {
						mapping[candidate] = value
						categories[candidate] = category
						assigned = true
						break
					}
				}
				if !assigned {
					return nil, fail("redaction_placeholder_limit")
				}
				break
			}
		}
	}
	meta := ObservationMeta{ScopeVersion: ScopeVersion, ScanDisposition: "no_user_input"}
	if len(mapping) > 0 {
		meta.ScanDisposition = "known_values_only"
		meta.CacheHit = true
	}
	p.mu.Lock()
	if len(mapping) > 0 {
		p.stats.KnownValuesOnly++
		p.stats.CacheHits++
	} else {
		p.stats.NoUserInput++
	}
	p.mu.Unlock()
	prepared := &Prepared{Snapshot: &Snapshot{mapping: mapping}, body: []byte(`{}`), categories: categories}
	return &UserSnapshot{prepared: prepared, meta: meta}, nil
}

// Privacy still supports the existing text-only engineering subset. Schema and
// metadata strings do not participate in this capability check or detection.
func userInputMediaSupported(tree any) bool {
	var content func(any) bool
	content = func(v any) bool {
		blocks, _ := v.([]any)
		for _, raw := range blocks {
			b, _ := raw.(map[string]any)
			switch b["type"] {
			case "image", "image_url", "input_image", "audio", "input_audio", "input_file", "file", "document":
				return false
			case "tool_result":
				if !content(b["content"]) {
					return false
				}
			}
		}
		return true
	}
	root, _ := tree.(map[string]any)
	if !content(root["system"]) {
		return false
	}
	for _, key := range []string{"messages", "input"} {
		items, _ := root[key].([]any)
		for _, raw := range items {
			o, _ := raw.(map[string]any)
			if !content(o["content"]) || !content(o["output"]) {
				return false
			}
		}
	}
	return true
}
