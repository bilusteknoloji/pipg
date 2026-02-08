# CLAUDE.md — pipg

## Project Summary

**pipg** is a Python package installer written in Go. It is a **drop-in
replacement for `pip install`** — nothing more. It does NOT manage projects,
virtual environments, lock files, or pyproject.toml. It simply installs
packages, just like `pip install`, but downloads them **concurrently** using
goroutines.

Think of it as: `pip install` but faster, thanks to parallel downloads. That's
the entire value proposition.

**pipg is NOT:**

- A project manager (unlike uv, poetry, pdm)
- A virtual environment manager
- A build tool
- It does NOT create any config files (no toml, no lock files, no yaml, nothing)

**pipg IS:**

- A fast package installer that works exactly like `pip install`
- You point it at a Python environment (venv or system) and it installs packages there
- Same mental model as pip: `pipg install requests` → done

## Usage

```bash
pipg install requests
pipg install "flask>=3.0" "sqlalchemy<2.0"
pipg install -r requirements.txt
```

The user should be able to use `pipg install` anywhere they'd use `pip
install`. No setup, no config files, no ceremony.

## Architecture

### Modules

```
pipg/
├── cmd/
│   └── pipg/
│       └── main.go            # CLI entry point (cobra or bare flags)
├── internal/
│   ├── pypi/
│   │   ├── client.go          # PyPI JSON API client (GET https://pypi.org/pypi/{pkg}/json)
│   │   └── models.go          # PyPI API response structs
│   ├── resolver/
│   │   ├── resolver.go        # Dependency resolution (BFS/DFS, version conflict detection)
│   │   └── version.go         # PEP 440 version parsing & comparison
│   ├── downloader/
│   │   ├── downloader.go      # Concurrent download manager (errgroup + semaphore)
│   │   └── wheel.go           # Platform-compatible wheel selection (PEP 425 tags)
│   ├── installer/
│   │   ├── installer.go       # Wheel extract → site-packages
│   │   └── record.go          # RECORD, METADATA, INSTALLER file management
│   └── python/
│       └── env.go             # Detect active Python environment (sys.prefix, site-packages path, platform tag)
├── go.mod
├── go.sum
├── CLAUDE.md
└── README.md
```

### Flow

```
CLI parse args
    → Detect Python environment (venv / system)
    → Fetch metadata from PyPI API for each package
    → Build dependency tree (resolver)
    → Select compatible wheel for each package
    → Concurrent download (goroutines, default: GOMAXPROCS workers)
    → Install wheels sequentially or in parallel (unzip → site-packages)
    → Print result summary
```

## Technical Requirements & Rules

### PyPI API

- Endpoint: `GET https://pypi.org/pypi/{package_name}/json`
- Specific version: `GET https://pypi.org/pypi/{package_name}/{version}/json`
- From response: `info.requires_dist` → dependency list (PEP 508 format)
- From response: `urls[]` → downloadable files (wheel, sdist)
- Respect rate limits: max 8 concurrent requests, retry with backoff

### PEP 440 — Version Parsing

- Versions: `1.0`, `1.0.post1`, `1.0a1`, `1.0b2`, `1.0rc1`, `1.0.dev1`
- Specifiers: `>=1.0,<2.0`, `==1.5.*`, `~=1.4.2`, `!=1.3`
- Comparison: compare epoch, release, pre, post, dev segments separately
- Use an existing library like `github.com/aquasecurity/go-pep440-version` if available, otherwise write your own parser

### PEP 508 — Dependency Specifiers

- Format: `package_name[extra1,extra2] (>=1.0,<2.0) ; python_version >= "3.8"`
- Parse extras and environment markers
- Marker evaluation: get variables like `python_version`, `sys_platform`, `os_name` from the active Python
- Skip dependencies whose markers don't match the current environment

### PEP 425 — Wheel Compatibility Tags

- Wheel filename format: `{name}-{ver}-{python}-{abi}-{platform}.whl`
- Example: `requests-2.31.0-py3-none-any.whl`, `numpy-1.26.0-cp312-cp312-manylinux_2_17_x86_64.whl`
- Priority order: exact match > compatible > pure python (`py3-none-any`)
- Get compatible tag list from active Python: `python -c "import packaging.tags; ..."`
- If no wheel is found, do NOT fall back to sdist — raise an error (sdist build is complex, out of scope)

### Dependency Resolution

