package python_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/bilusteknoloji/pipg/internal/python"
)

func fakeRunner(output string, err error) python.CommandRunner {
	return func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		return []byte(output), err
	}
}

func fakeEnv(vars map[string]string) python.EnvLookup {
	return func(key string) string {
		return vars[key]
	}
}

func TestDetectVirtualEnv(t *testing.T) {
	svc := python.New(
		python.WithCommandRunner(fakeRunner(
			"/home/user/myproject/.venv\n"+
				"/home/user/myproject/.venv/lib/python3.12/site-packages\n"+
				"linux-x86_64\n"+
				"312\n"+
				"/home/user/myproject/.venv/bin/python3\n", nil,
		)),
		python.WithEnvLookup(fakeEnv(map[string]string{
			"VIRTUAL_ENV": "/home/user/myproject/.venv",
		})),
	)

	env, err := svc.Detect(context.Background())
	if err != nil {
		t.Fatalf("Detect() error: %v", err)
	}

	if !env.IsVirtualEnv {
		t.Error("expected IsVirtualEnv to be true")
	}
	if env.Prefix != "/home/user/myproject/.venv" {
		t.Errorf("expected prefix %q, got %q", "/home/user/myproject/.venv", env.Prefix)
	}
	if env.SitePackages != "/home/user/myproject/.venv/lib/python3.12/site-packages" {
		t.Errorf("unexpected site-packages: %q", env.SitePackages)
	}
	if env.PlatformTag != "linux-x86_64" {
		t.Errorf("expected platform tag %q, got %q", "linux-x86_64", env.PlatformTag)
	}
	if env.PythonVersion != "312" {
		t.Errorf("expected python version %q, got %q", "312", env.PythonVersion)
	}
	if env.PythonPath != "/home/user/myproject/.venv/bin/python3" {
		t.Errorf("expected python path %q, got %q", "/home/user/myproject/.venv/bin/python3", env.PythonPath)
	}
}

func TestDetectSystemPython(t *testing.T) {
	svc := python.New(
		python.WithCommandRunner(fakeRunner(
			"/usr\n"+
				"/usr/lib/python3.11/site-packages\n"+
				"macosx-14.0-arm64\n"+
				"311\n"+
				"/usr/bin/python3\n", nil,
		)),
		python.WithEnvLookup(fakeEnv(nil)),
	)

	env, err := svc.Detect(context.Background())
	if err != nil {
		t.Fatalf("Detect() error: %v", err)
	}

	if env.IsVirtualEnv {
		t.Error("expected IsVirtualEnv to be false")
	}
	if env.Prefix != "/usr" {
		t.Errorf("expected prefix %q, got %q", "/usr", env.Prefix)
	}
	if env.SitePackages != "/usr/lib/python3.11/site-packages" {
		t.Errorf("unexpected site-packages: %q", env.SitePackages)
	}
	if env.PlatformTag != "macosx-14.0-arm64" {
		t.Errorf("expected platform tag %q, got %q", "macosx-14.0-arm64", env.PlatformTag)
	}
	if env.PythonVersion != "311" {
		t.Errorf("expected python version %q, got %q", "311", env.PythonVersion)
	}
}

func TestDetectCustomPythonBin(t *testing.T) {
	var capturedName string

	svc := python.New(
		python.WithPythonBin("/usr/local/bin/python3.12"),
		python.WithCommandRunner(func(_ context.Context, name string, _ ...string) ([]byte, error) {
			capturedName = name

			return []byte("/usr/local\n/usr/local/lib/python3.12/site-packages\nlinux-x86_64\n312\n/usr/local/bin/python3.12\n"), nil
		}),
		python.WithEnvLookup(fakeEnv(nil)),
	)

	env, err := svc.Detect(context.Background())
	if err != nil {
		t.Fatalf("Detect() error: %v", err)
	}

	if capturedName != "/usr/local/bin/python3.12" {
		t.Errorf("expected command %q, got %q", "/usr/local/bin/python3.12", capturedName)
	}
	if env.PythonPath != "/usr/local/bin/python3.12" {
		t.Errorf("expected python path %q, got %q (from sys.executable)", "/usr/local/bin/python3.12", env.PythonPath)
	}
}

func TestDetectPythonNotFound(t *testing.T) {
	svc := python.New(
		python.WithCommandRunner(fakeRunner("", fmt.Errorf("executable not found"))),
		python.WithEnvLookup(fakeEnv(nil)),
	)

	_, err := svc.Detect(context.Background())
	if err == nil {
		t.Fatal("expected error when python binary not found, got nil")
	}
}

func TestDetectUnexpectedOutput(t *testing.T) {
	tests := []struct {
		name   string
		output string
	}{
		{"empty output", ""},
		{"too few lines", "/usr\n/usr/lib/site-packages\nlinux\n312\n"},
		{"too many lines", "/usr\n/usr/lib/site-packages\nlinux\n312\n/usr/bin/python3\nextra\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := python.New(
				python.WithCommandRunner(fakeRunner(tt.output, nil)),
				python.WithEnvLookup(fakeEnv(nil)),
			)

			_, err := svc.Detect(context.Background())
			if err == nil {
				t.Fatalf("expected error for %s, got nil", tt.name)
			}
		})
	}
}

func TestDetectTrimsWhitespace(t *testing.T) {
	svc := python.New(
		python.WithCommandRunner(fakeRunner(
			"  /usr  \n  /usr/lib/python3.12/site-packages  \n  linux-x86_64  \n  312  \n  /usr/bin/python3  \n", nil,
		)),
		python.WithEnvLookup(fakeEnv(nil)),
	)

	env, err := svc.Detect(context.Background())
	if err != nil {
		t.Fatalf("Detect() error: %v", err)
	}

	if env.Prefix != "/usr" {
		t.Errorf("expected trimmed prefix %q, got %q", "/usr", env.Prefix)
	}
	if env.PythonVersion != "312" {
		t.Errorf("expected trimmed version %q, got %q", "312", env.PythonVersion)
	}
}
