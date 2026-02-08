package installer_test

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bilusteknoloji/pipg/internal/downloader"
	"github.com/bilusteknoloji/pipg/internal/installer"
	"github.com/bilusteknoloji/pipg/internal/python"
)

// createWheel creates a test wheel ZIP file at the given path with the
// specified entries. Each entry is a map of filename → content.
func createWheel(t *testing.T, path string, entries map[string]string) {
	t.Helper()

	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("creating wheel file: %v", err)
	}

	w := zip.NewWriter(f)

	for name, content := range entries {
		fw, err := w.Create(name)
		if err != nil {
			t.Fatalf("creating zip entry %s: %v", name, err)
		}

		if _, err := fw.Write([]byte(content)); err != nil {
			t.Fatalf("writing zip entry %s: %v", name, err)
		}
	}

	if err := w.Close(); err != nil {
		t.Fatalf("closing zip writer: %v", err)
	}

	if err := f.Close(); err != nil {
		t.Fatalf("closing wheel file: %v", err)
	}
}

func testEnv(t *testing.T) *python.Environment {
	t.Helper()

	prefix := t.TempDir()
	sitePackages := filepath.Join(prefix, "lib", "python3.12", "site-packages")

	if err := os.MkdirAll(sitePackages, 0o755); err != nil {
		t.Fatalf("creating site-packages: %v", err)
	}

	return &python.Environment{
		PythonPath:    "python3",
		Prefix:        prefix,
		SitePackages:  sitePackages,
		PlatformTag:   "linux-x86_64",
		PythonVersion: "312",
		IsVirtualEnv:  true,
	}
}

func TestInstallSimpleWheel(t *testing.T) {
	env := testEnv(t)
	wheelDir := t.TempDir()
	wheelPath := filepath.Join(wheelDir, "six-1.16.0-py3-none-any.whl")

	createWheel(t, wheelPath, map[string]string{
		"six.py":                             "# six compatibility library\n",
		"six-1.16.0.dist-info/METADATA":      "Name: six\nVersion: 1.16.0\n",
		"six-1.16.0.dist-info/WHEEL":         "Wheel-Version: 1.0\n",
		"six-1.16.0.dist-info/RECORD":        "",
		"six-1.16.0.dist-info/top_level.txt": "six\n",
	})

	svc := installer.New(env)

	err := svc.Install(context.Background(), []downloader.Result{
		{Name: "six", Version: "1.16.0", FilePath: wheelPath, Size: 100},
	})
	if err != nil {
		t.Fatalf("Install() error: %v", err)
	}

	// Verify package file was extracted.
	sixPath := filepath.Join(env.SitePackages, "six.py")
	content, err := os.ReadFile(sixPath)
	if err != nil {
		t.Fatalf("reading six.py: %v", err)
	}

	if string(content) != "# six compatibility library\n" {
		t.Errorf("six.py content = %q, want %q", string(content), "# six compatibility library\n")
	}

	// Verify dist-info was extracted.
	metadataPath := filepath.Join(env.SitePackages, "six-1.16.0.dist-info", "METADATA")
	if _, statErr := os.Stat(metadataPath); statErr != nil {
		t.Errorf("METADATA not found: %v", statErr)
	}

	// Verify INSTALLER file was written.
	installerPath := filepath.Join(env.SitePackages, "six-1.16.0.dist-info", "INSTALLER")
	installerContent, err := os.ReadFile(installerPath)
	if err != nil {
		t.Fatalf("reading INSTALLER: %v", err)
	}

	if string(installerContent) != "pipg\n" {
		t.Errorf("INSTALLER content = %q, want %q", string(installerContent), "pipg\n")
	}

	// Verify RECORD file was written.
	recordPath := filepath.Join(env.SitePackages, "six-1.16.0.dist-info", "RECORD")
	recordContent, err := os.ReadFile(recordPath)
	if err != nil {
		t.Fatalf("reading RECORD: %v", err)
	}

	if len(recordContent) == 0 {
		t.Error("RECORD is empty")
	}

	// RECORD should contain entries for extracted files.
	recordStr := string(recordContent)
	if !strings.Contains(recordStr, "six.py") {
		t.Error("RECORD does not contain six.py entry")
	}

	// RECORD self-entry should have empty hash and size.
	if !strings.Contains(recordStr, "six-1.16.0.dist-info/RECORD,,") {
		t.Error("RECORD does not contain self-entry with empty hash/size")
	}
}

