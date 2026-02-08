package installer

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/bilusteknoloji/pipg/internal/downloader"
	"github.com/bilusteknoloji/pipg/internal/python"
)

// Installer defines the interface for installing downloaded wheel files.
type Installer interface {
	Install(ctx context.Context, downloads []downloader.Result) error
}

// Option configures a Service.
type Option func(*Service)

// WithLogger sets the structured logger.
func WithLogger(l *slog.Logger) Option {
	return func(s *Service) {
		if l != nil {
			s.logger = l
		}
	}
}

// Service handles extracting wheel files into site-packages.
type Service struct {
	env    *python.Environment
	logger *slog.Logger
}

// compile-time proof that Service implements Installer.
var _ Installer = (*Service)(nil)

// New creates a new wheel installer targeting the given Python environment.
func New(env *python.Environment, opts ...Option) *Service {
	s := &Service{
		env:    env,
		logger: slog.Default(),
	}

	for _, opt := range opts {
		opt(s)
	}

	return s
}

// Install extracts all downloaded wheel files into site-packages.
// It handles .data directories, writes RECORD and INSTALLER files,
// and sets executable permissions on scripts.
func (s *Service) Install(ctx context.Context, downloads []downloader.Result) error {
	for _, dl := range downloads {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("installation canceled: %w", err)
		}

		if err := s.installWheel(dl); err != nil {
			return fmt.Errorf("installing %s: %w", dl.Name, err)
		}

		s.logger.Debug("installed", slog.String("package", dl.Name))
	}

	return nil
}

// installWheel extracts a single wheel file into site-packages.
func (s *Service) installWheel(dl downloader.Result) error {
	r, err := zip.OpenReader(dl.FilePath)
	if err != nil {
		return fmt.Errorf("opening wheel %s: %w", dl.FilePath, err)
	}
	defer func() { _ = r.Close() }()

	siteDir := s.env.SitePackages
	dataSuffix := ".data/"

	var records []RecordEntry
	var distInfoDir string

	for _, f := range r.File {
		if f.FileInfo().IsDir() {
			continue
		}

		destPath, category := s.resolveDestination(f.Name, siteDir, dataSuffix)
		if destPath == "" {
			continue
		}

		// ZipSlip protection: ensure destination is within expected base.
		base := s.baseForCategory(category, siteDir)
		if !isInsideDir(destPath, base) {
			return fmt.Errorf("zip slip detected: %s resolves outside %s", f.Name, base)
		}

		if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
			return fmt.Errorf("creating directory for %s: %w", f.Name, err)
		}

		if err := extractFile(f, destPath); err != nil {
			return fmt.Errorf("extracting %s: %w", f.Name, err)
		}

		// Make scripts executable.
		if category == categoryScripts {
			if err := os.Chmod(destPath, 0o755); err != nil {
				return fmt.Errorf("setting executable permission on %s: %w", destPath, err)
			}
		}

		// Track dist-info directory.
		if strings.Contains(f.Name, ".dist-info/") {
			dir := filepath.Join(siteDir, strings.SplitN(f.Name, "/", 2)[0])
			distInfoDir = dir
		}

		// Compute relative path from site-packages for RECORD.
		relPath, err := filepath.Rel(siteDir, destPath)
		if err != nil {
			relPath = f.Name
		}

		hash, size, err := HashFile(destPath)
		if err != nil {
			return fmt.Errorf("hashing %s: %w", destPath, err)
		}

		records = append(records, RecordEntry{Path: relPath, Hash: hash, Size: size})
	}

	if distInfoDir == "" {
		return fmt.Errorf("no .dist-info directory found in %s", dl.FilePath)
	}

	if err := WriteInstaller(distInfoDir); err != nil {
		return fmt.Errorf("writing INSTALLER: %w", err)
	}

	// Add INSTALLER to records.
	installerPath := filepath.Join(distInfoDir, "INSTALLER")

	hash, size, err := HashFile(installerPath)
	if err != nil {
		return fmt.Errorf("hashing INSTALLER: %w", err)
	}

	relInstaller, _ := filepath.Rel(siteDir, installerPath)
	records = append(records, RecordEntry{Path: relInstaller, Hash: hash, Size: size})

	// Generate console_scripts from entry_points.txt.
	binDir := filepath.Join(s.env.Prefix, "bin")
	scriptRecords, err := InstallConsoleScripts(distInfoDir, binDir, s.env.PythonPath)
	if err != nil {
		return fmt.Errorf("installing console scripts: %w", err)
	}

	records = append(records, scriptRecords...)

	if err := WriteRecord(distInfoDir, records); err != nil {
		return fmt.Errorf("writing RECORD: %w", err)
	}

	return nil
}

// fileCategory describes where a wheel entry should be extracted.
type fileCategory int

const (
	categorySitePackages fileCategory = iota
	categoryScripts
	categoryData
	categorySkip
)

// resolveDestination determines the target path for a wheel entry.
// Wheel entries can be:
//   - Regular files → site-packages/
//   - .data/purelib/* → site-packages/
//   - .data/platlib/* → site-packages/
//   - .data/scripts/* → prefix/bin/
//   - .data/data/* → prefix/
//   - .data/headers/* → prefix/include/
func (s *Service) resolveDestination(name, siteDir, dataSuffix string) (string, fileCategory) {
	// Check if this is a .data directory entry.
	dataIdx := strings.Index(name, dataSuffix)
	if dataIdx == -1 {
		// Regular file → extract to site-packages.
		return filepath.Join(siteDir, name), categorySitePackages
	}

	// Extract the part after ".data/": e.g., "scripts/flask" or "purelib/flask/__init__.py"
	remainder := name[dataIdx+len(dataSuffix):]

	slashIdx := strings.Index(remainder, "/")
	if slashIdx == -1 {
		return "", categorySkip
	}

	subdir := remainder[:slashIdx]
	rest := remainder[slashIdx+1:]

	if rest == "" {
		return "", categorySkip
	}

	switch subdir {
	case "purelib", "platlib":
		return filepath.Join(siteDir, rest), categorySitePackages
	case "scripts":
		return filepath.Join(s.env.Prefix, "bin", rest), categoryScripts
	case "data":
		return filepath.Join(s.env.Prefix, rest), categoryData
	case "headers":
		return filepath.Join(s.env.Prefix, "include", rest), categoryData
	default:
		return "", categorySkip
	}
}

// baseForCategory returns the expected base directory for ZipSlip validation.
func (s *Service) baseForCategory(cat fileCategory, siteDir string) string {
	switch cat {
	case categorySitePackages:
		return siteDir
	case categoryScripts, categoryData:
		return s.env.Prefix
	default:
		return siteDir
	}
}

// extractFile extracts a single file from the zip archive.
func extractFile(f *zip.File, destPath string) error {
	src, err := f.Open()
	if err != nil {
		return fmt.Errorf("opening zip entry: %w", err)
	}
	defer func() { _ = src.Close() }()

	dst, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("creating %s: %w", destPath, err)
	}

	if _, err := io.Copy(dst, src); err != nil {
		_ = dst.Close()

		return fmt.Errorf("writing %s: %w", destPath, err)
	}

	return dst.Close()
}

// isInsideDir checks that path is inside dir after resolving symlinks.
func isInsideDir(path, dir string) bool {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}

	absDir, err := filepath.Abs(dir)
	if err != nil {
		return false
	}

	return strings.HasPrefix(absPath, absDir+string(filepath.Separator)) || absPath == absDir
}
