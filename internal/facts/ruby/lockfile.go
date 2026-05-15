package ruby

import (
	"strings"
)

// LockfileFacts is the typed projection of Gemfile.lock that Ruby rules consume.
// All fields are zero-valued when Gemfile.lock is not observable or fails to parse.
type LockfileFacts struct {
	// BundledWith is the value of the BUNDLED WITH block (may be empty for
	// legacy Bundler 1.x lockfiles that do not include the block).
	BundledWith string

	// RubyVersion is the value of the RUBY VERSION block (may be empty).
	RubyVersion string

	// Platforms lists every entry in the PLATFORMS block.
	Platforms []string

	// Sources lists every remote: URL declared by GEM/GIT/PATH/PLUGIN SOURCE
	// blocks, in the order they appear.
	Sources []string

	// DirectDeps maps gem name to version constraint, from the DEPENDENCIES
	// block. The value preserves the constraint as written in the lockfile
	// (for example "~> 8.0", ">= 1.2.3", or empty when no constraint is
	// listed).
	DirectDeps map[string]string

	// Specs maps gem name to exact version, derived from each block's specs:
	// section. Platform-suffixed gem names like "nokogiri (1.13.6-x86_64-linux)"
	// are normalized to "nokogiri" -> "1.13.6".
	Specs map[string]string

	// HasGitGems is true when at least one GIT block is present.
	HasGitGems bool

	// HasPathGems is true when at least one PATH block is present.
	HasPathGems bool

	// NativeExtGems lists gems detected as native-extension gems via the
	// curated list. Order matches first appearance in the lockfile.
	NativeExtGems []string
}

// nativeExtensionGems is the curated list of well-known gems that ship with
// native (C/C++) extensions. The list is intentionally not exhaustive; rules
// that consume it use NativeExtGems to drive severity escalation only, not
// correctness gates.
//
// Source: design-docs/43-ruby-on-docker.md section 4.5.
var nativeExtensionGems = map[string]bool{
	"nokogiri":     true,
	"pg":           true,
	"mysql2":       true,
	"sqlite3":      true,
	"grpc":         true,
	"ffi":          true,
	"oj":           true,
	"bcrypt":       true,
	"nio4r":        true,
	"puma":         true,
	"rugged":       true,
	"protobuf":     true,
	"sassc":        true,
	"eventmachine": true,
	"unf_ext":      true,
	"racc":         true,
	"bigdecimal":   true,
}

// ParseLockfile parses Gemfile.lock content into typed facts. Returns nil when
// the input is empty or when parsing fails for any reason. Partial input that
// looks plausibly like a lockfile is accepted; missing sections leave the
// corresponding fields zero-valued.
func ParseLockfile(content []byte) *LockfileFacts {
	if len(content) == 0 {
		return nil
	}
	text := string(content)
	if strings.TrimSpace(text) == "" {
		return nil
	}

	facts := &LockfileFacts{
		DirectDeps: map[string]string{},
		Specs:      map[string]string{},
	}

	scanner := &lockfileScanner{}
	for line := range strings.SplitSeq(text, "\n") {
		scanner.feed(line, facts)
	}

	if !looksLikeLockfile(facts) {
		return nil
	}

	scanner.populateNativeExtGems(facts)
	return facts
}

// looksLikeLockfile guards against returning a half-empty struct for inputs
// that did not actually contain a recognizable lockfile section.
func looksLikeLockfile(facts *LockfileFacts) bool {
	return facts.BundledWith != "" ||
		facts.RubyVersion != "" ||
		len(facts.Platforms) > 0 ||
		len(facts.Sources) > 0 ||
		len(facts.Specs) > 0 ||
		len(facts.DirectDeps) > 0 ||
		facts.HasGitGems ||
		facts.HasPathGems
}

// section identifies the current top-level section in the lockfile.
type section int

const (
	sectionNone section = iota
	sectionGEM
	sectionGit
	sectionPath
	sectionPluginSource
	sectionPlatforms
	sectionDependencies
	sectionRubyVersion
	sectionBundledWith
	sectionChecksums
)

// lockfileScanner holds the parser state across lines.
type lockfileScanner struct {
	current   section
	inSpecs   bool
	specOrder []string
}

// feed processes a single line and mutates facts in-place.
func (s *lockfileScanner) feed(rawLine string, facts *LockfileFacts) {
	// Preserve trailing line meaning by stripping only \r (CRLF lockfiles).
	line := strings.TrimRight(rawLine, "\r")
	trimmed := strings.TrimSpace(line)

	if trimmed == "" {
		// Blank line ends the current spec/dep/platform listing but keeps the
		// section header semantics off.
		s.inSpecs = false
		return
	}

	if s.handleSectionHeader(line, trimmed) {
		return
	}

	switch s.current {
	case sectionGEM, sectionGit, sectionPath, sectionPluginSource:
		s.handleSourceBlockLine(line, trimmed, facts)
	case sectionPlatforms:
		facts.Platforms = append(facts.Platforms, trimmed)
	case sectionDependencies:
		s.handleDependencyLine(trimmed, facts)
	case sectionRubyVersion:
		if facts.RubyVersion == "" {
			facts.RubyVersion = trimmed
		}
	case sectionBundledWith:
		if facts.BundledWith == "" {
			facts.BundledWith = trimmed
		}
	case sectionChecksums, sectionNone:
		// CHECKSUMS and any unknown section are ignored.
	}
}

