package ruby

import (
	"regexp"
	"strings"

	"github.com/distribution/reference"
	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	rubyfacts "github.com/wharflab/tally/internal/facts/ruby"

	"github.com/wharflab/tally/internal/facts"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/semantic"
	"github.com/wharflab/tally/internal/stagename"
)

// rubyImageVersionRE extracts X.Y from a ruby:* image tag like
// "3.3-slim", "3.0.6-bookworm", "2.7.0p100-alpine3.18".
var rubyImageVersionRE = regexp.MustCompile(`^(\d+)\.(\d+)`)

// longRunningRubyServerCommands is the set of ENTRYPOINT/CMD basenames
// that mark a stage as a long-running Ruby server (web app or worker).
// YJIT delivers its 15-30% speedup on these workloads.
var longRunningRubyServerCommands = map[string]bool{
	"rails":     true,
	"puma":      true,
	"unicorn":   true,
	"thrust":    true,
	"rackup":    true,
	"sidekiq":   true,
	"falcon":    true,
	"thin":      true,
	"passenger": true,
	"iodine":    true,
}

// YJITNotEnabledOnSupportedRuntimeRuleCode is the full rule code.
const YJITNotEnabledOnSupportedRuntimeRuleCode = rules.TallyRulePrefix + "ruby/yjit-not-enabled-on-supported-runtime"

// yjitNotEnabledFixPriority keeps this rule's edits ordered alongside
// the other Ruby rules.
const yjitNotEnabledFixPriority = 88

// minYJITSupportedMajor and minYJITSupportedMinor define the minimum Ruby
// branch where YJIT is production-ready (3.3 per upstream release notes).
// Earlier branches (3.0/3.1) had YJIT but it was experimental and had
// Rails-specific regressions, so the rule should not fire there.
const (
	minYJITSupportedMajor = 3
	minYJITSupportedMinor = 3
)

// YJITNotEnabledOnSupportedRuntimeRule flags final Ruby/Rails runtime
// stages on Ruby 3.3+ that don't enable YJIT — a near-free 15-30% CPU
// win on most Rails workloads.
type YJITNotEnabledOnSupportedRuntimeRule struct{}

// NewYJITNotEnabledOnSupportedRuntimeRule creates the rule.
func NewYJITNotEnabledOnSupportedRuntimeRule() *YJITNotEnabledOnSupportedRuntimeRule {
	return &YJITNotEnabledOnSupportedRuntimeRule{}
}

// Metadata returns the rule metadata.
func (r *YJITNotEnabledOnSupportedRuntimeRule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            YJITNotEnabledOnSupportedRuntimeRuleCode,
		Name:            "Enable YJIT on Ruby 3.3+ runtime",
		Description:     "Ruby 3.3+ runtime does not enable YJIT (RUBY_YJIT_ENABLE=1)",
		DocURL:          rules.TallyDocURL(YJITNotEnabledOnSupportedRuntimeRuleCode),
		DefaultSeverity: rules.SeverityInfo,
		Category:        "performance",
		FixPriority:     yjitNotEnabledFixPriority,
	}
}

// Check runs the rule.
func (r *YJITNotEnabledOnSupportedRuntimeRule) Check(input rules.LintInput) []rules.Violation {
	meta := r.Metadata()
	rubyFacts := input.Facts.RubyFacts()

	finalIdx := input.FinalStageIndex()
	if finalIdx < 0 || finalIdx >= len(input.Stages) {
		return nil
	}

	stage := input.Stages[finalIdx]
	sf := input.Facts.Stage(finalIdx)
	if sf == nil {
		return nil
	}
	if sf.BaseImageOS == semantic.BaseImageOSWindows {
		return nil
	}
	if stagename.LooksLikeDev(stage.Name) {
		return nil
	}
	if !stageLooksLikeRuby(input.Semantic, finalIdx, stage, sf) {
		return nil
	}
	if !stageLooksLikeLongRunningRubyServer(stage, sf) {
		// CLI-only Ruby images don't benefit from YJIT — JIT warmup
		// dominates short-lived processes.
		return nil
	}
	if !rubyVersionSupportsYJIT(input, finalIdx, rubyFacts) {
		return nil
	}
	if stageEnablesYJIT(sf, stage) {
		return nil
	}

	loc := finalStageFromLocation(input, finalIdx)
	v := rules.NewViolation(loc, meta.Code, meta.Description, meta.DefaultSeverity).
		WithDocURL(meta.DocURL).
		WithDetail(yjitNotEnabledDetail()).
		WithSuggestedFix(buildYJITFix(input, finalIdx, meta.FixPriority))
	return []rules.Violation{v}
}

func yjitNotEnabledDetail() string {
	return "YJIT is Ruby 3.3+'s production-ready JIT compiler. On most Rails workloads it delivers a " +
		"15–30% CPU win at near-zero cost — but it is opt-in. Enable it via `ENV RUBY_YJIT_ENABLE=\"1\"` " +
		"or by passing `--yjit` to the server entrypoint. The corpus shows only 3 of 196 Rails " +
		"Dockerfiles do this; for projects on Ruby 3.3+ this is a near-free performance gain."
}

