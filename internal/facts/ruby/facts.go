// Package ruby provides parsed, cached views of common Ruby/Rails project
// files in the build context (Gemfile, Gemfile.lock, .ruby-version,
// .tool-versions). Rules under the tally/ruby/* namespace consume the typed
// facts here instead of re-parsing the files themselves.
//
// All entry points return nil when the file is unobservable or fails to parse:
// rules then degrade gracefully to Dockerfile-only mode.
package ruby

import (
	"strings"
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

	// HasEncryptedCredentials reports whether the project ships any of the
	// canonical Rails encrypted-credentials files (config/credentials.yml.enc
	// or config/credentials/<env>.yml.enc for one of the standard envs).
	// Rules that need to know whether RAILS_MASTER_KEY is actually load-bearing
	// at build time (e.g. asset-precompile-without-dummy-key) consult this.
	HasEncryptedCredentials bool

	// EncryptedCredentialsPaths records the observed credentials files in the
	// build context. Order matches the probe order. Empty when no credentials
	// files are observable.
	EncryptedCredentialsPaths []string
}

// ContextFileReader is the minimal subset of internal/context.BuildContext
// that the loader needs. Declared locally so the package does not import
// internal/facts or internal/context, keeping import direction clean.
type ContextFileReader interface {
	// FileExists reports whether path resolves to a regular file in the
	// build context.
	FileExists(path string) bool

	// ReadFile reads a regular file's content from the build context.
	ReadFile(path string) ([]byte, error)

	// IsIgnored reports whether the path is excluded by .dockerignore.
	// Implementations that don't model .dockerignore should return
	// (false, nil); a non-nil error reduces to "treat as ignored" so we
	// don't reason over inputs the build process cannot see.
	IsIgnored(path string) (bool, error)
}

// Standard project-root paths the loader inspects. Using slashes matches the
// build context's path semantics across operating systems.
//
// The credentialsEnc* names are file paths the loader probes with
// FileExists, not secret material — gosec G101 false-positives on the
// "credentials" substring.
const (
	gemfileLockPath              = "Gemfile.lock"
	gemfilePath                  = "Gemfile"
	rubyVersionPath              = ".ruby-version"
	toolVersionsPath             = ".tool-versions"
	credentialsEncFilePath       = "config/credentials.yml.enc" // #nosec G101 -- file path, not a secret
	credentialsEncEnvDirPath     = "config/credentials"         // #nosec G101 -- directory path, not a secret
	credentialsEncFilenameSuffix = ".yml.enc"                   // #nosec G101 -- filename suffix, not a secret
)

// credentialsEncEnvNames is the curated list of Rails environments whose
// per-env encrypted credentials file we probe. Rails ships `production`,
// `development`, and `test` by default; `staging` is the most common
// custom env we see in the corpus.
var credentialsEncEnvNames = []string{"production", "development", "test", "staging"}

// Load reads the four well-known Ruby project files from the build context
// and returns parsed RubyFacts. A nil reader yields a non-nil RubyFacts with
// every pointer field nil (rules can then call .Lockfile == nil etc. without
// special-casing). Read errors and .dockerignore-excluded paths are silently
// treated as "no signal" — the corresponding pointer is left nil.
//
// Memoization is the caller's responsibility; for the standard rule pipeline
// it happens inside *facts.FileFacts via sync.Once.
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
	facts.EncryptedCredentialsPaths = probeEncryptedCredentials(reader)
	facts.HasEncryptedCredentials = len(facts.EncryptedCredentialsPaths) > 0
	return facts
}

// probeEncryptedCredentials returns every observable Rails encrypted-credentials
// file in the build context. Both the canonical single-file form
// (`config/credentials.yml.enc`) and the per-environment form
// (`config/credentials/<env>.yml.enc`) are supported. Paths returned by this
// function pass the .dockerignore filter — i.e. they are observable to the
// build process.
func probeEncryptedCredentials(reader ContextFileReader) []string {
	if reader == nil {
		return nil
	}
	candidates := make([]string, 0, 1+len(credentialsEncEnvNames))
	candidates = append(candidates, credentialsEncFilePath)
	for _, env := range credentialsEncEnvNames {
		candidates = append(candidates, credentialsEncEnvDirPath+"/"+env+credentialsEncFilenameSuffix)
	}
	var found []string
	for _, candidate := range candidates {
		if !contextFileObservable(reader, candidate) {
			continue
		}
		found = append(found, candidate)
	}
	return found
}

// contextFileObservable reports whether path resolves to a regular file in
// the build context that is not excluded by .dockerignore. Read errors are
// treated as "not observable" so rules degrade gracefully when the build
// context cannot answer.
func contextFileObservable(reader ContextFileReader, path string) bool {
	if reader == nil {
		return false
	}
	ignored, err := reader.IsIgnored(path)
	if err != nil || ignored {
		return false
	}
	return reader.FileExists(path)
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
	fields := strings.Fields(raw)
	if len(fields) == 0 {
		return ""
	}
	if len(fields) >= 2 && strings.EqualFold(fields[0], "ruby") {
		return fields[1]
	}
	return fields[0]
}

// safeRead wraps reader.ReadFile so callers do not need to discriminate
// "missing", "unreadable", or "ignored by .dockerignore" — all three reduce
// to "no observable signal". The .dockerignore check happens first so we
// never reason over inputs the build process would not see.
func safeRead(reader ContextFileReader, path string) ([]byte, bool) {
	ignored, err := reader.IsIgnored(path)
	if err != nil || ignored {
		return nil, false
	}
	if !reader.FileExists(path) {
		return nil, false
	}
	data, readErr := reader.ReadFile(path)
	if readErr != nil {
		return nil, false
	}
	return data, true
}
