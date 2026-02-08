package downloader

import (
	"fmt"
	"strings"

	"github.com/bilusteknoloji/pipg/internal/pypi"
)

// WheelTag represents a PEP 425 compatibility tag.
type WheelTag struct {
	Python   string // e.g., "cp312", "py3"
	ABI      string // e.g., "cp312", "none"
	Platform string // e.g., "manylinux_2_17_x86_64", "any"
}

// ParseWheelFilename parses a wheel filename into its components.
// Format: {name}-{ver}-{python}-{abi}-{platform}.whl
func ParseWheelFilename(filename string) (name, version string, tag WheelTag, err error) {
	filename = strings.TrimSuffix(filename, ".whl")

	parts := strings.Split(filename, "-")
	if len(parts) < 5 {
		return "", "", WheelTag{}, fmt.Errorf("invalid wheel filename %q: expected at least 5 parts", filename)
	}

	// Last 3 parts are always python-abi-platform.
	// First part is name, second is version.
	// Optional build tag is between version and python tag.
	tag = WheelTag{
		Python:   parts[len(parts)-3],
		ABI:      parts[len(parts)-2],
		Platform: parts[len(parts)-1],
	}

	name = parts[0]
	version = parts[1]

	return name, version, tag, nil
}

// SelectWheel selects the best compatible wheel from the available URLs.
// compatTags must be ordered by priority (most preferred first).
// Returns an error if no compatible wheel is found (does NOT fall back to sdist).
func SelectWheel(urls []pypi.URL, compatTags []WheelTag) (pypi.URL, error) {
	bestPriority := len(compatTags)
	var bestURL pypi.URL

	found := false

	for _, u := range urls {
		if u.PackageType != "bdist_wheel" {
			continue
		}

		_, _, tag, err := ParseWheelFilename(u.Filename)
		if err != nil {
			continue
		}

		for i, ct := range compatTags {
			if i >= bestPriority {
				break
			}

			if tagMatches(tag, ct) {
				bestPriority = i
				bestURL = u
				found = true

				break
			}
		}

		if bestPriority == 0 {
			break // can't do better than the highest priority
		}
	}

	if !found {
		return pypi.URL{}, fmt.Errorf("no compatible wheel found (tried %d URLs)", len(urls))
	}

	return bestURL, nil
}

// tagMatches checks if a wheel tag matches a compatibility tag.
// Wheel tags can have compound values separated by "." (e.g., "py2.py3"),
// meaning the wheel supports any of those values.
func tagMatches(wheel, compat WheelTag) bool {
	return fieldMatches(wheel.Python, compat.Python) &&
		fieldMatches(wheel.ABI, compat.ABI) &&
		fieldMatches(wheel.Platform, compat.Platform)
}

// fieldMatches checks if a wheel tag field matches a compat tag value.
// The wheel field may contain multiple values separated by ".".
func fieldMatches(wheelField, compatValue string) bool {
	for _, w := range strings.Split(wheelField, ".") {
		if w == compatValue {
			return true
		}
	}

	return false
}
