package main

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/bilusteknoloji/pipg/internal/cache"
	"github.com/bilusteknoloji/pipg/internal/downloader"
	"github.com/bilusteknoloji/pipg/internal/installer"
	"github.com/bilusteknoloji/pipg/internal/pypi"
	"github.com/bilusteknoloji/pipg/internal/python"
	"github.com/bilusteknoloji/pipg/internal/resolver"
)

var version = "0.0.0"

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	rootCmd := &cobra.Command{
		Use:           "pipg",
		Short:         "A fast Python package installer",
		Long:          "pipg is a drop-in replacement for pip install that downloads packages concurrently.",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	installCmd := &cobra.Command{
		Use:   "install [packages...]",
		Short: "Install Python packages",
		Args:  cobra.MinimumNArgs(0),
		RunE:  runInstall,
	}

	installCmd.Flags().StringP("requirements", "r", "", "Install from requirements file")
	installCmd.Flags().IntP("jobs", "j", 0, "Max concurrent downloads (default: GOMAXPROCS)")
	installCmd.Flags().String("python", "python3", "Python binary to use")
	installCmd.Flags().String("target", "", "Target directory (default: auto-detect site-packages)")
	installCmd.Flags().BoolP("verbose", "v", false, "Verbose output")
	installCmd.Flags().Bool("dry-run", false, "Show the plan without downloading or installing")
	installCmd.Flags().Bool("no-deps", false, "Skip dependencies, install only specified packages")

	rootCmd.AddCommand(installCmd)

	return rootCmd.Execute()
}

// installFlags holds parsed CLI flags for the install command.
type installFlags struct {
	reqFile   string
	jobs      int
	pythonBin string
	targetDir string
	verbose   bool
	dryRun    bool
	noDeps    bool
}

func parseInstallFlags(cmd *cobra.Command) installFlags {
	reqFile, _ := cmd.Flags().GetString("requirements")
	jobs, _ := cmd.Flags().GetInt("jobs")
	pythonBin, _ := cmd.Flags().GetString("python")
	targetDir, _ := cmd.Flags().GetString("target")
	verbose, _ := cmd.Flags().GetBool("verbose")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	noDeps, _ := cmd.Flags().GetBool("no-deps")

	return installFlags{reqFile, jobs, pythonBin, targetDir, verbose, dryRun, noDeps}
}

func runInstall(cmd *cobra.Command, args []string) error {
	start := time.Now()
	flags := parseInstallFlags(cmd)

	requirements, err := collectRequirements(args, flags.reqFile)
	if err != nil {
		return err
	}

	if len(requirements) == 0 {
		return fmt.Errorf("no packages specified; use 'pipg install <pkg>' or 'pipg install -r requirements.txt'")
	}

	logger := newLogger(flags.verbose)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	env, err := detectEnv(ctx, flags.pythonBin, flags.targetDir, logger)
	if err != nil {
		return err
	}

	httpClient := &http.Client{Timeout: 30 * time.Second}
	pypiClient := pypi.New(pypi.WithHTTPClient(httpClient), pypi.WithLogger(logger))

	resolved, err := resolveDeps(ctx, requirements, pypiClient, flags.noDeps, env, logger)
	if err != nil {
		return err
	}

	compatTags := buildCompatTags(env)

	plans, err := selectWheels(ctx, resolved, pypiClient, compatTags, env)
	if err != nil {
		return err
	}

	if flags.dryRun {
		printDryRun(plans)

		return nil
	}

	results, tmpDir, err := downloadPackages(ctx, plans, flags.jobs, httpClient, logger)
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	printDownloadResults(results)

	fmt.Println("\nInstalling...")

	inst := installer.New(env, installer.WithLogger(logger))
	if err := inst.Install(ctx, results); err != nil {
		return fmt.Errorf("installing packages: %w", err)
	}

	fmt.Printf("  ✓ %d packages installed\n", len(results))
	fmt.Printf("\nDone in %.1fs\n", time.Since(start).Seconds())

	return nil
}

func newLogger(verbose bool) *slog.Logger {
	logLevel := slog.LevelWarn
	if verbose {
		logLevel = slog.LevelDebug
	}

	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel}))
}