// rubyVersionSupportsYJIT reports whether the Ruby version associated
// with the stage is 3.3 or later. Resolves via:
//
//  1. Stage's external base image tag (`ruby:3.3-slim` etc.).
//  2. RubyFacts.RubyVersion (.ruby-version / .tool-versions / lockfile).
//
// Returns false for unparsable versions, ARG-templated FROMs without a
// resolvable version, or branches < 3.3.
func rubyVersionSupportsYJIT(input rules.LintInput, stageIdx int, rubyFacts *rubyfacts.RubyFacts) bool {
	// Try the stage's base image first.
	if base := input.Semantic.ExternalBase(stageIdx); base != nil {
		raw := base.Effective
		if raw == "" {
			raw = base.Raw
		}
		if branch := extractRubyBranchFromImageRef(raw); branch != "" {
			if major, minor, ok := parseMajorMinor(branch); ok {
				return majorMinorAtLeast(major, minor, minYJITSupportedMajor, minYJITSupportedMinor)
			}
		}
	}
	// Fall back to RubyFacts.RubyVersion (e.g. "3.3.5" or "3.3.5p100").
	if rubyFacts != nil && rubyFacts.RubyVersion != "" {
		if major, minor, ok := parseMajorMinor(rubyFacts.RubyVersion); ok {
			return majorMinorAtLeast(major, minor, minYJITSupportedMajor, minYJITSupportedMinor)
		}
	}
	return false
}

// stageEnablesYJIT reports whether the stage's effective state already
// enables YJIT. Recognized signals:
//
//  1. ENV RUBY_YJIT_ENABLE=1 (any truthy value).
//  2. ENV RUBYOPT contains --yjit.
//  3. ENTRYPOINT/CMD passes --yjit to the Ruby/Rails binary.
//  4. ENTRYPOINT/CMD sets RUBYOPT inline before invoking Ruby.
func stageEnablesYJIT(sf *facts.StageFacts, stage instructions.Stage) bool {
	if envBoundValue(sf, "RUBY_YJIT_ENABLE") != "" {
		return true
	}
	if rubyopt := envBoundValue(sf, "RUBYOPT"); strings.Contains(rubyopt, "--yjit") {
		return true
	}
	for _, cmd := range stage.Commands {
		switch c := cmd.(type) {
		case *instructions.EntrypointCommand:
			if cmdLineHasYJIT(c.CmdLine) {
				return true
			}
		case *instructions.CmdCommand:
			if cmdLineHasYJIT(c.CmdLine) {
				return true
			}
		}
	}
	return false
}

func cmdLineHasYJIT(cmdLine []string) bool {
	for _, a := range cmdLine {
		if strings.Contains(a, "--yjit") {
			return true
		}
		if strings.Contains(a, "RUBY_YJIT_ENABLE") {
			return true
		}
	}
	return false
}

// stageLooksLikeLongRunningRubyServer reports whether the stage has the
// shape of a long-running Ruby server (web app or worker process), which
// is where YJIT delivers its 15-30% speedup. Short-lived CLI images are
// out of scope — JIT warmup dominates.
func stageLooksLikeLongRunningRubyServer(stage instructions.Stage, sf *facts.StageFacts) bool {
	for _, name := range stageRuntimeCommandBasenames(stage) {
		if longRunningRubyServerCommands[name] {
			return true
		}
	}
	// sf reserved for future EXPOSE/HEALTHCHECK heuristics.
	_ = sf
	return false
}

// finalStageFromLocation returns the source location of the final stage's
// FROM line.
func finalStageFromLocation(input rules.LintInput, stageIdx int) rules.Location {
	if stageIdx < 0 || stageIdx >= len(input.Stages) {
		return rules.NewFileLocation(input.File)
	}
	stage := input.Stages[stageIdx]
	if len(stage.Location) == 0 {
		return rules.NewFileLocation(input.File)
	}
	return rules.NewLocationFromRanges(input.File, stage.Location)
}

// extractRubyBranchFromImageRef parses a ruby:* image reference and
// returns the X.Y branch from its tag. Returns "" for non-Ruby images
// or unparsable tags.
func extractRubyBranchFromImageRef(raw string) string {
	if raw == "" {
		return ""
	}
	named, err := reference.ParseNormalizedNamed(strings.ToLower(raw))
	if err != nil {
		return ""
	}
	if reference.FamiliarName(named) != "ruby" {
		return ""
	}
	tagged, ok := named.(reference.Tagged)
	if !ok {
		return ""
	}
	m := rubyImageVersionRE.FindStringSubmatch(tagged.Tag())
	if m == nil {
		return ""
	}
	return m[1] + "." + m[2]
}

func majorMinorAtLeast(haveMajor, haveMinor, wantMajor, wantMinor int) bool {
	if haveMajor != wantMajor {
		return haveMajor > wantMajor
	}
	return haveMinor >= wantMinor
}

// buildYJITFix proposes inserting `ENV RUBY_YJIT_ENABLE="1"` at the top
// of the final stage. Insertion is zero-width at column 0 of the line
// after the stage's `FROM`.
func buildYJITFix(input rules.LintInput, stageIdx, priority int) *rules.SuggestedFix {
	if stageIdx < 0 || stageIdx >= len(input.Stages) {
		return nil
	}
	stage := input.Stages[stageIdx]
	if len(stage.Location) == 0 {
		return nil
	}
	insertLine := stage.Location[len(stage.Location)-1].End.Line + 1
	return &rules.SuggestedFix{
		Description: `Add ENV RUBY_YJIT_ENABLE="1" to enable YJIT on this Ruby 3.3+ runtime`,
		Safety:      rules.FixSuggestion,
		Priority:    priority,
		IsPreferred: true,
		Edits: []rules.TextEdit{{
			Location: rules.NewRangeLocation(input.File, insertLine, 0, insertLine, 0),
			NewText:  "ENV RUBY_YJIT_ENABLE=\"1\"\n",
		}},
	}
}

func init() {
	rules.Register(NewYJITNotEnabledOnSupportedRuntimeRule())
}
