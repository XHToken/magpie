package routerredact

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestOrdinaryPrivacyBatchesShareThirtySecondBudget(t *testing.T) {
	spec := privacyTestSpecification(t)
	var deadlines []time.Time
	runtime := &privacyTestRuntime{fingerprint: spec.RuntimeFingerprint(), suite: spec.CapabilitySuite()}
	runtime.classify = func(ctx context.Context, _ int, fields []privacyTestField) ([]byte, error) {
		deadline, ok := ctx.Deadline()
		if !ok {
			t.Fatal("local extraction has no deadline")
		}
		deadlines = append(deadlines, deadline)
		return privacyTestEnvelope(fields, map[string]string{"Alice": "private_person"}), nil
	}
	texts := make([]string, 17)
	for i := range texts {
		texts[i] = "Alice"
	}
	for _, throughPipeline := range []bool{false, true} {
		name := "direct"
		if throughPipeline {
			name = "pipeline"
		}
		t.Run(name, func(t *testing.T) {
			deadlines = nil
			extractor := NewRuntimeExtractor(runtime, spec)
			if throughPipeline {
				pipeline := New(Options{Extractor: extractor})
				defer pipeline.Close()
				in := testInput(texts...)
				in.Versions, in.SessionVerified = spec.Versions(), false
				prepared := mustPrepare(t, pipeline, in)
				prepared.Close()
			} else {
				result, err := extractor.Extract(context.Background(), Extraction{Versions: spec.Versions(), Texts: texts})
				if err != nil || !result.Complete || len(result.Entities) != len(texts) {
					t.Fatal("complete batch extraction failed", err)
				}
			}
			if len(deadlines) != 2 || !deadlines[0].Equal(deadlines[1]) {
				t.Fatal("batches renewed their shared extraction deadline", deadlines)
			}
			// No waiting: distinguish the old 15-second limit with ample load margin.
			if remaining := time.Until(deadlines[0]); remaining <= 20*time.Second || remaining > 30*time.Second {
				t.Fatal("ordinary extraction did not receive the 30-second budget", remaining)
			}
		})
	}
}

func TestPrivacyPolicyRetiresPriorAcceptanceIdentity(t *testing.T) {
	const oldConfig = "2b3b8a2b1ae307d29f71d2909ff933f642c2559d1e6066376ee4e96f99ad36cb"
	for _, trial := range []bool{false, true} {
		binding := ModelBinding{ModelID: "spark-x2.5-1.7B", RuntimeID: "windows-amd64-cpu"}
		spec, err := newPrivacySpecification(binding, trial)
		if err != nil {
			t.Fatal(err)
		}
		prefix := "privacy-go-v6:"
		if trial {
			prefix = "privacy-trial-v2:"
		}
		if spec.Budget() != 30*time.Second || !strings.HasPrefix(spec.CapabilitySuite(), prefix) || spec.Fingerprints().Config == oldConfig {
			t.Fatal("new budget/coordinate policy inherited its previous acceptance identity", spec.Fingerprints())
		}
		stale := spec.Versions()
		stale.Config = oldConfig
		in := Extraction{Versions: stale, Texts: []string{"Alice"}}
		raw := privacyParseEnvelope(privacyParseContent(privacyParseField(0, privacyParseFinding("private_person", "Alice", 0, 5))), "stop")
		if result, err := spec.Parse(context.Background(), in, raw); err == nil || result.Complete {
			t.Fatal("new parser accepted a request bound to the old policy")
		}
	}
}
