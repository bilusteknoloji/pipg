# pipg

A fast Python package installer written in Go. Drop-in replacement for `pip install` — downloads packages concurrently using goroutines.

**pipg is NOT** a project manager, virtual environment manager, or build tool.
It simply installs packages, just like `pip install`, but faster.

## Installation

```bash
go install github.com/bilusteknoloji/pipg/cmd/pipg@latest
```

## Usage

```bash
pipg install requests
pipg install "flask>=3.0" "sqlalchemy<2.0"
pipg install -r requirements.txt
```

### Flags

```
--jobs, -j N          Max concurrent downloads (default: GOMAXPROCS)
--python PATH         Python binary to use (default: python3)
--target DIR          Target directory (default: auto-detect site-packages)
--verbose, -v         Verbose output
--dry-run             Show the plan without downloading or installing
--no-deps             Skip dependencies, install only specified packages
```

## How It Works

```
CLI parse args
  → Detect Python environment (venv / system)
  → Fetch metadata from PyPI JSON API
  → Build dependency tree (resolver)
  → Select compatible wheel for each package (PEP 425)
  → Concurrent download with SHA256 verification
  → Install wheels to site-packages
  → Print result summary
```

## Architecture

```
pipg/
├── cmd/pipg/          CLI entry point
├── internal/
│   ├── pypi/          PyPI JSON API client
│   ├── resolver/      Dependency resolution + PEP 440/508 parsing
│   ├── downloader/    Concurrent download manager + wheel selection
│   ├── installer/     Wheel extraction to site-packages
│   └── python/        Python environment detection
```

## Development

```bash
# Build
go build ./...

# Test
go test -race -count=1 ./...

# Lint
golangci-lint run ./...
```

## License

MIT
