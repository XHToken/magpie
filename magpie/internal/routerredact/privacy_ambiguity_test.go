package routerredact

import (
	"context"
	"testing"
)

func TestPrivacyRecoveryRejectsOverlappingExactMatches(t *testing.T) {
	spec := privacyParseSpec(t)
	for _, tc := range []struct{ name, source, value string }{
		{"chinese", "田田田", "田田"},
		{"non_bmp", "🙂🙂🙂", "🙂🙂"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := Extraction{Versions: spec.Versions(), Texts: []string{tc.source}}
			content := privacyParseContent(privacyParseField(0, privacyParseFinding("private_person", tc.value, 0, 1)))
			result, err := spec.Parse(context.Background(), input, privacyParseEnvelope(content, "stop"))
			if err == nil || PrivacyParseDiagnosticCode(err) != "span_text" || result.Complete || len(result.Entities) != 0 {
				t.Fatalf("ambiguous overlapping matches were accepted: complete=%v entities=%d error=%v", result.Complete, len(result.Entities), err)
			}
			// Exact supplied coordinates disambiguate the same repeated source.
			content = privacyParseContent(privacyParseField(0, privacyParseFinding("private_person", tc.value, 1, 3)))
			result, err = spec.Parse(context.Background(), input, privacyParseEnvelope(content, "stop"))
			if err != nil || !result.Complete || len(result.Entities) != 1 || result.Entities[0].End != len(tc.source) {
				t.Fatalf("exact repeated-source coordinates were rejected: %v", err)
			}
		})
	}
}
