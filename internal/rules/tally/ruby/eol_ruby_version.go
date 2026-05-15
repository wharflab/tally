package ruby

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/distribution/reference"

	rubyfacts "github.com/wharflab/tally/internal/facts/ruby"

	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/semantic"
)

// EOLRubyVersionRuleCode is the full rule code.
const EOLRubyVersionRuleCode = rules.TallyRulePrefix + "ruby/eol-ruby-version"

// eolRubyVersionFixPriority keeps this rule's edits ordered alongside
// the other Ruby rules.
const eolRubyVersionFixPriority = 88

// EOLRubyVersionRule flags `FROM ruby:X.Y` references where X.Y is past
// upstream end-of-life, plus references resolved from ARG/.ruby-version.
type EOLRubyVersionRule struct{}

// NewEOLRubyVersionRule creates the rule.
func NewEOLRubyVersionRule() *EOLRubyVersionRule {
	return &EOLRubyVersionRule{}
}

// Metadata returns the rule metadata.
func (r *EOLRubyVersionRule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            EOLRubyVersionRuleCode,
		Name:            "Ruby base image is end-of-life",
		Description:     "Base image uses an end-of-life Ruby version with no upstream security patches",
		DocURL:          rules.TallyDocURL(EOLRubyVersionRuleCode),
		DefaultSeverity: rules.SeverityWarning,
		Category:        "security",
		FixPriority:     eolRubyVersionFixPriority,
	}
}

// rubyBranchEOL is the curated end-of-life table for Ruby major.minor
// branches. Source: https://www.ruby-lang.org/en/downloads/branches/.
//
// Each entry's RetiredAt is the date the branch entered "no upstream support
// of any kind" — past this date the rule emits at error severity. Branches
// with a RetiredAt zero value are still receiving security patches; those
// emit at warning severity.
//
// To bump the table, update the dates from the upstream branches page and
// run `make test`.
var rubyBranchEOL = map[string]rubyBranchInfo{
	// Ruby 2.x — fully out of support.
	"2.4": {RetiredAt: mustParse("2020-04-05"), Status: "EOL"},
	"2.5": {RetiredAt: mustParse("2021-03-31"), Status: "EOL"},
	"2.6": {RetiredAt: mustParse("2022-04-12"), Status: "EOL"},
	"2.7": {RetiredAt: mustParse("2023-03-31"), Status: "EOL"},
	// Ruby 3.x — 3.0 and 3.1 are retired; 3.2/3.3/3.4 are supported.
	"3.0": {RetiredAt: mustParse("2024-03-31"), Status: "EOL"},
	"3.1": {RetiredAt: mustParse("2025-03-31"), Status: "EOL"},
}

// supportedRubyBranches is the list of currently-supported Ruby branches,
// most-recent first. The fix uses this list to suggest a replacement.
var supportedRubyBranches = []string{"3.4", "3.3", "3.2"}

// rubyBranchInfo tracks a Ruby branch's end-of-life metadata.
type rubyBranchInfo struct {
	// RetiredAt is the date upstream stopped publishing patches (any kind).
	RetiredAt time.Time
	// Status is a short label: "EOL" for fully retired branches.
	Status string
}

func mustParse(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic("eol_ruby_version: invalid hardcoded date " + s)
	}
	return t
}

// rubyTagVersionRE extracts the X.Y from a ruby:* image tag, e.g.
// "3.3-slim", "3.0.6-bookworm", "2.7.0p100-alpine3.18".
var rubyTagVersionRE = regexp.MustCompile(`^(\d+)\.(\d+)`)

// Check runs the rule.
func (r *EOLRubyVersionRule) Check(input rules.LintInput) []rules.Violation {
	meta := r.Metadata()
	rubyFacts := input.Facts.RubyFacts()

	var violations []rules.Violation
	for stageIdx := range input.Stages {
		sf := input.Facts.Stage(stageIdx)
		if sf == nil {
			continue
		}
		if sf.BaseImageOS == semantic.BaseImageOSWindows {
			continue
		}
		v := r.checkStage(input, stageIdx, rubyFacts, meta)
		if v != nil {
			violations = append(violations, *v)
		}
	}
	return violations
}

