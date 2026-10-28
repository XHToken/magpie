package routerruntime

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/yetone/magpie/internal/dirs"
	"github.com/yetone/magpie/internal/routermanifest"
)

func TestFileInstallerRepairsMaterializedAliasesWithoutDownloadingWeights(t *testing.T) {
	var archive bytes.Buffer
	g := gzip.NewWriter(&archive)
	tw := tar.NewWriter(g)
	for _, h := range []*tar.Header{
		{Name: "release/libggml.dylib", Linkname: "libggml.0.dylib", Typeflag: tar.TypeSymlink, Mode: 0o777},
		{Name: "release/libggml.0.dylib", Linkname: "libggml.0.23.0.dylib", Typeflag: tar.TypeSymlink, Mode: 0o777},
		{Name: "release/libggml.0.23.0.dylib", Typeflag: tar.TypeReg, Mode: 0o755, Size: 7},
		{Name: "release/llama-server", Typeflag: tar.TypeReg, Mode: 0o755, Size: 7},
	} {
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if h.Size != 0 {
			if _, err := tw.Write([]byte("payload")); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := g.Close(); err != nil {
		t.Fatal(err)
	}
	contents := map[string][]byte{"/runtime": archive.Bytes(), "/model": []byte("fixture-gguf"), "/template": []byte("fixture-template")}
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if b, ok := contents[r.URL.Path]; ok {
			_, _ = w.Write(b)
		} else {
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	m := routermanifest.Model{ID: "fixture", Artifact: artifactFor("model", "model.gguf", contents["/model"]), Template: artifactFor("template", "template.jinja", contents["/template"])}
	m.Artifact.URL, m.Template.URL = server.URL+"/model", server.URL+"/template"
	r := routermanifest.Runtime{ID: "fixture", Executable: "llama-server", Backend: "cpu", Artifact: artifactFor("runtime", "runtime.tar.gz", contents["/runtime"])}
	r.Artifact.URL, r.Artifact.Format, r.Artifact.ExpandedBytes = server.URL+"/runtime", "tar.gz", 1024
	root := t.TempDir()
	f := &FileInstaller{Layout: dirs.Layout{Models: filepath.Join(root, "models"), Runtime: filepath.Join(root, "runtime")}}
	ctx := context.Background()
	spec, err := f.Install(ctx, m, r, nil)
	if err != nil {
		t.Fatal(err)
	}
	weights, err := os.Stat(spec.ModelPath)
	if err != nil {
		t.Fatal(err)
	}
	before := calls.Load()
	for _, repair := range []bool{false, true} {
		if repair {
			alias := filepath.Join(filepath.Dir(spec.Executable), "libggml.dylib")
			if err := os.WriteFile(alias, []byte("damaged"), 0o700); err != nil {
				t.Fatal(err)
			}
			if _, err := f.Verify(ctx, m, r); err == nil {
				t.Fatal("damaged alias remained installed")
			}
		}
		if _, err := f.Install(ctx, m, r, nil); err != nil {
			t.Fatal("reuse/repair failed", err)
		}
		if calls.Load() != before {
			t.Fatal("reuse/repair downloaded already verified artifacts")
		}
		after, err := os.Stat(spec.ModelPath)
		if err != nil || !os.SameFile(weights, after) || !weights.ModTime().Equal(after.ModTime()) {
			t.Fatal("repair replaced cached model weights", err)
		}
	}
	_, _, _, folder := f.paths(m, r)
	quarantines, err := filepath.Glob(folder + ".invalid-*")
	if err != nil || len(quarantines) != 1 {
		t.Fatalf("repair did not preserve damaged runtime: %v %v", quarantines, err)
	}
}
