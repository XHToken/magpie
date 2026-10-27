package routerruntime

import (
	"context"
	"errors"
	"testing"

	"github.com/yetone/magpie/internal/routerprocess"
)

func TestColdAdmissionRefusalDoesNotStartAChild(t *testing.T) {
	m, _, factory, _ := managerFixture(t, func(o *Options, _ *fixtureInstaller, _ *fixtureFactory) {
		o.BeforeStart = func(context.Context, string, string) error { return errors.New("device_check_not_passed") }
	})
	if err := m.Load(context.Background()); err == nil || factory.count.Load() != 0 {
		t.Fatal("insufficient cold-load resources launched a process", err)
	}
}

func TestGPUFailureRechecksCPUAdmission(t *testing.T) {
	i := &backendInstaller{installed: map[string]bool{"windows-amd64-cpu": true, "fixture-cuda": true}}
	f := &backendFactory{failed: map[string]bool{"cuda": true}, devices: map[string][]routerprocess.Device{"cuda": {{ID: "CUDA0", Name: "GPU", TotalBytes: 12 << 30, FreeBytes: 11 << 30}}}}
	m := autoBackendFixture(t, f, i, "cuda")
	var admissions []string
	m.o.BeforeStart = func(_ context.Context, _ string, backend string) error {
		admissions = append(admissions, backend)
		if backend == "cpu" {
			return errors.New("device_check_not_passed")
		}
		return nil
	}
	if err := m.Load(context.Background()); err == nil {
		t.Fatal("GPU failure bypassed CPU resource admission")
	}
	if len(admissions) != 2 || admissions[0] != "cuda" || admissions[1] != "cpu" || len(f.started) != 1 || f.started[0].Backend != "cuda" {
		t.Fatal("unsafe CPU fallback was launched", admissions, f.started)
	}
}
