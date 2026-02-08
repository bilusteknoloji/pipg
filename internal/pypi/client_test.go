package pypi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bilusteknoloji/pipg/internal/pypi"
)

func newTestPackageInfo() pypi.PackageInfo {
	return pypi.PackageInfo{
		Info: pypi.Info{
			Name:           "six",
			Version:        "1.17.0",
			Summary:        "Python 2 and 3 compatibility utilities",
			RequiresDist:   nil,
			RequiresPython: ">=2.7, !=3.0.*, !=3.1.*, !=3.2.*",
		},
		URLs: []pypi.URL{
			{
				Filename:      "six-1.17.0-py2.py3-none-any.whl",
				URL:           "https://files.pythonhosted.org/six-1.17.0-py2.py3-none-any.whl",
				Size:          11475,
				PackageType:   "bdist_wheel",
				PythonVersion: "py2.py3",
				Digests: pypi.Digests{
					SHA256:     "4721f391ed90541fddacab5acf947aa0d3dc7d27b2e1e8eda2be8970586c3274",
					MD5:        "090bac7d568f9c1f64b671de641ccdee",
					Blake2b256: "b7ce149a00dd41f10bc29e5921b496af8b574d8413afcd5e30dfa0ed46c2cc5e",
				},
			},
		},
	}
}

func encodeJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()

	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Errorf("encoding JSON response: %v", err)
	}
}

func newTestClient(t *testing.T, handler http.HandlerFunc) pypi.Client {
	t.Helper()

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	return pypi.New(
		pypi.WithHTTPClient(srv.Client()),
		pypi.WithBaseURL(srv.URL+"/pypi"),
	)
}

func TestGetPackage(t *testing.T) {
	expected := newTestPackageInfo()

	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/pypi/six/json" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)

			return
		}

		if got := r.Header.Get("Accept"); got != "application/json" {
			t.Errorf("expected Accept: application/json, got %q", got)
		}

		w.Header().Set("Content-Type", "application/json")
		encodeJSON(t, w, expected)
	})

	info, err := client.GetPackage(context.Background(), "six")
	if err != nil {
		t.Fatalf("GetPackage() error: %v", err)
	}

	if info.Info.Name != "six" {
		t.Errorf("expected name %q, got %q", "six", info.Info.Name)
	}
	if info.Info.Version != "1.17.0" {
		t.Errorf("expected version %q, got %q", "1.17.0", info.Info.Version)
	}
	if len(info.URLs) != 1 {
		t.Fatalf("expected 1 URL, got %d", len(info.URLs))
	}
	if info.URLs[0].PackageType != "bdist_wheel" {
		t.Errorf("expected packagetype %q, got %q", "bdist_wheel", info.URLs[0].PackageType)
	}
	if info.URLs[0].Digests.SHA256 != expected.URLs[0].Digests.SHA256 {
		t.Errorf("expected sha256 %q, got %q", expected.URLs[0].Digests.SHA256, info.URLs[0].Digests.SHA256)
	}
}

func TestGetPackageVersion(t *testing.T) {
	expected := newTestPackageInfo()

	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/pypi/six/1.17.0/json" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)

			return
		}

		w.Header().Set("Content-Type", "application/json")
		encodeJSON(t, w, expected)
	})

	info, err := client.GetPackageVersion(context.Background(), "six", "1.17.0")
	if err != nil {
		t.Fatalf("GetPackageVersion() error: %v", err)
	}

	if info.Info.Version != "1.17.0" {
		t.Errorf("expected version %q, got %q", "1.17.0", info.Info.Version)
	}
}

func TestGetPackageNotFound(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	})

	_, err := client.GetPackage(context.Background(), "nonexistent-package-xyz")
	if err == nil {
		t.Fatal("expected error for non-existent package, got nil")
	}
}

func TestGetPackageServerError(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	})

	_, err := client.GetPackage(context.Background(), "some-package")
	if err == nil {
		t.Fatal("expected error for server error response, got nil")
	}
}

func TestGetPackageInvalidJSON(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(`{invalid json`)); err != nil {
			t.Errorf("writing response: %v", err)
		}
	})

	_, err := client.GetPackage(context.Background(), "some-package")
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestGetPackageContextCanceled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		time.Sleep(5 * time.Second)
	}))
	t.Cleanup(srv.Close)

	client := pypi.New(
		pypi.WithHTTPClient(srv.Client()),
		pypi.WithBaseURL(srv.URL+"/pypi"),
	)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := client.GetPackage(ctx, "some-package")
	if err == nil {
		t.Fatal("expected error for canceled context, got nil")
	}
}

func TestGetPackageRetry(t *testing.T) {
	attempts := 0
	expected := newTestPackageInfo()

	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		if attempts < 3 {
			http.Error(w, "server error", http.StatusInternalServerError)

			return
		}

		w.Header().Set("Content-Type", "application/json")
		encodeJSON(t, w, expected)
	})

	info, err := client.GetPackage(context.Background(), "six")
	if err != nil {
		t.Fatalf("GetPackage() error after retries: %v", err)
	}

	if info.Info.Name != "six" {
		t.Errorf("expected name %q, got %q", "six", info.Info.Name)
	}
	if attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts)
	}
}

func TestGetPackageRetriesExhausted(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "server error", http.StatusInternalServerError)
	})

	_, err := client.GetPackage(context.Background(), "some-package")
	if err == nil {
		t.Fatal("expected error after retries exhausted, got nil")
	}
}

func TestGetPackageRequiresDist(t *testing.T) {
	pkg := pypi.PackageInfo{
		Info: pypi.Info{
			Name:    "flask",
			Version: "3.0.0",
			RequiresDist: []string{
				"blinker>=1.9.0",
				"click>=8.1.3",
				`importlib-metadata>=3.6.0; python_version < "3.10"`,
				"itsdangerous>=2.2.0",
				"jinja2>=3.1.2",
			},
		},
	}

	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		encodeJSON(t, w, pkg)
	})

	info, err := client.GetPackage(context.Background(), "flask")
	if err != nil {
		t.Fatalf("GetPackage() error: %v", err)
	}

	if len(info.Info.RequiresDist) != 5 {
		t.Fatalf("expected 5 requires_dist entries, got %d", len(info.Info.RequiresDist))
	}
	if info.Info.RequiresDist[0] != "blinker>=1.9.0" {
		t.Errorf("expected first dep %q, got %q", "blinker>=1.9.0", info.Info.RequiresDist[0])
	}
}
