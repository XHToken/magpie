package routerservice

import (
	"encoding/json"
	"github.com/yetone/magpie/internal/routermanifest"
	"github.com/yetone/magpie/internal/routerredact"
	"github.com/yetone/magpie/internal/routerruntime"
	"runtime"
	"strings"
	"testing"
	"unicode/utf8"
)

type redactionFixtureInvoker struct {
	LocalInvoker
	spec *routerredact.PrivacySpecification
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
	r := newRedactionService(opts.LocalRuntime, opts.ExperimentalRedaction)
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
