package ruby

import (
	"path"
	"strings"

	"github.com/wharflab/tally/internal/facts"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/semantic"
	"github.com/wharflab/tally/internal/shell"
	"github.com/wharflab/tally/internal/sourcemap"
	"github.com/wharflab/tally/internal/stagename"
)

// jemallocSymlinkCreatingCommands are the parsed command names that can
// materialize a libjemalloc.so file at the target path. Substring scans on the
// raw script are too loose — `find`, `echo`, or stray references would falsely
// suppress the symlink half of the fix, leaving LD_PRELOAD pointing at a
// missing file.
var jemallocSymlinkCreatingCommands = map[string]bool{
	"ln":      true,
	"cp":      true,
	"mv":      true,
	"install": true,
}

// JemallocInstalledButNotPreloadedRuleCode is the full rule code.
const JemallocInstalledButNotPreloadedRuleCode = rules.TallyRulePrefix + "ruby/jemalloc-installed-but-not-preloaded"

// Default fix priority — same tier as the PHP rules so cross-rule edit ordering
// stays predictable when both fire on the same Dockerfile.
const jemallocFixPriority = 88

// JemallocInstalledButNotPreloadedRule flags final stages that install a
// jemalloc package without setting LD_PRELOAD or jemalloc-knob MALLOC_CONF.
type JemallocInstalledButNotPreloadedRule struct{}

// NewJemallocInstalledButNotPreloadedRule creates the rule.
func NewJemallocInstalledButNotPreloadedRule() *JemallocInstalledButNotPreloadedRule {
	return &JemallocInstalledButNotPreloadedRule{}
}

// Metadata returns the rule metadata.
func (r *JemallocInstalledButNotPreloadedRule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            JemallocInstalledButNotPreloadedRuleCode,
		Name:            "jemalloc must be preloaded when installed",
		Description:     "Final image installs jemalloc but does not preload it via LD_PRELOAD or MALLOC_CONF",
		DocURL:          rules.TallyDocURL(JemallocInstalledButNotPreloadedRuleCode),
		DefaultSeverity: rules.SeverityWarning,
		Category:        "performance",
		FixPriority:     jemallocFixPriority,
	}
}

// jemallocPackageManagers are the OS package managers whose package names follow
// jemalloc distro conventions. Application managers (npm/pip/composer/...)
// cannot install the system allocator even if they happen to vendor a package
// with "jemalloc" in its name.
var jemallocPackageManagers = map[string]bool{
	"apt":      true,
	"apt-get":  true,
	"apk":      true,
	"dnf":      true,
	"microdnf": true,
	"yum":      true,
	"zypper":   true,
}

// aptPackageManagers are the apt-family managers whose canonical libjemalloc2
// path is /usr/lib/<arch>-linux-gnu/libjemalloc.so.2 and for which we can
// suggest the Rails-generator-style ln + LD_PRELOAD fix.
var aptPackageManagers = map[string]bool{
	"apt":     true,
	"apt-get": true,
}

// jemallocPackageNames lists the distro package names that ship the jemalloc
// allocator. Comparison is against the version-stripped, lowercased package
// name (see shell.StripPackageVersion).
var jemallocPackageNames = map[string]bool{
	"libjemalloc1":    true, // Debian/Ubuntu legacy
	"libjemalloc2":    true, // Debian/Ubuntu current
	"libjemalloc-dev": true, // Debian/Ubuntu dev headers
	"jemalloc":        true, // Alpine, Arch
	"jemalloc-dev":    true, // Alpine dev headers
}

// jemallocMallocConfKnobs are the option keys that only have meaning to
// jemalloc itself. If MALLOC_CONF carries any of these, jemalloc is loaded
// somehow (often via base-image LD_PRELOAD or a `ln + LD_PRELOAD` step that
// our heuristics could not see).
var jemallocMallocConfKnobs = []string{
	"narenas:",
	"background_thread:",
	"dirty_decay_ms:",
	"muzzy_decay_ms:",
	"thp:",
}

// Check runs the rule.
func (r *JemallocInstalledButNotPreloadedRule) Check(input rules.LintInput) []rules.Violation {
	meta := r.Metadata()

	var violations []rules.Violation
	for stageIdx, stage := range input.Stages {
		if stagename.LooksLikeDev(stage.Name) {
			continue
		}

		sf := input.Facts.Stage(stageIdx)
		if sf == nil || !sf.IsLast {
			continue
		}
		if sf.BaseImageOS == semantic.BaseImageOSWindows {
			continue
		}

		if stageHasJemallocLoadSignal(sf) {
			continue
		}

		violations = append(violations, r.checkStage(input.File, sf, input.SourceMap(), meta)...)
	}
	return violations
}

