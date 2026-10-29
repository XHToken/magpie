package routerredact

import (
	"context"
	"encoding/json"
	"github.com/yetone/magpie/internal/cloudcodec"
	"reflect"
	"strings"
	"testing"
)

func userFixture(t *testing.T, p *Pipeline, in Input) *UserSnapshot {
	t.Helper()
	s, e := p.ScanUserInput(context.Background(), in)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(s.Close)
	return s
}
func userTargetFixture(t *testing.T, s *UserSnapshot, in Input) *Prepared {
	t.Helper()
	v, e := cloudcodec.NewUserTextView(string(in.Protocol), in.Body)
	if e != nil {
		t.Fatal(e)
	}
	target, e := v.BindTarget(in.Body, func(b []byte) ([]byte, error) { return b, nil })
	if e != nil {
		t.Fatal(e)
	}
	p, e := s.PrepareTarget(in.Protocol, in.Body, target)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(p.Close)
	return p
}
func TestUserInputCacheIgnoresNonUserMaterialAndReplacesEchoes(t *testing.T) {
	var scanned [][]string
	detector := synthetic("Elodie")
	p := New(Options{ReversibleAmounts: true, Extractor: ExtractorFunc(func(ctx context.Context, in Extraction) (Result, error) {
		scanned = append(scanned, append([]string(nil), in.Texts...))
		return detector.Extract(ctx, in)
	})})
	defer p.Close()
	in := testInput("Contact Elodie")
	first := userFixture(t, p, in)
	out := userTargetFixture(t, first, in)
	if !strings.Contains(bodyString(t, out), "<PERSON:1>") {
		t.Fatal("user not masked")
	}
	in.Body = []byte(`{"model":"other","messages":[{"role":"system","content":"NON_USER Elodie"},{"role":"user","content":"Contact Elodie"},{"role":"assistant","content":"Elodie unknownsecret"},{"role":"tool","tool_call_id":"call","content":"Elodie unknownsecret"}],"tools":[{"type":"function","function":{"name":"Elodie","description":"NON_USER Elodie","parameters":{"type":"object","properties":{"Elodie":{"type":"string"}}}}}]}`)
	cached := userFixture(t, p, in)
	out = userTargetFixture(t, cached, in)
	if len(scanned) != 1 || cached.meta.ScanDisposition != "cache_hit" {
		t.Fatal("non-user data invalidated user cache", scanned, cached.meta)
	}
	var root map[string]any
	raw, _ := out.Body()
	json.Unmarshal(raw, &root)
	messages := root["messages"].([]any)
	if messages[0].(map[string]any)["content"] != "NON_USER Elodie" || messages[2].(map[string]any)["content"] != "<PERSON:1> unknownsecret" {
		t.Fatal("invalid mapping scope", string(raw))
	}
	in.Body = []byte(`{"model":"m","messages":[{"role":"tool","tool_call_id":"call","content":"Elodie unknownsecret"}]}`)
	callback := userFixture(t, p, in)
	out = userTargetFixture(t, callback, in)
	if len(scanned) != 1 || callback.meta.ScanDisposition != "known_values_only" || !strings.Contains(bodyString(t, out), "<PERSON:1> unknownsecret") {
		t.Fatal("callback invoked model or lost mapping", scanned, callback.meta, bodyString(t, out))
	}
	in.SessionVerified = false
	empty := userFixture(t, p, in)
	out = userTargetFixture(t, empty, in)
	if len(scanned) != 1 || empty.meta.ScanDisposition != "no_user_input" || !strings.Contains(bodyString(t, out), "Elodie") {
		t.Fatal("untrusted callback borrowed private state")
	}
}
func TestUserInputHistoryChangesAndUntrustedSessionsRescan(t *testing.T) {
	calls := 0
	p := New(Options{Extractor: ExtractorFunc(func(ctx context.Context, in Extraction) (Result, error) {
		calls++
		return synthetic("Elodie").Extract(ctx, in)
	})})
	defer p.Close()
	in := testInput("Elodie")
	userFixture(t, p, in)
	appended := testInput("Elodie", "new")
	userFixture(t, p, appended)
	if calls != 2 {
		t.Fatal(calls)
	}
	edited := testInput("edited", "Elodie")
	userFixture(t, p, edited)
	if calls != 3 {
		t.Fatal("edit inherited unrelated history")
	}
	in.SessionVerified = false
	in.SeedSessionID = "verified"
	userFixture(t, p, in)
	userFixture(t, p, in)
	if calls != 5 {
		t.Fatal("unverified session reused plaintext cache", calls)
	}
}
func TestKnownValueReplacementBoundariesLongestAndLiteralTokens(t *testing.T) {
	mapping := map[string]string{"<NAME:1>": "Ann", "<NAME:2>": "Ann Lee", "<CODE:1>": "12"}
	used := map[string]string{}
	got := replaceKnownValues("Ann Lee; Ann; Annual; 12; 123; <NAME:12>", mapping, used)
	if got != "<NAME:2>; <NAME:1>; Annual; <CODE:1>; 123; <NAME:12>" {
		t.Fatal(got)
	}
	if !reflect.DeepEqual(used, mapping) {
		t.Fatal(used)
	}
}
func TestUserInputOnlyActualTargetTokensMayRestore(t *testing.T) {
	p := New(Options{Extractor: synthetic("Elodie")})
	defer p.Close()
	in := testInput("Elodie")
	userFixture(t, p, in)
	in.Body = []byte(`{"model":"m","messages":[{"role":"tool","tool_call_id":"call","content":"unknown"}]}`)
	s := userFixture(t, p, in)
	out := userTargetFixture(t, s, in)
	got, e := out.RestoreText("<PERSON:1>")
	if e != nil || got != "<PERSON:1>" {
		t.Fatal("unused cached token restored", got, e)
	}
	// A user-supplied old token never exposes its prior plaintext.
	literal := testInput("<PERSON:1>")
	s = userFixture(t, p, literal)
	out = userTargetFixture(t, s, literal)
	got, e = out.RestoreText("<PERSON:1>")
	if e != nil || got != "<PERSON:1>" {
		t.Fatal("literal token read cached original", got, e)
	}
}

