![Version](https://img.shields.io/badge/version-0.0.0-orange.svg)

# pipg

A fast Python package installer written in Go. Drop-in replacement for `pip install`
- downloads packages concurrently using goroutines.

**pipg is NOT** a project manager, virtual environment manager, or build tool.
It simply installs packages, just like `pip install`, but faster.

---

## Installation

```bash
go install github.com/bilusteknoloji/pipg/cmd/pipg@latest
```

---

## Usage

```bash
pipg install requests
pipg install "flask>=3.0" "sqlalchemy<2.0"
pipg install -r requirements.txt
```

### Flags

```bash
pipg -h

pipg is a drop-in replacement for pip install that downloads packages concurrently.

Usage:
  pipg [command]

Available Commands:
  completion  Generate the autocompletion script for the specified shell
  help        Help about any command
  install     Install Python packages

Flags:
  -h, --help      help for pipg
  -v, --version   version for pipg

Use "pipg [command] --help" for more information about a command.

pipg install -h
Install Python packages

Usage:
  pipg install [packages...] [flags]

Flags:
      --dry-run               Show the plan without downloading or installing
  -h, --help                  help for install
  -j, --jobs int              Max concurrent downloads (default: GOMAXPROCS)
      --no-deps               Skip dependencies, install only specified packages
      --python string         Python binary to use (default "python3")
  -r, --requirements string   Install from requirements file
      --target string         Target directory (default: auto-detect site-packages)
  -v, --verbose               Verbose output
```

---

## How It Works

    CLI parse args
      → Detect Python environment (venv / system)
      → Fetch metadata from PyPI JSON API
      → Build dependency tree (resolver)
      → Select compatible wheel for each package (PEP 425)
      → Concurrent download with SHA256 verification
      → Install wheels to site-packages
      → Print result summary

---

## Architecture

    pipg/
    ├── cmd/pipg/          CLI entry point
    ├── internal/
    │   ├── pypi/          PyPI JSON API client
    │   ├── resolver/      Dependency resolution + PEP 440/508 parsing
    │   ├── downloader/    Concurrent download manager + wheel selection
    │   ├── installer/     Wheel extraction to site-packages
    │   └── python/        Python environment detection

---

## Development

```bash
# Build
go build ./...

# Test
go test -race -count=1 ./...

# Lint
golangci-lint run ./...
```

---

## License

This project is licensed under MIT

---

This project is intended to be a safe, welcoming space for collaboration, and
contributors are expected to adhere to the [code of conduct][coc].

[coc]: https://github.com/bilusteknoloji/pipg/blob/main/CODE_OF_CONDUCT.md