// handleSectionHeader detects top-level section keywords at column 0 and
// returns true when the line is consumed as a section header.
func (s *lockfileScanner) handleSectionHeader(line, trimmed string) bool {
	if line == "" || line[0] == ' ' || line[0] == '\t' {
		return false
	}
	switch trimmed {
	case "GEM":
		s.current = sectionGEM
		s.inSpecs = false
		return true
	case "GIT":
		s.current = sectionGit
		s.inSpecs = false
		return true
	case "PATH":
		s.current = sectionPath
		s.inSpecs = false
		return true
	case "PLUGIN SOURCE":
		s.current = sectionPluginSource
		s.inSpecs = false
		return true
	case "PLATFORMS":
		s.current = sectionPlatforms
		s.inSpecs = false
		return true
	case "DEPENDENCIES":
		s.current = sectionDependencies
		s.inSpecs = false
		return true
	case "RUBY VERSION":
		s.current = sectionRubyVersion
		s.inSpecs = false
		return true
	case "BUNDLED WITH":
		s.current = sectionBundledWith
		s.inSpecs = false
		return true
	case "CHECKSUMS":
		s.current = sectionChecksums
		s.inSpecs = false
		return true
	}
	return false
}

// handleSourceBlockLine consumes lines inside GEM/GIT/PATH/PLUGIN SOURCE
// blocks. Indent depth distinguishes header lines (2 spaces) from spec entries
// (4 spaces) and transitive deps (6 spaces).
func (s *lockfileScanner) handleSourceBlockLine(line, trimmed string, facts *LockfileFacts) {
	indent := leadingSpaces(line)

	if indent == 2 {
		s.inSpecs = false
		switch {
		case strings.HasPrefix(trimmed, "remote:"):
			url := strings.TrimSpace(strings.TrimPrefix(trimmed, "remote:"))
			if url != "" {
				facts.Sources = append(facts.Sources, url)
			}
			if s.current == sectionGit {
				facts.HasGitGems = true
			}
			if s.current == sectionPath {
				facts.HasPathGems = true
			}
		case trimmed == "specs:":
			s.inSpecs = true
		}
		return
	}

	if indent == 4 && s.inSpecs {
		// "actionpack (8.0.0)" or "nokogiri (1.13.6-x86_64-linux)".
		name, version := parseSpecLine(trimmed)
		if name == "" {
			return
		}
		if _, exists := facts.Specs[name]; !exists {
			facts.Specs[name] = version
			s.specOrder = append(s.specOrder, name)
		}
	}
	// indent >= 6 lines under specs: are transitive deps; we don't track them.
}

// handleDependencyLine consumes a single entry from the DEPENDENCIES block.
// Examples: "rails (~> 8.0)", "rails (~> 8.0)!", "rails!", "rails".
func (s *lockfileScanner) handleDependencyLine(trimmed string, facts *LockfileFacts) {
	name, constraint := parseDependencyLine(trimmed)
	if name == "" {
		return
	}
	if _, exists := facts.DirectDeps[name]; !exists {
		facts.DirectDeps[name] = constraint
	}
}

// populateNativeExtGems fills facts.NativeExtGems based on the curated list,
// preserving first-seen order from specOrder.
func (s *lockfileScanner) populateNativeExtGems(facts *LockfileFacts) {
	if len(s.specOrder) == 0 {
		return
	}
	seen := map[string]bool{}
	for _, name := range s.specOrder {
		if !nativeExtensionGems[name] || seen[name] {
			continue
		}
		seen[name] = true
		facts.NativeExtGems = append(facts.NativeExtGems, name)
	}
}

// parseSpecLine parses a "name (version[-platform])" entry and returns the
// gem name with platform-suffix stripped from the version.
func parseSpecLine(line string) (name, version string) {
	open := strings.IndexByte(line, '(')
	if open < 0 {
		return strings.TrimSpace(line), ""
	}
	name = strings.TrimSpace(line[:open])
	end := strings.IndexByte(line[open+1:], ')')
	if end < 0 {
		return name, ""
	}
	version = strings.TrimSpace(line[open+1 : open+1+end])
	if dash := strings.IndexByte(version, '-'); dash > 0 {
		// Platform suffix: keep numeric prefix, drop everything after.
		version = version[:dash]
	}
	return name, version
}

// parseDependencyLine parses a single DEPENDENCIES entry. The trailing "!"
// marker (which Bundler uses to flag pinned-to-source gems) is stripped from
// the name so DirectDeps keys match Specs keys.
func parseDependencyLine(line string) (name, constraint string) {
	if line == "" {
		return "", ""
	}
	open := strings.IndexByte(line, '(')
	if open >= 0 {
		name = strings.TrimSpace(line[:open])
		end := strings.IndexByte(line[open+1:], ')')
		if end >= 0 {
			constraint = strings.TrimSpace(line[open+1 : open+1+end])
		}
	} else {
		name = strings.TrimSpace(line)
	}
	name = strings.TrimSuffix(name, "!")
	name = strings.TrimSpace(name)
	return name, constraint
}

func leadingSpaces(line string) int {
	count := 0
	for i := range len(line) {
		if line[i] != ' ' {
			break
		}
		count++
	}
	return count
}
