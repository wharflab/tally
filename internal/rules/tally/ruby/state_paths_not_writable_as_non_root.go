package ruby

import (
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/wharflab/tally/internal/facts"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/semantic"
)

// StatePathsNotWritableAsNonRootRuleCode is the full rule code.
const StatePathsNotWritableAsNonRootRuleCode = rules.TallyRulePrefix + "ruby/state-paths-not-writable-as-non-root"

// statePathsNotWritableAsNonRootFixPriority keeps this rule's edits ordered
// alongside the other Ruby rules.
const statePathsNotWritableAsNonRootFixPriority = 88

// railsStateDirs are the canonical Rails directories that need to be
// writable by the non-root runtime user. These are Rails convention,
// not generic Docker.
var railsStateDirs = []string{"tmp", "log", "storage", "db"}

// railsServerCommand is the Rails CLI binary basename used to detect
// Rails-app-shaped runtime stages.
const railsServerCommand = "rails"

// StatePathsNotWritableAsNonRootRule flags Rails app stages that switch
// to a non-root USER but `COPY` application content without `--chown` (or
// a subsequent `chown -R`), leaving Rails state directories root-owned
// and unwritable to the runtime user.
type StatePathsNotWritableAsNonRootRule struct{}

// NewStatePathsNotWritableAsNonRootRule creates the rule.
func NewStatePathsNotWritableAsNonRootRule() *StatePathsNotWritableAsNonRootRule {
	return &StatePathsNotWritableAsNonRootRule{}
}

// Metadata returns the rule metadata.
func (r *StatePathsNotWritableAsNonRootRule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            StatePathsNotWritableAsNonRootRuleCode,
		Name:            "Rails state directories must be writable as the non-root runtime user",
		Description:     "Rails app COPY without --chown leaves state dirs (tmp, log, storage, db) root-owned at runtime",
		DocURL:          rules.TallyDocURL(StatePathsNotWritableAsNonRootRuleCode),
		DefaultSeverity: rules.SeverityWarning,
		Category:        "correctness",
		FixPriority:     statePathsNotWritableAsNonRootFixPriority,
	}
}

// Check runs the rule.
func (r *StatePathsNotWritableAsNonRootRule) Check(input rules.LintInput) []rules.Violation {
	meta := r.Metadata()

	var violations []rules.Violation
	for stageIdx, stage := range input.Stages {
		sf := input.Facts.Stage(stageIdx)
		if sf == nil {
			continue
		}
		if sf.BaseImageOS == semantic.BaseImageOSWindows {
			continue
		}
		// Only fire on Ruby/Rails-shaped stages.
		if !stageLooksLikeRuby(input.Semantic, stageIdx, stage, sf) {
			continue
		}
		// Require an explicit non-root USER to be set in this stage.
		if sf.EffectiveUser == "" || facts.IsRootUser(sf.EffectiveUser) {
			continue
		}
		// CLI-only gem images (no WORKDIR /rails, no bin/rails references)
		// are out of scope.
		if !stageLooksLikeRailsApp(stage, sf) {
			continue
		}
		violations = append(violations, r.checkStage(input, stage, sf, meta)...)
	}
	return violations
}

func (r *StatePathsNotWritableAsNonRootRule) checkStage(
	input rules.LintInput,
	stage instructions.Stage,
	sf *facts.StageFacts,
	meta rules.RuleMetadata,
) []rules.Violation {
	user := stripUserGroup(sf.EffectiveUser)

	// Find the offending COPY: any COPY of application content (not
	// stage-to-stage with --chown, not a single-file COPY of a known
	// vendor artifact) that lacks --chown to the non-root user. The
	// canonical violation shape is `COPY . .` (or `COPY /rails /rails`)
	// without --chown.
	var violations []rules.Violation
	for _, cmd := range stage.Commands {
		copyCmd, ok := cmd.(*instructions.CopyCommand)
		if !ok {
			continue
		}
		if !copyLooksLikeAppContent(copyCmd) {
			continue
		}
		if copyHasChownToUser(copyCmd, user) {
			continue
		}
		// If a later RUN performs `chown -R user dir(s)` covering all
		// observed state dirs, the COPY is acceptable.
		if stageHasRuntimeChown(sf, user) {
			continue
		}

		loc := copyInstructionLocation(input.File, copyCmd)
		v := rules.NewViolation(loc, meta.Code, meta.Description, meta.DefaultSeverity).
			WithDocURL(meta.DocURL).
			WithDetail(statePathsDetail(user))
		if fix := buildStatePathsFix(input.File, copyCmd, user, meta.FixPriority); fix != nil {
			v = v.WithSuggestedFix(fix)
		}
		violations = append(violations, v)
	}
	return violations
}