func TestInstallPackageWithSubdirectory(t *testing.T) {
	env := testEnv(t)
	wheelDir := t.TempDir()
	wheelPath := filepath.Join(wheelDir, "flask-3.0.0-py3-none-any.whl")

	createWheel(t, wheelPath, map[string]string{
		"flask/__init__.py":              "# flask\n",
		"flask/app.py":                   "class Flask: pass\n",
		"flask-3.0.0.dist-info/METADATA": "Name: flask\nVersion: 3.0.0\n",
		"flask-3.0.0.dist-info/WHEEL":    "Wheel-Version: 1.0\n",
		"flask-3.0.0.dist-info/RECORD":   "",
	})

	svc := installer.New(env)

	err := svc.Install(context.Background(), []downloader.Result{
		{Name: "flask", Version: "3.0.0", FilePath: wheelPath, Size: 200},
	})
	if err != nil {
		t.Fatalf("Install() error: %v", err)
	}

	// Verify nested package files.
	initPath := filepath.Join(env.SitePackages, "flask", "__init__.py")
	if _, err := os.Stat(initPath); err != nil {
		t.Errorf("flask/__init__.py not found: %v", err)
	}

	appPath := filepath.Join(env.SitePackages, "flask", "app.py")
	if _, err := os.Stat(appPath); err != nil {
		t.Errorf("flask/app.py not found: %v", err)
	}
}

func TestInstallWithDataDirectory(t *testing.T) {
	env := testEnv(t)
	wheelDir := t.TempDir()
	wheelPath := filepath.Join(wheelDir, "mypkg-1.0.0-py3-none-any.whl")

	createWheel(t, wheelPath, map[string]string{
		"mypkg/__init__.py":                      "# mypkg\n",
		"mypkg-1.0.0.dist-info/METADATA":         "Name: mypkg\nVersion: 1.0.0\n",
		"mypkg-1.0.0.dist-info/WHEEL":            "Wheel-Version: 1.0\n",
		"mypkg-1.0.0.dist-info/RECORD":           "",
		"mypkg-1.0.0.data/scripts/mypkg-cli":     "#!/usr/bin/env python3\nprint('hello')\n",
		"mypkg-1.0.0.data/data/etc/mypkg.conf":   "key=value\n",
		"mypkg-1.0.0.data/purelib/extra_mod.py":  "# extra module\n",
		"mypkg-1.0.0.data/headers/mypkg/mypkg.h": "#include <stdio.h>\n",
	})

	svc := installer.New(env)

	err := svc.Install(context.Background(), []downloader.Result{
		{Name: "mypkg", Version: "1.0.0", FilePath: wheelPath, Size: 300},
	})
	if err != nil {
		t.Fatalf("Install() error: %v", err)
	}

	// scripts → prefix/bin/
	scriptPath := filepath.Join(env.Prefix, "bin", "mypkg-cli")
	info, err := os.Stat(scriptPath)
	if err != nil {
		t.Fatalf("script not found: %v", err)
	}

	if info.Mode()&0o111 == 0 {
		t.Errorf("script %s is not executable, mode: %v", scriptPath, info.Mode())
	}

	// data → prefix/
	confPath := filepath.Join(env.Prefix, "etc", "mypkg.conf")
	if _, err := os.Stat(confPath); err != nil {
		t.Errorf("data file not found: %v", err)
	}

	// purelib → site-packages/
	purePath := filepath.Join(env.SitePackages, "extra_mod.py")
	if _, err := os.Stat(purePath); err != nil {
		t.Errorf("purelib file not found: %v", err)
	}

	// headers → prefix/include/
	headerPath := filepath.Join(env.Prefix, "include", "mypkg", "mypkg.h")
	if _, err := os.Stat(headerPath); err != nil {
		t.Errorf("header file not found: %v", err)
	}
}

func TestInstallMultiplePackages(t *testing.T) {
	env := testEnv(t)
	wheelDir := t.TempDir()

	// Create two wheels.
	wheel1 := filepath.Join(wheelDir, "pkg_a-1.0.0-py3-none-any.whl")
	createWheel(t, wheel1, map[string]string{
		"pkg_a/__init__.py":              "# a\n",
		"pkg_a-1.0.0.dist-info/METADATA": "Name: pkg-a\nVersion: 1.0.0\n",
		"pkg_a-1.0.0.dist-info/WHEEL":    "Wheel-Version: 1.0\n",
		"pkg_a-1.0.0.dist-info/RECORD":   "",
	})

	wheel2 := filepath.Join(wheelDir, "pkg_b-2.0.0-py3-none-any.whl")
	createWheel(t, wheel2, map[string]string{
		"pkg_b/__init__.py":              "# b\n",
		"pkg_b-2.0.0.dist-info/METADATA": "Name: pkg-b\nVersion: 2.0.0\n",
		"pkg_b-2.0.0.dist-info/WHEEL":    "Wheel-Version: 1.0\n",
		"pkg_b-2.0.0.dist-info/RECORD":   "",
	})

	svc := installer.New(env)

	err := svc.Install(context.Background(), []downloader.Result{
		{Name: "pkg-a", Version: "1.0.0", FilePath: wheel1, Size: 100},
		{Name: "pkg-b", Version: "2.0.0", FilePath: wheel2, Size: 100},
	})
	if err != nil {
		t.Fatalf("Install() error: %v", err)
	}

	// Verify both packages installed.
	for _, pkg := range []string{"pkg_a", "pkg_b"} {
		initPath := filepath.Join(env.SitePackages, pkg, "__init__.py")
		if _, err := os.Stat(initPath); err != nil {
			t.Errorf("%s/__init__.py not found: %v", pkg, err)
		}
	}
}

