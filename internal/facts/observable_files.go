package facts

import (
	"maps"
	pathpkg "path"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/moby/buildkit/frontend/dockerfile/command"
	"github.com/moby/buildkit/frontend/dockerfile/parser"

	"github.com/wharflab/tally/internal/semantic"
	"github.com/wharflab/tally/internal/shell"
)

// ContextFileReader provides read access to regular files in the build context.
type ContextFileReader interface {
	FileExists(path string) bool
	ReadFile(path string) ([]byte, error)
	IsIgnored(path string) (bool, error)
	IsHeredocFile(path string) bool
}

// BuildContextSource describes a COPY/ADD source reference against the Docker
// build context.
type BuildContextSource struct {
	Instruction          string
	SourcePath           string
	NormalizedSourcePath string
	Line                 int
	Location             []parser.Range
	AvailableInContext   bool
	AvailabilityErr      error
	// ObservableFileSourcePath is set when this source is a concrete regular
	// context file whose content can be observed at lint time.
	ObservableFileSourcePath string
}

// ObservableFileSource describes how a file's content became observable.
type ObservableFileSource string

const (
	ObservableFileSourceCopyHeredoc ObservableFileSource = "copy-heredoc"
	ObservableFileSourceAddHeredoc  ObservableFileSource = "add-heredoc"
	ObservableFileSourceAddContext  ObservableFileSource = "add-context"
	ObservableFileSourceCopyStage   ObservableFileSource = "copy-stage"
	ObservableFileSourceRun         ObservableFileSource = command.Run
	ObservableFileSourceCopyContext ObservableFileSource = "copy-context"
)

// ObservableFile describes an image file whose content can be observed at lint time.
type ObservableFile struct {
	// Path is the final in-image path.
	Path string

	// Source identifies how the content became observable.
	Source ObservableFileSource

	// Line is the 1-based instruction line that wrote this content.
	Line int

	// IsAppend is true when the observed content was appended instead of overwritten.
	IsAppend bool

	// Chmod is the chmod value applied as part of the observable write, when known.
	Chmod string

	// Chown is the chown value applied as part of the observable write, when known.
	Chown string

	once         sync.Once
	loadContent  func() (string, bool)
	content      string
	contentKnown bool
}

// ObservablePathView provides normalized, platform-aware matching helpers for
// an observable in-image path.
type ObservablePathView struct {
	normalized      string
	base            string
	segments        []string
	caseInsensitive bool
}

// Content returns the observable content for this file.
func (f *ObservableFile) Content() (string, bool) {
	if f == nil {
		return "", false
	}

	f.once.Do(func() {
		if f.loadContent == nil {
			return
		}
		f.content, f.contentKnown = f.loadContent()
		f.loadContent = nil
	})

	return f.content, f.contentKnown
}

// Normalized returns the normalized path representation used for matching.
func (v ObservablePathView) Normalized() string { return v.normalized }

// Base returns the base name of the normalized path.
func (v ObservablePathView) Base() string { return v.base }

// HasSegment reports whether the normalized path contains the given path segment.
func (v ObservablePathView) HasSegment(segment string) bool {
	return slices.Contains(v.segments, v.normalizeToken(segment))
}

// HasSuffix reports whether the normalized path ends with the given suffix.
func (v ObservablePathView) HasSuffix(suffix string) bool {
	return strings.HasSuffix(v.normalized, v.normalizePath(suffix))
}

func (v ObservablePathView) normalizePath(path string) string {
	return normalizeObservableMatchPath(path, v.caseInsensitive)
}

func (v ObservablePathView) normalizeToken(token string) string {
	if v.caseInsensitive {
		return strings.ToLower(token)
	}
	return token
}

type observableFileTracker struct {
	latest map[string]*ObservableFile
}

func newObservableFileTracker(parent map[string]*ObservableFile) *observableFileTracker {
	return &observableFileTracker{latest: maps.Clone(parent)}
}

func (t *observableFileTracker) overwrite(file *ObservableFile) {
	if t == nil || file == nil {
		return
	}
	if t.latest == nil {
		t.latest = make(map[string]*ObservableFile)
	}
	t.latest[normalizeObservablePath(file.Path)] = file
}

func (t *observableFileTracker) append(file *ObservableFile) {
	if t == nil || file == nil {
		return
	}

	path := normalizeObservablePath(file.Path)
	prev := t.latest[path]
	if prev == nil {
		delete(t.latest, path)
		return
	}

	t.latest[path] = &ObservableFile{
		Path:   path,
		Source: file.Source,
		Line:   file.Line,
		Chmod:  file.Chmod,
		Chown:  file.Chown,
		loadContent: func() (string, bool) {
			base, ok := prev.Content()
			if !ok {
				return "", false
			}
			extra, ok := file.Content()
			if !ok {
				return "", false
			}
			return base + extra, true
		},
	}
}

func (t *observableFileTracker) invalidate(path string) {
	if t == nil {
		return
	}
	delete(t.latest, normalizeObservablePath(path))
}

func (t *observableFileTracker) snapshot() map[string]*ObservableFile {
	if t == nil || len(t.latest) == 0 {
		return nil
	}
	return maps.Clone(t.latest)
}

