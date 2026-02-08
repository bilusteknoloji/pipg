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
	// Parse root requirements into the BFS queue.
	var queue []Requirement
	for _, r := range requirements {
		queue = append(queue, ParseRequirement(r))
	}

	resolved := make(map[string]*ResolvedPackage)  // name → resolved info
	constraints := make(map[string][]string)        // name → accumulated specifiers
	processing := make(map[string]bool)             // names we've already fetched deps for

	for len(queue) > 0 {
		req := queue[0]
		queue = queue[1:]

		name := req.Name

		// Accumulate constraint.
		if req.Specifier != "" {
			constraints[name] = append(constraints[name], req.Specifier)
		}

		// If already resolved, verify the resolved version still satisfies all constraints.
		if pkg, ok := resolved[name]; ok {
			ok, err := MatchesAll(pkg.Version, constraints[name])
			if err != nil {
				return nil, fmt.Errorf("checking constraints for %s: %w", name, err)
			}

			if !ok {
				return nil, fmt.Errorf("version conflict for %s: %s does not satisfy %v",
					name, pkg.Version, constraints[name])
			}

			continue
		}

		// Skip if we've already fetched and queued deps for this package.
		if processing[name] {
			continue
		}

		processing[name] = true

		s.logger.Debug("resolving package", slog.String("name", name))

		// Fetch package info from PyPI.
		info, err := s.client.GetPackage(ctx, name)
		if err != nil {
			return nil, fmt.Errorf("fetching %s from PyPI: %w", name, err)
		}

		// Collect available versions from releases.
		versions := availableVersions(info)

		// Find the highest version matching all constraints.
		best, err := FindBestVersion(versions, constraints[name])
		if err != nil {
			return nil, fmt.Errorf("finding best version for %s: %w", name, err)
		}

		if best == "" {
			return nil, fmt.Errorf("no compatible version found for %s matching %v", name, constraints[name])
		}

		s.logger.Debug("resolved version",
			slog.String("name", name),
			slog.String("version", best),
		)

		// Get requires_dist for the resolved version.
		var deps []string

		if best == info.Info.Version {
			deps = info.Info.RequiresDist
		} else {
			versionInfo, err := s.client.GetPackageVersion(ctx, name, best)
			if err != nil {
				return nil, fmt.Errorf("fetching %s version %s: %w", name, best, err)
			}

			deps = versionInfo.Info.RequiresDist
		}

		resolved[name] = &ResolvedPackage{
			Name:         name,
			Version:      best,
			Dependencies: filterDepNames(deps, s.markerEnv),
		}

		// Queue dependencies unless --no-deps.
		if !s.noDeps {
			for _, dep := range deps {
				req := ParseRequirement(dep)

				if req.Marker != "" && !EvalMarker(req.Marker, s.markerEnv) {
					continue
				}

				queue = append(queue, req)
			}
		}
	}

	result := make([]ResolvedPackage, 0, len(resolved))
	for _, pkg := range resolved {
		result = append(result, *pkg)
	}

	return result, nil
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