func detectEnv(ctx context.Context, pythonBin, targetDir string, logger *slog.Logger) (*python.Environment, error) {
	pyDetector := python.New(python.WithPythonBin(pythonBin))

	env, err := pyDetector.Detect(ctx)
	if err != nil {
		return nil, fmt.Errorf("detecting Python environment: %w", err)
	}

	if targetDir != "" {
		absTarget, err := filepath.Abs(targetDir)
		if err != nil {
			return nil, fmt.Errorf("resolving target directory: %w", err)
		}

		env.SitePackages = absTarget
	}

	logger.Debug("detected Python environment",
		slog.String("prefix", env.Prefix),
		slog.String("site-packages", env.SitePackages),
		slog.String("platform", env.PlatformTag),
		slog.String("version", env.PythonVersion),
		slog.Bool("venv", env.IsVirtualEnv),
	)

	return env, nil
}

func resolveDeps(ctx context.Context, requirements []string, pypiClient pypi.Client, noDeps bool, env *python.Environment, logger *slog.Logger) ([]resolver.ResolvedPackage, error) {
	fmt.Println("Resolving dependencies...")

	markerEnv := buildMarkerEnv(env)

	resolverSvc := resolver.New(pypiClient,
		resolver.WithNoDeps(noDeps),
		resolver.WithMarkerEnv(markerEnv),
		resolver.WithLogger(logger),
	)

	resolved, err := resolverSvc.Resolve(ctx, requirements)
	if err != nil {
		return nil, fmt.Errorf("resolving dependencies: %w", err)
	}

	resolvedMap := make(map[string]resolver.ResolvedPackage, len(resolved))
	for _, pkg := range resolved {
		resolvedMap[pkg.Name] = pkg
	}

	rootNames := make([]string, 0, len(requirements))
	for _, r := range requirements {
		rootNames = append(rootNames, resolver.NormalizeName(resolver.ParseRequirement(r).Name))
	}

	printDependencyTree(rootNames, resolvedMap)

	return resolved, nil
}

func printDryRun(plans []downloadPlan) {
	fmt.Printf("\nWould download %d packages:\n", len(plans))

	for _, p := range plans {
		fmt.Printf("  %s (%s)\n", p.wheelURL.Filename, formatSize(p.wheelURL.Size))
	}

	fmt.Println("\nDry run, no changes made.")
}

func printDownloadResults(results []downloader.Result) {
	for _, r := range results {
		suffix := ""
		if r.Cached {
			suffix = " (cached)"
		}

		fmt.Printf("  ✓ %s (%s)%s\n", filepath.Base(r.FilePath), formatSize(r.Size), suffix)
	}
}

type downloadPlan struct {
	pkg      resolver.ResolvedPackage
	wheelURL pypi.URL
}

// selectWheels finds a compatible wheel for each resolved package.
func selectWheels(ctx context.Context, resolved []resolver.ResolvedPackage, client pypi.Client, compatTags []downloader.WheelTag, env *python.Environment) ([]downloadPlan, error) {
	var plans []downloadPlan

	for _, pkg := range resolved {
		pkgInfo, err := client.GetPackageVersion(ctx, pkg.Name, pkg.Version)
		if err != nil {
			return nil, fmt.Errorf("fetching URLs for %s %s: %w", pkg.Name, pkg.Version, err)
		}

		wheel, err := downloader.SelectWheel(pkgInfo.URLs, compatTags)
		if err != nil {
			return nil, fmt.Errorf("no compatible wheel for %s %s (platform: %s, python: cp%s): %w",
				pkg.Name, pkg.Version, wheelPlatform(env.PlatformTag), env.PythonVersion, err)
		}

		plans = append(plans, downloadPlan{pkg: pkg, wheelURL: wheel})
	}

	return plans, nil
}

