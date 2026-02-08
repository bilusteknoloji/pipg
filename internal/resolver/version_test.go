package resolver_test

import (
	"testing"

	"github.com/bilusteknoloji/pipg/internal/resolver"
)

func TestMatchesAll(t *testing.T) {
	tests := []struct {
		name       string
		version    string
		specifiers []string
		want       bool
	}{
		{"no specifiers", "1.0.0", nil, true},
		{"single match", "1.5.0", []string{">=1.0"}, true},
		{"single no match", "0.9.0", []string{">=1.0"}, false},
		{"range match", "1.5.0", []string{">=1.0", "<2.0"}, true},
		{"range no match", "2.1.0", []string{">=1.0", "<2.0"}, false},
		{"exact match", "1.5.0", []string{"==1.5.0"}, true},
		{"exact no match", "1.5.1", []string{"==1.5.0"}, false},
		{"not equal match", "1.6.0", []string{"!=1.5.0"}, true},
		{"multiple constraints", "1.26.0", []string{">=1.25,<2.0", ">=1.26"}, true},
		{"multiple constraints fail", "1.25.0", []string{">=1.25,<2.0", ">=1.26"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolver.MatchesAll(tt.version, tt.specifiers)
			if err != nil {
				t.Fatalf("MatchesAll() error: %v", err)
			}

			if got != tt.want {
				t.Errorf("MatchesAll(%q, %v) = %v, want %v", tt.version, tt.specifiers, got, tt.want)
			}
		})
	}
}

func TestFindBestVersion(t *testing.T) {
	candidates := []string{"1.0.0", "1.5.0", "1.9.0", "2.0.0", "2.1.0", "3.0.0a1"}

	tests := []struct {
		name       string
		specifiers []string
		want       string
	}{
		{"no constraints", nil, "2.1.0"},
		{"upper bound", []string{"<2.0"}, "1.9.0"},
		{"range", []string{">=1.0", "<2.0"}, "1.9.0"},
		{"exact", []string{"==1.5.0"}, "1.5.0"},
		{"no match", []string{">=4.0"}, ""},
		{"skips prerelease", []string{">=2.0"}, "2.1.0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolver.FindBestVersion(candidates, tt.specifiers)
			if err != nil {
				t.Fatalf("FindBestVersion() error: %v", err)
			}

			if got != tt.want {
				t.Errorf("FindBestVersion() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSortVersionsDesc(t *testing.T) {
	input := []string{"1.0", "3.0", "2.0", "1.5", "invalid", "2.0.1"}

	got, err := resolver.SortVersionsDesc(input)
	if err != nil {
		t.Fatalf("SortVersionsDesc() error: %v", err)
	}

	want := []string{"3.0", "2.0.1", "2.0", "1.5", "1.0"}
	if len(got) != len(want) {
		t.Fatalf("got %d versions, want %d", len(got), len(want))
	}

	for i := range want {
		if got[i] != want[i] {
			t.Errorf("position %d: got %q, want %q", i, got[i], want[i])
		}
	}
}

func TestFormatPythonVersion(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"312", "3.12"},
		{"39", "3.9"},
		{"310", "3.10"},
		{"27", "2.7"},
		{"3", "3"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := resolver.FormatPythonVersion(tt.input); got != tt.want {
				t.Errorf("FormatPythonVersion(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
