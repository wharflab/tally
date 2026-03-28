// Package context provides build context awareness for Dockerfile linting.
// It handles .dockerignore parsing and file existence checking.
package context

import (
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/moby/patternmatcher"
)

type cachedFile struct {
	content []byte
	err     error
}

// BuildContext provides build-time context for context-aware rules.
// It manages .dockerignore patterns and file existence checking.
type BuildContext struct {
	// ContextDir is the absolute path to the build context directory.
	ContextDir string

	// DockerfilePath is the absolute path to the Dockerfile being linted.
	DockerfilePath string

	// heredocFiles are virtual files created by heredoc syntax.
	// These should not trigger copy-ignored-file warnings.
	heredocFiles map[string]bool

	// mu protects lazy initialization
	mu sync.RWMutex

	// patternMatcher is lazily initialized from .dockerignore
	patternMatcher *patternmatcher.PatternMatcher

	// patterns stores the raw patterns for debugging
	patterns []string

	// fileCache stores lazily read build-context files by normalized relative path.
	fileCache map[string]cachedFile

	// readFile allows tests to observe and control file reads.
	readFile func(string) ([]byte, error)

	// initialized tracks if patternMatcher was initialized
	initialized bool

	// initErr stores initialization error
	initErr error
}

// Option configures a BuildContext.
type Option func(*BuildContext)

// WithHeredocFiles sets the virtual heredoc files.
// These files are created by heredoc syntax in COPY/ADD commands
// and should not trigger copy-ignored-file warnings.
func WithHeredocFiles(files map[string]bool) Option {
	return func(ctx *BuildContext) {
		ctx.heredocFiles = files
	}
}

// New creates a new BuildContext for the given context directory.
// The dockerfilePath is used for relative path calculations.
func New(contextDir, dockerfilePath string, opts ...Option) (*BuildContext, error) {
	absContext, err := filepath.Abs(contextDir)
	if err != nil {
		return nil, err
	}

	absDockerfile := dockerfilePath
	if dockerfilePath != "" {
		absDockerfile, err = filepath.Abs(dockerfilePath)
		if err != nil {
			return nil, err
		}
	}

	ctx := &BuildContext{
		ContextDir:     absContext,
		DockerfilePath: absDockerfile,
		heredocFiles:   make(map[string]bool),
		fileCache:      make(map[string]cachedFile),
		readFile:       os.ReadFile,
	}

	for _, opt := range opts {
		opt(ctx)
	}

	return ctx, nil
}

// IsIgnored checks if a path would be ignored by .dockerignore.
// The path should be relative to the build context.
func (ctx *BuildContext) IsIgnored(path string) (bool, error) {
	if err := ctx.ensureInitialized(); err != nil {
		return false, err
	}

	ctx.mu.RLock()
	defer ctx.mu.RUnlock()

	if ctx.patternMatcher == nil {
		return false, nil
	}

	// Normalize path separators
	path = filepath.ToSlash(path)

	// Use MatchesOrParentMatches for correct directory matching
	return ctx.patternMatcher.MatchesOrParentMatches(path)
}

// FileExists checks if a file exists in the build context.
// The path should be relative to the build context.
// Returns false for directories (only regular files return true).
func (ctx *BuildContext) FileExists(path string) bool {
	_, fullPath, err := ctx.resolvePath(path)
	if err != nil {
		return false
	}
	fi, err := os.Stat(fullPath)
	if err != nil {
		return false
	}
	return !fi.IsDir()
}

// ReadFile reads a regular file from the build context.
// The path must be relative to the context root.
func (ctx *BuildContext) ReadFile(path string) ([]byte, error) {
	key, fullPath, err := ctx.resolvePath(path)
	if err != nil {
		return nil, err
	}

	ctx.mu.RLock()
	if cached, ok := ctx.fileCache[key]; ok {
		ctx.mu.RUnlock()
		if cached.err != nil {
			return nil, cached.err
		}
		return append([]byte(nil), cached.content...), nil
	}
	ctx.mu.RUnlock()

	ctx.mu.Lock()
	defer ctx.mu.Unlock()

	if cached, ok := ctx.fileCache[key]; ok {
		if cached.err != nil {
			return nil, cached.err
		}
		return append([]byte(nil), cached.content...), nil
	}

	content, readErr := ctx.readFile(fullPath)
	ctx.fileCache[key] = cachedFile{
		content: append([]byte(nil), content...),
		err:     readErr,
	}

	if readErr != nil {
		return nil, readErr
	}
	return content, nil
}

// IsHeredocFile checks if a path is a virtual heredoc file.
// Heredoc files are created inline in the Dockerfile and should
// not be checked against .dockerignore.
func (ctx *BuildContext) IsHeredocFile(path string) bool {
	ctx.mu.RLock()
	defer ctx.mu.RUnlock()
	return ctx.heredocFiles[path]
}

// AddHeredocFile marks a path as a virtual heredoc file.
func (ctx *BuildContext) AddHeredocFile(path string) {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	if ctx.heredocFiles == nil {
		ctx.heredocFiles = make(map[string]bool)
	}
	ctx.heredocFiles[path] = true
}

// Patterns returns the .dockerignore patterns (for debugging).
func (ctx *BuildContext) Patterns() []string {
	if err := ctx.ensureInitialized(); err != nil {
		return nil
	}

	ctx.mu.RLock()
	defer ctx.mu.RUnlock()
	return ctx.patterns
}

// HasIgnoreFile returns true if a .dockerignore file exists.
func (ctx *BuildContext) HasIgnoreFile() bool {
	if err := ctx.ensureInitialized(); err != nil {
		return false
	}

	ctx.mu.RLock()
	defer ctx.mu.RUnlock()
	return len(ctx.patterns) > 0
}

// HasIgnoreExclusions returns true if .dockerignore contains negated patterns (lines starting with !).
// When exclusions exist, static copy-source validation is unreliable because
// a directory may be excluded but a file inside it re-included.
func (ctx *BuildContext) HasIgnoreExclusions() bool {
	if err := ctx.ensureInitialized(); err != nil {
		return false
	}

	ctx.mu.RLock()
	defer ctx.mu.RUnlock()
	if ctx.patternMatcher == nil {
		return false
	}
	return ctx.patternMatcher.Exclusions()
}

// ensureInitialized lazily loads .dockerignore patterns.
func (ctx *BuildContext) ensureInitialized() error {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()

	if ctx.initialized {
		return ctx.initErr
	}

	ctx.initialized = true
	ctx.patterns, ctx.initErr = LoadDockerignore(ctx.ContextDir)
	if ctx.initErr != nil {
		return ctx.initErr
	}

	if len(ctx.patterns) > 0 {
		ctx.patternMatcher, ctx.initErr = patternmatcher.New(ctx.patterns)
	}

	return ctx.initErr
}

func (ctx *BuildContext) resolvePath(path string) (string, string, error) {
	normalized := filepath.Clean(filepath.FromSlash(path))
	if normalized == "" || normalized == "." || filepath.IsAbs(normalized) {
		return "", "", os.ErrNotExist
	}

	fullPath := filepath.Join(ctx.ContextDir, normalized)
	rel, err := filepath.Rel(ctx.ContextDir, fullPath)
	if err != nil {
		return "", "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", "", os.ErrNotExist
	}

	return filepath.ToSlash(rel), fullPath, nil
}
