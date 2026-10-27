package routermanifest

import (
	"strings"
	"testing"
)

func TestCatalogDownloadSourcesCoverEveryPlatformAndCUDADependency(t *testing.T) {
	c := Fixed()
	for _, runtime := range c.Runtimes {
		artifacts := []Artifact{runtime.Artifact}
		if runtime.AcceleratorDependencies != nil {
			artifacts = append(artifacts, runtime.AcceleratorDependencies.Artifact)
		}
		for _, a := range artifacts {
			urls := a.DownloadURLs()
			if len(urls) != 2 || urls[0] != "https://gh-proxy.com/"+a.URL || urls[1] != a.URL || !strings.HasSuffix(urls[0], "/"+a.FileName) {
				t.Fatalf("%s lacks domestic-first sources for %s: %v", runtime.ID, a.FileName, urls)
			}
		}
		if runtime.Dependencies != nil && len(runtime.Dependencies.Artifact.DownloadURLs()) != 1 {
			t.Fatal("Microsoft VC dependency must retain its own source")
		}
	}
}

func TestModelSourcesUsePinnedModelScopeWeightsAndTemplate(t *testing.T) {
	for _, m := range Fixed().Models {
		for _, a := range []Artifact{m.Artifact, m.Template} {
			urls := a.DownloadURLs()
			if len(urls) != 2 || urls[0] != a.URL || !strings.HasPrefix(urls[0], "https://hf-mirror.com/") || !strings.HasPrefix(urls[1], "https://modelscope.cn/models/XHToken/") || !strings.HasSuffix(urls[1], "/"+a.FileName) {
				t.Fatalf("%s: %v", a.ID, urls)
			}
			wantRevision := "8500d75a53f1e6592cfbb36fcb67c3e7380e6aab"
			if a == m.Template {
				wantRevision = "1648980d7798c24041bedf58aff82615c562b6c6"
			}
			if !strings.Contains(urls[1], "/resolve/"+wantRevision+"/") {
				t.Fatal("unpinned mirror", urls[1])
			}
			urls[0] = "https://invalid.example/changed"
			if a.DownloadURLs()[0] != a.URL {
				t.Fatal("source policy shares mutable storage")
			}
		}
	}
	a := Artifact{URL: "https://example.com/custom-runtime"}
	if urls := a.DownloadURLs(); len(urls) != 1 || urls[0] != a.URL {
		t.Fatal("custom source was rewritten", urls)
	}
}