func (r *JemallocInstalledButNotPreloadedRule) checkStage(
	file string,
	sf *facts.StageFacts,
	sm *sourcemap.SourceMap,
	meta rules.RuleMetadata,
) []rules.Violation {
	var violations []rules.Violation
	for _, runFacts := range sf.Runs {
		if runFacts == nil {
			continue
		}
		for _, ic := range runFacts.InstallCommands {
			if !installCommandInstallsJemalloc(ic) {
				continue
			}

			loc := jemallocViolationLocation(file, runFacts, ic, sm)
			v := rules.NewViolation(loc, meta.Code, meta.Description, meta.DefaultSeverity).
				WithDocURL(meta.DocURL).
				WithDetail(jemallocViolationDetail(ic.Manager))

			if fix := buildJemallocPreloadFix(file, sf, runFacts, ic, sm, meta.FixPriority); fix != nil {
				v = v.WithSuggestedFix(fix)
			}
			violations = append(violations, v)
		}
	}
	return violations
}

func jemallocViolationDetail(manager string) string {
	if aptPackageManagers[strings.ToLower(manager)] {
		return "Installing libjemalloc only adds the package to the image; it is not loaded by Ruby unless " +
			"LD_PRELOAD points at libjemalloc.so or MALLOC_CONF carries jemalloc-specific knobs. " +
			"Add `ln -sf /usr/lib/$(uname -m)-linux-gnu/libjemalloc.so.2 /usr/local/lib/libjemalloc.so` to the " +
			"install RUN, then set `ENV LD_PRELOAD=\"/usr/local/lib/libjemalloc.so\"` so long-lived Rails workers " +
			"actually use jemalloc."
	}
	return "Installing a jemalloc package only adds it to the image; it is not loaded by Ruby unless " +
		"LD_PRELOAD points at the jemalloc shared object or MALLOC_CONF carries jemalloc-specific knobs " +
		"(narenas:, background_thread:, dirty_decay_ms:, muzzy_decay_ms:, thp:)."
}

// stageHasJemallocLoadSignal returns true if the effective env for the stage
// shows that jemalloc will be loaded — either via LD_PRELOAD pointing at a
// jemalloc shared object, or via MALLOC_CONF carrying a jemalloc-specific knob.
//
// LD_PRELOAD detection is substring-based on "jemalloc" because the canonical
// paths vary by distro (e.g. /usr/lib/x86_64-linux-gnu/libjemalloc.so.2,
// /usr/local/lib/libjemalloc.so, /usr/lib/libjemalloc.so.2).
//
// MALLOC_CONF detection looks for option keys that only mean something to
// jemalloc itself; glibc malloc and tcmalloc honor neither MALLOC_CONF nor
// these specific knobs, so seeing them is strong evidence jemalloc is loaded.
func stageHasJemallocLoadSignal(sf *facts.StageFacts) bool {
	if sf == nil {
		return false
	}
	if envContainsJemallocLDPreload(sf.EffectiveEnv.Values["LD_PRELOAD"]) {
		return true
	}
	if mallocConfHasJemallocKnob(sf.EffectiveEnv.Values["MALLOC_CONF"]) {
		return true
	}
	return false
}

func envContainsJemallocLDPreload(value string) bool {
	if value == "" {
		return false
	}
	return strings.Contains(strings.ToLower(value), "jemalloc")
}

func mallocConfHasJemallocKnob(value string) bool {
	if value == "" {
		return false
	}
	lower := strings.ToLower(value)
	for _, knob := range jemallocMallocConfKnobs {
		if strings.Contains(lower, knob) {
			return true
		}
	}
	return false
}

// installCommandInstallsJemalloc reports whether a parsed install command
// contains a jemalloc-bearing distro package. Application-level package
// managers (npm/pip/composer/...) are excluded because their packages cannot
// install the system allocator even if a name collision exists.
func installCommandInstallsJemalloc(ic shell.InstallCommand) bool {
	if !jemallocPackageManagers[strings.ToLower(ic.Manager)] {
		return false
	}
	for _, pkg := range ic.Packages {
		if isJemallocPackage(pkg.Normalized) {
			return true
		}
	}
	return false
}

func isJemallocPackage(name string) bool {
	stripped := strings.ToLower(shell.StripPackageVersion(name))
	return jemallocPackageNames[stripped]
}

// jemallocViolationLocation returns the source location to attribute the
// violation to: prefer the line of the package token itself, falling back to
// the RUN instruction's first line if positions are unavailable.
//
// On RUN line 0, PackageArg.StartCol/EndCol are shell-relative because
// facts.RunFacts.SourceScript is reconstructed via shell.ReconstructSourceText,
// which strips the `RUN <flags>` prefix. We translate back to Dockerfile
// coordinates by adding shell.DockerfileRunCommandStartCol of the RUN's first
// line. Continuation lines (pkg.Line > 0) are already Dockerfile-relative.
func jemallocViolationLocation(
	file string,
	runFacts *facts.RunFacts,
	ic shell.InstallCommand,
	sm *sourcemap.SourceMap,
) rules.Location {
	if runFacts == nil || runFacts.Run == nil {
		return rules.NewFileLocation(file)
	}

	for _, pkg := range ic.Packages {
		if !isJemallocPackage(pkg.Normalized) {
			continue
		}
		runRanges := runFacts.Run.Location()
		if len(runRanges) == 0 {
			break
		}
		line := runRanges[0].Start.Line + pkg.Line
		startCol, endCol := pkg.StartCol, pkg.EndCol
		if pkg.Line == 0 && sm != nil {
			offset := shell.DockerfileRunCommandStartCol(sm.Line(line - 1))
			startCol += offset
			endCol += offset
		}
		return rules.NewRangeLocation(file, line, startCol, line, endCol)
	}
	return rules.NewLocationFromRanges(file, runFacts.Run.Location())
}

