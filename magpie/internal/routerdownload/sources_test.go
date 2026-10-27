package routerdownload

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/yetone/magpie/internal/routermanifest"
)

type sourceTransport func(*http.Request) (*http.Response, error)

func (f sourceTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// Requests keep the catalog URLs but all traffic goes to a loopback fixture.
func sourceClient(t *testing.T, handler http.HandlerFunc) *http.Client {
	t.Helper()
	s := httptest.NewServer(handler)
	t.Cleanup(s.Close)
	endpoint, err := url.Parse(s.URL)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Client{Transport: sourceTransport(func(r *http.Request) (*http.Response, error) {
		local := r.Clone(r.Context())
		local.Header.Set("X-Source-Host", r.URL.Hostname())
		local.URL.Scheme, local.URL.Host = endpoint.Scheme, endpoint.Host
		return s.Client().Transport.RoundTrip(local)
	})}
}

func modelArtifact(data []byte) routermanifest.Artifact {
	a := artifact(data)
	a.URL = routermanifest.Fixed().Models[0].Artifact.URL
	return a
}

func TestDownloadFallsBackFromRemoteFailuresAndReportsActualSource(t *testing.T) {
	for _, failure := range []string{"http", "truncated", "corrupt", "encoded", "unsolicited-partial", "connection", "timeout"} {
		t.Run(failure, func(t *testing.T) {
			data := []byte("fixed identical GGUF bytes")
			a := modelArtifact(data)
			var hosts []string
			client := sourceClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("X-Source-Host") == "hf-mirror.com" {
					switch failure {
					case "http":
						w.WriteHeader(http.StatusServiceUnavailable)
					case "truncated":
						w.Header().Set("Content-Length", fmt.Sprint(len(data)))
						w.Write(data[:8])
					case "corrupt":
						w.Write(bytes.Repeat([]byte("x"), len(data)))
					case "encoded":
						w.Header().Set("Content-Encoding", "gzip")
						w.Write(data)
					case "unsolicited-partial":
						w.WriteHeader(http.StatusPartialContent)
					case "connection":
						conn, _, err := w.(http.Hijacker).Hijack()
						if err == nil {
							conn.Close()
						}
					}
					return
				}
				if r.Header.Get("Range") != "" || r.Header.Get("If-Range") != "" {
					t.Error("source switch carried a different host's partial or validator")
				}
				w.Write(data)
			})
			if failure == "timeout" {
				transport := client.Transport
				client.Transport = sourceTransport(func(r *http.Request) (*http.Response, error) {
					if r.URL.Hostname() == "hf-mirror.com" {
						return nil, os.ErrDeadlineExceeded
					}
					return transport.RoundTrip(r)
				})
			}
			dest := filepath.Join(t.TempDir(), "model.gguf")
			if err := (&Downloader{Client: client}).Download(context.Background(), a, dest, func(p Progress) {
				if len(hosts) == 0 || hosts[len(hosts)-1] != p.Host {
					hosts = append(hosts, p.Host)
				}
			}); err != nil {
				t.Fatal(err)
			}
			if err := Verify(context.Background(), a, dest); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(hosts, []string{"hf-mirror.com", "modelscope.cn"}) {
				t.Fatal("wrong source order or progress host", hosts)
			}
		})
	}
}

func TestDownloadRuntimePrefersProxyAndFallsBackToGitHub(t *testing.T) {
	for _, proxyFails := range []bool{false, true} {
		t.Run(fmt.Sprint(proxyFails), func(t *testing.T) {
			data := []byte("fixed runtime archive")
			a := artifact(data)
			a.URL = routermanifest.Fixed().Runtimes[0].Artifact.URL
			var hosts []string
			client := sourceClient(t, func(w http.ResponseWriter, r *http.Request) {
				if proxyFails && r.Header.Get("X-Source-Host") == "gh-proxy.com" {
					w.WriteHeader(http.StatusForbidden)
					return
				}
				w.Write(data)
			})
			if err := (&Downloader{Client: client}).Download(context.Background(), a, filepath.Join(t.TempDir(), "runtime.zip"), func(p Progress) {
				if len(hosts) == 0 || hosts[len(hosts)-1] != p.Host {
					hosts = append(hosts, p.Host)
				}
			}); err != nil {
				t.Fatal(err)
			}
			want := []string{"gh-proxy.com"}
			if proxyFails {
				want = append(want, "github.com")
			}
			if !reflect.DeepEqual(hosts, want) {
				t.Fatal("wrong runtime source order", hosts)
			}
		})
	}
}

