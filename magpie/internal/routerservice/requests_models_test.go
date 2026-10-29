package routerservice

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/routerledger"
)

func TestRequestListShowsObservedLocalModelsBeforeCloudDispatch(t *testing.T) {
	h, ledger, _, _ := ledgerHTTPFixture(t, ModelOptions{})
	id, other := "11111111111111111111111111111111", "22222222222222222222222222222222"
	for _, requestID := range []string{id, other} {
		if err := ledger.Start(routerledger.Metadata{RequestID: requestID, AppID: "loomy", AppName: "Loomy", Operation: "generate", Path: "/v1/chat/completions"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := ledger.Finish(id, routerledger.Outcome{Status: "failed", Destination: "none", SafeErrorCode: "redaction_unavailable", FailureStage: "preflight", Decision: "cloud"}); err != nil {
		t.Fatal(err)
	}
	// Calls may arrive after the parent finishes, as in the real redaction
	// timeout. Q4/Q8 identities come from each call, never the current setting.
	for i, item := range []struct {
		requestID, purpose, model string
		started                   bool
	}{
		{id, "redaction", "spark-x2.6-4B", true},
		{id, "redaction", "spark-x2.6-4B", true},
		{id, "judgment", "spark-x2.6-4B-Q8_0", true},
		{id, "execution", "not-executed", false},
		{other, "execution", "other-request-model", true},
		{"", "redaction", "management-check-model", true},
	} {
		now := time.Now().UTC()
		call := routerledger.Call{RequestID: item.requestID, CallID: string(rune('a' + i)), Scope: "business", Purpose: item.purpose, ModelID: item.model, Mode: "experimental", StartedAt: now, EndedAt: now, Result: "canceled", SafeErrorCode: "budget_exhausted", InferenceStarted: item.started}
		if item.requestID == "" {
			call.Scope = "check"
		}
		if err := ledger.RecordCall(call); err != nil {
			t.Fatal(err)
		}
	}
	response := providerRequest(t, h, "GET", "/api/requests?status=failed&limit=1", cloudHTTPManagement, nil)
	var page struct {
		Requests []struct {
			routerledger.Record
			LocalModels []routerledger.ObservedModel `json:"localModels"`
		} `json:"requests"`
	}
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &page) != nil || len(page.Requests) != 1 {
		t.Fatalf("list %d %s", response.Code, response.Body.String())
	}
	record := page.Requests[0]
	want := []routerledger.ObservedModel{{Purpose: "redaction", ModelID: "spark-x2.6-4B"}, {Purpose: "judgment", ModelID: "spark-x2.6-4B-Q8_0"}}
	if !reflect.DeepEqual(record.LocalModels, want) {
		t.Fatalf("observed models %+v, want %+v", record.LocalModels, want)
	}
	if record.RequestID != id || record.LocalModel != "" || record.Model != "" || record.Destination != "none" || record.Attempts != 0 || record.Usage != nil {
		t.Fatalf("internal models changed the generation outcome: %+v", record)
	}
	processing := providerRequest(t, h, "GET", "/api/requests?status=processing", cloudHTTPManagement, nil)
	if processing.Code != 200 || json.Unmarshal(processing.Body.Bytes(), &page) != nil || len(page.Requests) != 1 || len(page.Requests[0].LocalModels) != 1 || page.Requests[0].LocalModels[0].ModelID != "other-request-model" {
		t.Fatalf("processing call model missing: %d %s", processing.Code, processing.Body.String())
	}
}
