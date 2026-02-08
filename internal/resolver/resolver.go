package resolver

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/bilusteknoloji/pipg/internal/pypi"
)

// Resolver defines the interface for resolving package dependencies.
type Resolver interface {
	Resolve(ctx context.Context, requirements []string) ([]ResolvedPackage, error)
}

// ResolvedPackage represents a package with its resolved version and dependencies.
type ResolvedPackage struct {
	Name         string
	Version      string
	Dependencies []string
}

// Option configures a Service.
type Option func(*Service)

// WithNoDeps disables dependency resolution; only root packages are resolved.
func WithNoDeps(noDeps bool) Option {
	return func(s *Service) {
		s.noDeps = noDeps
	}
}

// WithMarkerEnv sets the environment for evaluating PEP 508 markers.
func WithMarkerEnv(env MarkerEnv) Option {
	return func(s *Service) {
		s.markerEnv = env
	}
}

// WithLogger sets the structured logger.
func WithLogger(l *slog.Logger) Option {
	return func(s *Service) {
		if l != nil {
			s.logger = l
		}
	}
}

// Service resolves package dependencies using a simple BFS iterative approach.
type Service struct {
	client    pypi.Client
	noDeps    bool
	markerEnv MarkerEnv
	logger    *slog.Logger
}

// compile-time proof that Service implements Resolver.
var _ Resolver = (*Service)(nil)

// New creates a new dependency resolver with the given PyPI client.
func New(client pypi.Client, opts ...Option) *Service {
	s := &Service{
		client: client,
		logger: slog.Default(),
	}

	for _, opt := range opts {
		opt(s)
	}

	return s
}

// Resolve resolves all dependencies for the given package requirements.
// It walks the dependency tree using BFS, finds compatible versions,
// and returns the full list of packages to install.
func (s *Service) Resolve(ctx context.Context, requirements []string) ([]ResolvedPackage, error) {
	var queue []Requirement
	for _, r := range requirements {
		queue = append(queue, ParseRequirement(r))
	}

	resolved := make(map[string]*ResolvedPackage)
	constraints := make(map[string][]string)
	processing := make(map[string]bool)

	for len(queue) > 0 {
		req := queue[0]
		queue = queue[1:]

		if req.Specifier != "" {
			constraints[req.Name] = append(constraints[req.Name], req.Specifier)
		}

		if pkg, ok := resolved[req.Name]; ok {
			if err := s.verifyConstraints(pkg, constraints[req.Name]); err != nil {
				return nil, err
			}

			continue
		}

		if processing[req.Name] {
			continue
		}

		processing[req.Name] = true

		pkg, deps, err := s.resolvePackage(ctx, req.Name, constraints[req.Name])
		if err != nil {
			return nil, err
		}

		resolved[req.Name] = pkg
		queue = append(queue, s.filterDeps(deps)...)
	}

	result := make([]ResolvedPackage, 0, len(resolved))
	for _, pkg := range resolved {
		result = append(result, *pkg)
	}

	return result, nil
}

// verifyConstraints checks that a resolved package still satisfies all accumulated constraints.
func (s *Service) verifyConstraints(pkg *ResolvedPackage, specs []string) error {
	ok, err := MatchesAll(pkg.Version, specs)
	if err != nil {
		return fmt.Errorf("checking constraints for %s: %w", pkg.Name, err)
	}

	if !ok {
		return fmt.Errorf("version conflict for %s: %s does not satisfy %v",
			pkg.Name, pkg.Version, specs)
	}

	return nil
}

// resolvePackage fetches a package from PyPI, selects the best version, and returns
// the resolved package along with its raw dependency list.
func (s *Service) resolvePackage(ctx context.Context, name string, specs []string) (*ResolvedPackage, []string, error) {
	s.logger.Debug("resolving package", slog.String("name", name))

	info, err := s.client.GetPackage(ctx, name)
	if err != nil {
		return nil, nil, fmt.Errorf("fetching %s from PyPI: %w", name, err)
	}

	best, err := FindBestVersion(availableVersions(info), specs)
	if err != nil {
		return nil, nil, fmt.Errorf("finding best version for %s: %w", name, err)
	}

	if best == "" {
		return nil, nil, fmt.Errorf("no compatible version found for %s matching %v", name, specs)
	}

	s.logger.Debug("resolved version", slog.String("name", name), slog.String("version", best))

	deps, err := s.fetchDeps(ctx, info, name, best)
	if err != nil {
		return nil, nil, err
	}

	pkg := &ResolvedPackage{
		Name:         name,
		Version:      best,
		Dependencies: filterDepNames(deps, s.markerEnv),
	}

	return pkg, deps, nil
}

// fetchDeps returns requires_dist for a specific version.
func (s *Service) fetchDeps(ctx context.Context, info *pypi.PackageInfo, name, version string) ([]string, error) {
	if version == info.Info.Version {
		return info.Info.RequiresDist, nil
	}

	versionInfo, err := s.client.GetPackageVersion(ctx, name, version)
	if err != nil {
		return nil, fmt.Errorf("fetching %s version %s: %w", name, version, err)
	}

	return versionInfo.Info.RequiresDist, nil
}

// filterDeps filters dependency strings by marker environment and returns parsed requirements.
func (s *Service) filterDeps(deps []string) []Requirement {
	if s.noDeps {
		return nil
	}

	var reqs []Requirement

	for _, dep := range deps {
		req := ParseRequirement(dep)
		if req.Marker != "" && !EvalMarker(req.Marker, s.markerEnv) {
			continue
		}

		reqs = append(reqs, req)
	}

	return reqs
}

// availableVersions extracts version strings from a PackageInfo's releases.
// Falls back to info.Version if no releases are present.
func availableVersions(info *pypi.PackageInfo) []string {
	if len(info.Releases) > 0 {
		versions := make([]string, 0, len(info.Releases))

		for v, files := range info.Releases {
			if len(files) > 0 {
				versions = append(versions, v)
			}
		}

		return versions
	}

	// Fallback: only the latest version is known.
	if info.Info.Version != "" {
		return []string{info.Info.Version}
	}

	return nil
}

// filterDepNames extracts normalized dependency names from requires_dist,
// filtering by marker environment.
func filterDepNames(requiresDist []string, env MarkerEnv) []string {
	var names []string

	for _, dep := range requiresDist {
		req := ParseRequirement(dep)
		if req.Marker != "" && !EvalMarker(req.Marker, env) {
			continue
		}

		names = append(names, req.Name)
	}

	return names
}
