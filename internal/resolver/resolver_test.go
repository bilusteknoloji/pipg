package resolver_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/bilusteknoloji/pipg/internal/pypi"
	"github.com/bilusteknoloji/pipg/internal/resolver"
)

// mockClient implements pypi.Client for testing.
type mockClient struct {
	packages map[string]*pypi.PackageInfo
}

func (m *mockClient) GetPackage(_ context.Context, name string) (*pypi.PackageInfo, error) {
	info, ok := m.packages[name]
	if !ok {
		return nil, fmt.Errorf("package not found: %s", name)
	}

	return info, nil
}

func (m *mockClient) GetPackageVersion(_ context.Context, name, version string) (*pypi.PackageInfo, error) {
	key := name + "@" + version

	if info, ok := m.packages[key]; ok {
		return info, nil
	}

	return m.GetPackage(context.Background(), name)
}

func releases(versions ...string) map[string][]pypi.URL {
	r := make(map[string][]pypi.URL, len(versions))
	for _, v := range versions {
		r[v] = []pypi.URL{{Filename: "pkg-" + v + "-py3-none-any.whl"}}
	}

	return r
}

func TestResolveSimplePackage(t *testing.T) {
	client := &mockClient{
		packages: map[string]*pypi.PackageInfo{
			"six": {
				Info:     pypi.Info{Name: "six", Version: "1.17.0"},
				Releases: releases("1.16.0", "1.17.0"),
			},
		},
	}

	svc := resolver.New(client)
	result, err := svc.Resolve(context.Background(), []string{"six"})
	if err != nil {
		t.Fatalf("Resolve() error: %v", err)
	}

	if len(result) != 1 {
		t.Fatalf("expected 1 package, got %d", len(result))
	}

	if result[0].Name != "six" {
		t.Errorf("expected name %q, got %q", "six", result[0].Name)
	}

	if result[0].Version != "1.17.0" {
		t.Errorf("expected version %q, got %q", "1.17.0", result[0].Version)
	}
}

func TestResolveWithVersionConstraint(t *testing.T) {
	client := &mockClient{
		packages: map[string]*pypi.PackageInfo{
			"six": {
				Info:     pypi.Info{Name: "six", Version: "1.17.0"},
				Releases: releases("1.15.0", "1.16.0", "1.17.0"),
			},
		},
	}

	svc := resolver.New(client)
	result, err := svc.Resolve(context.Background(), []string{"six<1.17"})
	if err != nil {
		t.Fatalf("Resolve() error: %v", err)
	}

	if len(result) != 1 {
		t.Fatalf("expected 1 package, got %d", len(result))
	}

	if result[0].Version != "1.16.0" {
		t.Errorf("expected version %q, got %q", "1.16.0", result[0].Version)
	}
}

func TestResolveWithDependencies(t *testing.T) {
	client := &mockClient{
		packages: map[string]*pypi.PackageInfo{
			"flask": {
				Info: pypi.Info{
					Name:    "flask",
					Version: "3.0.0",
					RequiresDist: []string{
						"werkzeug>=3.0.0",
						"jinja2>=3.1.2",
					},
				},
				Releases: releases("3.0.0"),
			},
			"werkzeug": {
				Info:     pypi.Info{Name: "werkzeug", Version: "3.0.1"},
				Releases: releases("3.0.0", "3.0.1"),
			},
			"jinja2": {
				Info:     pypi.Info{Name: "jinja2", Version: "3.1.3"},
				Releases: releases("3.1.2", "3.1.3"),
			},
		},
	}

	svc := resolver.New(client)
	result, err := svc.Resolve(context.Background(), []string{"flask"})
	if err != nil {
		t.Fatalf("Resolve() error: %v", err)
	}

	if len(result) != 3 {
		t.Fatalf("expected 3 packages, got %d", len(result))
	}

	resolved := make(map[string]string)
	for _, pkg := range result {
		resolved[pkg.Name] = pkg.Version
	}

	if resolved["flask"] != "3.0.0" {
		t.Errorf("flask: expected %q, got %q", "3.0.0", resolved["flask"])
	}

	if resolved["werkzeug"] != "3.0.1" {
		t.Errorf("werkzeug: expected %q, got %q", "3.0.1", resolved["werkzeug"])
	}

	if resolved["jinja2"] != "3.1.3" {
		t.Errorf("jinja2: expected %q, got %q", "3.1.3", resolved["jinja2"])
	}
}

func TestResolveNoDeps(t *testing.T) {
	client := &mockClient{
		packages: map[string]*pypi.PackageInfo{
			"flask": {
				Info: pypi.Info{
					Name:    "flask",
					Version: "3.0.0",
					RequiresDist: []string{
						"werkzeug>=3.0.0",
					},
				},
				Releases: releases("3.0.0"),
			},
		},
	}

	svc := resolver.New(client, resolver.WithNoDeps(true))
	result, err := svc.Resolve(context.Background(), []string{"flask"})
	if err != nil {
		t.Fatalf("Resolve() error: %v", err)
	}

	if len(result) != 1 {
		t.Fatalf("expected 1 package (no-deps), got %d", len(result))
	}

	if result[0].Name != "flask" {
		t.Errorf("expected %q, got %q", "flask", result[0].Name)
	}
}

