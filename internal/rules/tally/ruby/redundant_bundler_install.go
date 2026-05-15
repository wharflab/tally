package ruby

import (
	"strings"

	"github.com/distribution/reference"
	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	rubyfacts "github.com/wharflab/tally/internal/facts/ruby"

	"github.com/wharflab/tally/internal/facts"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/semantic"
	"github.com/wharflab/tally/internal/shell"
	"github.com/wharflab/tally/internal/sourcemap"
	"github.com/wharflab/tally/internal/stagename"
)

// RedundantBundlerInstallRuleCode is the full rule code.
const RedundantBundlerInstallRuleCode = rules.TallyRulePrefix + "ruby/redundant-bundler-install"

// redundantBundlerInstallFixPriority keeps this rule's edits ordered alongside
// the other Ruby production-hygiene rules.
const redundantBundlerInstallFixPriority = 88

// RedundantBundlerInstallRule flags `gem install bundler` invocations on
// stages whose base image is an official `ruby:*` image — Bundler 2.x
// already ships in the image, so reinstalling it is redundant.
type RedundantBundlerInstallRule struct{}

// NewRedundantBundlerInstallRule creates the rule.
func NewRedundantBundlerInstallRule() *RedundantBundlerInstallRule {
	return &RedundantBundlerInstallRule{}
}

// Metadata returns the rule metadata.
func (r *RedundantBundlerInstallRule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            RedundantBundlerInstallRuleCode,
		Name:            "Redundant `gem install bundler` on official Ruby base",
		Description:     "`gem install bundler` is redundant on official ruby:* images that already ship Bundler 2.x",
		DocURL:          rules.TallyDocURL(RedundantBundlerInstallRuleCode),
		DefaultSeverity: rules.SeverityWarning,
		Category:        "performance",
		FixPriority:     redundantBundlerInstallFixPriority,
	}
}

// officialRubyImageNames are the familiar names that ship Bundler 2.x
// pre-installed. Matching is against the parsed reference's familiar name
// (no domain, no tag).
var officialRubyImageNames = map[string]bool{
	"ruby":                            true, // docker.io/library/ruby
	"ghcr.io/rails/devcontainer/ruby": true,
}

// Check runs the rule.
func (r *RedundantBundlerInstallRule) Check(input rules.LintInput) []rules.Violation {
	meta := r.Metadata()
	sm := input.SourceMap()
	rubyFacts := input.Facts.RubyFacts()

	var violations []rules.Violation
	for stageIdx, stage := range input.Stages {
		if stagename.LooksLikeDev(stage.Name) {
			continue
		}
		sf := input.Facts.Stage(stageIdx)
		if sf == nil {
			continue
		}
		if sf.BaseImageOS == semantic.BaseImageOSWindows {
			continue
		}
		if !stageBaseIsOfficialRuby(input.Semantic, stageIdx) {
			continue
		}
		violations = append(violations, r.checkStage(input, sf, sm, rubyFacts, meta)...)
	}
	return violations
}

func (r *RedundantBundlerInstallRule) checkStage(
	input rules.LintInput,
	sf *facts.StageFacts,
	sm *sourcemap.SourceMap,
	rubyFacts *rubyfacts.RubyFacts,
	meta rules.RuleMetadata,
) []rules.Violation {
	var violations []rules.Violation
	for _, runFacts := range sf.Runs {
		if runFacts == nil {
			continue
		}
		for _, ci := range runFacts.CommandInfos {
			if !isGemInstallBundler(ci) {
				continue
			}
			loc := redundantBundlerInstallLocation(input.File, runFacts, ci, sm)
			v := rules.NewViolation(loc, meta.Code, meta.Description, meta.DefaultSeverity).
				WithDocURL(meta.DocURL).
				WithDetail(redundantBundlerInstallDetail(rubyFacts))
			if fix := buildRedundantBundlerInstallFix(input, runFacts, ci, sm, meta.FixPriority); fix != nil {
				v = v.WithSuggestedFix(fix)
			}
			violations = append(violations, v)
		}
	}
	return violations
}

func redundantBundlerInstallDetail(rubyFacts *rubyfacts.RubyFacts) string {
	base := "Official `ruby:*` images ship Bundler 2.x pre-installed and resolved on `$PATH`. " +
		"Reinstalling Bundler via `gem install bundler` re-downloads and recompiles a tool that's " +
		"already there, slowing every build and introducing version drift between development and CI."
	if rubyFacts != nil && rubyFacts.Lockfile != nil && rubyFacts.Lockfile.BundledWith != "" {
		base += " If a specific Bundler version is required, prefer Bundler's own version-shim form " +
			"(`bundle _" + strings.TrimSpace(rubyFacts.Lockfile.BundledWith) + "_ install`) which uses " +
			"the version recorded in `Gemfile.lock`'s BUNDLED WITH block — the official image's " +
			"shim handles this without a fresh `gem install`."
	} else {
		base += " If a specific Bundler version is required, the `Gemfile.lock` BUNDLED WITH block " +
			"plus Bundler's version-aware shim (`bundle _<version>_ install`) handles it without a " +
			"fresh `gem install`."
	}
	return base
}

