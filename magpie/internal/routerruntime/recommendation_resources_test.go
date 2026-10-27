package routerruntime

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/yetone/magpie/internal/dirs"
	"github.com/yetone/magpie/internal/routermanifest"
)

func TestInstallationPlanCreditsOnlyVerifiedFilesAndDeduplicatesDependencies(t *testing.T) {
	ctx := context.Background()
	layout := dirs.Layout{Models: filepath.Join(t.TempDir(), "models"), Runtime: filepath.Join(t.TempDir(), "runtime")}
	catalog := routermanifest.Fixed()
	model := &catalog.Models[0]
	weight, template := []byte("fixture-weight"), []byte("fixture-template")
	model.Artifact = artifactFor("weight", "model.gguf", weight)
	model.Template = artifactFor("template", "template.jinja", template)
	model.InstallationBytes = 8000
	rt := catalog.Runtimes[0]
	rt.ID, rt.Platform, rt.Architecture, rt.Backend = "fixture-cpu", "windows", "amd64", "cpu"
	rt.Artifact = artifactFor("engine", "engine.zip", []byte("archive"))
	rt.Artifact.ExpandedBytes = 100
	dep := &routermanifest.Dependencies{Artifact: artifactFor("shared", "shared.zip", []byte("dependency"))}
	dep.Artifact.ExpandedBytes = 200
	rt.Dependencies = dep
	gpu := rt
	gpu.ID, gpu.Backend = "fixture-gpu", "vulkan"
	gpu.Artifact = artifactFor("gpu", "gpu.zip", []byte("gpu archive"))
	gpu.Artifact.ExpandedBytes = 300
	catalog.Runtimes = []routermanifest.Runtime{rt, gpu}
	f := &FileInstaller{Layout: layout}
	m := &Manager{o: Options{Catalog: catalog, RuntimeID: rt.ID, Installer: f, AutoBackend: true, BackendHints: func(context.Context) []string { return []string{"vulkan"} }}}
	plan := m.InstallationPlan(ctx, model.ID)
	expected := uint64(2*100 + 2*300 + 3*200 + rt.Artifact.Size + gpu.Artifact.Size + dep.Artifact.Size + (64 << 20))
	if plan.ModelBytes != 8000 || plan.RuntimeBytes != expected {
		t.Fatal(plan, expected)
	}
	modelPath, templatePath, archivePath, _ := f.paths(*model, rt)
	for target, data := range map[string][]byte{modelPath: weight, templatePath: template, archivePath: []byte("archive")} {
		if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	cached := m.InstallationPlan(ctx, model.ID)
	if cached.ModelBytes != 0 || cached.RuntimeBytes != expected-uint64(rt.Artifact.Size) {
		t.Fatal("exact cache was not credited", cached)
	}
	if err := os.WriteFile(modelPath, []byte("invalid-weight"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := m.InstallationPlan(ctx, model.ID); got.ModelBytes != 8000 {
		t.Fatal("corrupt cache was credited", got)
	}
}
