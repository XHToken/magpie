package routerredact

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func engineFixture(t *testing.T, values map[string]string) (*Pipeline, *privacyTestRuntime, *PrivacySpecification) {
	t.Helper()
	spec, err := NewEngineSpecification(ModelBinding{ModelID: "spark-x2.6-4B", RuntimeID: "windows-amd64-cpu"}, false)
	if err != nil {
		t.Fatal(err)
	}
	runtime := &privacyTestRuntime{fingerprint: spec.RuntimeFingerprint(), suite: spec.CapabilitySuite(), classify: func(_ context.Context, _ int, fields []privacyTestField) ([]byte, error) {
		return privacyTestEnvelope(fields, values), nil
	}}
	p := New(Options{Extractor: NewHybridExtractor(runtime, spec), ReversibleAmounts: true})
	t.Cleanup(p.Close)
	return p, runtime, spec
}

func TestUnifiedEngineLongFieldsExactAmountsAndSensitiveLabels(t *testing.T) {
	p, runtime, spec := engineFixture(t, map[string]string{"Elodie": "private_person", "88.6万元": "financial_amount", "2020-01-02": "private_date", "K": "secret"})
	text := strings.Repeat("普通内容。", 500) + "Elodie，金额88.6万元，生日2020-01-02，密码K。"
	in := testInput(text)
	in.Versions = spec.Versions()
	prepared, err := p.Prepare(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Close()
	body, _ := prepared.Body()
	for _, value := range []string{"Elodie", "88.6万元", "2020-01-02", "密码K"} {
		if strings.Contains(string(body), value) {
			t.Fatalf("sensitive value survived: %s", value)
		}
	}
	if runtime.calls.Load() < 2 {
		t.Fatal("long field was not split")
	}
	meta, _ := prepared.ObservationMeta()
	restored := string(body)
	for _, r := range meta.Replacements {
		restored = strings.ReplaceAll(restored, r.Placeholder, r.Original)
	}
	if !strings.Contains(restored, text) {
		t.Fatal("exact source was not recoverable")
	}
}

func TestUnifiedEngineRequiresEveryChunkAndCurrentSuite(t *testing.T) {
	p, runtime, spec := engineFixture(t, nil)
	in := testInput(strings.Repeat("普通内容。", 800))
	in.Versions = spec.Versions()
	runtime.classify = func(_ context.Context, call int, fields []privacyTestField) ([]byte, error) {
		if call == 2 {
			return []byte(`{"choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"{\"complete\":true,\"fields\":[]}"}}]}`), nil
		}
		return privacyTestEnvelope(fields, nil), nil
	}
	if _, err := p.Prepare(context.Background(), in); err == nil {
		t.Fatal("missing chunk completed preparation")
	}
	runtime.suite = "legacy-suite"
	before := runtime.calls.Load()
	if _, err := p.Prepare(context.Background(), in); err == nil || runtime.calls.Load() != before {
		t.Fatal("legacy evidence authorized unified extraction")
	}
}

func TestUnifiedEngineChunkLimitAndCacheScope(t *testing.T) {
	p, runtime, spec := engineFixture(t, nil)
	in := testInput("普通内容")
	in.Versions = spec.Versions()
	for _, scope := range []string{"target-a", "target-b", "target-b"} {
		in.CacheScope = scope
		prepared, err := p.Prepare(context.Background(), in)
		if err != nil {
			t.Fatal(err)
		}
		prepared.Close()
	}
	if runtime.calls.Load() != 2 {
		t.Fatal("cache crossed target scopes", runtime.calls.Load())
	}
	texts := make([]string, EngineMaxChunks+1)
	for i := range texts {
		texts[i] = "普通内容"
	}
	in = testInput(texts...)
	in.Versions = spec.Versions()
	_, err := p.Prepare(context.Background(), in)
	var failure *Failure
	if !errors.As(err, &failure) || failure.Code != "redaction_input_limit" {
		t.Fatal("chunk limit did not reject", err)
	}
	if runtime.calls.Load() != 2 {
		t.Fatal("over-limit plan started inference")
	}
}

func TestUnifiedEngineVersionIncludesMaterials(t *testing.T) {
	unified, err := NewEngineSpecification(ModelBinding{ModelID: "spark-x2.6-4B", RuntimeID: "windows-amd64-cpu"}, true)
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := NewExperimentalPrivacySpecification(ModelBinding{ModelID: "spark-x2.6-4B", RuntimeID: "windows-amd64-cpu"})
	if err != nil {
		t.Fatal(err)
	}
	if unified.Versions() == legacy.Versions() || !strings.HasPrefix(unified.CapabilitySuite(), EngineTrialVersion+":") {
		t.Fatal("hybrid borrowed legacy identity")
	}
	for label := range PrivacyLabels() {
		if _, ok := unified.labels()[label]; !ok {
			t.Fatal("lost legacy coverage", label)
		}
	}
}

func TestUnifiedEnginePublicFindingCannotEraseRuleSecret(t *testing.T) {
	p, _, spec := engineFixture(t, map[string]string{"synthetic@example.test": "public_information"})
	in := testInput("邮箱：synthetic@example.test")
	in.Versions = spec.Versions()
	prepared, err := p.Prepare(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Close()
	body, _ := prepared.Body()
	if strings.Contains(string(body), "synthetic@example.test") || !strings.Contains(string(body), "<EMAIL:1>") {
		t.Fatal("public finding overrode sensitive rule", string(body))
	}
}
