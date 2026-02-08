package resolver

import (
	"fmt"
	"sort"

	pep440 "github.com/aquasecurity/go-pep440-version"
)

// MatchesAll checks if a version string satisfies all the given specifier strings.
func MatchesAll(versionStr string, specifiers []string) (bool, error) {
	v, err := pep440.Parse(versionStr)
	if err != nil {
		return false, fmt.Errorf("parsing version %q: %w", versionStr, err)
	}

	for _, spec := range specifiers {
		ss, err := pep440.NewSpecifiers(spec)
		if err != nil {
			return false, fmt.Errorf("parsing specifier %q: %w", spec, err)
		}

		if !ss.Check(v) {
			return false, nil
		}
	}

	return true, nil
}

// FindBestVersion finds the highest version from candidates that satisfies all specifiers.
// Candidates are version strings. Pre-release versions are excluded unless no stable version matches.
// Returns empty string if no version matches.
func FindBestVersion(candidates []string, specifiers []string) (string, error) {
	sorted, err := SortVersionsDesc(candidates)
	if err != nil {
		return "", err
	}

	for _, v := range sorted {
		parsed, _ := pep440.Parse(v)
		if parsed.IsPreRelease() {
			continue
		}

		matches, err := MatchesAll(v, specifiers)
		if err != nil {
			return "", err
		}

		if matches {
			return v, nil
		}
	}

	return "", nil
}

// SortVersionsDesc sorts version strings in descending order (highest first).
// Invalid version strings are filtered out.
func SortVersionsDesc(versions []string) ([]string, error) {
	type parsed struct {
		raw string
		ver pep440.Version
	}

	var valid []parsed

	for _, raw := range versions {
		v, err := pep440.Parse(raw)
		if err != nil {
			continue
		}

		valid = append(valid, parsed{raw: raw, ver: v})
	}

	sort.Slice(valid, func(i, j int) bool {
		return valid[i].ver.GreaterThan(valid[j].ver)
	})

	result := make([]string, len(valid))
	for i, v := range valid {
		result[i] = v.raw
	}

	return result, nil
}

// FormatPythonVersion converts a compact version like "312" to dotted "3.12".
func FormatPythonVersion(v string) string {
	if len(v) >= 2 {
		return v[:1] + "." + v[1:]
	}

	return v
}