// downloadPackages downloads all planned packages concurrently with cache support.
// Caller is responsible for cleaning up tmpDir after installation.
func downloadPackages(ctx context.Context, plans []downloadPlan, jobs int, httpClient *http.Client, logger *slog.Logger) ([]downloader.Result, string, error) {
	tmpDir, err := os.MkdirTemp("", "pipg-downloads-*")
	if err != nil {
		return nil, "", fmt.Errorf("creating temp directory: %w", err)
	}

	requests := buildDownloadRequests(plans)

	workers := runtime.GOMAXPROCS(0)
	if jobs > 0 {
		workers = jobs
	}

	fmt.Printf("\nDownloading %d packages (%d workers)...\n", len(requests), workers)

	dlManager := newDownloader(tmpDir, jobs, httpClient, logger)

	results, err := dlManager.Download(ctx, requests)
	if err != nil {
		_ = os.RemoveAll(tmpDir)

		return nil, "", fmt.Errorf("downloading packages: %w", err)
	}

	return results, tmpDir, nil
}

func buildDownloadRequests(plans []downloadPlan) []downloader.Request {
	requests := make([]downloader.Request, len(plans))
	for i, p := range plans {
		requests[i] = downloader.Request{
			Name:     p.pkg.Name,
			Version:  p.pkg.Version,
			URL:      p.wheelURL.URL,
			SHA256:   p.wheelURL.Digests.SHA256,
			Filename: p.wheelURL.Filename,
		}
	}

	return requests
}

func newDownloader(tmpDir string, jobs int, httpClient *http.Client, logger *slog.Logger) *downloader.Manager {
	wheelCache, err := cache.New(cache.WithLogger(logger))
	if err != nil {
		logger.Debug("cache unavailable, continuing without cache", slog.String("error", err.Error()))
	}

	dlOpts := []downloader.Option{
		downloader.WithHTTPClient(httpClient),
		downloader.WithLogger(logger),
	}

	if wheelCache != nil {
		dlOpts = append(dlOpts, downloader.WithCache(wheelCache))
	}

	if jobs > 0 {
		dlOpts = append(dlOpts, downloader.WithMaxWorkers(jobs))
	}

	return downloader.New(tmpDir, dlOpts...)
}

// collectRequirements merges CLI args and requirements file entries.
func collectRequirements(args []string, reqFile string) ([]string, error) {
	var requirements []string

	requirements = append(requirements, args...)

	if reqFile != "" {
		fileReqs, err := parseRequirementsFile(reqFile)
		if err != nil {
			return nil, err
		}

		requirements = append(requirements, fileReqs...)
	}

	return requirements, nil
}

// parseRequirementsFile reads a pip-compatible requirements file.
// Skips comments, empty lines, and pip options (lines starting with -).
func parseRequirementsFile(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening requirements file %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	var reqs []string

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Strip inline comments.
		if idx := strings.Index(line, "#"); idx >= 0 {
			line = strings.TrimSpace(line[:idx])
		}

		// Skip empty lines and pip options (e.g., --index-url, -e, -c).
		if line == "" || strings.HasPrefix(line, "-") {
			continue
		}

		reqs = append(reqs, line)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading requirements file %s: %w", path, err)
	}

	return reqs, nil
}

// buildMarkerEnv creates a PEP 508 marker environment from the detected Python env.
func buildMarkerEnv(env *python.Environment) resolver.MarkerEnv {
	pyVer := resolver.FormatPythonVersion(env.PythonVersion)

	var sysPlatform, osName string

	switch {
	case strings.HasPrefix(env.PlatformTag, "macosx"):
		sysPlatform = "darwin"
		osName = "posix"
	case strings.HasPrefix(env.PlatformTag, "linux"):
		sysPlatform = "linux"
		osName = "posix"
	default:
		sysPlatform = "linux"
		osName = "posix"
	}

	return resolver.MarkerEnv{
		PythonVersion: pyVer,
		SysPlatform:   sysPlatform,
		OsName:        osName,
	}
}

