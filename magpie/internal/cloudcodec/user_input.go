package cloudcodec

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
)

const UserInputScope = "user-input-v1"
const UserInputSourceVersion = "original-user-text-v1"

var ErrUserInputScope = errors.New("redaction user input source conflict")

// UserTextView is derived only from the original validated wire protocol.
// Its positions are server-owned; no client field can declare provenance.
type UserTextView struct {
	appID             string
	latestUser        int
	protocol          string
	root              map[string]any
	body              []byte
	texts, identities []string
	slots             []userTextSlot
}
type userTextSlot struct {
	object           map[string]any
	key              string
	start, end, user int
}

func userObject(body []byte) (map[string]any, error) {
	d := json.NewDecoder(bytes.NewReader(body))
	d.UseNumber()
	var root map[string]any
	if err := d.Decode(&root); err != nil || root == nil {
		return nil, ErrUserInputScope
	}
	return root, nil
}
func NewUserTextView(protocol string, body []byte) (*UserTextView, error) {
	root, err := userObject(body)
	if err != nil {
		return nil, err
	}
	v := &UserTextView{protocol: protocol, root: root, body: bytes.Clone(body), latestUser: -1}
	add := func(o map[string]any, key string, u, p int) {
		if s, ok := o[key].(string); ok {
			v.slots = append(v.slots, userTextSlot{object: o, key: key, end: len(s), user: u})
			v.texts = append(v.texts, s)
			v.identities = append(v.identities, fmt.Sprintf("%s/%d/%d", UserInputSourceVersion, u, p))
		}
	}
	content := func(o map[string]any, key string, u int) {
		if _, ok := o[key].(string); ok {
			add(o, key, u, 0)
			return
		}
		blocks, _ := o[key].([]any)
		// Only direct legal user text blocks count. tool_result descendants never do.
		part := 0
		for _, raw := range blocks {
			b, _ := raw.(map[string]any)
			if b["type"] == "text" || b["type"] == "input_text" {
				add(b, "text", u, part)
				part++
			}
		}
	}
	switch protocol {
	case "responses":
		if _, ok := root["input"].(string); ok {
			v.latestUser = 0
			add(root, "input", 0, 0)
			break
		}
		items, _ := root["input"].([]any)
		u := 0
		for _, raw := range items {
			o, _ := raw.(map[string]any)
			if o["role"] == "user" && (o["type"] == nil || o["type"] == "message") {
				v.latestUser = u
				content(o, "content", u)
				u++
			}
		}
	case "chat", "anthropic":
		items, _ := root["messages"].([]any)
		u := 0
		for _, raw := range items {
			o, _ := raw.(map[string]any)
			if o["role"] == "user" {
				v.latestUser = u
				content(o, "content", u)
				u++
			}
		}
	default:
		return nil, ErrUserInputScope
	}
	return v, nil
}
func (v *UserTextView) Texts() []string      { return append([]string(nil), v.texts...) }
func (v *UserTextView) Identities() []string { return append([]string(nil), v.identities...) }
func (v *UserTextView) Rewrite(texts []string) ([]byte, error) {
	if len(texts) != len(v.slots) {
		return nil, ErrUserInputScope
	}
	fresh, err := NewUserTextViewForApp(v.protocol, v.body, v.appID)
	if err != nil {
		return nil, err
	}
	for i := len(fresh.slots) - 1; i >= 0; i-- {
		s := fresh.slots[i]
		original := s.object[s.key].(string)
		s.object[s.key] = original[:s.start] + texts[i] + original[s.end:]
	}
	return json.Marshal(fresh.root)
}

// UserTextTarget carries an internal provenance template through exactly the
// same codec and options expansion as the real request. Random markers replace
// user fragments only; equality of business strings never establishes a source.
type UserTextTarget struct {
	template       any
	markers, texts []string
	identities     []string
	sourceProtocol string
}

func (v *UserTextView) BindTarget(final []byte, transform func([]byte) ([]byte, error)) (*UserTextTarget, error) {
	nonce := make([]byte, 24)
	if _, err := rand.Read(nonce); err != nil {
		return nil, ErrUserInputScope
	}
	prefix := "__router_user_" + hex.EncodeToString(nonce) + "_"
	if bytes.Contains(v.body, []byte(prefix)) {
		return nil, ErrUserInputScope
	}
	markers := make([]string, len(v.texts))
	for i := range markers {
		markers[i] = fmt.Sprintf("%s%d__", prefix, i)
	}
	marked, err := v.Rewrite(markers)
	if err != nil {
		return nil, err
	}
	transformed, err := transform(marked)
	if err != nil {
		return nil, err
	}
	root, err := userObject(transformed)
	if err != nil {
		return nil, err
	}
	t := &UserTextTarget{template: root, markers: markers, texts: v.Texts(), identities: v.Identities(), sourceProtocol: v.protocol}
	counts := make([]int, len(markers))
	walkUserStrings(root, func(s string) string {
		for i, m := range markers {
			counts[i] += strings.Count(s, m)
		}
		return s
	})
	for _, count := range counts {
		if count == 0 {
			return nil, ErrUserInputScope
		}
	}
	original, err := t.Render(v.texts)
	if err != nil {
		return nil, err
	}
	expected, err := userObject(original)
	if err != nil {
		return nil, err
	}
	actual, err := userObject(final)
	if err != nil || !reflect.DeepEqual(expected, actual) {
		return nil, ErrUserInputScope
	}
	return t, nil
}
func (t *UserTextTarget) Render(texts []string) ([]byte, error) {
	if t == nil || len(texts) != len(t.markers) {
		return nil, ErrUserInputScope
	}
	pairs := make([]string, 0, len(texts)*2)
	for i, m := range t.markers {
		pairs = append(pairs, m, texts[i])
	}
	raw, err := json.Marshal(t.template)
	if err != nil {
		return nil, ErrUserInputScope
	}
	root, err := userObject(raw)
	if err != nil {
		return nil, err
	}
	if len(pairs) > 0 {
		replace := strings.NewReplacer(pairs...)
		walkUserStrings(root, replace.Replace)
	}
	return json.Marshal(root)
}
func walkUserStrings(v any, rewrite func(string) string) any {
	switch x := v.(type) {
	case string:
		return rewrite(x)
	case []any:
		for i, a := range x {
			x[i] = walkUserStrings(a, rewrite)
		}
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			x[k] = walkUserStrings(x[k], rewrite)
		}
	}
	return v
}

// MatchesSource rejects a sidecar accidentally paired with another request.
func (t *UserTextTarget) MatchesSource(v *UserTextView) bool {
	return t != nil && v != nil && t.sourceProtocol == v.protocol && reflect.DeepEqual(t.identities, v.Identities()) && reflect.DeepEqual(t.texts, v.Texts())
}