// stageBaseIsOfficialRuby reports whether a stage's effective base image is
// an official ruby:* image (or a known devcontainer Ruby image). Stage refs
// (`FROM <stage>`) walk the StageRef ancestry to the original external base.
func stageBaseIsOfficialRuby(sem *semantic.Model, stageIdx int) bool {
	base := sem.ExternalBase(stageIdx)
	if base == nil {
		return false
	}
	raw := base.Effective
	if raw == "" {
		raw = base.Raw
	}
	if raw == "" {
		return false
	}
	named, err := reference.ParseNormalizedNamed(strings.ToLower(raw))
	if err != nil {
		return false
	}
	familiar := reference.FamiliarName(named)
	return officialRubyImageNames[familiar]
}

// isGemInstallBundler reports whether a CommandInfo represents a
// `gem install bundler` invocation. The shape is:
//
//	gem install bundler
//	gem install bundler -v 2.5.6
//	gem install bundler --version 2.5.6
//	gem install -v 2.5.6 bundler
//
// We require the literal "bundler" as one of the install targets; other
// `gem install` lines are out of scope for this rule.
func isGemInstallBundler(ci shell.CommandInfo) bool {
	if !strings.EqualFold(ci.Name, "gem") {
		return false
	}
	if !strings.EqualFold(ci.Subcommand, "install") {
		return false
	}
	for i, a := range ci.Args {
		if i == 0 && strings.EqualFold(a, ci.Subcommand) {
			// Skip the `install` subcommand token itself.
			continue
		}
		if a == "bundler" {
			return true
		}
	}
	return false
}

// redundantBundlerInstallLocation prefers the precise command-name span when
// available, falling back to the RUN's first range.
func redundantBundlerInstallLocation(
	file string,
	runFacts *facts.RunFacts,
	cmd shell.CommandInfo,
	sm *sourcemap.SourceMap,
) rules.Location {
	if runFacts == nil || runFacts.Run == nil {
		return rules.NewFileLocation(file)
	}
	runRanges := runFacts.Run.Location()
	if len(runRanges) == 0 {
		return rules.NewFileLocation(file)
	}
	if cmd.HasCommandRange {
		line := runRanges[0].Start.Line + cmd.Line
		startCol := cmd.StartCol
		endCol := cmd.CommandEndCol
		// Per-line columns are script-relative on line 0; translate back to
		// Dockerfile coordinates.
		if cmd.Line == 0 && sm != nil {
			offset := shell.DockerfileRunCommandStartCol(sm.Line(line - 1))
			startCol += offset
			endCol += offset
		}
		// CommandEndLine may be on a later line — fall back to single-line
		// span when it differs.
		endLine := runRanges[0].Start.Line + cmd.CommandEndLine
		if endLine == line {
			return rules.NewRangeLocation(file, line, startCol, line, endCol)
		}
		return rules.NewRangeLocation(file, line, startCol, line, cmd.EndCol+(endCol-startCol))
	}
	return rules.NewLocationFromRanges(file, runRanges)
}

// buildRedundantBundlerInstallFix proposes deleting the entire RUN
// instruction when it contains *only* the `gem install bundler` step.
// Otherwise it proposes deleting just the `gem install bundler` from the
// chained command. In either case the fix is FixSuggestion — the user may
// have a deliberate reason for the install we can't see.
func buildRedundantBundlerInstallFix(
	input rules.LintInput,
	runFacts *facts.RunFacts,
	cmd shell.CommandInfo,
	sm *sourcemap.SourceMap,
	priority int,
) *rules.SuggestedFix {
	if runFacts == nil || runFacts.Run == nil {
		return nil
	}
	if sm == nil {
		return nil
	}
	// When the RUN has only a single command, delete the whole RUN line.
	// Detect "single command" via CommandInfos length.
	if len(runFacts.CommandInfos) == 1 && runFacts.CommandInfos[0].Name == cmd.Name {
		runRanges := runFacts.Run.Location()
		if len(runRanges) == 0 {
			return nil
		}
		startLine := runRanges[0].Start.Line
		endLine := sm.ResolveEndLine(runRanges[len(runRanges)-1].End.Line)
		if endLine <= 0 || startLine <= 0 {
			return nil
		}
		// Delete from column 0 of startLine to column 0 of endLine+1.
		return &rules.SuggestedFix{
			Description: "Remove `gem install bundler` — Bundler 2.x ships with the official ruby:* image",
			Safety:      rules.FixSuggestion,
			Priority:    priority,
			IsPreferred: true,
			Edits: []rules.TextEdit{{
				Location: rules.NewRangeLocation(input.File, startLine, 0, endLine+1, 0),
				NewText:  "",
			}},
		}
	}
	// Multi-command RUN: don't auto-rewrite (chains may have && or ; that
	// need careful handling). Leave the fix as a non-edit suggestion.
	return &rules.SuggestedFix{
		Description: "Remove the `gem install bundler` step from this chain — Bundler 2.x ships with the official ruby:* image",
		Safety:      rules.FixSuggestion,
		Priority:    priority,
		IsPreferred: false,
	}
}

// Compile-time assertion that we use a stable instructions reference.
var _ = (*instructions.RunCommand)(nil)

func init() {
	rules.Register(NewRedundantBundlerInstallRule())
}
