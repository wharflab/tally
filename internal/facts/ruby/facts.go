// Package ruby provides parsed, cached views of common Ruby/Rails project
// files in the build context (Gemfile, Gemfile.lock, .ruby-version,
// .tool-versions). Rules under the tally/ruby/* namespace consume the typed
// facts here instead of re-parsing the files themselves.
//
// All entry points return nil when the file is unobservable or fails to parse:
// rules then degrade gracefully to Dockerfile-only mode.
package ruby

import (
	"sync"
)

// RubyFacts bundles every observable Ruby project file the rule namespace
// cares about. Each pointer is nil when the underlying file is missing,
// unobservable, or malformed.
type RubyFacts struct {
	// Lockfile holds parsed Gemfile.lock data, or nil when no observable
	// lockfile is present.
	Lockfile *LockfileFacts

	// Gemfile holds parsed Gemfile data, or nil when no observable Gemfile is
	// present.
	Gemfile *GemfileFacts

	// RubyVersion is the project's resolved Ruby version, in priority order:
	//
	//  1. .ruby-version
	//  2. .tool-versions (the `ruby` line)
	//  3. Lockfile.RubyVersion (RUBY VERSION block)
	//
	// Empty when none of the sources yield a value.
	RubyVersion string
}

// ContextFileReader is the minimal subset of internal/context.BuildContext
// that the loader needs. Declared locally so the package does not import
// internal/facts or internal/context, keeping import direction clean.
type ContextFileReader interface {
	// FileExists reports whether path resolves to a regular file.
	FileExists(path string) bool

	// ReadFile reads a regular file's content.
	ReadFile(path string) ([]byte, error)
}

// Standard project-root paths the loader inspects. Using slashes matches the
// build context's path semantics across operating systems.
const (
	gemfileLockPath  = "Gemfile.lock"
	gemfilePath      = "Gemfile"
	rubyVersionPath  = ".ruby-version"
	toolVersionsPath = ".tool-versions"
)

// Load reads the four well-known Ruby project files from the build context
// and returns parsed RubyFacts. A nil reader yields a non-nil RubyFacts with
// every pointer field nil (rules can then call .Lockfile == nil etc. without
// special-casing). Read errors are silently treated as "no signal" — the
// corresponding pointer is left nil.
//
// Load itself is not memoized. Callers that need per-file caching should use
// LoadCached, which keys a sync.Once result by the supplied reader.
func Load(reader ContextFileReader) *RubyFacts {
	facts := &RubyFacts{}
	if reader == nil {
		return facts
	}

	if data, ok := safeRead(reader, gemfileLockPath); ok {
		facts.Lockfile = ParseLockfile(data)
	}
	if data, ok := safeRead(reader, gemfilePath); ok {
		facts.Gemfile = ParseGemfile(data)
	}
	facts.RubyVersion = resolveRubyVersion(reader, facts.Lockfile)
	return facts
}

// resolveRubyVersion picks the highest-priority Ruby version available.
func resolveRubyVersion(reader ContextFileReader, lock *LockfileFacts) string {
	if data, ok := safeRead(reader, rubyVersionPath); ok {
		if v := ParseRubyVersionFile(data); v != "" {
			return v
		}
	}
	if data, ok := safeRead(reader, toolVersionsPath); ok {
		if v := ParseToolVersionsFile(data); v != "" {
			return v
		}
	}
	if lock != nil && lock.RubyVersion != "" {
		return extractRubyVersionFromLockfile(lock.RubyVersion)
	}
	return ""
}

// extractRubyVersionFromLockfile parses the value of the lockfile RUBY VERSION
// block. Examples:
//
//	"ruby 3.3.5p100"  -> "3.3.5p100"
//	"3.3.5"           -> "3.3.5"
func extractRubyVersionFromLockfile(raw string) string {
	if raw == "" {
		return ""
	}
	// Drop a leading "ruby" word, then return the next field. We avoid
	// strings.Fields here so that values like "ruby 3.3.5p100 with stuff"
	// still return "3.3.5p100".
	parts := splitFieldsLimit(raw, 3)
	if len(parts) == 0 {
		return ""
	}
	if len(parts) >= 2 && (parts[0] == "ruby" || parts[0] == "Ruby") {
		return parts[1]
	}
	return parts[0]
}

func splitFieldsLimit(s string, limit int) []string {
	out := make([]string, 0, limit)
	start := -1
	for i := range len(s) {
		if s[i] == ' ' || s[i] == '\t' {
			if start >= 0 {
				out = append(out, s[start:i])
				start = -1
				if len(out) == limit {
					return out
				}
			}
			continue
		}
		if start < 0 {
			start = i
		}
	}
	if start >= 0 && len(out) < limit {
		out = append(out, s[start:])
	}
	return out
}

// safeRead wraps reader.ReadFile so callers do not need to discriminate
// "missing" from "unreadable" errors: both reduce to "no observable signal".
// FileExists is checked first so callers bypass costly reads (and any
// caching side effects) for absent files.
func safeRead(reader ContextFileReader, path string) ([]byte, bool) {
	if !reader.FileExists(path) {
		return nil, false
	}
	data, err := reader.ReadFile(path)
	if err != nil {
		return nil, false
	}
	return data, true
}

// cacheEntry pairs a memoized RubyFacts result with the sync.Once that
// produced it.
type cacheEntry struct {
	once  sync.Once
	facts *RubyFacts
}

// loaderCache memoizes RubyFacts per ContextFileReader. The cache is keyed by
// the interface value (which compares the underlying pointer for the
// *context.BuildContext implementation), so each Dockerfile gets its own
// entry. The cache lives for the lifetime of the process; in normal tally
// runs the number of distinct readers is bounded by the number of linted
// invocations, so a sync.Map is appropriate.
var loaderCache sync.Map

// LoadCached returns the cached RubyFacts for the supplied reader, computing
// it on first call. Subsequent calls with the same reader value return the
// same *RubyFacts, so rules that re-enter Check() do not re-parse Ruby files.
//
// LoadCached(nil) is equivalent to Load(nil) and is not cached.
func LoadCached(reader ContextFileReader) *RubyFacts {
	if reader == nil {
		return Load(nil)
	}

	value, _ := loaderCache.LoadOrStore(reader, &cacheEntry{})
	entry, ok := value.(*cacheEntry)
	if !ok {
		// Defensive: only this package writes to loaderCache, so the cast
		// should always succeed. Falling back to an uncached Load avoids a
		// panic if the cache is somehow corrupted.
		return Load(reader)
	}
	entry.once.Do(func() {
		entry.facts = Load(reader)
	})
	return entry.facts
}
