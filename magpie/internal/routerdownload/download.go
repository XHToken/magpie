// Package routerdownload downloads pinned artifacts and publishes only verified files.
package routerdownload

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/yetone/magpie/internal/filelock"
	"github.com/yetone/magpie/internal/routermanifest"
	"github.com/yetone/magpie/internal/steady"
)

const defaultStallTimeout = 30 * time.Second

var (
	ErrBusy      = errors.New("artifact destination is busy")
	ErrDiskSpace = errors.New("insufficient disk space")
	ErrIntegrity = errors.New("artifact integrity check failed")

	defaultClient = newDefaultClient()
)

func newDefaultClient() *http.Client {
	if base, ok := http.DefaultTransport.(*http.Transport); ok {
		tr := base.Clone()
		tr.DialContext = (&net.Dialer{
			Timeout:   15 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext
		tr.TLSHandshakeTimeout = 15 * time.Second
		tr.ResponseHeaderTimeout = 30 * time.Second
		return &http.Client{Transport: tr}
	}
	return http.DefaultClient
}

type Progress struct {
	Downloaded int64
	Total      int64
	Host       string
}

// Downloader's zero value uses the system disk-space check and HTTP client.
// A callback runs synchronously and must not re-enter the same destination.
type Downloader struct {
	Client       *http.Client
	FreeBytes    func(string) (uint64, error)
	StallTimeout time.Duration
}

type partial struct {
	URL          string `json:"url"`
	SHA256       string `json:"sha256"`
	Size         int64  `json:"size"`
	ETag         string `json:"etag,omitempty"`
	LastModified string `json:"lastModified,omitempty"`
}

// Download preserves .part and .part.json on cancellation or short reads. A
// new Downloader (or process) resumes them. A final file is always reverified.
// An OS lock rejects duplicate writers and is released even on process death.
func (d *Downloader) Download(ctx context.Context, a routermanifest.Artifact, destination string, report func(Progress)) error {
	if err := validate(a); err != nil {
		return err
	}
	u, err := url.Parse(a.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		return errors.New("artifact has no valid HTTP download URL")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	unlock, err := lockDestination(destination)
	if err != nil {
		return err
	}
	defer unlock()
	if _, err := os.Lstat(destination); err == nil {
		if err := Verify(ctx, a, destination); err == nil {
			if report != nil {
				report(Progress{Downloaded: a.Size, Total: a.Size})
			}
			return nil
		} else if ctx.Err() != nil {
			return ctx.Err()
		} else if !errors.Is(err, ErrIntegrity) {
			return err
		}
		if err := regularOrMissing(destination); err != nil {
			return err
		}
		if err := os.Remove(destination); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	sources, err := resumeSources(a, destination)
	if err != nil {
		return err
	}
	var failures []error
	for _, source := range sources {
		if err := ctx.Err(); err != nil {
			return err
		}
		u, err := url.Parse(source)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
			return errors.New("artifact has no valid HTTP download URL")
		}
		var progress func(Progress)
		if report != nil {
			progress = func(p Progress) { p.Host = u.Hostname(); report(p) }
		}
		current := a
		current.URL = source
		err = d.downloadSource(ctx, current, destination, progress)
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var failed *sourceError
		if !errors.As(err, &failed) {
			return err
		}
		failures = append(failures, fmt.Errorf("download from %s: %w", u.Hostname(), err))
	}
	return errors.Join(failures...)
}

// resumeSources prefers the persisted, catalog-approved source on a resumed
// job. A new job always uses the catalog order. Validators never cross hosts.
func resumeSources(a routermanifest.Artifact, destination string) ([]string, error) {
	sources := a.DownloadURLs()
	partPath, metaPath := destination+".part", destination+".part.json"
	for _, p := range []string{partPath, metaPath} {
		if err := regularOrMissing(p); err != nil {
			return nil, err
		}
	}
	b, err := os.ReadFile(metaPath)
	if errors.Is(err, os.ErrNotExist) {
		return sources, nil
	}
	if err != nil {
		return nil, err
	}
	var old partial
	if err := json.Unmarshal(b, &old); err != nil {
		return nil, fmt.Errorf("read resume metadata: %w", err)
	}
	if old.Size == a.Size && strings.EqualFold(old.SHA256, a.SHA256) {
		if info, err := os.Stat(partPath); err == nil && info.Size() > 0 && info.Size() <= a.Size {
			for i, source := range sources {
				if source == old.URL {
					copy(sources[1:i+1], sources[:i])
					sources[0] = source
					return sources, nil
				}
			}
		}
	}
	return sources, nil
}

// Only remote transfer failures may advance to a mirror. Filesystem failures,
// contention and cancellation remain terminal for the installation job.
type sourceError struct{ err error }

func (e *sourceError) Error() string { return e.err.Error() }
func (e *sourceError) Unwrap() error { return e.err }

// The caller keeps the destination lock across all source attempts.
func (d *Downloader) downloadSource(ctx context.Context, a routermanifest.Artifact, destination string, report func(Progress)) error {
	partPath, metaPath := destination+".part", destination+".part.json"
	for _, p := range []string{partPath, metaPath} {
		if err := regularOrMissing(p); err != nil {
			return err
		}
	}
	m := partial{URL: a.URL, SHA256: strings.ToLower(a.SHA256), Size: a.Size}
	var old partial
	if b, err := os.ReadFile(metaPath); err == nil {
		if err := json.Unmarshal(b, &old); err != nil {
			return fmt.Errorf("read resume metadata: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	offset := int64(0)
	if fi, err := os.Stat(partPath); err == nil {
		if old.URL == m.URL && old.SHA256 == m.SHA256 && old.Size == m.Size && fi.Size() <= a.Size {
			offset, m = fi.Size(), old
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if offset == 0 {
		if err := removePartial(partPath, metaPath); err != nil {
			return err
		}
	}
	space := d.FreeBytes
	if space == nil {
		space = freeBytes
	}
	available, err := space(filepath.Dir(destination))
	if a.ExpandedBytes < 0 || (err == nil && (uint64(a.Size-offset) > available || uint64(a.ExpandedBytes) > available-uint64(a.Size-offset))) {
		return ErrDiskSpace
	}
	if offset == a.Size {
		if err := Verify(ctx, a, partPath); err == nil {
			if report != nil {
				report(Progress{Downloaded: a.Size, Total: a.Size})
			}
			return finish(ctx, partPath, metaPath, destination)
		} else if ctx.Err() != nil {
			return ctx.Err()
		} else if !errors.Is(err, ErrIntegrity) {
			return err
		}
		if err := removePartial(partPath, metaPath); err != nil {
			return err
		}
		offset = 0
	}
	if report != nil {
		report(Progress{Downloaded: offset, Total: a.Size})
	}
	client := d.Client
	if client == nil {
		client = defaultClient
	}
	stallTimeout := d.StallTimeout
	if stallTimeout <= 0 {
		stallTimeout = defaultStallTimeout
	}
	// A changed validator or malformed partial response gets one fresh request;
	// a mid-stream stall that made forward progress gets up to three Range resumes.
	validatorResets := 0
	stallRetries := 0
	for {
		reqCtx, cancelReq := context.WithCancel(ctx)
		var stalled atomic.Bool
		timer := time.AfterFunc(stallTimeout, func() {
			stalled.Store(true)
			cancelReq()
		})
		cleanup := func() {
			timer.Stop()
			cancelReq()
		}
		req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, a.URL, nil)
		if err != nil {
			cleanup()
			return err
		}
		req.Header.Set("Accept-Encoding", "identity")
		if offset > 0 {
			req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
			if m.ETag != "" && !strings.HasPrefix(m.ETag, "W/") {
				req.Header.Set("If-Range", m.ETag)
			} else if m.LastModified != "" {
				req.Header.Set("If-Range", m.LastModified)
			}
		}
		resp, err := client.Do(req)
		timer.Stop()
		if err != nil {
			cancelReq()
			if ctx.Err() != nil {
				return ctx.Err()
			}
			var pathErr *os.PathError
			if errors.As(err, &pathErr) || errors.Is(err, ErrDiskSpace) || errors.Is(err, ErrBusy) {
				return err
			}
			if stalled.Load() {
				return &sourceError{fmt.Errorf("download stalled waiting for response headers after %s", stallTimeout)}
			}
			return &sourceError{err}
		}
		if resp.Header.Get("Content-Encoding") != "" && resp.Header.Get("Content-Encoding") != "identity" {
			resp.Body.Close()
			cleanup()
			return &sourceError{errors.New("encoded artifact response refused")}
		}
		if offset > 0 && resp.StatusCode == http.StatusPartialContent {
			changed := (m.ETag != "" && resp.Header.Get("ETag") != m.ETag) || (m.ETag == "" && m.LastModified != "" && resp.Header.Get("Last-Modified") != m.LastModified)
			if changed || !validRange(resp.Header.Get("Content-Range"), offset, a.Size) {
				resp.Body.Close()
				cleanup()
				if err := removePartial(partPath, metaPath); err != nil {
					return err
				}
				validatorResets++
				if validatorResets > 1 {
					return &sourceError{errors.New("resource changed while resuming")}
				}
				offset, m = 0, partial{URL: a.URL, SHA256: strings.ToLower(a.SHA256), Size: a.Size}
				continue
			}
		} else if resp.StatusCode == http.StatusOK {
			// Range ignored, or If-Range reports a new resource: replace the partial.
			offset = 0
		} else {
			resp.Body.Close()
			cleanup()
			return &sourceError{fmt.Errorf("download HTTP status %d", resp.StatusCode)}
		}
		if offset == 0 && resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			cleanup()
			return &sourceError{errors.New("unexpected partial response to full download")}
		}
		m.ETag, m.LastModified = resp.Header.Get("ETag"), resp.Header.Get("Last-Modified")
		b, _ := json.Marshal(m)
		flags := os.O_CREATE | os.O_WRONLY
		if offset == 0 {
			flags |= os.O_TRUNC
		}
		f, err := os.OpenFile(partPath, flags, 0o600)
		if err != nil {
			resp.Body.Close()
			cleanup()
			return err
		}
		if _, err := f.Seek(offset, io.SeekStart); err != nil {
			f.Close()
			resp.Body.Close()
			cleanup()
			return err
		}
		// Persist a reset before its new validator: a crash must never attach
		// the new resource's validator to the old resource's prefix.
		if offset == 0 {
			if err := f.Sync(); err != nil {
				f.Close()
				resp.Body.Close()
				cleanup()
				return err
			}
		}
		if err := steady.WriteFileContext(ctx, metaPath, b, 0o600); err != nil {
			f.Close()
			resp.Body.Close()
			cleanup()
			return err
		}
		if report != nil {
			report(Progress{Downloaded: offset, Total: a.Size})
		}
		startOffset := offset
		w := &progressWriter{writer: f, downloaded: offset, total: a.Size, report: report}
		timer.Reset(stallTimeout)
		_, copyErr := io.Copy(w, io.LimitReader(&stallReader{ctx: reqCtx, reader: resp.Body, timer: timer, timeout: stallTimeout}, a.Size-offset+1))
		resp.Body.Close()
		cleanup()
		syncErr, closeErr := f.Sync(), f.Close()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if w.err != nil {
			return w.err
		}
		if syncErr != nil {
			return syncErr
		}
		if closeErr != nil {
			return closeErr
		}
		if copyErr != nil {
			if stalled.Load() {
				if w.downloaded > startOffset && w.downloaded < a.Size && stallRetries < 3 {
					stallRetries++
					offset = w.downloaded
					continue
				}
				return &sourceError{fmt.Errorf("download stalled after %s at %d/%d bytes", stallTimeout, w.downloaded, a.Size)}
			}
			return &sourceError{fmt.Errorf("download body: %w", copyErr)}
		}
		if w.downloaded < a.Size {
			return &sourceError{io.ErrUnexpectedEOF}
		}
		if err := Verify(ctx, a, partPath); err != nil {
			if ctx.Err() == nil && errors.Is(err, ErrIntegrity) {
				if removeErr := removePartial(partPath, metaPath); removeErr != nil {
					return errors.Join(err, removeErr)
				}
				return &sourceError{err}
			}
			return err
		}
		return finish(ctx, partPath, metaPath, destination)
	}
}

func finish(ctx context.Context, partPath, metaPath, destination string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := steady.Rename(partPath, destination); err != nil {
		return err
	}
	// Rename commits the verified artifact; leftover metadata is harmless.
	_ = os.Remove(metaPath)
	return nil
}

func validRange(value string, offset, size int64) bool {
	var start, end, total int64
	n, err := fmt.Sscanf(value, "bytes %d-%d/%d", &start, &end, &total)
	return err == nil && n == 3 && value == fmt.Sprintf("bytes %d-%d/%d", start, end, total) && start == offset && end == size-1 && total == size
}

func validate(a routermanifest.Artifact) error {
	hash, err := hex.DecodeString(a.SHA256)
	if a.Size <= 0 || err != nil || len(hash) != sha256.Size {
		return errors.New("artifact requires confirmed size and SHA256")
	}
	return nil
}

// Verify checks bytes on disk, never metadata or the existence of a file alone.
func Verify(ctx context.Context, a routermanifest.Artifact, path string) error {
	if err := validate(a); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := checkParents(filepath.Dir(path)); err != nil {
		return err
	}
	if err := regularOrMissing(path); err != nil {
		return err
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return err
	}
	if !fi.Mode().IsRegular() || fi.Size() != a.Size {
		return fmt.Errorf("%w: size differs from %d", ErrIntegrity, a.Size)
	}
	h := sha256.New()
	if _, err := io.Copy(h, &contextReader{ctx: ctx, reader: f}); err != nil {
		return err
	}
	if !strings.EqualFold(hex.EncodeToString(h.Sum(nil)), a.SHA256) {
		return fmt.Errorf("%w: SHA256 mismatch", ErrIntegrity)
	}
	return nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(b []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(b)
}

type stallReader struct {
	ctx     context.Context
	reader  io.Reader
	timer   *time.Timer
	timeout time.Duration
}

func (r *stallReader) Read(b []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	r.timer.Reset(r.timeout)
	n, err := r.reader.Read(b)
	if n > 0 {
		r.timer.Reset(r.timeout)
	}
	return n, err
}

type progressWriter struct {
	writer            io.Writer
	downloaded, total int64
	report            func(Progress)
	err               error
}

func (w *progressWriter) Write(b []byte) (int, error) {
	n, err := w.writer.Write(b)
	w.err = err
	w.downloaded += int64(n)
	if w.report != nil {
		w.report(Progress{Downloaded: w.downloaded, Total: w.total})
	}
	return n, err
}

func removePartial(paths ...string) error {
	for _, p := range paths {
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func regularOrMissing(path string) error {
	fi, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("not a regular file: %s", path)
	}
	return nil
}

func safeParents(dir string) error {
	if !filepath.IsAbs(dir) {
		return errors.New("artifact destination must be absolute")
	}
	fi, err := os.Lstat(dir)
	if errors.Is(err, os.ErrNotExist) {
		if err := safeParents(filepath.Dir(dir)); err != nil {
			return err
		}
		return os.Mkdir(dir, 0o700)
	}
	if err != nil {
		return err
	}
	if !fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("unsafe destination parent: %s", dir)
	}
	if parent := filepath.Dir(dir); parent != dir {
		return safeParents(parent)
	}
	return nil
}

func checkParents(dir string) error {
	if !filepath.IsAbs(dir) {
		return errors.New("artifact destination must be absolute")
	}
	fi, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("unsafe destination parent: %s", dir)
	}
	if parent := filepath.Dir(dir); parent != dir {
		return checkParents(parent)
	}
	return nil
}

func lockDestination(path string) (func(), error) {
	if !filepath.IsAbs(path) {
		return nil, errors.New("artifact destination must be absolute")
	}
	if err := safeParents(filepath.Dir(path)); err != nil {
		return nil, err
	}
	lockPath := path + ".lock"
	if err := regularOrMissing(lockPath); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	held, err := filelock.TryLock(f)
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("lock artifact: %w", err)
	}
	if !held {
		f.Close()
		return nil, ErrBusy
	}
	return func() { filelock.Unlock(f); f.Close() }, nil
}
