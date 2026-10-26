package routermanifest

// Accelerator artifacts are pinned to the same engine commit as CPU. Original
// archives were independently downloaded/hashed; expansion limits include margin.
func acceleratedRuntimes() []Runtime {
	const commit = "3ad1ba7336986d98592d3e28cafd1a406715351f"
	var result []Runtime
	for _, v := range []struct {
		id, platform, arch, backend, file, hash, executable string
		size, expanded                                      int64
	}{
		{"windows-amd64-cuda12", "windows", "amd64", "cuda", "llama-b10828-bin-win-cuda-12.4-x64.zip", "1759c6ba0cd20a7ae5e723058d30980fa103df909b07f6afad9fcbf3d1fa70f1", "llama-server.exe", 254172427, 640 << 20},
		{"windows-amd64-vulkan", "windows", "amd64", "vulkan", "llama-b10828-bin-win-vulkan-x64.zip", "c8e8252564713b26ca55902fe1e3dd24d0dd5b8009726f96b773a9f3533d8ec7", "llama-server.exe", 35229658, 128 << 20},
		// Apple CPU fallback reuses the verified universal-backend archive with
		// explicit device=none and GPU layers=0, without copying model weights.
		{"darwin-arm64-cpu", "darwin", "arm64", "cpu", "llama-b10828-bin-macos-arm64.tar.gz", "a9748e059515dfe853940f7cfc0ac7ac7af33901c773a64569b9435e27b0f6cb", "llama-server", 11133769, 32 << 20},
		{"darwin-amd64-cpu", "darwin", "amd64", "cpu", "llama-b10828-bin-macos-x64.tar.gz", "61605c150e82f4a7e6ac0207b1e58dcd90ee74b29b6cc7b929d926283eaed856", "llama-server", 11188497, 32 << 20},
	} {
		format := "tar.gz"
		if v.platform == "windows" {
			format = "zip"
		}
		r := Runtime{ID: v.id, Version: "b10828", Commit: commit, Platform: v.platform, Architecture: v.arch, Backend: v.backend, Executable: v.executable, License: "MIT", Validation: "artifact_bytes_verified_native_acceleration_pending",
			Artifact: Artifact{ID: v.id + "-b10828", FileName: v.file, URL: "https://github.com/ggml-org/llama.cpp/releases/download/b10828/" + v.file, SHA256: v.hash, Size: v.size, Format: format, ExpandedBytes: v.expanded}}
		if v.platform == "windows" {
			r.Dependencies = windowsDependencies()
		}
		if v.backend == "cuda" {
			r.AcceleratorDependencies = cuda12Dependencies()
		}
		if v.id == "darwin-arm64-cpu" {
			// Preserve the existing installation/receipt so an installed Metal
			// runtime can supply CPU fallback without a separate download.
			r.Artifact.ID = "darwin-arm64-metal-b10828"
			r.Artifact.ExpandedBytes = 256 << 20
		}
		result = append(result, r)
	}
	return result
}

func cuda12Dependencies() *Dependencies {
	return &Dependencies{Family: "cuda", ID: "cuda12-x64-b10828", Version: "12.4.0.0", Architecture: "amd64",
		LicenseURL: "https://docs.nvidia.com/cuda/eula/index.html",
		Artifact:   Artifact{ID: "cudart-win-cuda-12.4-b10828", FileName: "cudart-llama-bin-win-cuda-12.4-x64.zip", URL: "https://github.com/ggml-org/llama.cpp/releases/download/b10828/cudart-llama-bin-win-cuda-12.4-x64.zip", SHA256: "8c79a9b226de4b3cacfd1f83d24f962d0773be79f1e7b75c6af4ded7e32ae1d6", Size: 391443627, Format: "zip", ExpandedBytes: 576 << 20},
		Files: []DependencyFile{
			{Source: "cublas64_12.dll", Name: "cublas64_12.dll", Size: 100033536, SHA256: "e40202fe4223c1cd2d2dce7beec59e1ed61c7801bd827309183be9b50e358f4c", Version: "6.14.11.1245"},
			{Source: "cublasLt64_12.dll", Name: "cublasLt64_12.dll", Size: 473551360, SHA256: "2a896460bef60ed57ef32b0875812f355a6984e671d638bb632f5e8c1d7a831f", Version: "6.14.11.1245"},
			{Source: "cudart64_12.dll", Name: "cudart64_12.dll", Size: 553984, SHA256: "d28e42265da7462162a54da6b7a99ea4fa2caf8139d862bb500db875d0b32dfc", Version: "6.14.11.12040"},
		}}
}

// RuntimeInstallationBytes estimates peak runtime-only space, including cached
// downloads, extracted sources and atomic execution assembly. Models are added
// separately; observations of free VRAM do not change installation identity.
func RuntimeInstallationBytes(r Runtime) int64 {
	peak := r.Artifact.Size + 2*r.Artifact.ExpandedBytes
	for _, d := range []*Dependencies{r.Dependencies, r.AcceleratorDependencies} {
		if d != nil {
			peak += d.Artifact.Size + 3*d.Artifact.ExpandedBytes
		}
	}
	return peak + (64 << 20)
}
