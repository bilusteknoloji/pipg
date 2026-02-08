package python

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// pythonScript is the single Python command that collects all environment info.
const pythonScript = `import sys, site, sysconfig
print(sys.prefix)
print(site.getsitepackages()[0])
print(sysconfig.get_platform())
print(f'{sys.version_info.major}{sys.version_info.minor}')
print(sys.executable)`

// expectedOutputLines is the number of lines expected from pythonScript.
const expectedOutputLines = 5

// Detector defines the interface for detecting a Python environment.
type Detector interface {
	Detect(ctx context.Context) (*Environment, error)
}

// Environment represents a detected Python environment.
type Environment struct {
	PythonPath    string // path to the python binary
	Prefix        string // sys.prefix
	SitePackages  string // site-packages directory
	PlatformTag   string // e.g., "macosx-14.0-arm64"
	PythonVersion string // e.g., "312"
	IsVirtualEnv  bool
}

// CommandRunner executes a command and returns its combined output.
type CommandRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

// EnvLookup looks up an environment variable.
type EnvLookup func(string) string

// Option configures a Service.
type Option func(*Service)

// WithPythonBin sets the python binary path.
// Defaults to "python3".
func WithPythonBin(bin string) Option {
	return func(s *Service) {
		if bin != "" {
			s.pythonBin = bin
		}
	}
}

// WithCommandRunner sets the command runner for executing external processes.
// Defaults to exec.CommandContext.
func WithCommandRunner(fn CommandRunner) Option {
	return func(s *Service) {
		if fn != nil {
			s.runCmd = fn
		}
	}
}

// WithEnvLookup sets the function used to read environment variables.
// Defaults to os.Getenv.
func WithEnvLookup(fn EnvLookup) Option {
	return func(s *Service) {
		if fn != nil {
			s.getenv = fn
		}
	}
}

// Service detects the active Python environment by inspecting
// environment variables and running the python binary.
type Service struct {
	pythonBin string
	runCmd    CommandRunner
	getenv    EnvLookup
}

// compile-time proof that Service implements Detector.
var _ Detector = (*Service)(nil)

// New creates a new Python environment detector.
func New(opts ...Option) *Service {
	s := &Service{
		pythonBin: "python3",
		runCmd:    defaultRunCmd,
		getenv:    os.Getenv,
	}

	for _, opt := range opts {
		opt(s)
	}

	return s
}

// Detect detects the active Python environment.
// It first checks the VIRTUAL_ENV env var, then runs the python binary
// to determine prefix, site-packages path, platform tag, and version.
func (s *Service) Detect(ctx context.Context) (*Environment, error) {
	env := &Environment{}

	if venv := s.getenv("VIRTUAL_ENV"); venv != "" {
		env.IsVirtualEnv = true
	}

	output, err := s.runCmd(ctx, s.pythonBin, "-c", pythonScript)
	if err != nil {
		return nil, fmt.Errorf("running %s: %w", s.pythonBin, err)
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) != expectedOutputLines {
		return nil, fmt.Errorf("unexpected output from %s: expected %d lines, got %d",
			s.pythonBin, expectedOutputLines, len(lines))
	}

	env.Prefix = strings.TrimSpace(lines[0])
	env.SitePackages = strings.TrimSpace(lines[1])
	env.PlatformTag = strings.TrimSpace(lines[2])
	env.PythonVersion = strings.TrimSpace(lines[3])
	env.PythonPath = strings.TrimSpace(lines[4])

	return env, nil
}

// defaultRunCmd executes a command using exec.CommandContext.
func defaultRunCmd(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output()
}
