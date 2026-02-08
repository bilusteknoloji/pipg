package cache_test

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/bilusteknoloji/pipg/internal/cache"
)

func sha256Hex(data []byte) string {
	h := sha256.Sum256(data)

	return hex.EncodeToString(h[:])
}

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()

	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("writing file %s: %v", path, err)
	}
}

func TestGetHit(t *testing.T) {
	dir := t.TempDir()

	content := []byte("wheel content")
	hash := sha256Hex(content)
	filename := "pkg-1.0.0-py3-none-any.whl"

	writeFile(t, filepath.Join(dir, filename), content)

	m, err := cache.New(cache.WithDir(dir))
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	path, ok := m.Get(filename, hash)
	if !ok {
		t.Fatal("expected cache hit, got miss")
	}

	if path != filepath.Join(dir, filename) {
		t.Errorf("path = %q, want %q", path, filepath.Join(dir, filename))
	}
}

func TestGetMiss(t *testing.T) {
	dir := t.TempDir()

	m, err := cache.New(cache.WithDir(dir))
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	_, ok := m.Get("nonexistent.whl", "abc")
	if ok {
		t.Fatal("expected cache miss, got hit")
	}
}

func TestGetSHA256Mismatch(t *testing.T) {
	dir := t.TempDir()

	content := []byte("original content")
	filename := "pkg-1.0.0-py3-none-any.whl"

	writeFile(t, filepath.Join(dir, filename), content)

	m, err := cache.New(cache.WithDir(dir))
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	_, ok := m.Get(filename, "0000000000000000000000000000000000000000000000000000000000000000")
	if ok {
		t.Fatal("expected cache miss on hash mismatch, got hit")
	}

	// File should have been removed.
	if _, err := os.Stat(filepath.Join(dir, filename)); err == nil {
		t.Error("stale cache file should have been removed")
	}
}

func TestGetEmptySHA256SkipsVerification(t *testing.T) {
	dir := t.TempDir()

	content := []byte("any content")
	filename := "pkg-1.0.0-py3-none-any.whl"

	writeFile(t, filepath.Join(dir, filename), content)

	m, err := cache.New(cache.WithDir(dir))
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	path, ok := m.Get(filename, "")
	if !ok {
		t.Fatal("expected cache hit with empty SHA256, got miss")
	}

	if path != filepath.Join(dir, filename) {
		t.Errorf("path = %q, want %q", path, filepath.Join(dir, filename))
	}
}

func TestPut(t *testing.T) {
	srcDir := t.TempDir()
	cacheDir := t.TempDir()

	content := []byte("wheel data")
	srcPath := filepath.Join(srcDir, "download.whl")

	writeFile(t, srcPath, content)

	m, err := cache.New(cache.WithDir(cacheDir))
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	filename := "pkg-1.0.0-py3-none-any.whl"
	if err := m.Put(srcPath, filename); err != nil {
		t.Fatalf("Put() error: %v", err)
	}

	// Verify cached file exists and matches.
	got, err := os.ReadFile(filepath.Join(cacheDir, filename))
	if err != nil {
		t.Fatalf("reading cached file: %v", err)
	}

	if string(got) != string(content) {
		t.Error("cached file content does not match source")
	}

	// Verify no .tmp file remains.
	entries, _ := os.ReadDir(cacheDir)
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			t.Errorf("temp file %q should not remain", e.Name())
		}
	}
}

func TestPutOverwritesExisting(t *testing.T) {
	cacheDir := t.TempDir()
	srcDir := t.TempDir()

	filename := "pkg-1.0.0-py3-none-any.whl"
	writeFile(t, filepath.Join(cacheDir, filename), []byte("old"))

	srcPath := filepath.Join(srcDir, "new.whl")
	writeFile(t, srcPath, []byte("new content"))

	m, err := cache.New(cache.WithDir(cacheDir))
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	if err := m.Put(srcPath, filename); err != nil {
		t.Fatalf("Put() error: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(cacheDir, filename))
	if err != nil {
		t.Fatalf("reading cached file: %v", err)
	}

	if string(got) != "new content" {
		t.Errorf("cached content = %q, want %q", got, "new content")
	}
}

func TestConcurrentPut(t *testing.T) {
	cacheDir := t.TempDir()
	srcDir := t.TempDir()

	m, err := cache.New(cache.WithDir(cacheDir))
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	var wg sync.WaitGroup

	for i := range 10 {
		wg.Add(1)

		go func(n int) {
			defer wg.Done()

			content := []byte("content-" + string(rune('A'+n)))
			src := filepath.Join(srcDir, "src-"+string(rune('A'+n))+".whl")

			writeFile(t, src, content)

			_ = m.Put(src, "shared.whl")
		}(i)
	}

	wg.Wait()

	// File should exist (one of the concurrent writes wins).
	if _, err := os.Stat(filepath.Join(cacheDir, "shared.whl")); err != nil {
		t.Errorf("expected cached file to exist: %v", err)
	}
}

func TestNewCreatesDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sub", "cache")

	_, err := cache.New(cache.WithDir(dir))
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("cache directory not created: %v", err)
	}

	if !info.IsDir() {
		t.Error("expected directory, got file")
	}
}

func TestNewWithEnvVar(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "env-cache")
	t.Setenv("PIPG_CACHE_DIR", dir)

	m, err := cache.New()
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	// Verify it uses the env var directory by caching a file there.
	srcDir := t.TempDir()
	srcPath := filepath.Join(srcDir, "test.whl")

	writeFile(t, srcPath, []byte("data"))

	if err := m.Put(srcPath, "test.whl"); err != nil {
		t.Fatalf("Put() error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "test.whl")); err != nil {
		t.Errorf("file not found in PIPG_CACHE_DIR: %v", err)
	}
}
