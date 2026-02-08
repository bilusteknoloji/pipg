package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
)

// Store defines the interface for wheel caching.
type Store interface {
	Get(filename, expectedSHA256 string) (path string, ok bool)
	Put(srcPath, filename string) error
}

// Option configures a Manager.
type Option func(*Manager)

// WithDir sets the cache directory. Overrides platform default.
func WithDir(dir string) Option {
	return func(m *Manager) {
		if dir != "" {
			m.dir = dir
		}
	}
}

// WithLogger sets the structured logger.
func WithLogger(l *slog.Logger) Option {
	return func(m *Manager) {
		if l != nil {
			m.logger = l
		}
	}
}

// Manager manages a local wheel cache directory.
type Manager struct {
	dir    string
	logger *slog.Logger
}

// compile-time proof that Manager implements Store.
var _ Store = (*Manager)(nil)

// New creates a new cache manager. If no dir is specified via WithDir or
// PIPG_CACHE_DIR, a platform-appropriate default is used.
func New(opts ...Option) (*Manager, error) {
	m := &Manager{
		logger: slog.Default(),
	}

	for _, opt := range opts {
		opt(m)
	}

	if m.dir == "" {
		m.dir = defaultCacheDir()
	}

	if err := os.MkdirAll(m.dir, 0o755); err != nil {
		return nil, fmt.Errorf("creating cache directory %s: %w", m.dir, err)
	}

	return m, nil
}

// Get checks whether a cached wheel with the given filename and SHA256 exists.
// Returns the full path and true if found and valid. If the file exists but the
// hash does not match, the stale file is removed and ok is false.
func (m *Manager) Get(filename, expectedSHA256 string) (string, bool) {
	path := filepath.Join(m.dir, filename)

	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return "", false
	}

	if expectedSHA256 != "" {
		got, err := hashFile(path)
		if err != nil {
			m.logger.Debug("cache hash error, removing", slog.String("file", filename), slog.String("error", err.Error()))
			_ = os.Remove(path)

			return "", false
		}

		if got != expectedSHA256 {
			m.logger.Debug("cache hash mismatch, removing", slog.String("file", filename))
			_ = os.Remove(path)

			return "", false
		}
	}

	m.logger.Debug("cache hit", slog.String("file", filename))

	return path, true
}

// Put copies srcPath into the cache under filename using atomic rename.
func (m *Manager) Put(srcPath, filename string) error {
	dstPath := filepath.Join(m.dir, filename)
	tmpPath := dstPath + ".tmp"

	src, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("opening source %s: %w", srcPath, err)
	}
	defer func() { _ = src.Close() }()

	dst, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("creating temp file %s: %w", tmpPath, err)
	}

	if _, err := io.Copy(dst, src); err != nil {
		_ = dst.Close()
		_ = os.Remove(tmpPath)

		return fmt.Errorf("copying to cache: %w", err)
	}

	if err := dst.Close(); err != nil {
		_ = os.Remove(tmpPath)

		return fmt.Errorf("closing temp file: %w", err)
	}

	if err := os.Rename(tmpPath, dstPath); err != nil {
		_ = os.Remove(tmpPath)

		return fmt.Errorf("renaming cache file: %w", err)
	}

	m.logger.Debug("cached", slog.String("file", filename))

	return nil
}

// hashFile computes the SHA256 hex digest of the file at path.
func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

// defaultCacheDir returns the platform-appropriate cache directory.
// Priority: PIPG_CACHE_DIR > platform default.
func defaultCacheDir() string {
	if dir := os.Getenv("PIPG_CACHE_DIR"); dir != "" {
		return dir
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "pipg", "wheels")
	}

	if runtime.GOOS == "darwin" {
		return filepath.Join(home, "Library", "Caches", "pipg", "wheels")
	}

	// Linux / other: respect XDG_CACHE_HOME.
	if xdg := os.Getenv("XDG_CACHE_HOME"); xdg != "" {
		return filepath.Join(xdg, "pipg", "wheels")
	}

	return filepath.Join(home, ".cache", "pipg", "wheels")
}
