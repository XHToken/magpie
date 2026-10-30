package routerledger

import (
	"testing"
	"time"
)

func TestSuccessfulOutcomeClearsTerminalError(t *testing.T) {
	s, path := openTest(t, Options{})
	m := metadata("successful-failover")
	if err := s.Start(m); err != nil {
		t.Fatal(err)
	}
	outcome := success()
	outcome.SafeErrorCode, outcome.FailureStage = "cloud_response_error", "cloud"
	outcome.CapabilityState = "judgment_failed"
	outcome.Attempts = 2
	outcome.AttemptDetails = []Attempt{
		{Number: 1, StartedAt: time.Now(), HTTPStatus: 200, ErrorCode: "cloud_response_error", Retried: true},
		{Number: 2, StartedAt: time.Now(), HTTPStatus: 200, OutputCommitted: true},
	}
	if err := s.Finish(m.RequestID, outcome); err != nil {
		t.Fatal(err)
	}
	check := func(s *Store) {
		t.Helper()
		r := get(t, s, m.RequestID)
		if r.Status != "succeeded" || r.SafeErrorCode != "" || r.FailureStage != "" {
			t.Fatalf("successful request retained a terminal error: %+v", r)
		}
		if r.AttemptDetails[0].ErrorCode != "cloud_response_error" || r.CapabilityState != "judgment_failed" {
			t.Fatalf("attempt diagnostics or local routing reason lost: %+v", r)
		}
	}
	check(s)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	check(reopened)
}