- A simple iterative resolver is sufficient (do NOT build a full SAT solver like pip's backtracking resolver)
- Algorithm:
  1. Start from root packages
  2. Fetch requires_dist for each package
  3. Walk the entire dependency tree using BFS
  4. If the same package is requested with multiple specifiers, find the intersection
  5. If intersection is empty → raise a conflict error and exit
  6. Select the highest compatible version for each package
- Check for circular dependencies
- SKIP extras support in v1 (can be added later)

### Concurrent Download

- Use `golang.org/x/sync/errgroup`
- Max concurrency: defaults to `runtime.GOMAXPROCS(0)` (i.e., number of available CPUs).
  User can override via -`-jobs N` flag.
- Each goroutine: HTTP GET → write to temp file → verify hash (PyPI sha256)
- If file hash doesn't match `digests.sha256` from PyPI response → error
- Retry: max 3 attempts, exponential backoff
- Progress display: print `downloading...` / `done ✓` line for each package
- All downloads over HTTPS. Do NOT disable TLS certificate verification. 
  Go's net/http handles this by default.

### Wheel Installation

- A wheel file is a ZIP archive
- Unzip and extract contents to `site-packages/`
- Also extract the `{package}-{version}.dist-info/` directory
- Write `pipg` to the `INSTALLER` file
- Update the `RECORD` file (path, hash, size for each file)
- If a `.data/` directory exists, distribute its `purelib`, `platlib`, `scripts`, `data` subdirectories to the correct locations
- Copy entry points from `scripts/` to `bin/` and make them executable

### Python Environment Detection

- First check the `VIRTUAL_ENV` env var → if set, it's a venv
- Then run `python3 -c "import sys; print(sys.prefix)"`
- site-packages path: `python3 -c "import site; print(site.getsitepackages()[0])"`
- Platform tag: `python3 -c "import sysconfig; print(sysconfig.get_platform())"`
- Python version: `python3 -c "import sys; print(f'{sys.version_info.major}{sys.version_info.minor}')"`

## CLI Design

Keep it dead simple. No subcommands beyond `install`. No config files. No init commands.

```
pipg install <pkg1> [pkg2] ...         # Install packages
pipg install -r requirements.txt       # Install from requirements.txt
pipg --version                         # Show version
pipg --help                            # Help

Flags:
  --jobs, -j N          Max concurrent downloads (default: GOMAXPROCS)
  --python PATH         Python binary to use (default: python3)
  --target DIR          Target directory (default: auto-detect site-packages)
  --verbose, -v         Verbose output
  --dry-run             Don't download/install, just show the plan
  --no-deps             Skip dependencies, install only specified packages
```

**That's it. No `pipg init`, no `pipg lock`, no `pipg sync`. Just install.**

## Output Format

```
$ pipg install flask
Resolving dependencies...
  flask 3.0.0
  ├── werkzeug >=3.0.0 → 3.0.1
  ├── jinja2 >=3.1.2 → 3.1.3
  │   └── markupsafe >=2.0 → 2.1.5
  ├── itsdangerous >=2.1.2 → 2.2.0
  ├── click >=8.1.3 → 8.1.7
  └── blinker >=1.6.2 → 1.7.0

Downloading 6 packages (8 workers)...
  ✓ flask-3.0.0-py3-none-any.whl (101 KB)
  ✓ werkzeug-3.0.1-py3-none-any.whl (226 KB)
  ✓ jinja2-3.1.3-py3-none-any.whl (133 KB)
  ✓ markupsafe-2.1.5-cp312-cp312-manylinux_x86_64.whl (23 KB)
  ✓ itsdangerous-2.2.0-py3-none-any.whl (16 KB)
  ✓ click-8.1.7-py3-none-any.whl (97 KB)
  ✓ blinker-1.7.0-py3-none-any.whl (13 KB)

Installing...
  ✓ 7 packages installed

Done in 1.2s
```

## Coding Standards

- Go 1.22+
- `go fmt` and `go vet` must pass without errors
- Error handling: wrap every error (`fmt.Errorf("downloading %s: %w", pkg, err)`)
- Logging: use `log/slog`
- Context propagation: all HTTP calls and long-running operations must accept `context.Context`
- Tests: every module should have a `_test.go` file with at least basic cases
- HTTP client: `net/http` is sufficient, timeout 30s
- Linting: use golangci-lint v2. Create a `.golangci.yml` config file at the
  project root. Enable at minimum: `govet`, `errcheck`, `staticcheck`,
  `unused`, `gosimple`, `ineffassign`. All code must pass `golangci-lint run`
  without errors.

### Package Design Rules (MANDATORY)

These rules apply to every package under `internal/`. No exceptions.

1. **No stuttering constructors.** The constructor must be `New`, never
   `New<PackageName>`. The package name already provides context:
   `downloader.New(...)` not `downloader.NewDownloader(...)`.

2. **Interface-first design.** Every package must define an interface that
   describes the behavior. The concrete struct implements it. This enables
   testing with mocks and enforces clean API boundaries.

3. **Compile-time interface proof.** Every concrete struct must have a
   compile-time assertion: `var _ <Interface> = (*<Struct>)(nil)`.

4. **Functional options pattern.** Use `Option func(*<Struct>)` for
   configuration. Constructors accept `...Option`. Provide `With<Field>`
   functions for each configurable field. Set sensible defaults in `New`.

5. **Naming convention:**
   - Interface: the "role" name (`Client`, `Resolver`, `Downloader`, `Installer`, `Detector`)
   - Struct: the implementation name (`Service`, `Manager`, etc.)
   - Constructor: `New(...Option)` or `New(requiredArg, ...Option)`

Example (canonical pattern for this project):
```go
package foo

type Foo interface {
    DoSomething(ctx context.Context) error
}

type Option func(*Service)

func WithBar(b string) Option {
    return func(s *Service) { s.bar = b }
}

type Service struct {
    bar string
}

var _ Foo = (*Service)(nil)

func New(opts ...Option) *Service {
    s := &Service{bar: "default"}
    for _, opt := range opts {
        opt(s)
    }
    return s
}

func (s *Service) DoSomething(ctx context.Context) error {
    // ...
}
```

## Go Dependencies

- `golang.org/x/sync` — errgroup
- `github.com/spf13/cobra` — CLI (optional, bare `flag` package is also fine)
- PEP 440 version library (use an existing one if available, otherwise write your own)
- Do NOT add other external dependencies, prefer the standard library

## Out of Scope (do NOT implement in v1)

- sdist build (running setup.py / pyproject.toml build systems)
- Package uninstall
- Cache mechanism
- Lock file generation
- Extras support (`pip install package[extra]`)
- Editable install (`pip install -e .`)
- Index mirror support (only pypi.org)
- Windows support (initial target: Linux + macOS)

## Test Strategy

- `internal/resolver/`: dependency tree tests with mock PyPI responses
- `internal/pypi/`: API client tests with httptest.Server
- `internal/downloader/`: concurrent download tests with httptest.Server
- `internal/installer/`: wheel extraction tests to a temp directory
- Integration test: download and install a real small package (e.g., `six`, `idna`)
