package routerobserve

import (
	"context"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/cloudcodec"
	"github.com/yetone/magpie/internal/routercloud"
	"github.com/yetone/magpie/internal/routerledger"
	"github.com/yetone/magpie/internal/routerruntime"
)

func TestLocalResultRetainsFinalX26Execution(t *testing.T) {
	for _, finalModel := range []string{"spark-x2.6-4B", "spark-x2.6-4B-Q8_0"} {
		for _, reportedUsage := range []bool{true, false} {
			name := finalModel + "/unknown-usage"
			if reportedUsage {
				name = finalModel + "/reported-usage"
			}
			t.Run(name, func(t *testing.T) {
				s, path := ledger(t)
				watch := begin(t, s, "x26-final-execution")
				calls := NewCalls(s, 32)
				defer calls.Close()
				observer := LocalObserver(WithLocalObserver(context.Background(), calls, "x26-final-execution", false, watch))
				started := time.Now().UTC().Add(-time.Minute)
				previousModel := "spark-x2.6-4B-Q8_0"
				if previousModel == finalModel {
					previousModel = "spark-x2.6-4B"
				}
				previous := routerruntime.LocalCallObservation{
					CallID: "earlier-execution", Purpose: routerruntime.LocalCallExecution,
					Mode: routerruntime.LocalCallFormal, ModelID: previousModel,
					StartedAt: started, EndedAt: started.Add(time.Second),
					Outcome: routerruntime.LocalCallFailed, ErrorCode: "local_generation_failed", Attempted: true,
					Usage: &cloudcodec.ReportedUsage{InputTokens: number(99), OutputTokens: number(1)},
				}
				observer.ObserveLocalCall(previous)
				final := previous
				final.CallID, final.ModelID = "final-execution", finalModel
				final.StartedAt, final.EndedAt = started.Add(2*time.Second), started.Add(3*time.Second)
				final.Outcome, final.ErrorCode, final.Usage = routerruntime.LocalCallSucceeded, "", nil
				if reportedUsage {
					final.Usage = &cloudcodec.ReportedUsage{InputTokens: number(7), OutputTokens: number(2), TotalTokens: number(9)}
				}
				observer.ObserveLocalCall(final)
				observer.ObserveLocalCall(previous) // A delayed duplicate cannot replace the final result.
				if err := watch.Finish(routercloud.Result{Destination: "local"}, nil); err != nil {
					t.Fatal(err)
				}
				if err := calls.Flush(context.Background()); err != nil {
					t.Fatal(err)
				}
				record := read(t, s, "x26-final-execution")
				if record.LocalModel != finalModel || record.LocalCallID != final.CallID {
					t.Fatalf("final local model/call lost: model=%q call=%q, want %q/%q", record.LocalModel, record.LocalCallID, finalModel, final.CallID)
				}
				if reportedUsage {
					if record.Usage == nil || record.Usage.InputTokens == nil || *record.Usage.InputTokens != 7 || record.Usage.TotalTokens == nil || *record.Usage.TotalTokens != 9 {
						t.Fatalf("final execution usage lost: %+v", record.Usage)
					}
				} else if record.Usage != nil {
					t.Fatalf("earlier execution usage leaked into final result: %+v", record.Usage)
				}
				summary, err := s.Summary(routerledger.Query{})
				if err != nil {
					t.Fatal(err)
				}
				for _, group := range summary.InternalCalls {
					if group.Purpose == "execution" && (group.Total != 2 || group.BusinessUsageCalls != 1 || group.OverheadUsageCalls != 1) {
						t.Fatalf("final execution usage ownership lost: %+v", group)
					}
				}
				if err := s.Close(); err != nil {
					t.Fatal(err)
				}
				reopened, err := routerledger.Open(path, routerledger.Options{})
				if err != nil {
					t.Fatal(err)
				}
				defer reopened.Close()
				if persisted := read(t, reopened, "x26-final-execution"); persisted.LocalModel != finalModel || persisted.LocalCallID != final.CallID {
					t.Fatalf("persisted X2.6 identity changed: %+v", persisted)
				}
			})
		}
	}
}