func (r *EOLRubyVersionRule) checkStage(
	input rules.LintInput,
	stageIdx int,
	rubyFacts *rubyfacts.RubyFacts,
	meta rules.RuleMetadata,
) *rules.Violation {
	base := input.Semantic.ExternalBase(stageIdx)
	if base == nil {
		return nil
	}
	raw := base.Effective
	if raw == "" {
		raw = base.Raw
	}
	if raw == "" {
		return nil
	}
	branch, isRuby := parseRubyImageBranch(raw)
	if !isRuby {
		return nil
	}
	if branch == "" {
		// Couldn't extract X.Y from the tag (likely an ARG-templated
		// version). Try to fall back to RubyFacts when observable.
		if rubyFacts != nil && rubyFacts.RubyVersion != "" {
			branch = majorMinorFromVersion(rubyFacts.RubyVersion)
		}
		if branch == "" {
			return nil
		}
	}
	info, eol := rubyBranchEOL[branch]
	if !eol {
		return nil
	}

	severity := meta.DefaultSeverity
	if !info.RetiredAt.IsZero() && info.RetiredAt.Before(time.Now()) {
		// Branch is past its retirement date — fully out of support.
		severity = rules.SeverityError
	}

	loc := stageBaseLocation(input, stageIdx)
	v := rules.NewViolation(loc, meta.Code, meta.Description, severity).
		WithDocURL(meta.DocURL).
		WithDetail(eolRubyVersionDetail(branch, info))
	if fix := buildEOLRubyVersionFix(input, stageIdx, raw, meta.FixPriority); fix != nil {
		v = v.WithSuggestedFix(fix)
	}
	return &v
}

func eolRubyVersionDetail(branch string, info rubyBranchInfo) string {
	var b strings.Builder
	b.WriteString("Ruby ")
	b.WriteString(branch)
	b.WriteString(" is past upstream end-of-life — the upstream Ruby team no longer publishes ")
	if !info.RetiredAt.IsZero() {
		b.WriteString("security patches as of ")
		b.WriteString(info.RetiredAt.Format("2006-01-02"))
		b.WriteString(". ")
	} else {
		b.WriteString("patches of any kind. ")
	}
	b.WriteString("Production images on this branch will accumulate unfixed CVEs over time. " +
		"Migrate to a currently-supported branch (")
	for i, br := range supportedRubyBranches {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString("Ruby ")
		b.WriteString(br)
	}
	b.WriteString(") — see https://www.ruby-lang.org/en/downloads/branches/ for the upstream support matrix.")
	return b.String()
}

// parseRubyImageBranch parses a ruby:* image reference and returns the
// X.Y branch and a boolean indicating whether the image is in fact an
// official Ruby image (or a familiar derivative). Returns ("", true) when
// the image IS Ruby but the tag doesn't carry a parseable major.minor.
func parseRubyImageBranch(raw string) (string, bool) {
	named, err := reference.ParseNormalizedNamed(strings.ToLower(raw))
	if err != nil {
		return "", false
	}
	familiar := reference.FamiliarName(named)
	if familiar != "ruby" {
		// Non-official Ruby derivatives (jruby, truffleruby, …) follow
		// different release cadences; the upstream-Ruby EOL table doesn't
		// apply.
		return "", false
	}
	tagged, ok := named.(reference.Tagged)
	if !ok {
		// untagged or digest-only
		return "", true
	}
	tag := tagged.Tag()
	m := rubyTagVersionRE.FindStringSubmatch(tag)
	if m == nil {
		return "", true
	}
	return m[1] + "." + m[2], true
}

// majorMinorFromVersion extracts the X.Y prefix from a full Ruby version
// string like "3.3.5p100" or "3.3.5". Returns "" when the version is
// malformed.
func majorMinorFromVersion(version string) string {
	parts := strings.Split(strings.TrimSpace(version), ".")
	if len(parts) < 2 {
		return ""
	}
	if _, err := strconv.Atoi(parts[0]); err != nil {
		return ""
	}
	// Minor may have non-digit suffix like "5p100"; just take the first
	// run of digits.
	minor := parts[1]
	end := 0
	for end < len(minor) && minor[end] >= '0' && minor[end] <= '9' {
		end++
	}
	if end == 0 {
		return ""
	}
	return parts[0] + "." + minor[:end]
}

