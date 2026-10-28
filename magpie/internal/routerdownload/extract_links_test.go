package routerdownload

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/routermanifest"
)

func TestExtractMaterializesLibraryAliasChains(t *testing.T) {
	// The b10828 macOS package lists forward links before their targets.
	entries := []archiveEntry{
		{"llama-b10828/libggml.dylib", []byte("libggml.0.dylib"), os.ModeSymlink | 0o777},
		{"llama-b10828/libggml.0.dylib", []byte("libggml.0.23.0.dylib"), os.ModeSymlink | 0o777},
		{"llama-b10828/libggml.0.23.0.dylib", []byte("library payload"), 0o755},
		{"bin/server", []byte("../llama-b10828/llama-server"), os.ModeSymlink | 0o777},
		{"llama-b10828/llama-server", []byte("server payload"), 0o755},
	}
	a, archive := localArtifact(t, tarGzipBytes(t, entries...))
	a.Format, a.ExpandedBytes = "tar.gz", 1024
	dest := filepath.Join(t.TempDir(), "installed")
	ctx := context.Background()
	if err := Extract(ctx, a, archive, dest); err != nil {
		t.Fatalf("valid library aliases must install: %v", err)
	}
	for _, name := range []string{"libggml.dylib", "libggml.0.dylib", "libggml.0.23.0.dylib"} {
		p := filepath.Join(dest, "llama-b10828", name)
		info, err := os.Lstat(p)
		if err != nil || !info.Mode().IsRegular() {
			t.Fatalf("alias must be a regular file: %s %v", name, err)
		}
		got, err := os.ReadFile(p)
		if err != nil || string(got) != "library payload" {
			t.Fatalf("alias content: %s %q %v", name, got, err)
		}
	}
	if err := VerifyDirectory(ctx, a, dest); err != nil {
		t.Fatal("expanded aliases failed directory verification", err)
	}
	before, err := os.ReadFile(filepath.Join(dest, receiptName))
	if err != nil {
		t.Fatal(err)
	}
	var receipt receipt
	if err := json.Unmarshal(before, &receipt); err != nil || len(receipt.Files) != len(entries) {
		t.Fatalf("aliases missing from integrity receipt: %v", err)
	}
	if err := Extract(ctx, a, archive, dest); err != nil {
		t.Fatal("verified aliases were not reusable", err)
	}
	after, err := os.ReadFile(filepath.Join(dest, receiptName))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("reuse changed the receipt", err)
	}
	if err := os.WriteFile(filepath.Join(dest, "llama-b10828/libggml.dylib"), []byte("changed payload"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := VerifyDirectory(ctx, a, dest); err == nil {
		t.Fatal("corrupted alias remained verified")
	}
}

func TestExtractRejectsUnsafeLibraryAliases(t *testing.T) {
	for i, target := range []string{
		"../../outside", "/absolute", "C:/absolute", `C:\outside`, `..\outside`,
		"//server/share", "missing", "directory", "link", "other", "directory/file",
		"library:stream", "NUL", "library.", "library ", receiptName,
		"../.router-verified.json", "directory//file", strings.Repeat("x", 4097),
	} {
		t.Run(fmt.Sprintf("target-%d", i), func(t *testing.T) {
			entries := []archiveEntry{
				{"runtime/link", []byte(target), os.ModeSymlink | 0o777},
				{"runtime/other", []byte("link"), os.ModeSymlink | 0o777},
				{"runtime/directory/", nil, os.ModeDir | 0o700},
				{"runtime/library", []byte("library payload"), 0o600},
			}
			a, archive := localArtifact(t, tarGzipBytes(t, entries...))
			a.Format, a.ExpandedBytes = "tar.gz", 1024
			dest := filepath.Join(t.TempDir(), "installed")
			if err := Extract(context.Background(), a, archive, dest); err == nil {
				t.Fatal("unsafe alias installed")
			}
			requireMissing(t, dest)
			stages, err := filepath.Glob(filepath.Join(filepath.Dir(dest), ".installed.extract-*"))
			if err != nil || len(stages) != 0 {
				t.Fatalf("failed extraction retained staging tree: %v %v", stages, err)
			}
		})
	}
}

func TestExtractRejectsAliasNamespaceConflictsAndHardLinks(t *testing.T) {
	link := archiveEntry{"alias", []byte("lib"), os.ModeSymlink | 0o777}
	file := archiveEntry{"lib", []byte("payload"), 0o600}
	for _, entries := range [][]archiveEntry{
		{file, link, link},
		{file, link, {"ALIAS", []byte("payload"), 0o600}},
		{file, link, {"alias/child", []byte("payload"), 0o600}},
		{file, {"alias/child", []byte("payload"), 0o600}, link},
		{{"dir/file", []byte("payload"), 0o600}, {"alias", []byte("dir"), os.ModeSymlink | 0o777}, {"copy", []byte("alias/file"), os.ModeSymlink | 0o777}},
	} {
		a, archive := localArtifact(t, tarGzipBytes(t, entries...))
		a.Format, a.ExpandedBytes = "tar.gz", 1024
		dest := filepath.Join(t.TempDir(), "installed")
		if err := Extract(context.Background(), a, archive, dest); err == nil {
			t.Fatal("conflicting alias namespace installed")
		}
		requireMissing(t, dest)
	}
	var b bytes.Buffer
	g := gzip.NewWriter(&b)
	tw := tar.NewWriter(g)
	if err := tw.WriteHeader(&tar.Header{Name: "hardlink", Linkname: "lib", Typeflag: tar.TypeLink, Mode: 0o700}); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := g.Close(); err != nil {
		t.Fatal(err)
	}
	a, archive := localArtifact(t, b.Bytes())
	a.Format, a.ExpandedBytes = "tar.gz", 1024
	dest := filepath.Join(t.TempDir(), "installed")
	if err := Extract(context.Background(), a, archive, dest); err == nil {
		t.Fatal("tar hard link accepted")
	}
	requireMissing(t, dest)
}

func TestExtractAliasCopiesCountTowardExpansionBound(t *testing.T) {
	for _, limit := range []int64{14, 15} {
		a, archive := localArtifact(t, tarGzipBytes(t,
			archiveEntry{"lib", []byte("12345"), 0o600},
			archiveEntry{"alias1", []byte("lib"), os.ModeSymlink | 0o777},
			archiveEntry{"alias2", []byte("alias1"), os.ModeSymlink | 0o777},
		))
		a.Format, a.ExpandedBytes = "tar.gz", limit
		dest := filepath.Join(t.TempDir(), "installed")
		err := Extract(context.Background(), a, archive, dest)
		if limit == 14 {
			if err == nil || !strings.Contains(err.Error(), "expanded-byte bound") {
				t.Fatalf("alias bytes escaped expansion limit: %v", err)
			}
			requireMissing(t, dest)
		} else if err != nil {
			t.Fatalf("exact expansion bound rejected: %v", err)
		}
	}
}

var pinnedArchiveDir = flag.String("pinned-runtime-archive-dir", "", "offline directory containing fixed catalog runtime archives for acceptance")

// Explicit offline acceptance only: never download a real archive from a test.
func TestPinnedRuntimeArchives(t *testing.T) {
	if *pinnedArchiveDir == "" {
		t.Skip("provide -pinned-runtime-archive-dir for official archive acceptance")
	}
	count := 0
	for _, runtime := range routermanifest.Fixed().Runtimes {
		archive := filepath.Join(*pinnedArchiveDir, runtime.Artifact.FileName)
		if _, err := os.Stat(archive); os.IsNotExist(err) {
			continue
		}
		count++
		t.Run(runtime.ID, func(t *testing.T) {
			dest := filepath.Join(t.TempDir(), "runtime")
			ctx := context.Background()
			for range 2 {
				if err := Extract(ctx, runtime.Artifact, archive, dest); err != nil {
					t.Fatal(err)
				}
				if err := VerifyDirectory(ctx, runtime.Artifact, dest); err != nil {
					t.Fatal(err)
				}
			}
			files := 0
			if err := filepath.WalkDir(dest, func(p string, d fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if !d.IsDir() {
					info, err := d.Info()
					if err != nil {
						return err
					}
					if !info.Mode().IsRegular() {
						t.Errorf("non-regular installed file: %s", p)
					}
					files++
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			t.Logf("verified official %s, %d regular files including receipt; install and reuse passed", runtime.Artifact.FileName, files)
		})
	}
	if count == 0 {
		t.Fatal("no fixed runtime archive found in supplied directory")
	}
}