func statePathsDetail(user string) string {
	dirs := strings.Join(railsStateDirs, ", ")
	return "When the runtime stage runs as `" + user + "`, copying application content without " +
		"`--chown=" + user + ":" + user + "` leaves the Rails state directories (" + dirs + ") " +
		"root-owned. Rails crashes on first request when ActionView, ActiveStorage, ActiveRecord, or " +
		"`bin/rails db:prepare` tries to write to them. Add `--chown=" + user + ":" + user + "` to the " +
		"offending COPY (the Rails generator template's exact pattern) or perform an explicit " +
		"`chown -R " + user + ":" + user + "` on the state directories before switching to USER."
}

// stageLooksLikeRailsApp reports whether the stage looks like a Rails app
// runtime image — has a WORKDIR /rails (or similar) and references
// `bin/rails`, `bundle exec rails`, or `rails server` somewhere. CLI-only
// gem images (no Rails) are out of scope.
func stageLooksLikeRailsApp(stage instructions.Stage, sf *facts.StageFacts) bool {
	// Quick wins: WORKDIR pointing at a typical Rails app directory.
	if sf.FinalWorkdir == "/rails" || sf.FinalWorkdir == "/app" ||
		strings.HasSuffix(sf.FinalWorkdir, "/rails") ||
		strings.HasSuffix(sf.FinalWorkdir, "/app") {
		return true
	}
	// ENTRYPOINT/CMD references rails-style binaries.
	for _, name := range stageRuntimeCommandBasenames(stage) {
		if name == railsServerCommand || name == "thrust" || name == "puma" || name == "rackup" {
			return true
		}
	}
	// Effective env explicitly declares Rails.
	if value, ok := sf.EffectiveEnv.Bindings["RAILS_ENV"]; ok && value.Value != "" {
		return true
	}
	return false
}

// stripUserGroup returns the user portion of a `user[:group]` string.
func stripUserGroup(s string) string {
	if user, _, ok := strings.Cut(s, ":"); ok {
		return user
	}
	return s
}

// copyLooksLikeAppContent reports whether a COPY is copying application
// content (a directory tree like `.`, `./`, `/rails`, or `--from=builder
// /rails`) rather than a single file or vendor artifact.
func copyLooksLikeAppContent(cmd *instructions.CopyCommand) bool {
	if cmd == nil {
		return false
	}
	// Heredoc COPYs aren't application-content COPYs; skip.
	if len(cmd.SourceContents) > 0 {
		return false
	}
	for _, src := range cmd.SourcePaths {
		if src == "." || src == "./" || src == "/rails" || src == "/app" {
			return true
		}
		// Deep paths into the project that imply directory content.
		if strings.HasSuffix(src, "/.") {
			return true
		}
	}
	return false
}

// copyHasChownToUser reports whether the COPY's --chown flag's value
// matches the supplied user (left side of any user:group). A bare user
// without group also counts.
func copyHasChownToUser(cmd *instructions.CopyCommand, user string) bool {
	if cmd == nil || cmd.Chown == "" {
		return false
	}
	chown := cmd.Chown
	if idx := strings.Index(chown, ":"); idx >= 0 {
		chown = chown[:idx]
	}
	return strings.EqualFold(strings.TrimSpace(chown), user)
}

