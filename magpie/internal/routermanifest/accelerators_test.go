package routermanifest

import "testing"

func TestAcceleratorCatalogPinsIndependentPayloadsAndAppleFallback(t *testing.T) {
	c := Fixed()
	for _, id := range []string{"windows-amd64-cuda12", "windows-amd64-vulkan", "darwin-arm64-metal", "darwin-arm64-cpu", "darwin-amd64-cpu"} {
		r, ok := c.Runtime(id)
		if !ok {
			t.Fatal("missing platform backend", id)
		}
		if err := r.Artifact.Validate(); err != nil {
			t.Fatal(err)
		}
		if r.Dependencies != nil {
			if err := r.Dependencies.Validate(); err != nil {
				t.Fatal(err)
			}
		}
		if r.AcceleratorDependencies != nil {
			if err := r.AcceleratorDependencies.Validate(); err != nil {
				t.Fatal(err)
			}
		}
		if RuntimeInstallationBytes(r) <= r.Artifact.ExpandedBytes {
			t.Fatal("incomplete peak reservation", r)
		}
	}
	cuda, _ := c.Runtime("windows-amd64-cuda12")
	if cuda.Artifact.ExpandedBytes <= 594068253 || cuda.AcceleratorDependencies.Artifact.ExpandedBytes <= 574138880 || len(cuda.AcceleratorDependencies.Files) != 3 {
		t.Fatal("GPU archive expansion/overlay not accounted for", cuda)
	}
	before := c.Models[0].Fingerprint(cuda)
	changed := cuda
	copy := *cuda.AcceleratorDependencies
	changed.AcceleratorDependencies = &copy
	copy.Version = "12.4.1.0"
	if before == c.Models[0].Fingerprint(changed) {
		t.Fatal("GPU dependency set not bound to fingerprint")
	}
	metal, _ := c.Runtime("darwin-arm64-metal")
	cpu, _ := c.Runtime("darwin-arm64-cpu")
	if metal.Artifact != cpu.Artifact || metal.ID == cpu.ID || metal.Backend == cpu.Backend {
		t.Fatal("Apple fallback must reuse bytes, not backend identity")
	}
	// Observed expansion of the exact pinned official Intel archive, including
	// regular copies of all 16 dylib aliases (not just tar payload sizes).
	intel, _ := c.Runtime("darwin-amd64-cpu")
	if intel.Artifact.ExpandedBytes < 57260670 {
		t.Fatal("Intel archive alias copies exceed installation budget")
	}
}