// buildJemallocPreloadFix returns the canonical Rails-style fix: insert a new
// `RUN ln -sf … && ENV LD_PRELOAD=…` block on the line *after* the install RUN.
// Only emitted for the apt-family case where the path layout is well-known.
// The edit is a single zero-width insertion at column 0 of the line following
// the install RUN, so it does not collide with content edits other rules might
// apply to the install RUN itself.
//
// Two compositional concerns:
//
//  1. The rule also fires when a stage already runs `ln -s ... libjemalloc.so`
//     and only forgot the `ENV LD_PRELOAD`. In that case adding a second
//     unconditional `ln -s` would fail with `File exists` once the user runs
//     `--fix-unsafe`. The fix detects an existing reference to the canonical
//     `libjemalloc.so` shared object in the stage and emits only the missing
//     `ENV` line in that case.
//  2. As a defense in depth even when no symlink reference is detected (e.g.
//     a base-image inherited link the rule cannot see), the emitted command
//     uses `ln -sf` so a re-run on already-linked layouts replaces rather than
//     fails.
func buildJemallocPreloadFix(
	file string,
	sf *facts.StageFacts,
	runFacts *facts.RunFacts,
	ic shell.InstallCommand,
	sm *sourcemap.SourceMap,
	priority int,
) *rules.SuggestedFix {
	if runFacts == nil || runFacts.Run == nil || sm == nil {
		return nil
	}
	if !aptPackageManagers[strings.ToLower(ic.Manager)] {
		return nil
	}

	runRanges := runFacts.Run.Location()
	if len(runRanges) == 0 {
		return nil
	}
	endLine := sm.ResolveEndLine(runRanges[len(runRanges)-1].End.Line)
	if endLine <= 0 {
		return nil
	}

	insertLine := endLine + 1
	var fixText, description string
	if stageReferencesJemallocSymlink(sf) {
		fixText = `ENV LD_PRELOAD="/usr/local/lib/libjemalloc.so"
`
		description = "Set LD_PRELOAD so the jemalloc symlink already in this stage is actually loaded"
	} else {
		fixText = `RUN ln -sf /usr/lib/$(uname -m)-linux-gnu/libjemalloc.so.2 /usr/local/lib/libjemalloc.so
ENV LD_PRELOAD="/usr/local/lib/libjemalloc.so"
`
		description = "Symlink libjemalloc.so and set LD_PRELOAD so jemalloc is actually loaded"
	}
	return &rules.SuggestedFix{
		Description: description,
		Safety:      rules.FixSuggestion,
		Priority:    priority,
		IsPreferred: true,
		Edits: []rules.TextEdit{{
			Location: rules.NewRangeLocation(file, insertLine, 0, insertLine, 0),
			NewText:  fixText,
		}},
	}
}

// stageReferencesJemallocSymlink reports whether the stage already contains a
// RUN that creates a libjemalloc.so file — typically a `ln -s` step that
// materializes the canonical symlink, or a `cp`/`mv`/`install` of an
// equivalently-named file.
//
// The check inspects parsed command invocations (not raw text) and only
// matches when a non-flag argument's basename is exactly `libjemalloc.so`.
// This rejects unrelated references like `find / -name 'libjemalloc.so*'`,
// `echo` of a docs string, or matches on `libjemalloc.so.2` (the apt-shipped
// versioned library, not the symlink target). Without this narrowing, the
// fix could emit only `ENV LD_PRELOAD=/usr/local/lib/libjemalloc.so` against
// a stage that never creates that file, leaving jemalloc unloaded.
func stageReferencesJemallocSymlink(sf *facts.StageFacts) bool {
	if sf == nil {
		return false
	}
	for _, rf := range sf.Runs {
		if rf == nil {
			continue
		}
		for _, ci := range rf.CommandInfos {
			if !jemallocSymlinkCreatingCommands[ci.Name] {
				continue
			}
			for _, arg := range ci.Args {
				if strings.HasPrefix(arg, "-") {
					continue
				}
				if path.Base(arg) == "libjemalloc.so" {
					return true
				}
			}
		}
	}
	return false
}

func init() {
	rules.Register(NewJemallocInstalledButNotPreloadedRule())
}
