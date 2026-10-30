package routerredact

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/yetone/magpie/internal/cloudcodec"
	"github.com/yetone/magpie/internal/routercloud"
)

func (s *UserSnapshot) prepareReliableTarget(ctx context.Context, protocol Protocol, body []byte, target *cloudcodec.UserTextTarget) (*Prepared, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.prepared == nil || target == nil {
		return nil, fail("redaction_released")
	}
	mapping, err := s.prepared.Snapshot.mappingCopy()
	if err != nil {
		return nil, err
	}
	original, err := wireDecode(body, 8<<20)
	if err != nil {
		return nil, err
	}
	literals := []string{}
	collectLiteralStrings(original, &literals)
	for _, text := range literals {
		count, e := issuedTokenCount(ctx, text, mapping, nil)
		if e != nil {
			return nil, e
		}
		if count > 0 {
			return nil, fail("redaction_placeholder_collision")
		}
	}
	rendered, err := target.Render(s.texts)
	if err != nil {
		return nil, fail("redaction_unavailable")
	}
	doc, err := parseRequest(protocol, rendered)
	if err != nil {
		return nil, err
	}
	matcher := newStrictKnownMatcher(mapping)
	used := map[string]string{}
	protected := map[int]bool{}
	for _, i := range doc.protected {
		protected[i] = true
	}
	if err = verifyControlTokens(ctx, doc.root, protected, mapping); err != nil {
		return nil, err
	}
	replacements := append([]string(nil), doc.texts...)
	count := 0
	for i, text := range doc.texts {
		replaced, e := matcher.replace(ctx, text, used)
		if e != nil {
			return nil, e
		}
		if protected[i] && replaced != text {
			return nil, fail("redaction_outbound_control_value")
		}
		replacements[i] = replaced
		before, e := issuedTokenCount(ctx, text, mapping, nil)
		if e != nil {
			return nil, e
		}
		var authority map[string]string
		if !protected[i] {
			authority = used
		}
		after, e := issuedTokenCount(ctx, replaced, mapping, authority)
		if e != nil {
			return nil, e
		}
		count += after - before
	}
	rendered, err = doc.render(replacements)
	if err != nil {
		return nil, err
	}
	if routercloud.ValidateInput(string(protocol), rendered) != nil {
		return nil, fail("wire_invalid")
	}
	meta := s.meta
	meta.KnownValueReplacements = count
	meta.OutboundVerified = true
	prepared := &Prepared{protocol: protocol, Snapshot: &Snapshot{mapping: used}, body: rendered, categories: cloneStrings(s.prepared.categories), scopeMeta: meta}
	if err = prepared.VerifyFinalBody(ctx, rendered); err != nil {
		prepared.Close()
		return nil, err
	}
	return prepared, nil
}

// VerifyFinalBody checks decoded strings, dynamic keys, numbers and nested JSON
// arguments after every conversion and extension. It never performs inference.
func (p *Prepared) VerifyFinalBody(ctx context.Context, body []byte) error {
	mapping, err := p.Snapshot.mappingCopy()
	if err != nil {
		return err
	}
	tree, err := wireDecode(body, 8<<20)
	if err != nil {
		return err
	}
	matcher := newStrictKnownMatcher(mapping)
	if p.protocol != "" {
		doc, e := parseRequest(p.protocol, body)
		if e != nil {
			return e
		}
		protected := map[int]bool{}
		for _, i := range doc.protected {
			protected[i] = true
		}
		if e = verifyControlTokens(ctx, doc.root, protected, mapping); e != nil {
			return e
		}
	}
	var walk func(any, int) error
	walk = func(v any, depth int) error {
		if depth > 128 {
			return fail("redaction_input_limit")
		}
		if e := contextError(ctx); e != nil {
			return e
		}
		switch x := v.(type) {
		case string:
			replaced, e := matcher.replace(ctx, x, map[string]string{})
			if e != nil {
				return e
			}
			if replaced != x {
				return fail("redaction_outbound_residual")
			}
			if len(x) > 1 && (x[0] == '{' || x[0] == '[') && json.Valid([]byte(x)) {
				nested, e := wireDecode([]byte(x), 8<<20)
				if e != nil {
					return e
				}
				return walk(nested, depth+1)
			}
		case json.Number:
			replaced, e := matcher.replace(ctx, string(x), map[string]string{})
			if e != nil {
				return e
			}
			if replaced != string(x) {
				return fail("redaction_outbound_control_value")
			}
		case []any:
			for _, a := range x {
				if e := walk(a, depth+1); e != nil {
					return e
				}
			}
		case map[string]any:
			for k, a := range x {
				if e := walk(k, depth+1); e != nil {
					return e
				}
				if e := walk(a, depth+1); e != nil {
					return e
				}
			}
		}
		return nil
	}
	return walk(tree, 0)
}

// Counting issued tokens is linear in text size, independent of entity count.
func issuedTokenCount(ctx context.Context, text string, mapping, used map[string]string) (int, error) {
	count := 0
	for at := 0; at < len(text); {
		if e := contextError(ctx); e != nil {
			return 0, e
		}
		next := strings.IndexByte(text[at:], '<')
		if next < 0 {
			break
		}
		at += next
		if end := strings.IndexByte(text[at:min(len(text), at+maxPlaceholderBytes)], '>'); end >= 0 {
			token := text[at : at+end+1]
			if original, ok := mapping[token]; ok {
				count++
				if used != nil {
					used[token] = original
				}
				at += end + 1
				continue
			}
		}
		at++
	}
	return count, nil
}

// Only parser-selected ordinary text and argument string leaves may carry
// issued tokens. A codec moving a user marker into an ID/schema fails closed.
func verifyControlTokens(ctx context.Context, v any, protected map[int]bool, mapping map[string]string) error {
	if e := contextError(ctx); e != nil {
		return e
	}
	switch x := v.(type) {
	case *wireText:
		// Protected fields remain literal strings in the parser's root. A
		// collected protected wireText would have no token authority either.
		if protected[x.index] {
			return fail("redaction_outbound_control_value")
		}
	case *wireArguments:
		return verifyControlTokens(ctx, x.root, protected, mapping)
	case *wireValues:
		return verifyControlTokens(ctx, x.root, protected, mapping)
	case string:
		count, e := issuedTokenCount(ctx, x, mapping, nil)
		if e != nil {
			return e
		}
		if count > 0 {
			return fail("redaction_outbound_control_value")
		}
	case []any:
		for _, value := range x {
			if e := verifyControlTokens(ctx, value, protected, mapping); e != nil {
				return e
			}
		}
	case map[string]any:
		for key, value := range x {
			if e := verifyControlTokens(ctx, key, protected, mapping); e != nil {
				return e
			}
			if e := verifyControlTokens(ctx, value, protected, mapping); e != nil {
				return e
			}
		}
	}
	return nil
}
