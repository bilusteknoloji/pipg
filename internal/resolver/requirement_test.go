package resolver_test

import (
	"testing"

	"github.com/bilusteknoloji/pipg/internal/resolver"
)

func TestParseRequirement(t *testing.T) {
	tests := []struct {
		input     string
		wantName  string
		wantSpec  string
		wantMark  string
	}{
		{"flask", "flask", "", ""},
		{"Flask", "flask", "", ""},
		{"flask>=3.0", "flask", ">=3.0", ""},
		{"flask>=3.0,<4.0", "flask", ">=3.0,<4.0", ""},
		{"flask (>=3.0)", "flask", ">=3.0", ""},
		{
			`importlib-metadata>=3.6.0; python_version < "3.10"`,
			"importlib-metadata", ">=3.6.0", `python_version < "3.10"`,
		},
		{"my_package", "my-package", "", ""},
		{"My.Package>=1.0", "my-package", ">=1.0", ""},
		{"package[extra]>=1.0", "package", ">=1.0", ""},
		{"requests", "requests", "", ""},
		{`typing-extensions>=3.7.4; python_version < "3.8"`,
			"typing-extensions", ">=3.7.4", `python_version < "3.8"`},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			req := resolver.ParseRequirement(tt.input)

			if req.Name != tt.wantName {
				t.Errorf("Name = %q, want %q", req.Name, tt.wantName)
			}
			if req.Specifier != tt.wantSpec {
				t.Errorf("Specifier = %q, want %q", req.Specifier, tt.wantSpec)
			}
			if req.Marker != tt.wantMark {
				t.Errorf("Marker = %q, want %q", req.Marker, tt.wantMark)
			}
		})
	}
}

func TestNormalizeName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Flask", "flask"},
		{"my_package", "my-package"},
		{"My.Package", "my-package"},
		{"some--name", "some-name"},
		{"a_.b", "a-b"},
		{"requests", "requests"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := resolver.NormalizeName(tt.input); got != tt.want {
				t.Errorf("NormalizeName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestEvalMarker(t *testing.T) {
	env := resolver.MarkerEnv{
		PythonVersion: "3.12",
		SysPlatform:   "linux",
		OsName:        "posix",
	}

	tests := []struct {
		name   string
		marker string
		want   bool
	}{
		{"empty marker", "", true},
		{"python version match", `python_version >= "3.8"`, true},
		{"python version no match", `python_version < "3.10"`, false},
		{"python version equal", `python_version == "3.12"`, true},
		{"platform match", `sys_platform == "linux"`, true},
		{"platform no match", `sys_platform == "win32"`, false},
		{"platform not equal", `sys_platform != "win32"`, true},
		{"os match", `os_name == "posix"`, true},
		{"os no match", `os_name == "nt"`, false},
		{"and both true", `python_version >= "3.8" and sys_platform == "linux"`, true},
		{"and one false", `python_version >= "3.8" and sys_platform == "win32"`, false},
		{"or first true", `sys_platform == "linux" or sys_platform == "win32"`, true},
		{"or second true", `sys_platform == "darwin" or sys_platform == "linux"`, true},
		{"or both false", `sys_platform == "darwin" or sys_platform == "win32"`, false},
		{"extra skipped", `extra == "docs"`, false},
		{"extra with and", `python_version >= "3.8" and extra == "test"`, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolver.EvalMarker(tt.marker, env); got != tt.want {
				t.Errorf("EvalMarker(%q) = %v, want %v", tt.marker, got, tt.want)
			}
		})
	}
}

func TestEvalMarkerVersionComparison(t *testing.T) {
	// Ensure version comparison is semantic, not lexicographic.
	// "3.9" < "3.12" semantically, but "3.9" > "3.12" lexicographically.
	env := resolver.MarkerEnv{PythonVersion: "3.9"}

	tests := []struct {
		marker string
		want   bool
	}{
		{`python_version < "3.12"`, true},
		{`python_version >= "3.12"`, false},
		{`python_version < "3.10"`, true},
		{`python_version > "3.8"`, true},
	}

	for _, tt := range tests {
		t.Run(tt.marker, func(t *testing.T) {
			if got := resolver.EvalMarker(tt.marker, env); got != tt.want {
				t.Errorf("EvalMarker(%q) with python 3.9 = %v, want %v", tt.marker, got, tt.want)
			}
		})
	}
}
