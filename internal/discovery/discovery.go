// Package discovery provides Dockerfile discovery with glob pattern support.
package discovery

import (
	"cmp"
	"os"
	"path/filepath"
	"slices"

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

// isExcluded checks if a path matches any exclusion pattern using a three-step
// matching strategy:
//
//  1. Match against the full absolute path (for absolute patterns)
//  2. Match against just the filename/basename (for simple patterns like "*.bak")
//  3. Match against each suffix subpath produced by splitPath (for relative patterns
//     like "vendor/*" or "test/**")
//
// The subpath matching (step 3) allows patterns like "vendor/*" to match files that
// are direct children of any "vendor" directory component in the path, without
// matching deeply nested files. For example, "vendor/*" matches "vendor/Dockerfile"
// but not "sub/vendor/Dockerfile" when processed as a subpath.
//
// Complexity: O(patterns × path_depth) matching operations per file, which is
// acceptable for typical directory hierarchies (5-10 levels) with modest patterns.
//
// Note: doublestar.Match expects forward slashes as path separators even on Windows.
// We normalize all paths to forward slashes before matching for cross-platform compatibility.
func isExcluded(absPath string, excludePatterns []string) bool {
	// Normalize path to forward slashes for doublestar (which always uses /)
	absPathSlash := filepath.ToSlash(absPath)
	base := filepath.ToSlash(filepath.Base(absPath))

	for _, pattern := range excludePatterns {
		// Normalize pattern to forward slashes as well
		pattern = filepath.ToSlash(pattern)

		// Step 1: Match against full absolute path
		matched, err := doublestar.Match(pattern, absPathSlash)
		if err == nil && matched {
			return true
		}

		// Step 2: Match against just the filename
		matched, err = doublestar.Match(pattern, base)
		if err == nil && matched {
			return true
		}

		// Step 3: Match against each suffix subpath from splitPath
		// This enables patterns like "vendor/*" to match any vendor directory
		// at any level in the path hierarchy
		parts := splitPath(absPath)
		for i := range parts {
			subpath := filepath.ToSlash(filepath.Join(parts[i:]...))
			matched, err = doublestar.Match(pattern, subpath)
			if err == nil && matched {
				return true
			}
		}
	}
	return false
}

// splitPath splits a path into its individual directory and filename components.
// For example, "/home/user/vendor/Dockerfile" returns ["home", "user", "vendor", "Dockerfile"].
// On Windows, "C:\foo\bar" returns ["foo", "bar"] (drive letter is stripped).
// Used by isExcluded to generate suffix subpaths for pattern matching.
func splitPath(path string) []string {
	var parts []string
	for path != "" {
		dir, file := filepath.Split(path)
		if file != "" {
			parts = append([]string{file}, parts...)
		}
		path = filepath.Clean(dir)

		// Stop at Unix root or current directory
		if path == "/" || path == "." {
			break
		}

		// Stop at Windows volume root (e.g., "C:\")
		// filepath.VolumeName returns "C:" for "C:\", empty for Unix paths
		vol := filepath.VolumeName(path)
		if vol != "" && (path == vol || path == vol+string(filepath.Separator)) {
			break
		}
	}
	return parts
}
