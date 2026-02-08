package resolver

import (
	"regexp"
	"strings"

	pep440 "github.com/aquasecurity/go-pep440-version"
)

// Requirement represents a parsed PEP 508 dependency specifier.
type Requirement struct {
	Name      string // normalized package name
	Specifier string // version specifier, e.g., ">=3.0,<4.0"
	Marker    string // environment marker, e.g., `python_version < "3.10"`
}

// MarkerEnv holds environment variables used for evaluating PEP 508 markers.
type MarkerEnv struct {
	PythonVersion string // e.g., "3.12"
	SysPlatform   string // e.g., "darwin", "linux"
	OsName        string // e.g., "posix"
}

// ParseRequirement parses a PEP 508 requirement string.
//
// Supported formats:
//
//	"flask"
//	"flask>=3.0"
//	"flask>=3.0,<4.0"
//	"flask (>=3.0)"
//	"importlib-metadata>=3.6.0; python_version < \"3.10\""
func ParseRequirement(s string) Requirement {
	marker := ""

	parts := strings.SplitN(s, ";", 2)
	nameSpec := strings.TrimSpace(parts[0])

	if len(parts) > 1 {
		marker = strings.TrimSpace(parts[1])
	}

	// Strip extras: package[extra1,extra2]
	if idx := strings.Index(nameSpec, "["); idx >= 0 {
		if endIdx := strings.Index(nameSpec, "]"); endIdx > idx {
			nameSpec = nameSpec[:idx] + nameSpec[endIdx+1:]
		}
	}

	// Strip parenthesized specifier: package (>=1.0)
	nameSpec = strings.NewReplacer("(", "", ")", "").Replace(nameSpec)
	nameSpec = strings.TrimSpace(nameSpec)

	// Split name from specifier at first operator char
	specStart := strings.IndexAny(nameSpec, "><=!~")
	name := nameSpec
	specifier := ""

	if specStart >= 0 {
		name = strings.TrimSpace(nameSpec[:specStart])
		specifier = strings.TrimSpace(nameSpec[specStart:])
	}

	return Requirement{
		Name:      NormalizeName(name),
		Specifier: specifier,
		Marker:    marker,
	}
}

// NormalizeName normalizes a Python package name per PEP 503.
// Converts to lowercase and replaces runs of [-_.] with a single hyphen.
func NormalizeName(name string) string {
	name = strings.ToLower(name)

	var b strings.Builder

	prevHyphen := false

	for i := range len(name) {
		switch name[i] {
		case '-', '_', '.':
			if !prevHyphen {
				b.WriteByte('-')
				prevHyphen = true
			}
		default:
			b.WriteByte(name[i])
			prevHyphen = false
		}
	}

	return b.String()
}

// EvalMarker evaluates a PEP 508 environment marker against the given environment.
// Returns true if the marker matches (dependency should be included).
// Returns true for empty markers.
func EvalMarker(marker string, env MarkerEnv) bool {
	marker = strings.TrimSpace(marker)
	if marker == "" {
		return true
	}

	// Skip extra-conditional dependencies in v1
	if strings.Contains(marker, "extra") {
		return false
	}

	// Evaluate OR groups: any group true → true
	for _, orGroup := range splitOutside(marker, " or ") {
		// Evaluate AND terms: all terms true → group true
		allTrue := true

		for _, term := range splitOutside(strings.TrimSpace(orGroup), " and ") {
			if !evalTerm(strings.TrimSpace(term), env) {
				allTrue = false

				break
			}
		}

		if allTrue {
			return true
		}
	}

	return false
}

var markerTermRe = regexp.MustCompile(
	`^\s*([\w.]+|"[^"]*"|'[^']*')\s*(>=|<=|!=|==|~=|>|<|not\s+in|in)\s*([\w.]+|"[^"]*"|'[^']*')\s*$`,
)

// evalTerm evaluates a single marker term like `python_version >= "3.8"`.
func evalTerm(term string, env MarkerEnv) bool {
	m := markerTermRe.FindStringSubmatch(term)
	if m == nil {
		return true // unknown format, assume satisfied
	}

	left := resolveMarkerValue(m[1], env)
	op := m[2]
	right := resolveMarkerValue(m[3], env)

	lVar := unquote(m[1])
	if isVersionVariable(lVar) || isVersionVariable(unquote(m[3])) {
		return compareVersionMarker(left, op, right)
	}

	return compareStringMarker(left, op, right)
}

// resolveMarkerValue resolves a marker token to its actual value.
func resolveMarkerValue(token string, env MarkerEnv) string {
	token = unquote(token)

	switch token {
	case "python_version":
		return env.PythonVersion
	case "python_full_version":
		return env.PythonVersion
	case "sys_platform":
		return env.SysPlatform
	case "os_name":
		return env.OsName
	default:
		return token
	}
}

func unquote(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}

	return s
}

func isVersionVariable(name string) bool {
	return name == "python_version" || name == "python_full_version"
}

func compareVersionMarker(left, op, right string) bool {
	lv, err1 := pep440.Parse(left)
	rv, err2 := pep440.Parse(right)

	if err1 != nil || err2 != nil {
		return compareStringMarker(left, op, right)
	}

	cmp := lv.Compare(rv)

	switch op {
	case ">=":
		return cmp >= 0
	case "<=":
		return cmp <= 0
	case ">":
		return cmp > 0
	case "<":
		return cmp < 0
	case "==":
		return cmp == 0
	case "!=":
		return cmp != 0
	case "~=":
		return cmp >= 0
	default:
		return false
	}
}

func compareStringMarker(left, op, right string) bool {
	switch op {
	case "==":
		return left == right
	case "!=":
		return left != right
	case "in":
		return strings.Contains(right, left)
	case "not in":
		return !strings.Contains(right, left)
	default:
		return left == right
	}
}

// splitOutside splits a string on a separator, but only when the separator
// is not inside parentheses or quotes. Handles simple "and" / "or" splitting.
func splitOutside(s, sep string) []string {
	var parts []string

	depth := 0
	inQuote := byte(0)
	start := 0

	for i := 0; i < len(s); i++ {
		switch {
		case inQuote != 0:
			if s[i] == inQuote {
				inQuote = 0
			}
		case s[i] == '"' || s[i] == '\'':
			inQuote = s[i]
		case s[i] == '(':
			depth++
		case s[i] == ')':
			depth--
		case depth == 0 && i+len(sep) <= len(s) && s[i:i+len(sep)] == sep:
			parts = append(parts, s[start:i])
			start = i + len(sep)
			i += len(sep) - 1
		}
	}

	parts = append(parts, s[start:])

	return parts
}
