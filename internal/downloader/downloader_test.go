package downloader_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/bilusteknoloji/pipg/internal/downloader"
)

func newTestServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	return srv
}

func sha256Hex(data []byte) string {
	h := sha256.Sum256(data)

	return hex.EncodeToString(h[:])
}

func TestDownloadSingle(t *testing.T) {
	content := []byte("fake wheel content for testing")
	hash := sha256Hex(content)

	srv := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(content)
	}))

	dir := t.TempDir()
	mgr := downloader.New(dir, downloader.WithHTTPClient(srv.Client()))

	results, err := mgr.Download(context.Background(), []downloader.Request{
		{
			Name:     "testpkg",
			Version:  "1.0.0",
			URL:      srv.URL + "/testpkg-1.0.0-py3-none-any.whl",
			SHA256:   hash,
			Filename: "testpkg-1.0.0-py3-none-any.whl",
		},
	})
	if err != nil {
		t.Fatalf("Download() error: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	if results[0].Name != "testpkg" {
		t.Errorf("Name = %q, want %q", results[0].Name, "testpkg")
	}

	if results[0].Version != "1.0.0" {
		t.Errorf("Version = %q, want %q", results[0].Version, "1.0.0")
	}

	if results[0].Size != int64(len(content)) {
		t.Errorf("Size = %d, want %d", results[0].Size, len(content))
	}

	wantPath := filepath.Join(dir, "testpkg-1.0.0-py3-none-any.whl")
	if results[0].FilePath != wantPath {
		t.Errorf("FilePath = %q, want %q", results[0].FilePath, wantPath)
	}

	// Verify file exists and content matches.
	got, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatalf("reading downloaded file: %v", err)
	}

	if string(got) != string(content) {
		t.Errorf("file content mismatch")
	}
}

func TestDownloadConcurrent(t *testing.T) {
	packages := []struct {
		name    string
		content []byte
	}{
		{"pkg-a", []byte("content of package a")},
		{"pkg-b", []byte("content of package b")},
		{"pkg-c", []byte("content of package c")},
	}

	srv := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, p := range packages {
			if r.URL.Path == "/"+p.name+".whl" {
				_, _ = w.Write(p.content)

				return
			}
		}
		http.NotFound(w, r)
	}))

	dir := t.TempDir()
	mgr := downloader.New(dir,
		downloader.WithHTTPClient(srv.Client()),
		downloader.WithMaxWorkers(3),
	)

	var requests []downloader.Request
	for _, p := range packages {
		requests = append(requests, downloader.Request{
			Name:     p.name,
			Version:  "1.0.0",
			URL:      srv.URL + "/" + p.name + ".whl",
			SHA256:   sha256Hex(p.content),
			Filename: p.name + "-1.0.0-py3-none-any.whl",
		})
	}

	results, err := mgr.Download(context.Background(), requests)
	if err != nil {
		t.Fatalf("Download() error: %v", err)
	}

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	for i, r := range results {
		if r.Name != packages[i].name {
			t.Errorf("result[%d].Name = %q, want %q", i, r.Name, packages[i].name)
		}
	}
}

func TestDownloadSHA256Mismatch(t *testing.T) {
	srv := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("actual content"))
	}))

	dir := t.TempDir()
	mgr := downloader.New(dir, downloader.WithHTTPClient(srv.Client()))

	_, err := mgr.Download(context.Background(), []downloader.Request{
		{
			Name:     "badpkg",
			Version:  "1.0.0",
			URL:      srv.URL + "/badpkg.whl",
			SHA256:   "0000000000000000000000000000000000000000000000000000000000000000",
			Filename: "badpkg-1.0.0-py3-none-any.whl",
		},
	})
	if err == nil {
		t.Fatal("expected SHA256 mismatch error, got nil")
	}

	// Verify temp file was cleaned up.
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			t.Errorf("temp file %q was not cleaned up", e.Name())
		}
	}
}

func TestDownloadEmptySHA256Skips(t *testing.T) {
	content := []byte("some content no hash check")

	srv := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(content)
	}))

	dir := t.TempDir()
	mgr := downloader.New(dir, downloader.WithHTTPClient(srv.Client()))

	results, err := mgr.Download(context.Background(), []downloader.Request{
		{
			Name:     "nohash",
			Version:  "1.0.0",
			URL:      srv.URL + "/nohash.whl",
			SHA256:   "",
			Filename: "nohash-1.0.0-py3-none-any.whl",
		},
	})
	if err != nil {
		t.Fatalf("Download() error: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
}

func TestDownloadRetry(t *testing.T) {
	content := []byte("retry success content")
	hash := sha256Hex(content)

	var attempts atomic.Int32

	srv := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := attempts.Add(1)
		if n < 3 {
			w.WriteHeader(http.StatusInternalServerError)

			return
		}

		_, _ = w.Write(content)
	}))

	dir := t.TempDir()
	mgr := downloader.New(dir, downloader.WithHTTPClient(srv.Client()))

	results, err := mgr.Download(context.Background(), []downloader.Request{
		{
			Name:     "retrypkg",
			Version:  "1.0.0",
			URL:      srv.URL + "/retrypkg.whl",
			SHA256:   hash,
			Filename: "retrypkg-1.0.0-py3-none-any.whl",
		},
	})
	if err != nil {
		t.Fatalf("Download() error: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	if got := attempts.Load(); got != 3 {
		t.Errorf("expected 3 attempts, got %d", got)
	}
}