func literalObservableFile(
	path string,
	source ObservableFileSource,
	line int,
	isAppend bool,
	chmod, chown, content string,
) *ObservableFile {
	return &ObservableFile{
		Path:     normalizeObservablePath(path),
		Source:   source,
		Line:     line,
		IsAppend: isAppend,
		Chmod:    chmod,
		Chown:    chown,
		loadContent: func() (string, bool) {
			return content, true
		},
	}
}

func contextObservableFile(
	path string,
	source ObservableFileSource,
	line int,
	chmod, chown, sourcePath string,
	ctx ContextFileReader,
) *ObservableFile {
	return &ObservableFile{
		Path:   normalizeObservablePath(path),
		Source: source,
		Line:   line,
		Chmod:  chmod,
		Chown:  chown,
		loadContent: func() (string, bool) {
			if ctx == nil {
				return "", false
			}
			content, err := ctx.ReadFile(sourcePath)
			if err != nil {
				return "", false
			}
			return string(content), true
		},
	}
}

func stageCopyObservableFile(path string, line int, chmod, chown string, source *ObservableFile) *ObservableFile {
	if source == nil {
		return nil
	}
	return &ObservableFile{
		Path:   normalizeObservablePath(path),
		Source: ObservableFileSourceCopyStage,
		Line:   line,
		Chmod:  chmod,
		Chown:  chown,
		loadContent: func() (string, bool) {
			return source.Content()
		},
	}
}

// ObservablePathView returns a platform-aware path view for matching against
// stage observable files.
func (s *StageFacts) ObservablePathView(path string) ObservablePathView {
	caseInsensitive := s != nil && s.BaseImageOS == semantic.BaseImageOSWindows
	normalized := normalizeObservableMatchPath(path, caseInsensitive)
	return ObservablePathView{
		normalized:      normalized,
		base:            pathpkg.Base(normalized),
		segments:        strings.Split(normalized, "/"),
		caseInsensitive: caseInsensitive,
	}
}

// ScanObservableFiles iterates over observable files and provides each file's
// platform-aware path view to the callback. Returning false stops the scan.
func (s *StageFacts) ScanObservableFiles(scan func(*ObservableFile, ObservablePathView) bool) {
	if s == nil || scan == nil {
		return
	}
	for _, file := range s.ObservableFiles {
		if file == nil {
			continue
		}
		if !scan(file, s.ObservablePathView(file.Path)) {
			return
		}
	}
}

// HasObservablePathSuffix reports whether any observable file path in the stage
// ends with one of the supplied suffixes, using platform-aware matching.
func (s *StageFacts) HasObservablePathSuffix(suffixes ...string) bool {
	if s == nil || len(suffixes) == 0 {
		return false
	}

	found := false
	s.ScanObservableFiles(func(_ *ObservableFile, path ObservablePathView) bool {
		if slices.ContainsFunc(suffixes, path.HasSuffix) {
			found = true
			return false
		}
		return true
	})

	return found
}

func normalizeObservablePath(path string) string {
	if path == "" {
		return ""
	}
	return pathpkg.Clean(path)
}

func normalizeObservableMatchPath(path string, caseInsensitive bool) string {
	if path == "" {
		return ""
	}
	path = pathpkg.Clean(strings.ReplaceAll(path, `\`, "/"))
	if caseInsensitive {
		path = strings.ToLower(path)
	}
	return path
}

func resolveCopyDestPath(rawDest, sourceName, workdir string, sourceCount int) (string, bool) {
	if rawDest == "" || sourceName == "" {
		return "", false
	}

	dest := rawDest
	if !pathpkg.IsAbs(dest) {
		dest = pathpkg.Join(workdir, dest)
	}
	dest = pathpkg.Clean(dest)

	if sourceCount <= 1 && !copyDestLooksLikeDirectory(rawDest) {
		return dest, true
	}
	if !copyDestLooksLikeDirectory(rawDest) {
		return "", false
	}

	base := strings.TrimSuffix(sourceName, "/")
	if base == "" {
		return "", false
	}

	return pathpkg.Join(dest, pathpkg.Base(base)), true
}

func copyDestLooksLikeDirectory(dest string) bool {
	cleaned := pathpkg.Clean(dest)
	return strings.HasSuffix(dest, "/") || cleaned == "." || cleaned == ".."
}

func resolveRuntimeScriptPath(path, workdir string) string {
	if path == "" {
		return ""
	}
	if pathpkg.IsAbs(path) {
		return normalizeObservablePath(path)
	}
	if workdir == "" {
		workdir = "/"
	}
	return pathpkg.Clean(pathpkg.Join(workdir, path))
}

func resolveStageCopySourcePath(path, workdir string) string {
	if path == "" {
		return ""
	}
	if pathpkg.IsAbs(path) {
		return normalizeObservablePath(path)
	}
	if workdir == "" {
		workdir = "/"
	}
	return pathpkg.Clean(pathpkg.Join(workdir, path))
}

func normalizeBuildContextSourcePath(path string) string {
	cleaned := filepath.Clean(path)
	if cleaned == "." {
		return "."
	}
	return filepath.ToSlash(cleaned)
}

func isBuildContextURLSource(path string) bool {
	return shell.IsURL(path) || strings.HasPrefix(path, "git://")
}