func TestResolveSkipsMarkerMismatch(t *testing.T) {
	client := &mockClient{
		packages: map[string]*pypi.PackageInfo{
			"flask": {
				Info: pypi.Info{
					Name:    "flask",
					Version: "3.0.0",
					RequiresDist: []string{
						"werkzeug>=3.0.0",
						`importlib-metadata>=3.6.0; python_version < "3.10"`,
					},
				},
				Releases: releases("3.0.0"),
			},
			"werkzeug": {
				Info:     pypi.Info{Name: "werkzeug", Version: "3.0.1"},
				Releases: releases("3.0.1"),
			},
		},
	}

	env := resolver.MarkerEnv{PythonVersion: "3.12", SysPlatform: "linux", OsName: "posix"}
	svc := resolver.New(client, resolver.WithMarkerEnv(env))

	result, err := svc.Resolve(context.Background(), []string{"flask"})
	if err != nil {
		t.Fatalf("Resolve() error: %v", err)
	}

	resolved := make(map[string]string)
	for _, pkg := range result {
		resolved[pkg.Name] = pkg.Version
	}

	if _, ok := resolved["importlib-metadata"]; ok {
		t.Error("importlib-metadata should be skipped for python 3.12")
	}

	if len(result) != 2 {
		t.Fatalf("expected 2 packages (flask + werkzeug), got %d", len(result))
	}
}

func TestResolveVersionConflict(t *testing.T) {
	client := &mockClient{
		packages: map[string]*pypi.PackageInfo{
			"a": {
				Info: pypi.Info{
					Name:         "a",
					Version:      "1.0.0",
					RequiresDist: []string{"shared>=2.0"},
				},
				Releases: releases("1.0.0"),
			},
			"b": {
				Info: pypi.Info{
					Name:         "b",
					Version:      "1.0.0",
					RequiresDist: []string{"shared<2.0"},
				},
				Releases: releases("1.0.0"),
			},
			"shared": {
				Info:     pypi.Info{Name: "shared", Version: "2.1.0"},
				Releases: releases("1.0.0", "1.9.0", "2.0.0", "2.1.0"),
			},
		},
	}

	svc := resolver.New(client)
	_, err := svc.Resolve(context.Background(), []string{"a", "b"})
	if err == nil {
		t.Fatal("expected version conflict error, got nil")
	}
}

func TestResolvePackageNotFound(t *testing.T) {
	client := &mockClient{packages: map[string]*pypi.PackageInfo{}}

	svc := resolver.New(client)
	_, err := svc.Resolve(context.Background(), []string{"nonexistent"})
	if err == nil {
		t.Fatal("expected error for non-existent package, got nil")
	}
}

func TestResolveNoCompatibleVersion(t *testing.T) {
	client := &mockClient{
		packages: map[string]*pypi.PackageInfo{
			"pkg": {
				Info:     pypi.Info{Name: "pkg", Version: "1.0.0"},
				Releases: releases("1.0.0"),
			},
		},
	}

	svc := resolver.New(client)
	_, err := svc.Resolve(context.Background(), []string{"pkg>=5.0"})
	if err == nil {
		t.Fatal("expected error for no compatible version, got nil")
	}
}

func TestResolveCircularDeps(t *testing.T) {
	client := &mockClient{
		packages: map[string]*pypi.PackageInfo{
			"a": {
				Info: pypi.Info{
					Name:         "a",
					Version:      "1.0.0",
					RequiresDist: []string{"b>=1.0"},
				},
				Releases: releases("1.0.0"),
			},
			"b": {
				Info: pypi.Info{
					Name:         "b",
					Version:      "1.0.0",
					RequiresDist: []string{"a>=1.0"},
				},
				Releases: releases("1.0.0"),
			},
		},
	}

	svc := resolver.New(client)
	result, err := svc.Resolve(context.Background(), []string{"a"})
	if err != nil {
		t.Fatalf("Resolve() error on circular deps: %v", err)
	}

	if len(result) != 2 {
		t.Fatalf("expected 2 packages, got %d", len(result))
	}
}

func TestResolveMultipleRoots(t *testing.T) {
	client := &mockClient{
		packages: map[string]*pypi.PackageInfo{
			"requests": {
				Info:     pypi.Info{Name: "requests", Version: "2.31.0"},
				Releases: releases("2.31.0"),
			},
			"six": {
				Info:     pypi.Info{Name: "six", Version: "1.17.0"},
				Releases: releases("1.17.0"),
			},
		},
	}

	svc := resolver.New(client)
	result, err := svc.Resolve(context.Background(), []string{"requests", "six"})
	if err != nil {
		t.Fatalf("Resolve() error: %v", err)
	}

	if len(result) != 2 {
		t.Fatalf("expected 2 packages, got %d", len(result))
	}
}
