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
		if m.Tier == "1.7B" {
			continue
		}
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

func TestSmallModelSourcesRetainOfficialPinsAcrossFallback(t *testing.T) {
	for _, m := range Fixed().Models {
		if m.Tier != "1.7B" {
			continue
		}
		for _, a := range []Artifact{m.Artifact, m.Template} {
			urls := a.DownloadURLs()
			if len(urls) != 3 || urls[0] != a.URL || urls[2] != strings.Replace(a.URL, "https://hf-mirror.com/", "https://huggingface.co/", 1) || strings.Contains(urls[2], "/main/") {
				t.Fatalf("1.7B fallback lost pinned identity: %v", urls)
			}
			// The domestic mirror sits between hf-mirror and the official host,
			// pinned to a commit and serving the same file name.
			wantRevision := "/resolve/8aebdd1184370b5ebb93d42876faf75304f23346/"
			if a == m.Template {
				wantRevision = "/resolve/ab6acab5eca04380414ab2be415bc98a368b92f1/"
			}
			if !strings.HasPrefix(urls[1], "https://modelscope.cn/models/XHToken/") || !strings.Contains(urls[1], wantRevision) || !strings.HasSuffix(urls[1], "/"+a.FileName) {
				t.Fatalf("1.7B lacks a pinned ModelScope source: %v", urls)
			}
		}
	}
}
