package downloader

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
)

const maxRetries = 3

// retryableError wraps errors that are transient and can be retried.
type retryableError struct {
	err error
}

func (e *retryableError) Error() string { return e.err.Error() }
func (e *retryableError) Unwrap() error { return e.err }

// Downloader defines the interface for downloading resolved packages.
type Downloader interface {
	Download(ctx context.Context, requests []Request) ([]Result, error)
}

// Request describes a single file to download.
type Request struct {
	Name     string // package name
	Version  string // resolved version
	URL      string // direct download URL
	SHA256   string // expected sha256 hex digest
	Filename string // e.g., "flask-3.0.0-py3-none-any.whl"
}

// Result represents the outcome of downloading a single package.
type Result struct {
	Name     string
	Version  string
	FilePath string // path to the downloaded .whl file
	Size     int64
}

// Option configures a Manager.
type Option func(*Manager)

// WithMaxWorkers sets the maximum number of concurrent download workers.
// Defaults to runtime.GOMAXPROCS(0).
func WithMaxWorkers(n int) Option {
	return func(m *Manager) {
		if n > 0 {
			m.maxWorkers = n
		}
	}
}

// WithHTTPClient sets the HTTP client used for downloads.
func WithHTTPClient(c *http.Client) Option {
	return func(m *Manager) {
		if c != nil {
			m.httpClient = c
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

// Manager manages concurrent package downloads using errgroup.
type Manager struct {
	targetDir  string
	maxWorkers int
	httpClient *http.Client
	logger     *slog.Logger
}

// compile-time proof that Manager implements Downloader.
var _ Downloader = (*Manager)(nil)

// New creates a new concurrent download manager for the given target directory.
func New(targetDir string, opts ...Option) *Manager {
	m := &Manager{
		targetDir:  targetDir,
		maxWorkers: runtime.GOMAXPROCS(0),
		httpClient: &http.Client{},
		logger:     slog.Default(),
	}

	for _, opt := range opts {
		opt(m)
	}

	return m
}

// Download downloads all requested packages concurrently.
// Each download verifies the SHA256 hash against the expected digest.
// Returns the list of downloaded files or the first error encountered.
func (m *Manager) Download(ctx context.Context, requests []Request) ([]Result, error) {
	results := make([]Result, len(requests))

	var mu sync.Mutex

	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(m.maxWorkers)

	for i, req := range requests {
		g.Go(func() error {
			m.logger.Debug("downloading", slog.String("package", req.Name), slog.String("url", req.URL))

			result, err := m.downloadWithRetry(ctx, req)
			if err != nil {
				return fmt.Errorf("downloading %s: %w", req.Name, err)
			}

			mu.Lock()
			results[i] = result
			mu.Unlock()

			m.logger.Debug("downloaded",
				slog.String("package", req.Name),
				slog.Int64("size", result.Size),
			)

			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}

	return results, nil
}

// downloadWithRetry attempts to download a file up to maxRetries times
// with exponential backoff between attempts.
func (m *Manager) downloadWithRetry(ctx context.Context, req Request) (Result, error) {
	var lastErr error

	for attempt := range maxRetries {
		if attempt > 0 {
			backoff := time.Duration(math.Pow(2, float64(attempt))) * 500 * time.Millisecond
			m.logger.Debug("retrying download",
				slog.String("package", req.Name),
				slog.Int("attempt", attempt+1),
				slog.Duration("backoff", backoff),
			)

			select {
			case <-ctx.Done():
				return Result{}, fmt.Errorf("download canceled: %w", ctx.Err())
			case <-time.After(backoff):
			}
		}

		result, err := m.doDownload(ctx, req)
		if err == nil {
			return result, nil
		}

		// Only retry transient errors (5xx, network). Permanent errors
		// (4xx, sha256 mismatch) fail immediately.
		var re *retryableError
		if !errors.As(err, &re) {
			return Result{}, err
		}

		lastErr = err
		m.logger.Debug("download attempt failed",
			slog.String("package", req.Name),
			slog.Int("attempt", attempt+1),
			slog.String("error", err.Error()),
		)
	}

	return Result{}, fmt.Errorf("after %d attempts: %w", maxRetries, lastErr)
}

// doDownload performs a single download: HTTP GET → temp file → verify hash → rename.
func (m *Manager) doDownload(ctx context.Context, req Request) (Result, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, req.URL, nil)
	if err != nil {
		return Result{}, fmt.Errorf("creating request: %w", err)
	}

	resp, err := m.httpClient.Do(httpReq)
	if err != nil {
		// Network errors are transient and retryable.
		return Result{}, &retryableError{err: fmt.Errorf("requesting %s: %w", req.URL, err)}
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		err := fmt.Errorf("unexpected status %d from %s", resp.StatusCode, req.URL)

		// 5xx errors are transient; 4xx are permanent.
		if resp.StatusCode >= http.StatusInternalServerError {
			return Result{}, &retryableError{err: err}
		}

		return Result{}, err
	}

	destPath := filepath.Join(m.targetDir, req.Filename)
	tmpPath := destPath + ".tmp"

	f, err := os.Create(tmpPath)
	if err != nil {
		return Result{}, fmt.Errorf("creating temp file: %w", err)
	}

	// Stream to file and hash simultaneously.
	h := sha256.New()
	size, copyErr := io.Copy(io.MultiWriter(f, h), resp.Body)

	// Always close the file before handling errors.
	if err := f.Close(); err != nil && copyErr == nil {
		copyErr = fmt.Errorf("closing temp file: %w", err)
	}

	if copyErr != nil {
		_ = os.Remove(tmpPath)

		return Result{}, fmt.Errorf("writing %s: %w", req.Filename, copyErr)
	}

	// Verify SHA256 hash.
	if req.SHA256 != "" {
		got := hex.EncodeToString(h.Sum(nil))
		if got != req.SHA256 {
			_ = os.Remove(tmpPath)

			return Result{}, fmt.Errorf("sha256 mismatch for %s: expected %s, got %s",
				req.Filename, req.SHA256, got)
		}
	}

	// Rename to final path.
	if err := os.Rename(tmpPath, destPath); err != nil {
		_ = os.Remove(tmpPath)

		return Result{}, fmt.Errorf("renaming %s: %w", req.Filename, err)
	}

	return Result{
		Name:     req.Name,
		Version:  req.Version,
		FilePath: destPath,
		Size:     size,
	}, nil
}
