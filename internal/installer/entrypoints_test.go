package installer_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bilusteknoloji/pipg/internal/installer"
)

func TestParseEntryPoints(t *testing.T) {
	dir := t.TempDir()
	epPath := filepath.Join(dir, "entry_points.txt")

	content := `[console_scripts]
ipython = IPython:start_ipython
ipython3 = IPython:start_ipython

[gui_scripts]
some_gui = mymod:main
`
	if err := os.WriteFile(epPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	scripts, err := installer.ParseEntryPoints(epPath)
	if err != nil {
		t.Fatalf("ParseEntryPoints() error: %v", err)
	}

	if len(scripts) != 2 {
		t.Fatalf("expected 2 console scripts, got %d", len(scripts))
	}

	if scripts[0].Name != "ipython" {
		t.Errorf("scripts[0].Name = %q, want %q", scripts[0].Name, "ipython")
	}

	if scripts[0].Module != "IPython" {
		t.Errorf("scripts[0].Module = %q, want %q", scripts[0].Module, "IPython")
	}

	if scripts[0].Attr != "start_ipython" {
		t.Errorf("scripts[0].Attr = %q, want %q", scripts[0].Attr, "start_ipython")
	}

	if scripts[1].Name != "ipython3" {
		t.Errorf("scripts[1].Name = %q, want %q", scripts[1].Name, "ipython3")
	}
}

func TestParseEntryPointsWithExtras(t *testing.T) {
	dir := t.TempDir()
	epPath := filepath.Join(dir, "entry_points.txt")

	content := `[console_scripts]
flask = flask.cli:main [dotenv]
`
	if err := os.WriteFile(epPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	scripts, err := installer.ParseEntryPoints(epPath)
	if err != nil {
		t.Fatalf("ParseEntryPoints() error: %v", err)
	}

	if len(scripts) != 1 {
		t.Fatalf("expected 1 script, got %d", len(scripts))
	}

	if scripts[0].Module != "flask.cli" {
		t.Errorf("Module = %q, want %q", scripts[0].Module, "flask.cli")
	}

	if scripts[0].Attr != "main" {
		t.Errorf("Attr = %q, want %q", scripts[0].Attr, "main")
	}
}

func TestParseEntryPointsNoFile(t *testing.T) {
	scripts, err := installer.ParseEntryPoints("/nonexistent/entry_points.txt")
	if err != nil {
		t.Fatalf("expected nil error for missing file, got: %v", err)
	}

	if len(scripts) != 0 {
		t.Errorf("expected 0 scripts, got %d", len(scripts))
	}
}

func TestParseEntryPointsNoConsoleScripts(t *testing.T) {
	dir := t.TempDir()
	epPath := filepath.Join(dir, "entry_points.txt")

	content := `[gui_scripts]
myapp = mymod:main
`
	if err := os.WriteFile(epPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	scripts, err := installer.ParseEntryPoints(epPath)
	if err != nil {
		t.Fatalf("ParseEntryPoints() error: %v", err)
	}

	if len(scripts) != 0 {
		t.Errorf("expected 0 console scripts, got %d", len(scripts))
	}
}

func TestGenerateScript(t *testing.T) {
	cs := installer.ConsoleScript{
		Name:   "ipython",
		Module: "IPython",
		Attr:   "start_ipython",
	}

	got := string(installer.GenerateScript("/usr/bin/python3", cs))

	if !strings.HasPrefix(got, "#!/usr/bin/python3\n") {
		t.Error("script should start with shebang")
	}

	if !strings.Contains(got, "from IPython import start_ipython") {
		t.Error("script should import the module and attr")
	}

	if !strings.Contains(got, "sys.exit(start_ipython())") {
		t.Error("script should call sys.exit with the attr")
	}

	if !strings.Contains(got, "sys.argv[0].removesuffix") {
		t.Error("script should use removesuffix for argv cleanup")
	}
}

func TestInstallConsoleScripts(t *testing.T) {
	dir := t.TempDir()
	distInfo := filepath.Join(dir, "site-packages", "pkg-1.0.0.dist-info")
	binDir := filepath.Join(dir, "bin")

	if err := os.MkdirAll(distInfo, 0o755); err != nil {
		t.Fatal(err)
	}

	epContent := `[console_scripts]
mycli = mypackage.cli:main
`
	if err := os.WriteFile(filepath.Join(distInfo, "entry_points.txt"), []byte(epContent), 0o644); err != nil {
		t.Fatal(err)
	}

	records, err := installer.InstallConsoleScripts(distInfo, binDir, "/usr/bin/python3")
	if err != nil {
		t.Fatalf("InstallConsoleScripts() error: %v", err)
	}

	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}

	// Verify script was created.
	scriptPath := filepath.Join(binDir, "mycli")
	info, err := os.Stat(scriptPath)
	if err != nil {
		t.Fatalf("script not found: %v", err)
	}

	// Verify executable.
	if info.Mode()&0o111 == 0 {
		t.Errorf("script should be executable, mode: %v", info.Mode())
	}

	// Verify content.
	content, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(content), "from mypackage.cli import main") {
		t.Error("script content should contain correct import")
	}
}

func TestInstallConsoleScriptsNoEntryPoints(t *testing.T) {
	dir := t.TempDir()
	distInfo := filepath.Join(dir, "pkg-1.0.0.dist-info")
	binDir := filepath.Join(dir, "bin")

	if err := os.MkdirAll(distInfo, 0o755); err != nil {
		t.Fatal(err)
	}

	// No entry_points.txt file.
	records, err := installer.InstallConsoleScripts(distInfo, binDir, "/usr/bin/python3")
	if err != nil {
		t.Fatalf("InstallConsoleScripts() error: %v", err)
	}

	if len(records) != 0 {
		t.Errorf("expected 0 records, got %d", len(records))
	}
}
