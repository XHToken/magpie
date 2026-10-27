// Package routermanifest owns the fixed, reviewable model/runtime catalog.
package routermanifest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"path/filepath"
	"strings"
)

const (
	spark26Rev  = "83b47fda018fd14f51aa63341f78266f628af5a5"
	spark26Base = "66e3ae7bee54e0a523d692cb8ef841f35409807b"
)

// Artifact is immutable source metadata. Missing size/hash/URL means blocked.
type Artifact struct {
	ID            string `json:"id"`
	URL           string `json:"url"`
	FileName      string `json:"fileName"`
	SHA256        string `json:"sha256"`
	Size          int64  `json:"size"`
	Format        string `json:"format"`
	ExpandedBytes int64  `json:"expandedBytes"`
}

func (a Artifact) Validate() error {
	u, err := url.Parse(a.URL)
	h, hashErr := hex.DecodeString(a.SHA256)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || hashErr != nil || len(h) != 32 || a.Size <= 0 || a.ID == "" || a.FileName == "" || filepath.Base(a.FileName) != a.FileName || a.FileName == "." || a.FileName == ".." {
		return errors.New("incomplete artifact metadata")
	}
	if a.Format != "file" && a.Format != "zip" && a.Format != "tar.gz" {
		return errors.New("unsupported artifact format")
	}
	if a.Format != "file" && a.ExpandedBytes <= 0 {
		return errors.New("archive expansion budget unknown")
	}
	return nil
}

type Model struct {
	ID                  string   `json:"id"`
	Name                string   `json:"name,omitempty"`
	Tier                string   `json:"tier"`
	Revision            string   `json:"revision"`
	Quantization        string   `json:"quantization"`
	License             string   `json:"license"`
	Purpose             string   `json:"purpose"`
	ContextTokens       int      `json:"contextTokens"` // Explicit serving cap, not native maximum.
	NativeContextTokens int      `json:"nativeContextTokens"`
	Artifact            Artifact `json:"artifact"`
	Template            Artifact `json:"template"`
	TemplateRevision    string   `json:"templateRevision"`
	InstallationBytes   int64    `json:"installationBytes"` // Conservative peak reservation.
}

// DisplayName keeps the published model version separate from its hardware tier.
func (m Model) DisplayName() string {
	if m.Name != "" {
		return m.Name
	}
	if suffix := "-" + m.Quantization + ".gguf"; strings.HasSuffix(m.Artifact.FileName, suffix) {
		return strings.TrimSuffix(m.Artifact.FileName, suffix)
	}
	return m.ID
}

type Runtime struct {
	ID                      string        `json:"id"`
	Version                 string        `json:"version"`
	Commit                  string        `json:"commit"`
	Platform                string        `json:"platform"`
	Architecture            string        `json:"architecture"`
	Backend                 string        `json:"backend"`
	License                 string        `json:"license"`
	Executable              string        `json:"executable"`
	Artifact                Artifact      `json:"artifact"`
	Validation              string        `json:"validation"`
	Dependencies            *Dependencies `json:"dependencies,omitempty"`
	AcceleratorDependencies *Dependencies `json:"acceleratorDependencies,omitempty"`
	// Execution is the server-owned effective plan, empty in the fixed catalog.
	Execution string `json:"execution,omitempty"`
}