func TestUserInputStructureDoesNotAuthorizeOldTokens(t *testing.T) {
	p := New(Options{Extractor: synthetic("Elodie")})
	defer p.Close()
	in := testInput("Elodie")
	userFixture(t, p, in)
	in.Body = []byte(`{"model":"m","messages":[{"role":"assistant","tool_calls":[{"id":"call","type":"function","function":{"name":"f","arguments":"{\"<PERSON:1>\":true}"}}]}]}`)
	s := userFixture(t, p, in)
	out := userTargetFixture(t, s, in)
	got, e := out.RestoreText("<PERSON:1>")
	if e != nil || got != "<PERSON:1>" {
		t.Fatal("structure key authorized private restoration", got, e)
	}
}

func TestUserInputEmptyTextNeedsNoDetector(t *testing.T) {
	p := New(Options{})
	defer p.Close()
	in := testInput("")
	s := userFixture(t, p, in)
	out := userTargetFixture(t, s, in)
	meta, e := out.ObservationMeta()
	if e != nil || meta.ScanDisposition != "no_user_input" || meta.LocalInferenceCalls != 0 {
		t.Fatal(meta, e)
	}
}

func TestKnownValueUnicodeShortBoundary(t *testing.T) {
	got := replaceKnownValues("张三；联系张三；张三丰", map[string]string{"<NAME:1>": "张三"}, map[string]string{})
	if got != "<NAME:1>；联系张三；张三丰" {
		t.Fatal(got)
	}
}

func TestKnownValueLongASCIIWithinChineseSentence(t *testing.T) {
	mapping := map[string]string{"<PHONE:1>": "13311112222", "<NAME:1>": "Elodie"}
	got := replaceKnownValues("电话13311112222请联系Elodie；v13311112222；ElodieX", mapping, map[string]string{})
	if got != "电话<PHONE:1>请联系<NAME:1>；v13311112222；ElodieX" {
		t.Fatal(got)
	}
}