// buildCompatTags generates PEP 425 compatible wheel tags ordered by priority.
func buildCompatTags(env *python.Environment) []downloader.WheelTag {
	pyVer := env.PythonVersion                 // e.g., "312"
	platform := wheelPlatform(env.PlatformTag) // e.g., "macosx_14_0_arm64"
	cp := "cp" + pyVer                         // e.g., "cp312"
	pyMajor := "py" + pyVer[:1]                // e.g., "py3"

	var tags []downloader.WheelTag

	platforms := expandPlatform(platform)

	// Native CPython + platform.
	for _, plat := range platforms {
		tags = append(tags, downloader.WheelTag{Python: cp, ABI: cp, Platform: plat})
	}

	// Stable ABI + platform.
	for _, plat := range platforms {
		tags = append(tags, downloader.WheelTag{Python: cp, ABI: "abi3", Platform: plat})
	}

	// CPython, no ABI, specific platform.
	for _, plat := range platforms {
		tags = append(tags, downloader.WheelTag{Python: cp, ABI: "none", Platform: plat})
	}

	// Pure Python, specific platform.
	for _, plat := range platforms {
		tags = append(tags, downloader.WheelTag{Python: pyMajor, ABI: "none", Platform: plat})
	}

	// Universal (any platform).
	tags = append(tags, downloader.WheelTag{Python: cp, ABI: "none", Platform: "any"})
	tags = append(tags, downloader.WheelTag{Python: pyMajor, ABI: "none", Platform: "any"})

	return tags
}

// expandPlatform expands a platform tag into a priority-ordered list including
// manylinux variants (Linux) and lower macOS version variants.
func expandPlatform(platform string) []string {
	platforms := []string{platform}

	if strings.HasPrefix(platform, "linux_") {
		arch := strings.TrimPrefix(platform, "linux_")

		for _, ml := range []string{
			"manylinux_2_35", "manylinux_2_34", "manylinux_2_31",
			"manylinux_2_28", "manylinux_2_17", "manylinux2014",
		} {
			platforms = append(platforms, ml+"_"+arch)
		}
	}

	if strings.HasPrefix(platform, "macosx_") {
		parts := strings.SplitN(platform, "_", 4) // macosx, major, minor, arch
		if len(parts) == 4 {
			arch := parts[3]
			major, _ := strconv.Atoi(parts[1])

			// Universal2 for current version.
			platforms = append(platforms,
				fmt.Sprintf("macosx_%s_%s_universal2", parts[1], parts[2]),
			)

			// Lower macOS versions (arm64 starts at 11, x86_64 down to 10.9).
			minMajor := 10
			if arch == "arm64" {
				minMajor = 11
			}

			for v := major - 1; v >= minMajor; v-- {
				minor := "0"
				if v == 10 {
					minor = "9"
				}

				platforms = append(platforms,
					fmt.Sprintf("macosx_%d_%s_%s", v, minor, arch),
					fmt.Sprintf("macosx_%d_%s_universal2", v, minor),
				)
			}
		}
	}

	return platforms
}

// wheelPlatform converts a sysconfig platform tag to wheel format.
// "macosx-14.0-arm64" → "macosx_14_0_arm64"
func wheelPlatform(sysTag string) string {
	s := strings.ReplaceAll(sysTag, "-", "_")

	return strings.ReplaceAll(s, ".", "_")
}

// printDependencyTree prints the resolved packages as a dependency tree.
func printDependencyTree(roots []string, resolved map[string]resolver.ResolvedPackage) {
	visited := make(map[string]bool)

	for _, root := range roots {
		pkg, ok := resolved[root]
		if !ok {
			continue
		}

		fmt.Printf("  %s %s\n", pkg.Name, pkg.Version)

		visited[root] = true

		printSubTree(pkg.Dependencies, resolved, "  ", visited)
	}
}

func printSubTree(deps []string, resolved map[string]resolver.ResolvedPackage, prefix string, visited map[string]bool) {
	for i, depName := range deps {
		pkg, ok := resolved[depName]
		if !ok {
			continue
		}

		isLast := i == len(deps)-1

		connector := "├── "
		childPrefix := "│   "

		if isLast {
			connector = "└── "
			childPrefix = "    "
		}

		fmt.Printf("%s%s%s %s\n", prefix, connector, pkg.Name, pkg.Version)

		if !visited[depName] && len(pkg.Dependencies) > 0 {
			visited[depName] = true
			printSubTree(pkg.Dependencies, resolved, prefix+childPrefix, visited)
		}
	}
}

// formatSize returns a human-readable file size.
func formatSize(bytes int64) string {
	switch {
	case bytes >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(1<<20))
	case bytes >= 1<<10:
		return fmt.Sprintf("%d KB", bytes/(1<<10))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}