// stageHasRuntimeChown reports whether any RUN in the stage performs
// `chown -R user[:group] <state-dirs>` covering all four canonical Rails
// state directories. A partial chown (e.g. only `tmp log`) is not
// sufficient — Rails crashes on whichever directory is missing.
func stageHasRuntimeChown(sf *facts.StageFacts, user string) bool {
	if sf == nil {
		return false
	}
	for _, runFacts := range sf.Runs {
		if runFacts == nil {
			continue
		}
		for _, ci := range runFacts.CommandInfos {
			if !strings.EqualFold(ci.Name, "chown") {
				continue
			}
			if !chownTargetsUser(ci.Args, user) {
				continue
			}
			if !chownIsRecursive(ci.Args) {
				continue
			}
			if chownCoversAllStateDirs(ci.Args) {
				return true
			}
		}
	}
	return false
}

func chownTargetsUser(args []string, user string) bool {
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			continue
		}
		// First non-flag arg is the user/group spec.
		owner := a
		if idx := strings.Index(owner, ":"); idx >= 0 {
			owner = owner[:idx]
		}
		return strings.EqualFold(strings.TrimSpace(owner), user)
	}
	return false
}

func chownIsRecursive(args []string) bool {
	for _, a := range args {
		if a == "-R" || a == "--recursive" {
			return true
		}
		// Combined short flags (-Rh, -fR, etc.) — scan letters.
		if strings.HasPrefix(a, "-") && !strings.HasPrefix(a, "--") {
			for _, c := range a[1:] {
				if c == 'R' {
					return true
				}
			}
		}
	}
	return false
}

func chownCoversAllStateDirs(args []string) bool {
	covered := make(map[string]bool, len(railsStateDirs))
	sawUserSpec := false
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			continue
		}
		if !sawUserSpec {
			// Skip the first non-flag arg (the user[:group] spec).
			sawUserSpec = true
			continue
		}
		// Strip leading ./ or trailing /
		path := strings.TrimPrefix(a, "./")
		path = strings.TrimSuffix(path, "/")
		// Match against state dir basenames.
		for _, d := range railsStateDirs {
			if path == d || strings.HasSuffix(path, "/"+d) {
				covered[d] = true
			}
		}
	}
	for _, d := range railsStateDirs {
		if !covered[d] {
			return false
		}
	}
	return true
}

// copyInstructionLocation returns the COPY instruction's source location.
func copyInstructionLocation(file string, cmd *instructions.CopyCommand) rules.Location {
	if cmd == nil {
		return rules.NewFileLocation(file)
	}
	loc := cmd.Location()
	if len(loc) == 0 {
		return rules.NewFileLocation(file)
	}
	return rules.NewLocationFromRanges(file, loc)
}

// buildStatePathsFix proposes adding `--chown=user:user` to the offending
// COPY. The fix is FixSuggestion because the user/group may need
// adjustment in unusual setups (group not matching user, numeric uid/gid,
// etc.).
func buildStatePathsFix(
	file string,
	cmd *instructions.CopyCommand,
	user string,
	priority int,
) *rules.SuggestedFix {
	if cmd == nil {
		return nil
	}
	loc := cmd.Location()
	if len(loc) == 0 {
		return nil
	}
	startLine := loc[0].Start.Line
	// Position.Character is the 0-based column; rules.NewRangeLocation
	// expects 1-based columns.
	startCol := loc[0].Start.Character + 1
	// Insert the --chown flag immediately after `COPY` (4 chars + space = 5).
	const copyKeywordLen = len("COPY ")
	insertCol := startCol + copyKeywordLen
	chownFlag := "--chown=" + user + ":" + user + " "
	return &rules.SuggestedFix{
		Description: "Add --chown=" + user + ":" + user + " so Rails state dirs are writable at runtime",
		Safety:      rules.FixSuggestion,
		Priority:    priority,
		IsPreferred: true,
		Edits: []rules.TextEdit{{
			Location: rules.NewRangeLocation(file, startLine, insertCol, startLine, insertCol),
			NewText:  chownFlag,
		}},
	}
}

func init() {
	rules.Register(NewStatePathsNotWritableAsNonRootRule())
}