// stageBaseLocation returns the source location of the FROM line for a stage.
func stageBaseLocation(input rules.LintInput, stageIdx int) rules.Location {
	if stageIdx < 0 || stageIdx >= len(input.Stages) {
		return rules.NewFileLocation(input.File)
	}
	stage := input.Stages[stageIdx]
	if len(stage.Location) == 0 {
		return rules.NewFileLocation(input.File)
	}
	return rules.NewLocationFromRanges(input.File, stage.Location)
}

// buildEOLRubyVersionFix proposes rewriting the FROM tag to the most
// recent supported Ruby branch, preserving any variant suffix
// (`-slim`, `-alpine`, `-bookworm`, etc.).
func buildEOLRubyVersionFix(
	input rules.LintInput,
	stageIdx int,
	raw string,
	priority int,
) *rules.SuggestedFix {
	// Only emit a fix when the FROM is a literal ruby:X.Y tag we can
	// rewrite cleanly. ARG-templated bases need user judgment about
	// whether to update the ARG default vs the FROM value.
	if strings.Contains(raw, "$") {
		return nil
	}
	target := supportedRubyBranches[0]
	// Rewrite the entire major.minor[.patch[pNNN]] portion of the tag —
	// not just the major.minor prefix. `strings.Replace(raw, branch,
	// target, 1)` would carry `ruby:3.1.6` to `ruby:3.4.6`, which may
	// not be a published tag on Docker Hub. Preserve only the variant
	// suffix (`-slim`, `-alpine`, `-bookworm`, ...).
	newRaw := rewriteRubyTagToSupportedBranch(raw, target)
	if newRaw == "" || newRaw == raw {
		return nil
	}
	// Find the location of the raw tag in the FROM source. Use the
	// stage's FROM range and search across all lines covered by the
	// instruction (ARG-templated FROMs may use line continuations).
	if stageIdx < 0 || stageIdx >= len(input.Stages) {
		return nil
	}
	stage := input.Stages[stageIdx]
	if len(stage.Location) == 0 {
		return nil
	}
	sm := input.SourceMap()
	if sm == nil {
		return nil
	}
	startLine := stage.Location[0].Start.Line
	endLine := stage.Location[len(stage.Location)-1].End.Line
	var lineNo, idx int
	for lineNo = startLine; lineNo <= endLine; lineNo++ {
		line := sm.Line(lineNo - 1)
		idx = strings.Index(line, raw)
		if idx >= 0 {
			break
		}
	}
	if idx < 0 || lineNo > endLine {
		return nil
	}
	// rules.NewRangeLocation columns are 0-based; strings.Index returns
	// a 0-based offset.
	startCol := idx
	endCol := startCol + len(raw)
	return &rules.SuggestedFix{
		Description: "Bump the Ruby base image to a supported branch (" + target + ")",
		Safety:      rules.FixSuggestion,
		Priority:    priority,
		IsPreferred: true,
		Edits: []rules.TextEdit{{
			Location: rules.NewRangeLocation(input.File, lineNo, startCol, lineNo, endCol),
			NewText:  newRaw,
		}},
	}
}

// rewriteRubyTagToSupportedBranch rewrites a `ruby:X.Y[.Z[pNNN]][-variant]`
// reference to use the supplied supported branch (X.Y), preserving only
// the variant suffix. Returns "" when the input doesn't match a parseable
// ruby:* reference (the rule should suppress its fix in that case).
//
// Examples:
//
//	ruby:3.0          -> ruby:3.4
//	ruby:3.0-slim     -> ruby:3.4-slim
//	ruby:3.1.6        -> ruby:3.4
//	ruby:2.7.0-bookworm -> ruby:3.4-bookworm
//	ruby:2.7.0p100-alpine -> ruby:3.4-alpine
//	docker.io/library/ruby:3.0 -> docker.io/library/ruby:3.4
func rewriteRubyTagToSupportedBranch(raw, targetBranch string) string {
	colon := strings.LastIndex(raw, ":")
	if colon < 0 || colon == len(raw)-1 {
		return ""
	}
	before, tag := raw[:colon], raw[colon+1:]
	// Find the variant suffix (everything starting from the first `-`
	// in the tag — `slim`, `alpine`, `bookworm`, etc. don't appear in
	// the version itself).
	variant := ""
	if dash := strings.IndexByte(tag, '-'); dash >= 0 {
		variant = tag[dash:] // includes the leading `-`
	}
	return before + ":" + targetBranch + variant
}

func init() {
	rules.Register(NewEOLRubyVersionRule())
}
