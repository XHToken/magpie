package routerruntime

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"testing"

	"github.com/yetone/magpie/internal/routermanifest"
)

type downloadSourceTransport func(*http.Request) (*http.Response, error)

func (f downloadSourceTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestInstallerDownloadMirrorsKeepReceiptsAndReportCurrentHost(t *testing.T) {
	f := newWindowsDependencyFixture(t, nil)
	catalog := routermanifest.Fixed()
	runtime, _ := catalog.Runtime("windows-amd64-cpu")
	modelURL, templateURL, runtimeURL := f.model.Artifact.URL, f.model.Template.URL, f.runtime.Artifact.URL
	f.model.Artifact.URL = catalog.Models[0].Artifact.URL
	f.model.Template.URL = catalog.Models[0].Template.URL
	f.runtime.Artifact.URL = runtime.Artifact.URL
	modelFingerprint := f.model.Fingerprint(f.runtime)
	runtimeFingerprint := RuntimeFingerprint(f.runtime)
	base := f.installer.Downloader.Client.Transport
	f.installer.Downloader.Client.Transport = downloadSourceTransport(func(r *http.Request) (*http.Response, error) {
		var target string
		switch r.URL.String() {
		case f.runtime.Dependencies.Artifact.URL:
			target = r.URL.String()
		case f.runtime.Artifact.DownloadURLs()[0], f.model.Artifact.DownloadURLs()[0], f.model.Template.DownloadURLs()[0]:
			target = runtimeURL + "-unavailable"
		case f.runtime.Artifact.DownloadURLs()[1]:
			target = runtimeURL
		case f.model.Artifact.DownloadURLs()[1]:
			target = modelURL
		case f.model.Template.DownloadURLs()[1]:
			target = templateURL
		default:
			return nil, fmt.Errorf("unexpected external request: %s", r.URL.Hostname())
		}
		local := r.Clone(r.Context())
		address, err := url.Parse(target)
		if err != nil {
			return nil, err
		}
		local.URL = address
		return base.RoundTrip(local)
	})
	hosts := make(map[string]map[string]bool)
	if _, err := f.installer.Install(context.Background(), f.model, f.runtime, func(p Progress) {
		if hosts[p.Stage] == nil {
			hosts[p.Stage] = make(map[string]bool)
		}
		hosts[p.Stage][p.Host] = true
	}); err != nil {
		t.Fatal(err)
	}
	for stage, expected := range map[string][]string{
		"runtime":  {"gh-proxy.com", "github.com"},
		"model":    {"hf-mirror.com", "modelscope.cn"},
		"template": {"hf-mirror.com", "modelscope.cn"},
	} {
		for _, host := range expected {
			if !hosts[stage][host] {
				t.Fatalf("%s did not report %s: %v", stage, host, hosts[stage])
			}
		}
	}
	if _, err := f.installer.Verify(context.Background(), f.model, f.runtime); err != nil {
		t.Fatal("mirrors broke canonical extraction receipts", err)
	}
	if modelFingerprint != f.model.Fingerprint(f.runtime) || runtimeFingerprint != RuntimeFingerprint(f.runtime) {
		t.Fatal("transport source changed model or runtime identity")
	}
}
