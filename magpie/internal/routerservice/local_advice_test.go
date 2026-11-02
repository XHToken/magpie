package routerservice

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/routerconfig"
	"github.com/yetone/magpie/internal/routerdevice"
	"github.com/yetone/magpie/internal/routermanifest"
	"github.com/yetone/magpie/internal/routerruntime"
)

func TestRecommendationDiskPlansUseRealVolumesWithoutPooling(t *testing.T) {
	first := routerdevice.Snapshot{DiskVolume: routerdevice.Known("one"), DiskFreeBytes: routerdevice.Known(20 * routerdevice.GiB)}
	second := routerdevice.Snapshot{DiskVolume: routerdevice.Known("two"), DiskFreeBytes: routerdevice.Known(4 * routerdevice.GiB)}
	plan := routerruntime.InstallationFootprint{ModelBytes: 8 * routerdevice.GiB, RuntimeBytes: 3 * routerdevice.GiB}
	budgets := modelDiskBudgets("/models", "/runtime", first, second, plan)
	if len(budgets) != 2 || budgets[0].Required != 10*routerdevice.GiB || budgets[1].Required != 5*routerdevice.GiB || *budgets[1].Available.Value >= budgets[1].Required {
		t.Fatal(budgets)
	}
	second.DiskVolume = routerdevice.Known("one")
	budgets = modelDiskBudgets("/models", "/runtime", first, second, plan)
	if len(budgets) != 1 || budgets[0].Required != 13*routerdevice.GiB || *budgets[0].Available.Value != 4*routerdevice.GiB {
		t.Fatal("same-volume components were not combined conservatively", budgets)
	}
}

func TestRecommendationSamplesMinimumAndNeverLoads(t *testing.T) {
	layout, _ := fixture(t)
	store := routerconfig.New(layout)
	installer, factory := &managedFixtureInstaller{}, &managedFixtureFactory{}
	catalog := routermanifest.Fixed()
	manager, err := routerruntime.New(context.Background(), routerruntime.Options{Catalog: catalog, RuntimeID: "windows-amd64-cpu", Store: routerruntime.ConfigIntentStore{Store: store}, Installer: installer, Factory: factory})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close(context.Background())
	calls := 0
	detector := routerdevice.New(routerdevice.ProviderFunc(func(context.Context, string) (routerdevice.Snapshot, error) {
		calls++
		available := uint64(12) * routerdevice.GiB
		if calls == 3 {
			available = 7 * routerdevice.GiB
		}
		return routerdevice.Snapshot{System: routerdevice.Known(routerdevice.System{OS: "windows", Version: routerdevice.Known("10.0.26200"), Architecture: routerdevice.Known("amd64"), LogicalCPUs: routerdevice.Known(8)}), MemoryBytes: routerdevice.Known(uint64(16) * routerdevice.GiB), AvailableMemoryBytes: routerdevice.Known(available), DiskFreeBytes: routerdevice.Known(uint64(100) * routerdevice.GiB), Backends: routerdevice.Known([]routerdevice.Backend{{Kind: routerdevice.CPU, State: routerdevice.Available}})}, nil
	}))
	local := &LocalModels{Manager: manager, Detector: detector, Layout: layout, Catalog: catalog}
	report := local.detect(context.Background(), layout.Models)
	if report.Advice == nil || report.Advice.State != "recommended" || report.Advice.ModelID != "spark-x2.6-1.7B-Q8_0" || report.Recommendation.Tier != routerdevice.Tier1_7B || report.Device.AvailableMemoryBytes.Value == nil || *report.Device.AvailableMemoryBytes.Value != 7*routerdevice.GiB || calls != 6 {
		t.Fatal("transient free-memory spike was used for admission", report, calls)
	}
	if manager.Status().Intent.Enabled || installer.calls.Load() != 0 || factory.calls.Load() != 0 {
		t.Fatal("resource detection enabled, installed or loaded a model")
	}
}

func TestSpark17ServiceAdviceAndManualLoadUseSmallModelBudgets(t *testing.T) {
	layout, _ := fixture(t)
	catalog := routermanifest.Fixed()
	manager, err := routerruntime.New(context.Background(), routerruntime.Options{Catalog: catalog, RuntimeID: "windows-amd64-cpu", Store: routerruntime.ConfigIntentStore{Store: routerconfig.New(layout)}, Installer: &managedFixtureInstaller{}, Factory: &managedFixtureFactory{}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Close(context.Background()) })
	free := uint64(5 * routerdevice.GiB)
	local := &LocalModels{Manager: manager, Layout: layout, Catalog: catalog, Detector: routerdevice.New(routerdevice.ProviderFunc(func(context.Context, string) (routerdevice.Snapshot, error) {
		return routerdevice.Snapshot{System: routerdevice.Known(routerdevice.System{OS: "windows", Version: routerdevice.Known("10.0.26200"), Architecture: routerdevice.Known("amd64"), LogicalCPUs: routerdevice.Known(4)}), MemoryBytes: routerdevice.Known(uint64(8 * routerdevice.GiB)), AvailableMemoryBytes: routerdevice.Known(free), DiskFreeBytes: routerdevice.Known(uint64(100 * routerdevice.GiB)), Backends: routerdevice.Known([]routerdevice.Backend{{Kind: routerdevice.CPU, State: routerdevice.Available}})}, nil
	}))}
	report := local.detect(context.Background(), layout.Models)
	if report.Advice == nil || report.Advice.ModelID != "spark-x2.6-1.7B-Q8_0" || report.Recommendation.Tier != routerdevice.Tier1_7B || report.Recommendation.MaxTier != routerdevice.Tier1_7B {
		t.Fatal("8 GiB CPU host did not receive the 1.7B recommendation", report)
	}
	for _, id := range []string{"spark-x2.6-1.7B", "spark-x2.6-1.7B-Q8_0"} {
		free = 4 * routerdevice.GiB
		if err := local.admitModel(context.Background(), id, "load", true); err != nil {
			t.Fatal("manual 1.7B load inherited the 4B memory budget", id, err)
		}
		free--
		if err := local.admitModel(context.Background(), id, "load", true); err == nil {
			t.Fatal("known memory shortage permitted loading", id)
		}
	}
}

func TestPerformanceStreamUsesVisibleTokensAndReportedUsage(t *testing.T) {
	start := time.Unix(0, 0)
	input := "data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"hidden\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"First\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"rest\"},\"finish_reason\":\"length\"}],\"usage\":{\"completion_tokens\":96}}\n\n" +
		"data: [DONE]\n\n"
	calls := 0
	sample, err := readPerformanceStream(strings.NewReader(input), start, func() time.Time { calls++; return start.Add(time.Duration(calls) * time.Second) })
	if err != nil || sample.Tokens != 96 || sample.FirstTokenMS != 1000 || sample.TokensPerSecond != 95 {
		t.Fatal(sample, err)
	}
	if _, err := readPerformanceStream(strings.NewReader(strings.ReplaceAll(input, "data: [DONE]", "")), start, time.Now); err == nil {
		t.Fatal("partial generation accepted as complete performance evidence")
	}
}
