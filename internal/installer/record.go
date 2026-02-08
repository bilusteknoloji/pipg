package installer

import (
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// RecordEntry represents a single line in a RECORD file.
type RecordEntry struct {
	Path string
	Hash string // sha256=<digest>
	Size int64
}

// WriteRecord writes a RECORD file to the dist-info directory.
// The RECORD file itself is listed with empty hash and size per PEP 376.
func WriteRecord(distInfoDir string, entries []RecordEntry) error {
	recordPath := filepath.Join(distInfoDir, "RECORD")

	f, err := os.Create(recordPath)
	if err != nil {
		return fmt.Errorf("creating RECORD: %w", err)
	}
	defer func() { _ = f.Close() }()

	w := csv.NewWriter(f)

	for _, e := range entries {
		if err := w.Write([]string{e.Path, e.Hash, fmt.Sprintf("%d", e.Size)}); err != nil {
			return fmt.Errorf("writing RECORD entry: %w", err)
		}
	}

	// The RECORD file itself is listed with empty hash and size.
	relRecord := filepath.Join(filepath.Base(distInfoDir), "RECORD")
	if err := w.Write([]string{relRecord, "", ""}); err != nil {
		return fmt.Errorf("writing RECORD self-entry: %w", err)
	}

	w.Flush()

	if err := w.Error(); err != nil {
		return fmt.Errorf("flushing RECORD: %w", err)
	}

	return f.Close()
}

// WriteInstaller writes the INSTALLER file with "pipg" as the content.
func WriteInstaller(distInfoDir string) error {
	path := filepath.Join(distInfoDir, "INSTALLER")

	return os.WriteFile(path, []byte("pipg\n"), 0o644)
}

// HashFile computes the sha256 digest of a file and returns it
// in the format "sha256=<hex>" along with the file size.
func HashFile(path string) (hash string, size int64, err error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, fmt.Errorf("opening %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	h := sha256.New()

	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, fmt.Errorf("hashing %s: %w", path, err)
	}

	digest := "sha256=" + hex.EncodeToString(h.Sum(nil))

	return digest, n, nil
}