func TestDownloadRetriesExhausted(t *testing.T) {
	srv := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))

	dir := t.TempDir()
	mgr := downloader.New(dir, downloader.WithHTTPClient(srv.Client()))

	_, err := mgr.Download(context.Background(), []downloader.Request{
		{
			Name:     "failpkg",
			Version:  "1.0.0",
			URL:      srv.URL + "/failpkg.whl",
			SHA256:   "abc",
			Filename: "failpkg-1.0.0-py3-none-any.whl",
		},
	})
	if err == nil {
		t.Fatal("expected error after retries exhausted, got nil")
	}
}

func TestDownloadContextCanceled(t *testing.T) {
	srv := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("data"))
	}))

	dir := t.TempDir()
	mgr := downloader.New(dir, downloader.WithHTTPClient(srv.Client()))

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := mgr.Download(ctx, []downloader.Request{
		{
			Name:     "canceled",
			Version:  "1.0.0",
			URL:      srv.URL + "/canceled.whl",
			SHA256:   "",
			Filename: "canceled-1.0.0-py3-none-any.whl",
		},
	})
	if err == nil {
		t.Fatal("expected context canceled error, got nil")
	}
}

func TestDownloadHTTPNotFound(t *testing.T) {
	srv := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))

	dir := t.TempDir()
	mgr := downloader.New(dir, downloader.WithHTTPClient(srv.Client()))

	_, err := mgr.Download(context.Background(), []downloader.Request{
		{
			Name:     "missing",
			Version:  "1.0.0",
			URL:      srv.URL + "/missing.whl",
			SHA256:   "",
			Filename: "missing-1.0.0-py3-none-any.whl",
		},
	})
	if err == nil {
		t.Fatal("expected HTTP 404 error, got nil")
	}
}

func TestDownloadEmptyRequests(t *testing.T) {
	dir := t.TempDir()
	mgr := downloader.New(dir)

	results, err := mgr.Download(context.Background(), nil)
	if err != nil {
		t.Fatalf("Download(nil) error: %v", err)
	}

	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestWithMaxWorkersIgnoresInvalid(t *testing.T) {
	content := []byte("test")
	hash := sha256Hex(content)

	srv := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(content)
	}))

	dir := t.TempDir()

	// Zero and negative values should be ignored (use default).
	mgr := downloader.New(dir,
		downloader.WithHTTPClient(srv.Client()),
		downloader.WithMaxWorkers(0),
		downloader.WithMaxWorkers(-1),
	)

	results, err := mgr.Download(context.Background(), []downloader.Request{
		{
			Name:     "pkg",
			Version:  "1.0.0",
			URL:      srv.URL + "/pkg.whl",
			SHA256:   hash,
			Filename: "pkg-1.0.0-py3-none-any.whl",
		},
	})
	if err != nil {
		t.Fatalf("Download() error: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
}

func TestWithHTTPClientIgnoresNil(t *testing.T) {
	content := []byte("test")

	srv := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(content)
	}))

	dir := t.TempDir()

	// nil should be ignored, then set the real client.
	mgr := downloader.New(dir,
		downloader.WithHTTPClient(nil),
		downloader.WithHTTPClient(srv.Client()),
	)

	_, err := mgr.Download(context.Background(), []downloader.Request{
		{
			Name:     "pkg",
			Version:  "1.0.0",
			URL:      srv.URL + "/pkg.whl",
			SHA256:   "",
			Filename: "pkg-1.0.0-py3-none-any.whl",
		},
	})
	if err != nil {
		t.Fatalf("Download() error: %v", err)
	}
}

func TestWithLoggerIgnoresNil(t *testing.T) {
	// Should not panic when passing nil logger.
	dir := t.TempDir()
	_ = downloader.New(dir, downloader.WithLogger(nil))
}

// mockCache implements downloader.Cache for testing.
type mockCache struct {
	store map[string]string // filename → path
	puts  []string         // filenames that were Put
}

func newMockCache() *mockCache {
	return &mockCache{store: make(map[string]string)}
}

func (c *mockCache) Get(filename, _ string) (string, bool) {
	path, ok := c.store[filename]

	return path, ok
}

func (c *mockCache) Put(srcPath, filename string) error {
	c.puts = append(c.puts, filename)
	c.store[filename] = srcPath

	return nil
}

