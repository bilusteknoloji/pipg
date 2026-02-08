package downloader_test

import (
	"testing"

	"github.com/bilusteknoloji/pipg/internal/downloader"
	"github.com/bilusteknoloji/pipg/internal/pypi"
)

func TestParseWheelFilename(t *testing.T) {
	tests := []struct {
		filename    string
		wantName    string
		wantVersion string
		wantTag     downloader.WheelTag
	}{
		{
			"flask-3.0.0-py3-none-any.whl",
			"flask", "3.0.0",
			downloader.WheelTag{Python: "py3", ABI: "none", Platform: "any"},
		},
		{
			"numpy-1.26.0-cp312-cp312-manylinux_2_17_x86_64.whl",
			"numpy", "1.26.0",
			downloader.WheelTag{Python: "cp312", ABI: "cp312", Platform: "manylinux_2_17_x86_64"},
		},
		{
			"MarkupSafe-2.1.5-cp312-cp312-macosx_10_9_universal2.whl",
			"MarkupSafe", "2.1.5",
			downloader.WheelTag{Python: "cp312", ABI: "cp312", Platform: "macosx_10_9_universal2"},
		},
		{
			"six-1.16.0-py2.py3-none-any.whl",
			"six", "1.16.0",
			downloader.WheelTag{Python: "py2.py3", ABI: "none", Platform: "any"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			name, version, tag, err := downloader.ParseWheelFilename(tt.filename)
			if err != nil {
				t.Fatalf("ParseWheelFilename(%q) error: %v", tt.filename, err)
			}

			if name != tt.wantName {
				t.Errorf("name = %q, want %q", name, tt.wantName)
			}

			if version != tt.wantVersion {
				t.Errorf("version = %q, want %q", version, tt.wantVersion)
			}

			if tag != tt.wantTag {
				t.Errorf("tag = %+v, want %+v", tag, tt.wantTag)
			}
		})
	}
}

func TestParseWheelFilenameInvalid(t *testing.T) {
	tests := []string{
		"flask-3.0.0.tar.gz",
		"flask.whl",
		"flask-3.0.0.whl",
		"too-few-parts.whl",
	}

	for _, filename := range tests {
		t.Run(filename, func(t *testing.T) {
			_, _, _, err := downloader.ParseWheelFilename(filename)
			if err == nil {
				t.Errorf("ParseWheelFilename(%q) expected error, got nil", filename)
			}
		})
	}
}

func TestSelectWheel(t *testing.T) {
	urls := []pypi.URL{
		{Filename: "pkg-1.0.0-cp312-cp312-manylinux_2_17_x86_64.whl", PackageType: "bdist_wheel", URL: "https://example.com/manylinux.whl"},
		{Filename: "pkg-1.0.0-py3-none-any.whl", PackageType: "bdist_wheel", URL: "https://example.com/pure.whl"},
		{Filename: "pkg-1.0.0.tar.gz", PackageType: "sdist", URL: "https://example.com/sdist.tar.gz"},
	}

	compatTags := []downloader.WheelTag{
		{Python: "cp312", ABI: "cp312", Platform: "manylinux_2_17_x86_64"},
		{Python: "cp312", ABI: "none", Platform: "any"},
		{Python: "py3", ABI: "none", Platform: "any"},
	}

	got, err := downloader.SelectWheel(urls, compatTags)
	if err != nil {
		t.Fatalf("SelectWheel() error: %v", err)
	}

	if got.URL != "https://example.com/manylinux.whl" {
		t.Errorf("SelectWheel() selected %q, want manylinux wheel", got.Filename)
	}
}

func TestSelectWheelPurePython(t *testing.T) {
	urls := []pypi.URL{
		{Filename: "pkg-1.0.0-py3-none-any.whl", PackageType: "bdist_wheel", URL: "https://example.com/pure.whl"},
	}

	compatTags := []downloader.WheelTag{
		{Python: "cp312", ABI: "cp312", Platform: "manylinux_2_17_x86_64"},
		{Python: "py3", ABI: "none", Platform: "any"},
	}

	got, err := downloader.SelectWheel(urls, compatTags)
	if err != nil {
		t.Fatalf("SelectWheel() error: %v", err)
	}

	if got.URL != "https://example.com/pure.whl" {
		t.Errorf("SelectWheel() selected %q, want pure python wheel", got.Filename)
	}
}

func TestSelectWheelCompoundTag(t *testing.T) {
	urls := []pypi.URL{
		{Filename: "six-1.16.0-py2.py3-none-any.whl", PackageType: "bdist_wheel", URL: "https://example.com/six.whl"},
	}

	compatTags := []downloader.WheelTag{
		{Python: "py3", ABI: "none", Platform: "any"},
	}

	got, err := downloader.SelectWheel(urls, compatTags)
	if err != nil {
		t.Fatalf("SelectWheel() error: %v", err)
	}

	if got.URL != "https://example.com/six.whl" {
		t.Errorf("SelectWheel() should match compound tag py2.py3 against py3")
	}
}

func TestSelectWheelNoMatch(t *testing.T) {
	urls := []pypi.URL{
		{Filename: "pkg-1.0.0-cp311-cp311-win_amd64.whl", PackageType: "bdist_wheel"},
		{Filename: "pkg-1.0.0.tar.gz", PackageType: "sdist"},
	}

	compatTags := []downloader.WheelTag{
		{Python: "cp312", ABI: "cp312", Platform: "manylinux_2_17_x86_64"},
		{Python: "py3", ABI: "none", Platform: "any"},
	}

	_, err := downloader.SelectWheel(urls, compatTags)
	if err == nil {
		t.Fatal("SelectWheel() expected error for no compatible wheel, got nil")
	}
}

func TestSelectWheelSkipsSdist(t *testing.T) {
	urls := []pypi.URL{
		{Filename: "pkg-1.0.0.tar.gz", PackageType: "sdist"},
	}

	compatTags := []downloader.WheelTag{
		{Python: "py3", ABI: "none", Platform: "any"},
	}

	_, err := downloader.SelectWheel(urls, compatTags)
	if err == nil {
		t.Fatal("SelectWheel() should not select sdist, expected error")
	}
}