func TestDownloadResumesModelScopeAfterRestartAndReusesVerifiedCache(t *testing.T) {
	data := []byte("complete fixed model bytes")
	a := modelArtifact(data)
	backup := a
	backup.URL = a.DownloadURLs()[1]
	dest := filepath.Join(t.TempDir(), "model.gguf")
	seedPartial(t, backup, dest, data[:8])
	var calls atomic.Int32
	client := sourceClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("X-Source-Host") != "modelscope.cn" || r.Header.Get("Range") != "bytes=8-" || r.Header.Get("If-Range") != `"old"` {
			t.Errorf("backup resume lost: host=%s range=%s if-range=%s", r.Header.Get("X-Source-Host"), r.Header.Get("Range"), r.Header.Get("If-Range"))
		}
		w.Header().Set("ETag", `"old"`)
		w.Header().Set("Content-Range", fmt.Sprintf("bytes 8-%d/%d", len(data)-1, len(data)))
		w.WriteHeader(http.StatusPartialContent)
		w.Write(data[8:])
	})
	d := &Downloader{Client: client}
	for i := 0; i < 2; i++ {
		if err := d.Download(context.Background(), a, dest, nil); err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != 1 {
		t.Fatal("verified cache downloaded again", calls.Load())
	}
}

func TestDownloadAllSourcesMustPassIntegrity(t *testing.T) {
	a := modelArtifact([]byte("good"))
	var calls atomic.Int32
	client := sourceClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Write([]byte("evil"))
	})
	dest := filepath.Join(t.TempDir(), "model.gguf")
	if err := (&Downloader{Client: client}).Download(context.Background(), a, dest, nil); !errors.Is(err, ErrIntegrity) {
		t.Fatal("bad mirrors bypassed integrity", err)
	}
	if calls.Load() != 2 {
		t.Fatal("did not try each source once", calls.Load())
	}
	requireMissing(t, dest)
	requireMissing(t, dest+".part")
}

func TestDownloadCancellationPreservesPrimaryPartialWithoutFailover(t *testing.T) {
	data := bytes.Repeat([]byte("fixed model bytes"), 4096)
	a := modelArtifact(data)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var calls atomic.Int32
	client := sourceClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Length", fmt.Sprint(len(data)))
		w.Write(data)
	})
	dest := filepath.Join(t.TempDir(), "model.gguf")
	err := (&Downloader{Client: client}).Download(ctx, a, dest, func(p Progress) {
		if p.Downloaded > 0 {
			cancel()
		}
	})
	if !errors.Is(err, context.Canceled) || calls.Load() != 1 {
		t.Fatalf("cancellation switched sources: calls=%d err=%v", calls.Load(), err)
	}
	if info, err := os.Stat(dest + ".part"); err != nil || info.Size() <= 0 || info.Size() >= a.Size {
		t.Fatalf("canceled partial lost: %v, %v", info, err)
	}
	requireMissing(t, dest)
}

func TestDownloadDoesNotFailOverForLocalFailuresOrCancellation(t *testing.T) {
	for _, failure := range []string{"disk-space", "disk-probe", "metadata", "cancel"} {
		t.Run(failure, func(t *testing.T) {
			a := modelArtifact([]byte("fixed model"))
			var calls atomic.Int32
			client := sourceClient(t, func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(503) })
			dest := filepath.Join(t.TempDir(), "model.gguf")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			d := &Downloader{Client: client}
			switch failure {
			case "disk-space":
				d.FreeBytes = func(string) (uint64, error) { return 0, nil }
			case "disk-probe":
				d.FreeBytes = func(string) (uint64, error) { return 0, errors.New("disk probe failed") }
			case "metadata":
				if err := os.WriteFile(dest+".part.json", []byte("invalid JSON"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			err := d.Download(ctx, a, dest, func(p Progress) {
				if failure == "cancel" {
					cancel()
				}
			})
			if err == nil || calls.Load() != 0 {
				t.Fatalf("local failure contacted a source: calls=%d err=%v", calls.Load(), err)
			}
			if failure == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
		})
	}
}

func TestDownloadSourceSwitchDropsUntrustedResumeURL(t *testing.T) {
	data := []byte("fixed model bytes")
	a := modelArtifact(data)
	dest := filepath.Join(t.TempDir(), "model.gguf")
	seedPartial(t, a, dest, data[:5])
	meta, _ := json.Marshal(partial{URL: "https://untrusted.invalid/model", SHA256: a.SHA256, Size: a.Size, ETag: `"other"`})
	if err := os.WriteFile(dest+".part.json", meta, 0o600); err != nil {
		t.Fatal(err)
	}
	client := sourceClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Source-Host") != "hf-mirror.com" || r.Header.Get("Range") != "" || r.Header.Get("If-Range") != "" {
			t.Error("untrusted source metadata influenced request")
		}
		w.Write(data)
	})
	if err := (&Downloader{Client: client}).Download(context.Background(), a, dest, nil); err != nil {
		t.Fatal(err)
	}
}