func TestDownloadFileRename(t *testing.T) {
	content := []byte("final destination content")
	hash := sha256Hex(content)

	srv := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(content)
	}))

	dir := t.TempDir()
	mgr := downloader.New(dir, downloader.WithHTTPClient(srv.Client()))

	_, err := mgr.Download(context.Background(), []downloader.Request{
		{
			Name:     "pkg",
			Version:  "1.0.0",
			URL:      srv.URL + "/pkg.whl",
			SHA256:   hash,
			Filename: "pkg-1.0.0-py3-none-any.whl",
		},
	})
	if err != nil {
		t.Fatalf("Download() error: %v", err)
	}

	// Verify final file exists and no .tmp remains.
	finalPath := filepath.Join(dir, "pkg-1.0.0-py3-none-any.whl")
	if _, err := os.Stat(finalPath); err != nil {
		t.Errorf("final file not found: %v", err)
	}

	tmpPath := finalPath + ".tmp"
	if _, err := os.Stat(tmpPath); err == nil {
		t.Error("temp file should not exist after successful download")
	}
}

func TestDownloadPartialFailure(t *testing.T) {
	content := []byte("good content")
	hash := sha256Hex(content)

	var reqCount atomic.Int32

	srv := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqCount.Add(1)

		if r.URL.Path == "/good.whl" {
			_, _ = w.Write(content)

			return
		}

		w.WriteHeader(http.StatusInternalServerError)
	}))

	dir := t.TempDir()
	mgr := downloader.New(dir, downloader.WithHTTPClient(srv.Client()))

	_, err := mgr.Download(context.Background(), []downloader.Request{
		{
			Name:     "good",
			Version:  "1.0.0",
			URL:      srv.URL + "/good.whl",
			SHA256:   hash,
			Filename: "good-1.0.0-py3-none-any.whl",
		},
		{
			Name:     "bad",
			Version:  "1.0.0",
			URL:      srv.URL + "/bad.whl",
			SHA256:   "",
			Filename: "bad-1.0.0-py3-none-any.whl",
		},
	})
	if err == nil {
		t.Fatal("expected error for partial failure, got nil")
	}

	fmt.Println("partial failure error:", err)
}

func TestDownloadCacheHit(t *testing.T) {
	// Create a cached file — no HTTP server needed.
	cacheDir := t.TempDir()
	content := []byte("cached wheel data")
	filename := "cached-1.0.0-py3-none-any.whl"
	cachedPath := filepath.Join(cacheDir, filename)

	if err := os.WriteFile(cachedPath, content, 0o644); err != nil {
		t.Fatal(err)
	}

	mc := newMockCache()
	mc.store[filename] = cachedPath

	dir := t.TempDir()
	mgr := downloader.New(dir, downloader.WithCache(mc))

	results, err := mgr.Download(context.Background(), []downloader.Request{
		{
			Name:     "cached",
			Version:  "1.0.0",
			URL:      "http://should-not-be-called/cached.whl",
			SHA256:   sha256Hex(content),
			Filename: filename,
		},
	})
	if err != nil {
		t.Fatalf("Download() error: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	if !results[0].Cached {
		t.Error("expected Cached=true for cache hit")
	}

	if results[0].FilePath != cachedPath {
		t.Errorf("FilePath = %q, want %q", results[0].FilePath, cachedPath)
	}

	if results[0].Size != int64(len(content)) {
		t.Errorf("Size = %d, want %d", results[0].Size, len(content))
	}
}

func TestDownloadCacheMissThenPut(t *testing.T) {
	content := []byte("fresh download")
	hash := sha256Hex(content)

	srv := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(content)
	}))

	mc := newMockCache()

	dir := t.TempDir()
	mgr := downloader.New(dir,
		downloader.WithHTTPClient(srv.Client()),
		downloader.WithCache(mc),
	)

	filename := "fresh-1.0.0-py3-none-any.whl"

	results, err := mgr.Download(context.Background(), []downloader.Request{
		{
			Name:     "fresh",
			Version:  "1.0.0",
			URL:      srv.URL + "/fresh.whl",
			SHA256:   hash,
			Filename: filename,
		},
	})
	if err != nil {
		t.Fatalf("Download() error: %v", err)
	}

	if results[0].Cached {
		t.Error("expected Cached=false for cache miss")
	}

	// Verify Put was called.
	if len(mc.puts) != 1 || mc.puts[0] != filename {
		t.Errorf("expected Put(%q), got %v", filename, mc.puts)
	}
}

func TestDownloadNilCacheNoEffect(t *testing.T) {
	content := []byte("no cache content")
	hash := sha256Hex(content)

	srv := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(content)
	}))

	dir := t.TempDir()
	mgr := downloader.New(dir,
		downloader.WithHTTPClient(srv.Client()),
		downloader.WithCache(nil),
	)

	results, err := mgr.Download(context.Background(), []downloader.Request{
		{
			Name:     "pkg",
			Version:  "1.0.0",
			URL:      srv.URL + "/pkg.whl",
			SHA256:   hash,
			Filename: "pkg-1.0.0-py3-none-any.whl",
		},
	})
	if err != nil {
		t.Fatalf("Download() error: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	if results[0].Cached {
		t.Error("expected Cached=false with nil cache")
	}
}
