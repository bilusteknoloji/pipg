package installer

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ConsoleScript represents a parsed console_scripts entry point.
type ConsoleScript struct {
	Name   string // script name, e.g., "ipython"
	Module string // module path, e.g., "IPython"
	Attr   string // callable attribute, e.g., "start_ipython"
}

// ParseEntryPoints reads an entry_points.txt file and returns the console_scripts.
func ParseEntryPoints(path string) ([]ConsoleScript, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}

		return nil, fmt.Errorf("opening entry_points.txt: %w", err)
	}
	defer func() { _ = f.Close() }()

	var scripts []ConsoleScript

	inConsoleScripts := false

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments.
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Section header.
		if strings.HasPrefix(line, "[") {
			inConsoleScripts = line == "[console_scripts]"

			continue
		}

		if !inConsoleScripts {
			continue
		}

		// Parse "name = module:attr" or "name = module:attr [extras]".
		cs, err := parseScriptEntry(line)
		if err != nil {
			continue
		}

		scripts = append(scripts, cs)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading entry_points.txt: %w", err)
	}

	return scripts, nil
}

// parseScriptEntry parses a single console_scripts entry.
// Format: "name = module:attr" or "name = module:attr [extras]"
func parseScriptEntry(line string) (ConsoleScript, error) {
	parts := strings.SplitN(line, "=", 2)
	if len(parts) != 2 {
		return ConsoleScript{}, fmt.Errorf("invalid entry: %q", line)
	}

	name := strings.TrimSpace(parts[0])
	value := strings.TrimSpace(parts[1])

	// Strip optional extras like " [extra1,extra2]".
	if idx := strings.Index(value, "["); idx >= 0 {
		value = strings.TrimSpace(value[:idx])
	}

	colonParts := strings.SplitN(value, ":", 2)
	if len(colonParts) != 2 {
		return ConsoleScript{}, fmt.Errorf("invalid entry value: %q", value)
	}

	return ConsoleScript{
		Name:   name,
		Module: strings.TrimSpace(colonParts[0]),
		Attr:   strings.TrimSpace(colonParts[1]),
	}, nil
}

// GenerateScript creates a Python wrapper script for a console_scripts entry point.
// Output matches what pip generates.
func GenerateScript(pythonPath string, cs ConsoleScript) []byte {
	script := fmt.Sprintf(`#!%s
import sys
from %s import %s
if __name__ == '__main__':
    sys.argv[0] = sys.argv[0].removesuffix('.exe')
    sys.exit(%s())
`, pythonPath, cs.Module, cs.Attr, cs.Attr)

	return []byte(script)
}

// InstallConsoleScripts reads entry_points.txt, generates wrapper scripts,
// and installs them to the bin directory. Returns RECORD entries for the scripts.
func InstallConsoleScripts(distInfoDir, binDir, pythonPath string) ([]RecordEntry, error) {
	epPath := filepath.Join(distInfoDir, "entry_points.txt")

	scripts, err := ParseEntryPoints(epPath)
	if err != nil {
		return nil, err
	}

	if len(scripts) == 0 {
		return nil, nil
	}

	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating bin directory: %w", err)
	}

	var records []RecordEntry

	for _, cs := range scripts {
		scriptPath := filepath.Join(binDir, cs.Name)
		content := GenerateScript(pythonPath, cs)

		if err := os.WriteFile(scriptPath, content, 0o755); err != nil {
			return nil, fmt.Errorf("writing script %s: %w", cs.Name, err)
		}

		hash, size, err := HashFile(scriptPath)
		if err != nil {
			return nil, fmt.Errorf("hashing script %s: %w", cs.Name, err)
		}

		// Record path relative to site-packages uses ../../../bin/name format,
		// but pip uses the absolute path in some cases. We'll use the relative
		// path from the dist-info's perspective.
		records = append(records, RecordEntry{
			Path: filepath.Join("..", "..", "..", "bin", cs.Name),
			Hash: hash,
			Size: size,
		})
	}

	return records, nil
}