func TestInstallContextCanceled(t *testing.T) {
	env := testEnv(t)
	wheelDir := t.TempDir()
	wheelPath := filepath.Join(wheelDir, "pkg-1.0.0-py3-none-any.whl")

	createWheel(t, wheelPath, map[string]string{
		"pkg/__init__.py":              "# pkg\n",
		"pkg-1.0.0.dist-info/METADATA": "Name: pkg\nVersion: 1.0.0\n",
		"pkg-1.0.0.dist-info/WHEEL":    "Wheel-Version: 1.0\n",
		"pkg-1.0.0.dist-info/RECORD":   "",
	})

	svc := installer.New(env)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := svc.Install(ctx, []downloader.Result{
		{Name: "pkg", Version: "1.0.0", FilePath: wheelPath, Size: 100},
	})
	if err == nil {
		t.Fatal("expected context canceled error, got nil")
	}
}

func TestInstallInvalidWheelFile(t *testing.T) {
	env := testEnv(t)

	// Create a file that is not a valid ZIP.
	badPath := filepath.Join(t.TempDir(), "bad-1.0.0-py3-none-any.whl")
	if err := os.WriteFile(badPath, []byte("not a zip"), 0o644); err != nil {
		t.Fatal(err)
	}

	svc := installer.New(env)

	err := svc.Install(context.Background(), []downloader.Result{
		{Name: "bad", Version: "1.0.0", FilePath: badPath, Size: 9},
	})
	if err == nil {
		t.Fatal("expected error for invalid wheel, got nil")
	}
}

func TestInstallNoDistInfo(t *testing.T) {
	env := testEnv(t)
	wheelDir := t.TempDir()
	wheelPath := filepath.Join(wheelDir, "nodist-1.0.0-py3-none-any.whl")

	// Wheel without dist-info directory.
	createWheel(t, wheelPath, map[string]string{
		"nodist/__init__.py": "# no dist-info\n",
	})

	svc := installer.New(env)

	err := svc.Install(context.Background(), []downloader.Result{
		{Name: "nodist", Version: "1.0.0", FilePath: wheelPath, Size: 50},
	})
	if err == nil {
		t.Fatal("expected error for missing dist-info, got nil")
	}
}

func TestInstallWithConsoleScripts(t *testing.T) {
	env := testEnv(t)
	wheelDir := t.TempDir()
	wheelPath := filepath.Join(wheelDir, "mycli-1.0.0-py3-none-any.whl")

	createWheel(t, wheelPath, map[string]string{
		"mycli/__init__.py":                      "# mycli\n",
		"mycli/cli.py":                           "def main(): pass\n",
		"mycli-1.0.0.dist-info/METADATA":         "Name: mycli\nVersion: 1.0.0\n",
		"mycli-1.0.0.dist-info/WHEEL":            "Wheel-Version: 1.0\n",
		"mycli-1.0.0.dist-info/RECORD":           "",
		"mycli-1.0.0.dist-info/entry_points.txt": "[console_scripts]\nmycli = mycli.cli:main\n",
	})

	svc := installer.New(env)

	err := svc.Install(context.Background(), []downloader.Result{
		{Name: "mycli", Version: "1.0.0", FilePath: wheelPath, Size: 100},
	})
	if err != nil {
		t.Fatalf("Install() error: %v", err)
	}

	// Verify console script was generated in bin/.
	scriptPath := filepath.Join(env.Prefix, "bin", "mycli")
	info, err := os.Stat(scriptPath)
	if err != nil {
		t.Fatalf("console script not found: %v", err)
	}

	if info.Mode()&0o111 == 0 {
		t.Errorf("script should be executable, mode: %v", info.Mode())
	}

	content, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(content), "from mycli.cli import main") {
		t.Error("script should contain correct import")
	}

	if !strings.Contains(string(content), "sys.exit(main())") {
		t.Error("script should call sys.exit with main()")
	}

	// Verify RECORD includes the script.
	recordPath := filepath.Join(env.SitePackages, "mycli-1.0.0.dist-info", "RECORD")
	recordContent, err := os.ReadFile(recordPath)
	if err != nil {
		t.Fatalf("reading RECORD: %v", err)
	}

	if !strings.Contains(string(recordContent), "bin/mycli") {
		t.Error("RECORD should contain console script entry")
	}
}

func TestInstallEmptyDownloads(t *testing.T) {
	env := testEnv(t)
	svc := installer.New(env)

	err := svc.Install(context.Background(), nil)
	if err != nil {
		t.Fatalf("Install(nil) error: %v", err)
	}
}

func TestWithLoggerIgnoresNil(t *testing.T) {
	env := testEnv(t)
	_ = installer.New(env, installer.WithLogger(nil))
}
