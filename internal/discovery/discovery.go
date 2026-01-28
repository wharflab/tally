// Package discovery provides Dockerfile discovery with glob pattern support.
package discovery

import (
	"cmp"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

// DiscoveredFile represents a Dockerfile discovered during file discovery.
type DiscoveredFile struct {
	// Path is the path to the Dockerfile.
	// For explicit file inputs, this preserves the original path (relative or absolute).
	// For discovered files (from directories/globs), this is an absolute path.
	Path string

	// ConfigRoot is the directory to use for config file discovery.
	// This is typically the directory containing the Dockerfile.
	ConfigRoot string

	// ContextDir is the build context directory (optional).
	// When set, enables context-aware rules like copy-ignored-file.
	ContextDir string
}

// Options configures file discovery behavior.
type Options struct {
	// Patterns are the glob patterns to match (default: DefaultPatterns()).
	// Supports doublestar patterns like "**/*.Dockerfile".
	Patterns []string

	// ExcludePatterns are glob patterns to exclude from results.
	ExcludePatterns []string

	// ContextDir is the build context directory to use for all discovered files.
	// If empty, no context is set.
	ContextDir string
}

// DefaultPatterns returns the default Dockerfile patterns.
// These cover common naming conventions:
// - Dockerfile (standard)
// - Dockerfile.* (Dockerfile.dev, Dockerfile.prod, etc.)
// - *.Dockerfile (api.Dockerfile, frontend.Dockerfile, etc.)
// - Containerfile (Podman convention)
func DefaultPatterns() []string {
	return []string{
		"Dockerfile",
		"Dockerfile.*",
		"*.Dockerfile",
		"Containerfile",
		"Containerfile.*",
		"*.Containerfile",
	}
}

// Discover finds Dockerfiles matching the given inputs.
// Each input can be:
// - A specific file path
// - A directory (searched recursively with default patterns)
// - A glob pattern (expanded with doublestar)
//
// Results are deduplicated by absolute path and sorted.
func Discover(inputs []string, opts Options) ([]DiscoveredFile, error) {
	if len(opts.Patterns) == 0 {
		opts.Patterns = DefaultPatterns()
	}

	// Track seen paths to avoid duplicates
	seen := make(map[string]bool)
	var results []DiscoveredFile

	for _, input := range inputs {
		discovered, err := discoverInput(input, opts, seen)
		if err != nil {
			return nil, err
		}
		results = append(results, discovered...)
	}

	// Sort by path for deterministic output
	slices.SortFunc(results, func(a, b DiscoveredFile) int {
		return cmp.Compare(a.Path, b.Path)
	})

	return results, nil
}

// discoverInput processes a single input (file, directory, or glob pattern).
func discoverInput(input string, opts Options, seen map[string]bool) ([]DiscoveredFile, error) {
	// Check if the input contains glob characters. If so, treat it as a glob pattern
	// without trying os.Stat (which fails on Windows with glob chars like *).
	if containsGlobChars(input) {
		return discoverGlob(input, opts, seen)
	}

	// Try as a literal file or directory
	info, err := os.Stat(input)
	if err == nil {
		if info.IsDir() {
			return discoverDirectory(input, opts, seen)
		}
		return discoverFile(input, opts, seen)
	}

	// Not a literal path, treat as glob pattern (for cases like non-existent patterns)
	if !os.IsNotExist(err) {
		return nil, err
	}

	return discoverGlob(input, opts, seen)
}

// containsGlobChars returns true if the path contains glob special characters.
func containsGlobChars(path string) bool {
	for _, c := range path {
		switch c {
		case '*', '?', '[', ']':
			return true
		}
	}
	return false
}

// discoverFile processes a specific file path.
// Preserves the original path format (relative or absolute) for user-provided inputs.
func discoverFile(path string, opts Options, seen map[string]bool) ([]DiscoveredFile, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}

	// Check for exclusion
	if isExcluded(absPath, opts.ExcludePatterns) {
		return nil, nil
	}

	// Skip if already seen
	if seen[absPath] {
		return nil, nil
	}
	seen[absPath] = true

	// Preserve original path for display, but use absolute for ConfigRoot
	df := DiscoveredFile{
		Path:       path, // Preserve original (might be relative)
		ConfigRoot: filepath.Dir(absPath),
		ContextDir: opts.ContextDir,
	}

	return []DiscoveredFile{df}, nil
}

// discoverDirectory recursively searches a directory for Dockerfiles.
func discoverDirectory(dir string, opts Options, seen map[string]bool) ([]DiscoveredFile, error) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}

	var results []DiscoveredFile

	// Build all patterns to check (recursive + direct)
	var patterns []string
	for _, pattern := range opts.Patterns {
		patterns = append(patterns,
			filepath.Join(absDir, "**", pattern), // Recursive
			filepath.Join(absDir, pattern),       // Direct
		)
	}

	// Find matches for all patterns
	for _, pattern := range patterns {
		discovered, err := globMatches(pattern, opts, seen)
		if err != nil {
			return nil, err
		}
		results = append(results, discovered...)
	}

	return results, nil
}

// globMatches expands a glob pattern and returns matching files.
func globMatches(pattern string, opts Options, seen map[string]bool) ([]DiscoveredFile, error) {
	matches, err := doublestar.FilepathGlob(pattern, doublestar.WithFilesOnly())
	if err != nil {
		return nil, err
	}

	var results []DiscoveredFile

	for _, match := range matches {
		absPath, err := filepath.Abs(match)
		if err != nil {
			return nil, err
		}

		// Check for exclusion
		if isExcluded(absPath, opts.ExcludePatterns) {
			continue
		}

		// Skip if already seen
		if seen[absPath] {
			continue
		}
		seen[absPath] = true

		df := DiscoveredFile{
			Path:       absPath,
			ConfigRoot: filepath.Dir(absPath),
			ContextDir: opts.ContextDir,
		}
		results = append(results, df)
	}

	return results, nil
}

// discoverGlob expands a glob pattern and returns matching files.
func discoverGlob(pattern string, opts Options, seen map[string]bool) ([]DiscoveredFile, error) {
	return globMatches(pattern, opts, seen)
}

// isExcluded checks if a path matches any exclusion pattern.
// Patterns use doublestar glob syntax (**, *, ?, [...]).
//
// For relative patterns (e.g., "vendor/*"), we automatically prepend "**/" to
// match at any directory depth. Use absolute patterns or leading "/" to match
// from the root.
//
// Note: doublestar.Match expects forward slashes as path separators even on Windows.
func isExcluded(absPath string, excludePatterns []string) bool {
	// Normalize path to forward slashes for doublestar
	pathSlash := filepath.ToSlash(absPath)

	for _, pattern := range excludePatterns {
		pattern = filepath.ToSlash(pattern)

		// For relative patterns, prepend **/ to match at any depth
		// This makes "vendor/*" equivalent to "**/vendor/*"
		if !strings.HasPrefix(pattern, "/") && !strings.HasPrefix(pattern, "**/") {
			pattern = "**/" + pattern
		}

		matched, err := doublestar.Match(pattern, pathSlash)
		if err == nil && matched {
			return true
		}
	}
	return false
}
