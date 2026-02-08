package installer_test

import (
	"encoding/csv"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bilusteknoloji/pipg/internal/installer"
)

func TestWriteRecord(t *testing.T) {
	dir := t.TempDir()
	distInfo := filepath.Join(dir, "pkg-1.0.0.dist-info")

	if err := os.MkdirAll(distInfo, 0o755); err != nil {
		t.Fatal(err)
	}

	entries := []installer.RecordEntry{
		{Path: "pkg/__init__.py", Hash: "sha256=abc123", Size: 42},
		{Path: "pkg/app.py", Hash: "sha256=def456", Size: 128},
		{Path: "pkg-1.0.0.dist-info/METADATA", Hash: "sha256=meta789", Size: 64},
	}

	if err := installer.WriteRecord(distInfo, entries); err != nil {
		t.Fatalf("WriteRecord() error: %v", err)
	}

	recordPath := filepath.Join(distInfo, "RECORD")
	content, err := os.ReadFile(recordPath)
	if err != nil {
		t.Fatalf("reading RECORD: %v", err)
	}

	// Parse as CSV.
	reader := csv.NewReader(strings.NewReader(string(content)))
	records, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("parsing RECORD as CSV: %v", err)
	}

	// 3 entries + 1 self-entry.
	if len(records) != 4 {
		t.Fatalf("expected 4 RECORD lines, got %d", len(records))
	}

	// Verify first entry.
	if records[0][0] != "pkg/__init__.py" {
		t.Errorf("record[0] path = %q, want %q", records[0][0], "pkg/__init__.py")
	}

	if records[0][1] != "sha256=abc123" {
		t.Errorf("record[0] hash = %q, want %q", records[0][1], "sha256=abc123")
	}

	if records[0][2] != "42" {
		t.Errorf("record[0] size = %q, want %q", records[0][2], "42")
	}

	// Verify self-entry (last line).
	selfEntry := records[len(records)-1]
	if selfEntry[0] != "pkg-1.0.0.dist-info/RECORD" {
		t.Errorf("self-entry path = %q, want %q", selfEntry[0], "pkg-1.0.0.dist-info/RECORD")
	}

	if selfEntry[1] != "" || selfEntry[2] != "" {
		t.Errorf("self-entry hash/size should be empty, got %q/%q", selfEntry[1], selfEntry[2])
	}
}

func TestWriteInstaller(t *testing.T) {
	dir := t.TempDir()
	distInfo := filepath.Join(dir, "pkg-1.0.0.dist-info")

	if err := os.MkdirAll(distInfo, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := installer.WriteInstaller(distInfo); err != nil {
		t.Fatalf("WriteInstaller() error: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(distInfo, "INSTALLER"))
	if err != nil {
		t.Fatalf("reading INSTALLER: %v", err)
	}

	if string(content) != "pipg\n" {
		t.Errorf("INSTALLER content = %q, want %q", string(content), "pipg\n")
	}
}

func TestHashFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")

	if err := os.WriteFile(path, []byte("hello world"), 0o644); err != nil {
		t.Fatal(err)
	}

	hash, size, err := installer.HashFile(path)
	if err != nil {
		t.Fatalf("HashFile() error: %v", err)
	}

	if size != 11 {
		t.Errorf("size = %d, want 11", size)
	}

	// sha256("hello world") = b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9
	wantHash := "sha256=b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9"
	if hash != wantHash {
		t.Errorf("hash = %q, want %q", hash, wantHash)
	}
}

func TestHashFileNotFound(t *testing.T) {
	_, _, err := installer.HashFile("/nonexistent/path/file.txt")
	if err == nil {
		t.Fatal("expected error for nonexistent file, got nil")
	}
}
