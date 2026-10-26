package routerledger

import (
	"path/filepath"
	"testing"
	"time"
)

func TestRedactionObservationUpdateAtCapacityRetainsOtherRequests(t *testing.T) {
	store, err := OpenRedactionObservations(filepath.Join(t.TempDir(), "observations.sqlite"), RedactionObservationOptions{MaxEntries: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	base := time.Now().UTC().Add(-time.Minute)
	for _, rec := range []RedactionObservation{
		{RequestID: "older", AppID: "codex", StartedAt: base.Format(time.RFC3339Nano)},
		{RequestID: "newer", AppID: "codex", StartedAt: base.Add(time.Second).Format(time.RFC3339Nano)},
		{RequestID: "newer", AppID: "codex", StartedAt: base.Add(2 * time.Second).Format(time.RFC3339Nano), Model: "fallback"},
	} {
		if err := store.Record(rec); err != nil {
			t.Fatal(err)
		}
	}
	result, err := store.Query(RedactionObservationFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Summary.Total != 2 || len(result.Observations) != 2 || result.Observations[0].Model != "fallback" || result.Observations[1].RequestID != "older" {
		t.Fatalf("updating an existing request evicted another request: %+v", result)
	}
}
