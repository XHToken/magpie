package routerledger

import (
	"path/filepath"
	"testing"
	"time"
)

func TestRedactionObservationStoreRecordQueryAndClear(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenRedactionObservations(filepath.Join(dir, "redaction_observations.sqlite"))
	if err != nil {
		t.Fatalf("OpenRedactionObservations: %v", err)
	}
	defer store.Close()

	base := time.Now().UTC().Add(-10 * time.Minute)
	if err := store.Record(RedactionObservation{
		RequestID:     "req_1",
		AppID:         "workbuddy",
		AppName:       "WorkBuddy",
		Destination:   "cloud",
		Model:         "deepseek-chat",
		StartedAt:     base.Format(time.RFC3339Nano),
		DurationMS:    18,
		CacheHit:      false,
		Hit:           false,
		EntityCount:   0,
		RawInput:      "帮我写一段快速排序代码",
		RedactedInput: "帮我写一段快速排序代码",
	}); err != nil {
		t.Fatalf("Record 1: %v", err)
	}

	if err := store.Record(RedactionObservation{
		RequestID:     "req_2",
		AppID:         "loomy",
		AppName:       "Loomy",
		Destination:   "cloud",
		Model:         "qwen-plus",
		StartedAt:     base.Add(time.Minute).Format(time.RFC3339Nano),
		DurationMS:    42,
		CacheHit:      false,
		Hit:           true,
		EntityCount:   2,
		Categories:    []string{"NAME", "PHONE"},
		RawInput:      "请联系张三，手机号是13800138000",
		RedactedInput: "请联系<NAME:1>，手机号是<PHONE:1>",
		Replacements: []RedactionReplacementItem{
			{Placeholder: "<NAME:1>", Category: "NAME", Raw: "张三", Count: 1},
			{Placeholder: "<PHONE:1>", Category: "PHONE", Raw: "13800138000", Count: 1},
		},
	}); err != nil {
		t.Fatalf("Record 2: %v", err)
	}

	all, err := store.Query(RedactionObservationFilter{Limit: 10})
	if err != nil {
		t.Fatalf("Query all: %v", err)
	}
	if all.Summary.Total != 2 || all.Summary.Hits != 1 || all.Summary.Passthrough != 1 || all.Summary.TotalEntities != 2 {
		t.Fatalf("unexpected summary: %+v", all.Summary)
	}
	if len(all.Observations) != 2 || all.Observations[0].RequestID != "req_2" || all.Observations[1].RequestID != "req_1" {
		t.Fatalf("unexpected observation order: %+v", all.Observations)
	}
	if len(all.Observations[0].Replacements) != 2 || all.Observations[0].Replacements[0].Placeholder != "<NAME:1>" {
		t.Fatalf("unexpected replacements: %+v", all.Observations[0].Replacements)
	}

	hitsOnly, err := store.Query(RedactionObservationFilter{HitOnly: true, Limit: 10})
	if err != nil {
		t.Fatalf("Query HitOnly: %v", err)
	}
	if len(hitsOnly.Observations) != 1 || hitsOnly.Observations[0].RequestID != "req_2" {
		t.Fatalf("expected only req_2 for HitOnly, got %+v", hitsOnly.Observations)
	}

	searchRes, err := store.Query(RedactionObservationFilter{Query: "13800138000", Limit: 10})
	if err != nil {
		t.Fatalf("Query search: %v", err)
	}
	if len(searchRes.Observations) != 1 || searchRes.Observations[0].RequestID != "req_2" {
		t.Fatalf("expected req_2 for keyword search, got %+v", searchRes.Observations)
	}

	if err := store.Clear(); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	afterClear, err := store.Query(RedactionObservationFilter{Limit: 10})
	if err != nil {
		t.Fatalf("Query after clear: %v", err)
	}
	if afterClear.Summary.Total != 0 || len(afterClear.Observations) != 0 {
		t.Fatalf("expected empty store after Clear, got %+v", afterClear)
	}
}
