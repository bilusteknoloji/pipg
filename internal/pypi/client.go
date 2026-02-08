package pypi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"time"
)

const (
	defaultBaseURL = "https://pypi.org/pypi"
	maxRetries     = 3
	clientTimeout  = 30 * time.Second
)

// Client defines the interface for communicating with the PyPI JSON API.
type Client interface {
	GetPackage(ctx context.Context, name string) (*PackageInfo, error)
	GetPackageVersion(ctx context.Context, name, version string) (*PackageInfo, error)
}

// Option configures a Service.
type Option func(*Service)

// WithHTTPClient sets the HTTP client used for API requests.
func WithHTTPClient(c *http.Client) Option {
	return func(s *Service) {
		if c != nil {
			s.httpClient = c
		}
	}
}

// WithBaseURL sets a custom base URL (useful for testing with httptest.Server).
func WithBaseURL(url string) Option {
	return func(s *Service) {
		if url != "" {
			s.baseURL = url
		}
	}
}

// WithLogger sets the structured logger.
func WithLogger(l *slog.Logger) Option {
	return func(s *Service) {
		if l != nil {
			s.logger = l
		}
	}
}

// Service communicates with the PyPI JSON API over HTTP.
type Service struct {
	httpClient *http.Client
	baseURL    string
	logger     *slog.Logger
}

// compile-time proof that Service implements Client.
var _ Client = (*Service)(nil)

// New creates a new PyPI API service.
func New(opts ...Option) *Service {
	s := &Service{
		httpClient: &http.Client{Timeout: clientTimeout},
		baseURL:    defaultBaseURL,
		logger:     slog.Default(),
	}

	for _, opt := range opts {
		opt(s)
	}

	return s
}

// GetPackage fetches metadata for a package from PyPI.
// Endpoint: GET {baseURL}/{package_name}/json
func (s *Service) GetPackage(ctx context.Context, name string) (*PackageInfo, error) {
	url := fmt.Sprintf("%s/%s/json", s.baseURL, name)

	return s.fetch(ctx, url, name)
}

// GetPackageVersion fetches metadata for a specific version of a package.
// Endpoint: GET {baseURL}/{package_name}/{version}/json
func (s *Service) GetPackageVersion(ctx context.Context, name, version string) (*PackageInfo, error) {
	url := fmt.Sprintf("%s/%s/%s/json", s.baseURL, name, version)

	return s.fetch(ctx, url, name)
}

// fetch performs an HTTP GET with retry and exponential backoff, then decodes the response.
// Only transient errors (5xx, network errors) are retried; permanent errors (404, bad JSON)
// are returned immediately.
func (s *Service) fetch(ctx context.Context, url, name string) (*PackageInfo, error) {
	var lastErr error

	for attempt := range maxRetries {
		if attempt > 0 {
			backoff := time.Duration(math.Pow(2, float64(attempt))) * 500 * time.Millisecond
			s.logger.Debug("retrying PyPI request",
				slog.String("package", name),
				slog.Int("attempt", attempt+1),
				slog.Duration("backoff", backoff),
			)

			select {
			case <-ctx.Done():
				return nil, fmt.Errorf("fetching %s: %w", name, ctx.Err())
			case <-time.After(backoff):
			}
		}

		info, err := s.doRequest(ctx, url)
		if err == nil {
			return info, nil
		}

		var re *retryableError
		if !errors.As(err, &re) {
			return nil, fmt.Errorf("fetching %s: %w", name, err)
		}

		lastErr = err
		s.logger.Debug("PyPI request failed",
			slog.String("package", name),
			slog.Int("attempt", attempt+1),
			slog.String("error", err.Error()),
		)
	}

	return nil, fmt.Errorf("fetching %s after %d attempts: %w", name, maxRetries, lastErr)
}

// retryableError indicates a transient error that should be retried.
type retryableError struct {
	err error
}

func (e *retryableError) Error() string { return e.err.Error() }
func (e *retryableError) Unwrap() error { return e.err }

// doRequest performs a single HTTP GET and decodes the JSON response.
// Returns a retryableError for transient failures (5xx, network errors).
func (s *Service) doRequest(ctx context.Context, url string) (*PackageInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request for %s: %w", url, err)
	}

	req.Header.Set("Accept", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, &retryableError{err: fmt.Errorf("requesting %s: %w", url, err)}
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("package not found at %s", url)
	}

	if resp.StatusCode >= http.StatusInternalServerError {
		return nil, &retryableError{err: fmt.Errorf("server error %d from %s", resp.StatusCode, url)}
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d from %s", resp.StatusCode, url)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &retryableError{err: fmt.Errorf("reading response from %s: %w", url, err)}
	}

	var info PackageInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return nil, fmt.Errorf("decoding response from %s: %w", url, err)
	}

	return &info, nil
}
