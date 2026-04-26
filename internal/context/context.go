// Package context provides build context awareness for Dockerfile linting.
// It handles .dockerignore parsing and file existence checking.
package context

import (
	"errors"
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

var errNotRegularFile = errors.New("build context path is not a regular file")

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

	// lstat allows tests to observe and control path validation.
	lstat func(string) (os.FileInfo, error)

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
		lstat:          os.Lstat,
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
	_, _, err := ctx.resolvePath(path)
	return err == nil
}

// PathExists checks if a file or directory exists in the build context.
// The path should be relative to the build context.
func (ctx *BuildContext) PathExists(path string) bool {
	_, _, err := ctx.resolveExistingPath(path, false)
	return err == nil
}

// ReadFile reads a regular file from the build context.
// The path must be relative to the context root.
func (ctx *BuildContext) ReadFile(path string) ([]byte, error) {
	key, fullPath, err := ctx.resolvePath(path)
	if err != nil {
		if key != "" {
			ctx.mu.Lock()
			ctx.fileCache[key] = cachedFile{err: err}
			ctx.mu.Unlock()
		}
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
	return ctx.resolveExistingPath(path, true)
}

func (ctx *BuildContext) resolveExistingPath(path string, regularOnly bool) (string, string, error) {
	normalized := filepath.Clean(filepath.FromSlash(path))
	if normalized == "" || filepath.IsAbs(normalized) {
		return "", "", os.ErrNotExist
	}
	if normalized == "." && regularOnly {
		return "", "", os.ErrNotExist
	}

	fullPath := ctx.ContextDir
	if normalized != "." {
		fullPath = filepath.Join(ctx.ContextDir, normalized)
	}
	rel, err := filepath.Rel(ctx.ContextDir, fullPath)
	if err != nil {
		return "", "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", "", os.ErrNotExist
	}

	key := filepath.ToSlash(rel)
	if err := ctx.validateExistingPath(normalized, regularOnly); err != nil {
		return key, fullPath, err
	}

	return key, fullPath, nil
}

func (ctx *BuildContext) validateExistingPath(normalized string, regularOnly bool) error {
	if normalized == "." {
		fi, err := ctx.lstat(ctx.ContextDir)
		if err != nil {
			return err
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			return errNotRegularFile
		}
		if regularOnly && !fi.Mode().IsRegular() {
			return errNotRegularFile
		}
		if !regularOnly && !fi.Mode().IsRegular() && !fi.IsDir() {
			return errNotRegularFile
		}
		return nil
	}

	current := ctx.ContextDir
	parts := strings.Split(normalized, string(filepath.Separator))
	for idx, part := range parts {
		current = filepath.Join(current, part)
		fi, err := ctx.lstat(current)
		if err != nil {
			return err
		}
		mode := fi.Mode()
		if mode&os.ModeSymlink != 0 {
			return errNotRegularFile
		}
		isLast := idx == len(parts)-1
		if isLast {
			if regularOnly && !mode.IsRegular() {
				return errNotRegularFile
			}
			if !regularOnly && !mode.IsRegular() && !fi.IsDir() {
				return errNotRegularFile
			}
			continue
		}
		if !fi.IsDir() {
			return os.ErrNotExist
		}
	}
	return nil
}
