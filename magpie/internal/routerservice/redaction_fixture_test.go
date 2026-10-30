package routerservice

import (
	"context"
	"encoding/json"
	"github.com/yetone/magpie/internal/dirs"
	"github.com/yetone/magpie/internal/routermanifest"
	"github.com/yetone/magpie/internal/routerredact"
	"github.com/yetone/magpie/internal/routerruntime"
	"github.com/yetone/magpie/internal/testkeyvault"
	"os"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"unicode/utf8"
)

type redactionFixtureInvoker struct {
	LocalInvoker
	spec *routerredact.PrivacySpecification
}

func newFixtureRedactionService(local *LocalModels, experimental ...bool) *RedactionService {
	return newRedactionServiceWithStore(local, func(l dirs.Layout) (*routerredact.ResultStore, error) {
		return routerredact.OpenResultStoreWithKeyProtector(l, testkeyvault.Protector{})
	}, experimental...)
}

var issuedFixtureToken = regexp.MustCompile(`<([A-Z][A-Z0-9_]*):v1[a-f0-9]{32}:([0-9]+)>`)

// Existing fixtures describe echo behavior using short symbolic tokens. Resolve
// those symbols from the actual request, so random namespaces remain exercised.
func fixtureEchoTokens(body []byte, response string) string {
	decoded := strings.ReplaceAll(strings.ReplaceAll(string(body), `\u003c`, "<"), `\u003e`, ">")
	pairs := []string{}
	for _, m := range issuedFixtureToken.FindAllStringSubmatch(decoded, -1) {
		pairs = append(pairs, "<"+m[1]+":"+m[2]+">", m[0])
		if len(m[1]) >= 2 {
			suffix := m[1][len(m[1])-2:] + ":" + m[2] + ">"
			pairs = append(pairs, suffix, m[0][len(m[1])-1:])
		}
	}
	if len(pairs) == 0 {
		return response
	}
	return strings.NewReplacer(pairs...).Replace(response)
}
func fixtureLegacyTokens(body string) string {
	decoded := strings.ReplaceAll(strings.ReplaceAll(body, `\u003c`, "<"), `\u003e`, ">")
	return issuedFixtureToken.ReplaceAllString(decoded, `<$1:$2>`)
}

func prepareFixtureHistory(t *testing.T, r *RedactionService, app, protocol string, body []byte, selected ...int) {
	t.Helper()
	p, s, err := r.current()
	if err != nil {
		t.Fatal(err)
	}
	task, err := p.NewHistoryTask(context.Background(), routerredact.Input{AppID: app, Protocol: routerredact.Protocol(protocol), AuthenticatedScope: app + "/1", RecognitionMaterial: s.RecognitionMaterialID(), Versions: s.Versions(), Body: body}, selected)
	if err != nil {
		t.Fatal(err)
	}
	for !task.Progress().Done {
		if _, err = task.Step(context.Background(), p); err != nil {
			t.Fatal(err)
		}
	}
}

func (r redactionFixtureInvoker) Require(p routerruntime.Purpose) error {
	if underlying, ok := r.LocalInvoker.(interface {
		Require(routerruntime.Purpose) error
	}); ok {
		return underlying.Require(p)
	}
	if p != routerruntime.Redaction {
		return routerruntime.ErrCapability
	}
	return nil
}
func (r redactionFixtureInvoker) Status() routerruntime.Status {
	return routerruntime.Status{Capabilities: map[routerruntime.Purpose]routerruntime.Capability{routerruntime.Redaction: {Ready: true, State: "available", Fingerprint: r.spec.RuntimeFingerprint(), Suite: r.spec.CapabilitySuite()}}}
}
func redactionTestOptions(t *testing.T, opts ModelOptions) ModelOptions {
	t.Helper()
	if opts.Redaction != nil {
		return opts
	}
	if opts.Routing != nil && opts.Routing.Redactor != nil {
		opts.Redaction, _ = opts.Routing.Redactor.(*RedactionService)
		return opts
	}
	r := newFixtureRedactionService(opts.LocalRuntime, opts.ExperimentalRedaction)
	if r.results == nil && r.resultsError == nil {
		state := t.TempDir()
		if err := os.Chmod(state, 0700); err != nil {
			t.Fatal(err)
		}
		r.results, r.resultsError = routerredact.OpenResultStoreWithKeyProtector(dirs.Layout{State: state}, testkeyvault.Protector{})
	}
	if (opts.LocalRuntime == nil || opts.LocalRuntime.Manager == nil) && opts.Routing != nil && opts.Routing.LocalInvoker != nil {
		r.binding = routerredact.ModelBinding{ModelID: "spark-x2.6-4B", RuntimeID: routerruntime.PlatformRuntime(routermanifest.Fixed(), runtime.GOOS, runtime.GOARCH)}
		spec, err := routerredact.NewEngineSpecification(r.binding, false)
		if err != nil {
			t.Fatal(err)
		}
		r.invoker = redactionFixtureInvoker{opts.Routing.LocalInvoker, spec}
	}
	opts.Redaction = r
	t.Cleanup(r.close)
	return opts
}

// Generates complete strict model output from the actual multi-field request.
// Deliberately invalid-output tests provide raw responses instead.
func strictRedactionFixture(body []byte, values map[string]string) []byte {
	var request struct{ Messages []struct{ Content string } }
	_ = json.Unmarshal(body, &request)
	var input struct {
		Fields []struct {
			Field int
			Text  string
		}
	}
	if len(request.Messages) > 1 {
		_ = json.Unmarshal([]byte(request.Messages[1].Content), &input)
	}
	fields := []any{}
	for _, field := range input.Fields {
		findings := []any{}
		for value, label := range values {
			for offset := 0; offset < len(field.Text); {
				at := strings.Index(field.Text[offset:], value)
				if at < 0 {
					break
				}
				at += offset
				findings = append(findings, map[string]any{"label": label, "start": utf8.RuneCountInString(field.Text[:at]), "end": utf8.RuneCountInString(field.Text[:at+len(value)]), "text": value, "confidence": 1})
				offset = at + len(value)
			}
		}
		fields = append(fields, map[string]any{"field": field.Field, "complete": true, "findings": findings})
	}
	content, _ := json.Marshal(map[string]any{"complete": true, "fields": fields})
	envelope, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"index": 0, "finish_reason": "stop", "message": map[string]any{"role": "assistant", "content": string(content)}}}})
	return envelope
}
