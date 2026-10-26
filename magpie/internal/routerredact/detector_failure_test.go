package routerredact

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestPipelineRejectsIncompleteRuleScan(t *testing.T) {
	for _, tc := range []struct {
		name, pattern, prefix string
	}{
		{"initial_search", `(a+)+$`, ""},
		{"continuation_search", `synthetic-secret|(a+)+$`, "synthetic-secret "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rules := NewRuleDetector([]RuleDefinition{{Name: "bounded-secret", Label: "SECRET", Pattern: tc.pattern}})
			if len(rules.rules) != 1 {
				t.Fatal("fixture rule did not compile")
			}
			// The exponential non-match necessarily exceeds this matcher budget;
			// the test makes no assumption about the runner's wall-clock speed.
			rules.rules[0].re.MatchTimeout = time.Nanosecond
			orch := NewOrchestrator(rules, NewDictionaryDetector(nil), nil)
			pipeline := New(Options{Extractor: orch})
			defer pipeline.Close()
			input := testInput(tc.prefix + strings.Repeat("a", 128) + "!")
			prepared, err := pipeline.Prepare(context.Background(), input)
			if prepared != nil {
				prepared.Close()
			}
			if err == nil {
				t.Fatal("incomplete rule scan produced a cloud-dispatchable request")
			}
			if err.Error() != "redaction_extraction_failed" {
				t.Fatalf("rule scan returned unsafe/unexpected error: %v", err)
			}
			if got := pipeline.Stats().Blocked; got != 1 {
				t.Fatalf("blocked requests = %d, want 1", got)
			}
		})
	}
}