func (m Model) Fingerprint(r Runtime) string {
	b, _ := json.Marshal(struct {
		Model   Model
		Runtime Runtime
	}{m, r})
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

type Catalog struct {
	SchemaVersion int       `json:"schemaVersion"`
	Models        []Model   `json:"models"`
	Runtimes      []Runtime `json:"runtimes"`
	VerifiedAt    string    `json:"verifiedAt"`
	Sources       []string  `json:"sources"`
}

func (c Catalog) Model(id string) (Model, bool) {
	for _, m := range c.Models {
		if m.ID == id {
			return m, true
		}
	}
	return Model{}, false
}
func (c Catalog) Runtime(id string) (Runtime, bool) {
	for _, r := range c.Runtimes {
		if r.ID == id {
			return r, true
		}
	}
	return Runtime{}, false
}

// Fixed returns a detached catalog. Mirrors are trusted only after exact hash
// verification against the recorded official repository metadata.
func Fixed() Catalog {
	c := Catalog{SchemaVersion: 1, VerifiedAt: "2026-10-27", Sources: []string{
		"https://api.github.com/repos/ggml-org/llama.cpp/releases/tags/b10828",
		"https://huggingface.co/api/models/XHToken/Spark-X2.6-4B-GGUF/revision/" + spark26Rev + "?blobs=true",
		"https://huggingface.co/api/models/XHToken/Spark-X2.6-4B/revision/" + spark26Base + "?blobs=true",
		"https://modelscope.cn/api/v1/models/XHToken/Spark-X2.6-4B-GGUF/repo/files?Revision=8500d75a53f1e6592cfbb36fcb67c3e7380e6aab&Recursive=true",
		"https://modelscope.cn/api/v1/models/XHToken/Spark-X2.6-4B/repo/files?Revision=1648980d7798c24041bedf58aff82615c562b6c6&Recursive=true",
	}}
	// X2.6 retains the spark2_5 architecture supported by b10828, but uses its
	// own published template. Metadata checked against the official repo 2026-10-27.
	c.Models = append(c.Models, Model{
		ID: "spark-x2.6-4B", Name: "Spark-X2.6-4B-Q4_K_M", Tier: "4B", Revision: spark26Rev,
		Quantization: "Q4_K_M", License: "Apache-2.0", Purpose: "public_execution_validation_only",
		ContextTokens: 8192, NativeContextTokens: 1048576,
		Artifact: Artifact{ID: "Spark-X2.6-4B-" + spark26Rev,
			URL:      "https://hf-mirror.com/XHToken/Spark-X2.6-4B-GGUF/resolve/" + spark26Rev + "/Spark-X2.6-4B-Q4_K_M.gguf",
			FileName: "Spark-X2.6-4B-Q4_K_M.gguf", SHA256: "91cb2811aebb47ac916aa619d27d4cdd7f4143106b7917accb8ead4cd7f5cbd9",
			Size: 3543441312, Format: "file"},
		Template: Artifact{ID: "Spark-X2.6-4B-template-" + spark26Base,
			URL:      "https://hf-mirror.com/XHToken/Spark-X2.6-4B/resolve/" + spark26Base + "/chat_template.jinja",
			FileName: "chat_template.jinja", SHA256: "480576bd2ef80726a0dba6406d72ca37f4f4f9ca747c7f1c8d159dbdf40f6929",
			Size: 5717, Format: "file"}, TemplateRevision: spark26Base,
		InstallationBytes: 2*3543441312 + (576 << 20),
	})
	q8 := c.Models[0]
	q8.ID = "spark-x2.6-4B-Q8_0"
	q8.Name = "Spark-X2.6-4B-Q8_0"
	q8.Quantization = "Q8_0"
	q8.Artifact = Artifact{ID: "Spark-X2.6-4B-Q8_0-" + spark26Rev,
		URL:      "https://hf-mirror.com/XHToken/Spark-X2.6-4B-GGUF/resolve/" + spark26Rev + "/Spark-X2.6-4B-Q8_0.gguf",
		FileName: "Spark-X2.6-4B-Q8_0.gguf", SHA256: "145c2aa606820663f26ad530b8fc141ed885e2e9ca29f9be7c3bfe8931d58b81",
		Size: 4375021824, Format: "file"}
	q8.InstallationBytes = 2*q8.Artifact.Size + (576 << 20)
	c.Models = append(c.Models, q8)
	for _, v := range []struct {
		id, platform, arch, backend, asset, hash, exe string
		size                                          int64
	}{
		{"windows-amd64-cpu", "windows", "amd64", "cpu", "llama-b10828-bin-win-cpu-x64.zip", "221ef3c3d4f649e426708942fbb9dd369bc7ab7aa0153543323195459522b8cc", "llama-server.exe", 18414076},
		{"darwin-arm64-metal", "darwin", "arm64", "metal", "llama-b10828-bin-macos-arm64.tar.gz", "a9748e059515dfe853940f7cfc0ac7ac7af33901c773a64569b9435e27b0f6cb", "llama-server", 11133769},
		{"linux-amd64-cpu", "linux", "amd64", "cpu", "llama-b10828-bin-ubuntu-x64.tar.gz", "75085018390cb7a14d0048ad68fd2d7190a9f4a82cfdc531f1fbff156105929e", "llama-server", 16748330},
	} {
		format := "tar.gz"
		if v.platform == "windows" {
			format = "zip"
		}
		c.Runtimes = append(c.Runtimes, Runtime{ID: v.id, Version: "b10828", Commit: "3ad1ba7336986d98592d3e28cafd1a406715351f", Platform: v.platform, Architecture: v.arch, Backend: v.backend, License: "MIT", Executable: v.exe, Validation: "artifact_metadata_verified_native_model_pending", Artifact: Artifact{ID: v.id + "-b10828", URL: "https://github.com/ggml-org/llama.cpp/releases/download/b10828/" + v.asset, FileName: v.asset, SHA256: v.hash, Size: v.size, Format: format, ExpandedBytes: 256 << 20}})
		if v.platform == "windows" {
			c.Runtimes[len(c.Runtimes)-1].Dependencies = windowsDependencies()
		}
	}
	c.Runtimes = append(c.Runtimes, acceleratedRuntimes()...)
	return c
}
