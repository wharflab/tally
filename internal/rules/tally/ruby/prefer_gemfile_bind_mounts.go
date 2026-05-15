package ruby

import (
	"slices"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/wharflab/tally/internal/facts"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/semantic"
	"github.com/wharflab/tally/internal/stagename"
)

// dockerfileSyntaxBuildKitMarkers identifies BuildKit-frontend syntax
// pragmas. Cache and bind mounts require one of these.
var dockerfileSyntaxBuildKitMarkers = []string{"docker/dockerfile", "dockerfile/labs"}

// hasBuildKitSyntaxPragma reports whether the Dockerfile carries a
// `# syntax=docker/dockerfile:1` (or compatible) directive at its top.
func hasBuildKitSyntaxPragma(input rules.LintInput) bool {
	for line := range strings.SplitSeq(string(input.Source), "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "#") {
			break
		}
		if strings.Contains(trimmed, "syntax=") {
			for _, m := range dockerfileSyntaxBuildKitMarkers {
				if strings.Contains(trimmed, m) {
					return true
				}
			}
		}
	}
	return false
}

// PreferGemfileBindMountsRuleCode is the full rule code.
const PreferGemfileBindMountsRuleCode = rules.TallyRulePrefix + "ruby/prefer-gemfile-bind-mounts"

// preferGemfileBindMountsFixPriority keeps this rule's edits ordered alongside
// the other Ruby rules.
const preferGemfileBindMountsFixPriority = 88

// PreferGemfileBindMountsRule flags `COPY Gemfile Gemfile.lock` patterns
// that are followed by `bundle install`, suggesting the BuildKit
// `--mount=type=bind` form instead.
type PreferGemfileBindMountsRule struct{}

// NewPreferGemfileBindMountsRule creates the rule.
func NewPreferGemfileBindMountsRule() *PreferGemfileBindMountsRule {
	return &PreferGemfileBindMountsRule{}
}

// Metadata returns the rule metadata.
func (r *PreferGemfileBindMountsRule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            PreferGemfileBindMountsRuleCode,
		Name:            "Prefer BuildKit bind mounts for Gemfile/Gemfile.lock",
		Description:     "`COPY Gemfile Gemfile.lock` followed by `bundle install` can be replaced by a BuildKit bind mount",
		DocURL:          rules.TallyDocURL(PreferGemfileBindMountsRuleCode),
		DefaultSeverity: rules.SeverityInfo,
		Category:        "performance",
		FixPriority:     preferGemfileBindMountsFixPriority,
	}
}

// Check runs the rule.
func (r *PreferGemfileBindMountsRule) Check(input rules.LintInput) []rules.Violation {
	meta := r.Metadata()

	// Only suggest the bind-mount form on Dockerfiles that already opt
	// into BuildKit syntax — `--mount=type=bind` requires it.
	if !hasBuildKitSyntaxPragma(input) {
		return nil
	}

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
		if !stageLooksLikeRuby(input.Semantic, stageIdx, stage, sf) {
			continue
		}

		// Find a `COPY Gemfile Gemfile.lock ...` instruction followed
		// later in the same stage by `bundle install`.
		copyCmd := findGemfileCopy(stage)
		if copyCmd == nil {
			continue
		}
		if !stageHasBundleInstall(sf) {
			continue
		}

		loc := copyInstructionLocation(input.File, copyCmd)
		v := rules.NewViolation(loc, meta.Code, meta.Description, meta.DefaultSeverity).
			WithDocURL(meta.DocURL).
			WithDetail(preferGemfileBindMountsDetail()).
			WithSuggestedFix(&rules.SuggestedFix{
				Description: "Replace COPY Gemfile Gemfile.lock + RUN bundle install with " +
					"`RUN --mount=type=bind,source=Gemfile,target=Gemfile " +
					"--mount=type=bind,source=Gemfile.lock,target=Gemfile.lock " +
					"--mount=type=cache,target=${BUNDLE_PATH}/cache,sharing=locked bundle install`",
				Safety:      rules.FixSuggestion,
				Priority:    meta.FixPriority,
				IsPreferred: false,
			})
		violations = append(violations, v)
		// Report once per stage.
		break
	}
	return violations
}

func preferGemfileBindMountsDetail() string {
	return "94 of 196 Rails Dockerfiles in the corpus `COPY Gemfile Gemfile.lock`; 0 use a BuildKit bind " +
		"mount. With `# syntax=docker/dockerfile:1`, the manifests can be bind-mounted directly into the " +
		"`bundle install` RUN — they never appear as layer content. Combined with `--mount=type=cache` " +
		"for `${BUNDLE_PATH}/cache`, this is the modern shape of a Ruby dependency stage."
}

// findGemfileCopy returns the first COPY in the stage that copies
// Gemfile and Gemfile.lock as standalone files (not as part of a
// catch-all `COPY . .`).
func findGemfileCopy(stage instructions.Stage) *instructions.CopyCommand {
	for _, cmd := range stage.Commands {
		copyCmd, ok := cmd.(*instructions.CopyCommand)
		if !ok {
			continue
		}
		if copyMatchesGemfileManifests(copyCmd) {
			return copyCmd
		}
	}
	return nil
}

// copyMatchesGemfileManifests reports whether a COPY explicitly copies
// Gemfile and Gemfile.lock (in some order). `COPY . .` doesn't count —
// that's a wholesale tree copy, not the bind-mount pattern.
func copyMatchesGemfileManifests(cmd *instructions.CopyCommand) bool {
	if cmd == nil || len(cmd.SourceContents) > 0 {
		return false
	}
	hasGemfile := false
	hasLock := false
	for _, src := range cmd.SourcePaths {
		switch src {
		case "Gemfile":
			hasGemfile = true
		case "Gemfile.lock":
			hasLock = true
		case "Gemfile*", "Gemfile.*":
			// Glob form covers both.
			return true
		}
		// Wholesale source — `.` and `./` and `/rails` don't count.
		if src == "." || src == "./" || strings.HasPrefix(src, "/") {
			return false
		}
	}
	return hasGemfile && hasLock
}

// stageHasBundleInstall reports whether any RUN in the stage runs
// `bundle install`.
func stageHasBundleInstall(sf *facts.StageFacts) bool {
	for _, runFacts := range sf.Runs {
		if runFacts == nil {
			continue
		}
		if slices.ContainsFunc(runFacts.CommandInfos, isBundleInstall) {
			return true
		}
	}
	return false
}

func init() {
	rules.Register(NewPreferGemfileBindMountsRule())
}
