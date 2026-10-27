package routerruntime

import (
	"context"
	"errors"
	"testing"
)

func TestPerformanceGatesTrialAuthorizationWithoutChangingIntent(t *testing.T) {
	m, _, _, _ := managerFixture(t, func(o *Options, _ *fixtureInstaller, _ *fixtureFactory) { o.PerformanceRequired = true })
	for _, purpose := range []Purpose{Judgment, Execution} {
		if err := m.CheckCapability(context.Background(), Check{Purpose: purpose, Suite: "fixture", Trial: true, Run: func(context.Context, string) (string, error) { return "evidence", nil }}); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.EnableTrial(context.Background()); !errors.Is(err, ErrCapability) {
		t.Fatal("unmeasured response performance authorized automatic hybrid use", err)
	}
	binding := m.PerformanceBinding()
	if !m.RecordPerformance(binding, []PerformanceSample{{96, 10, 1000}, {96, 10, 1000}}) {
		t.Fatal("sample rejected")
	}
	if err := m.EnableTrial(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := m.Require(Execution); err != nil {
		t.Fatal(err)
	}
	m.RecordPerformance(binding, []PerformanceSample{{96, 4, 1000}, {96, 4, 1000}})
	if err := m.Require(Execution); !errors.Is(err, ErrCapability) || !m.Status().Intent.Trial.Execution || m.Status().Capabilities[Execution].TrialReady {
		t.Fatal("slow measurements failed to stop new automatic use or rewrote user intent", err)
	}
}

func TestPerformanceEvidenceSeparatesSlowPendingAndAcceptable(t *testing.T) {
	for _, tc := range []struct {
		name    string
		samples []PerformanceSample
		state   string
	}{
		{"at boundaries", []PerformanceSample{{32, 5, 15000}, {32, 5, 15000}}, "acceptable"},
		{"slow generation", []PerformanceSample{{96, 4, 1000}, {96, 4, 1000}}, "slow"},
		{"slow first answer", []PerformanceSample{{96, 10, 15001}, {96, 10, 15001}}, "slow"},
		{"too short", []PerformanceSample{{31, 10, 1000}, {96, 10, 1000}}, "unknown"},
		{"one sample", []PerformanceSample{{96, 10, 1000}}, "unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := SummarizePerformance(tc.samples); got.State != tc.state {
				t.Fatal(got)
			}
		})
	}
}

func TestPerformanceEvidenceBindsModelAndRemainsDetached(t *testing.T) {
	m, _, _, _ := managerFixture(t, func(o *Options, _ *fixtureInstaller, _ *fixtureFactory) { o.PerformanceRequired = true })
	if err := m.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	binding := m.PerformanceBinding()
	if !m.RecordPerformance(binding, []PerformanceSample{{96, 10, 1000}, {96, 10, 1000}}) {
		t.Fatal("current sample rejected")
	}
	status := m.Status()
	status.Performance.Samples[0].TokensPerSecond = 1
	if m.Status().Performance.Samples[0].TokensPerSecond != 10 {
		t.Fatal("status exposed mutable performance evidence")
	}
	if err := m.Disable(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := m.Enable(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := m.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	if m.RecordPerformance(binding, []PerformanceSample{{96, 10, 1000}, {96, 10, 1000}}) {
		t.Fatal("a restarted process accepted its predecessor's measurement")
	}
	if m.Status().Performance.State != "unknown" {
		t.Fatal("a restarted process retained its predecessor's speed rating")
	}
	if err := m.Disable(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := m.Select(context.Background(), "spark-x2.6-4B-Q8_0"); err != nil {
		t.Fatal(err)
	}
	if m.RecordPerformance(binding, []PerformanceSample{{96, 10, 1000}, {96, 10, 1000}}) || m.Status().Performance.State != "unknown" {
		t.Fatal("new model reused the previous quantization's speed evidence")
	}
}

func TestPerformanceEvidenceRejectsReusedPIDAfterUnexpectedExit(t *testing.T) {
	m, _, factory, _ := managerFixture(t, nil)
	if err := m.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	before := m.PerformanceBinding()
	if err := factory.last().Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return m.Status().PID == 0 })
	if err := m.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	after := m.PerformanceBinding()
	if before.PID != after.PID || before.Epoch == after.Epoch {
		t.Fatal("fixture did not exercise PID reuse with a new process lifetime", before, after)
	}
	if m.RecordPerformance(before, []PerformanceSample{{96, 10, 1000}, {96, 10, 1000}}) {
		t.Fatal("a reused PID accepted the exited process's evidence")
	}
}
