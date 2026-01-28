// Package context provides build context awareness for Dockerfile linting.
// It handles .dockerignore parsing and file existence checking.
package context

import (
	"os"
	"path/filepath"
	"sync"

	"github.com/moby/patternmatcher"
)

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
	fullPath := filepath.Join(ctx.ContextDir, path)
	fi, err := os.Stat(fullPath)
	if err != nil {
		return false
	}
	return !fi.IsDir()
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
