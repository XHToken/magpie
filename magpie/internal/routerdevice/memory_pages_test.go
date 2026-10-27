package routerdevice

import (
	"math"
	"testing"
)

func TestDarwinMemoryPagesAndAdvice(t *testing.T) {
	for _, pageSize := range []uint64{4096, 16384} {
		for _, available := range []uint64{0, 8*GiB - pageSize, 8 * GiB, 12 * GiB} {
			got, err := darwinMemoryBytes(available/pageSize/4, available/pageSize-available/pageSize/4, pageSize, 32*GiB)
			if err != nil || got != available {
				t.Fatalf("page size %d: got %d, %v; want %d", pageSize, got, err, available)
			}
			s := fixture("darwin", 32*GiB)
			s.AvailableMemoryBytes = Known(got)
			a := AdviseModels(s, adviceOptions(), RunningModel{})
			want := ""
			if available >= 12*GiB {
				want = "q8"
			} else if available >= 8*GiB {
				want = "q4"
			}
			if a.ModelID != want || (want == "" && a.State != "deferred") {
				t.Fatalf("%d available: advice = %+v", got, a)
			}
		}
	}
}

func TestDarwinMemoryRejectsInvalidObservations(t *testing.T) {
	for _, input := range [][4]uint64{
		{1, 1, 0, 16 * GiB},
		{math.MaxUint64, 1, 4096, 16 * GiB},
		{math.MaxUint64 / 4096, 1, 4096, 16 * GiB},
		{1, 1, 4096, 0},
		{16 * GiB / 4096, 1, 4096, 16 * GiB},
	} {
		if got, err := darwinMemoryBytes(input[0], input[1], input[2], input[3]); err == nil || got != 0 {
			t.Fatalf("invalid observation %v became available memory: %d, %v", input, got, err)
		}
	}
}

func TestDarwin16GiBAdviceWithUnverifiedMetal(t *testing.T) {
	s := fixture("darwin", 16*GiB)
	s.Backends = Known([]Backend{{Kind: CPU, State: Available}, {Kind: Metal, State: UnknownAvailability}})
	s.AvailableMemoryBytes = Unknown[uint64]("probe unavailable")
	if a := AdviseModels(s, adviceOptions(), RunningModel{}); a.State != "deferred" || a.Reason != "available_memory_unknown" {
		t.Fatal("missing memory did not block admission", a)
	}
	available, err := darwinMemoryBytes(2*GiB/16384, 6*GiB/16384, 16384, 16*GiB)
	if err != nil {
		t.Fatal(err)
	}
	s.AvailableMemoryBytes = Known(available)
	a := AdviseModels(s, adviceOptions(), RunningModel{})
	q4, _ := a.Model("q4")
	if a.ModelID != "q4" || q4.Manual != CheckPassed {
		t.Fatal("no-cgo Mac with sufficient memory could not install Q4", a)
	}
}
