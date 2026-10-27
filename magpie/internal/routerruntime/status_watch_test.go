package routerruntime

import (
	"context"
	"testing"
)

func TestWatchStatusSnapshotAndClose(t *testing.T) {
	m, _, _, _ := managerFixture(t, func(o *Options, _ *fixtureInstaller, _ *fixtureFactory) {
		o.Store = &memoryIntent{i: Intent{Selected: "spark-x2.6-4B"}}
	})
	before, changed := m.WatchStatus()
	if err := m.Select(context.Background(), "spark-x2.6-4B-Q8_0"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-changed:
	default:
		t.Fatal("selection did not notify the status observer")
	}
	after, next := m.WatchStatus()
	if before.Intent.Selected != "spark-x2.6-4B" || after.Intent.Selected != "spark-x2.6-4B-Q8_0" || next == changed {
		t.Fatal("snapshot and change signal did not advance together")
	}
	if err := m.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-next:
	default:
		t.Fatal("close did not wake the status observer")
	}
	if _, signal := m.WatchStatus(); signal != nil {
		t.Fatal("closed Manager kept the observer waiting")
	}
}
