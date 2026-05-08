package ruby

import (
	"path"
	"slices"
	"strings"

	"github.com/distribution/reference"
	"github.com/moby/buildkit/frontend/dockerfile/instructions"
	dfshell "github.com/moby/buildkit/frontend/dockerfile/shell"

	"github.com/wharflab/tally/internal/facts"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/semantic"
	"github.com/wharflab/tally/internal/shell"
	"github.com/wharflab/tally/internal/sourcemap"
	"github.com/wharflab/tally/internal/stagename"
)

// jemallocCanonicalSymlinkPath is the path our suggested fix points
// LD_PRELOAD at. The "stage already creates the symlink" guardrail must
// verify this exact path is the *target* of an existing create/move, not just
// that the path appears anywhere in the script.
const jemallocCanonicalSymlinkPath = "/usr/local/lib/libjemalloc.so"

// jemallocSymlinkCreatingCommands are the parsed command names that can
// materialize a file at a target path. Substring scans on the raw script are
// too loose — `find`, `echo`, or stray references would falsely suppress the
// symlink half of the fix, leaving LD_PRELOAD pointing at a missing file.
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
	"jemalloc":        true, // Alpine, Arch, RHEL/Fedora/CentOS
	"jemalloc-dev":    true, // Alpine dev headers
	"jemalloc-devel":  true, // RHEL/Fedora/CentOS dev headers
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

		// Ruby-namespaced rule: only fire on stages that look like a Ruby
		// runtime. A non-Ruby image (Node, Python, ...) installing jemalloc
		// for unrelated reasons shouldn't get a Rails-flavored warning.
		if !stageLooksLikeRuby(input.Semantic, stageIdx, stage, sf, input.MetaArgs) {
			continue
		}

		if stageHasJemallocLoadSignal(sf) {
			continue
		}

		violations = append(violations, r.checkStage(input.File, sf, input.SourceMap(), meta)...)
	}
	return violations
}

// rubyEnvSignals are environment variable keys that strongly suggest a Ruby
// or Rails workload is the target of the stage.
var rubyEnvSignals = map[string]bool{
	"RUBY_VERSION":      true,
	"RUBY_MAJOR":        true,
	"RUBY_YJIT_ENABLE":  true,
	"RAILS_ENV":         true,
	"BUNDLER_VERSION":   true,
	"BUNDLE_PATH":       true,
	"BUNDLE_DEPLOYMENT": true,
	"BUNDLE_WITHOUT":    true,
	"GEM_HOME":          true,
}

// rubyRuntimeCommandNames are executable basenames whose presence as
// ENTRYPOINT or CMD strongly suggests a Ruby runtime.
var rubyRuntimeCommandNames = map[string]bool{
	"ruby":      true,
	"rails":     true,
	"bundle":    true,
	"rake":      true,
	"rackup":    true,
	"puma":      true,
	"unicorn":   true,
	"thin":      true,
	"passenger": true,
	"falcon":    true,
	"sidekiq":   true,
	"iodine":    true,
}

// rubyDerivativeImages are non-official image repositories widely used as
// Ruby/Rails runtime bases. Matched against the familiar name (no domain,
// no tag).
var rubyDerivativeImages = map[string]bool{
	"jruby":                  true,
	"truffleruby":            true,
	"rubylang/ruby":          true,
	"phusion/passenger-ruby": true,
}

// stageLooksLikeRuby reports whether the stage looks like a Ruby/Rails
// runtime: an official ruby:* base (including ARG-templated forms like
// `FROM ${RUBY_IMAGE}` resolved against meta ARGs), a known Ruby-runtime
// derivative, a stage env with Ruby/Rails/Bundler signals, or a runtime
// command that matches a Ruby app server.
//
// For stage refs (`FROM <stage>`) the classifier walks the StageRef
// ancestry until it finds the original external base image — a final
// stage `FROM builder` where `builder` is `FROM ruby:3.3-slim` is still
// classified as Ruby even when the final stage carries no explicit Ruby
// env or runtime command.
func stageLooksLikeRuby(
	sem *semantic.Model,
	stageIdx int,
	stage instructions.Stage,
	sf *facts.StageFacts,
	metaArgs []instructions.ArgCommand,
) bool {
	if base := sem.ExternalBase(stageIdx); base != nil && baseImageLooksLikeRuby(base.Raw, metaArgs) {
		return true
	}
	// Ruby env signals must come from an actual ENV instruction. ARG values
	// are also visible in EffectiveEnv.Values, but they are build-time only
	// and don't make a stage a Ruby runtime — `FROM debian` +
	// `ARG RAILS_ENV=production` should not be classified as Ruby.
	if sf != nil {
		for key := range sf.EffectiveEnv.Bindings {
			if rubyEnvSignals[key] || strings.HasPrefix(key, "BUNDLE_") {
				return true
			}
		}
	}
	for _, name := range stageRuntimeCommandBasenames(stage) {
		if rubyRuntimeCommandNames[name] {
			return true
		}
	}
	return false
}

// baseImageLooksLikeRuby reports whether a base image reference points at
// a Ruby or Rails runtime — the official ruby:* image, a familiar name
// that mentions "ruby" or "rails", or a known derivative. ARG-templated
// references like `FROM ${RUBY_IMAGE}` are resolved against meta ARGs
// (the ones declared before the first FROM) so the classification still
// works on Dockerfiles that parameterize the base image.
func baseImageLooksLikeRuby(raw string, metaArgs []instructions.ArgCommand) bool {
	if rawLooksLikeRuby(raw) {
		return true
	}
	if expanded, ok := expandWithMetaArgs(raw, metaArgs); ok && expanded != raw {
		return rawLooksLikeRuby(expanded)
	}
	return false
}

// rawLooksLikeRuby parses a base image reference and matches against the
// known Ruby/Rails name set. Returns false when the reference is unparsable
// (e.g. still contains a `${VAR}` placeholder).
func rawLooksLikeRuby(s string) bool {
	named, err := reference.ParseNormalizedNamed(strings.ToLower(s))
	if err != nil {
		return false
	}
	familiar := reference.FamiliarName(named)
	if familiar == "ruby" || rubyDerivativeImages[familiar] {
		return true
	}
	return strings.Contains(familiar, "ruby") || strings.Contains(familiar, "rails")
}

// expandWithMetaArgs resolves `${VAR}`/`$VAR` references in `raw` against
// the default values of meta ARGs (those declared before the first FROM,
// which are the only ARGs in scope for FROM expansion). Returns the
// expanded string and ok=true when expansion succeeded with no unmatched
// references.
func expandWithMetaArgs(raw string, metaArgs []instructions.ArgCommand) (string, bool) {
	if raw == "" || !strings.ContainsAny(raw, "$") {
		return raw, false
	}
	env := make([]string, 0, len(metaArgs))
	for _, arg := range metaArgs {
		for _, kv := range arg.Args {
			if kv.Value == nil {
				continue
			}
			env = append(env, kv.Key+"="+*kv.Value)
		}
	}
	lex := dfshell.NewLex('\\')
	res, err := lex.ProcessWordWithMatches(raw, dfshell.EnvsFromSlice(env))
	if err != nil || len(res.Unmatched) > 0 || res.Result == "" {
		return raw, false
	}
	return res.Result, true
}

// stageRuntimeCommandBasenames returns the lowercased basename of the first
// executable in the stage's last ENTRYPOINT and CMD, in declaration order.
// Returns an empty slice when neither ENTRYPOINT nor CMD is present.
func stageRuntimeCommandBasenames(stage instructions.Stage) []string {
	var lastEntrypoint *instructions.EntrypointCommand
	var lastCmd *instructions.CmdCommand
	for _, c := range stage.Commands {
		switch cc := c.(type) {
		case *instructions.EntrypointCommand:
			lastEntrypoint = cc
		case *instructions.CmdCommand:
			lastCmd = cc
		}
	}
	var out []string
	if lastEntrypoint != nil && len(lastEntrypoint.CmdLine) > 0 {
		out = append(out, commandBasename(lastEntrypoint.CmdLine[0]))
	}
	if lastCmd != nil && len(lastCmd.CmdLine) > 0 {
		out = append(out, commandBasename(lastCmd.CmdLine[0]))
	}
	return out
}

func commandBasename(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if fields := strings.Fields(s); len(fields) > 0 {
		s = fields[0]
	}
	return strings.ToLower(path.Base(s))
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
				WithDetail(jemallocViolationDetail(ic))

			if fix := buildJemallocPreloadFix(file, sf, runFacts, ic, sm, meta.FixPriority); fix != nil {
				v = v.WithSuggestedFix(fix)
			}
			violations = append(violations, v)
		}
	}
	return violations
}

// jemallocViolationDetail returns the human-readable detail attached to a
// violation. The canonical `ln -sf … libjemalloc.so.2 …` suggestion is only
// included when the install actually ships libjemalloc.so.2 — otherwise the
// advice would create a dangling symlink (e.g. libjemalloc1 ships .so.1).
func jemallocViolationDetail(ic shell.InstallCommand) string {
	if installCommandProvidesLibjemalloc2(ic) {
		return "Installing libjemalloc only adds the package to the image; it is not loaded by Ruby unless " +
			"LD_PRELOAD points at libjemalloc.so or MALLOC_CONF carries jemalloc-specific knobs. " +
			"Add `ln -sf /usr/lib/$(uname -m)-linux-gnu/libjemalloc.so.2 /usr/local/lib/libjemalloc.so` to the " +
			"install RUN, then set `ENV LD_PRELOAD=\"/usr/local/lib/libjemalloc.so\"` so long-lived Rails workers " +
			"actually use jemalloc."
	}
	if aptPackageManagers[strings.ToLower(ic.Manager)] {
		// libjemalloc1 (and any future apt variant we don't recognize as
		// libjemalloc.so.2-providing) ships a different .so version. Don't
		// suggest a hardcoded path here — that would steer users toward a
		// dangling symlink. Recommend migrating to libjemalloc2 instead.
		return "Installing libjemalloc only adds the package to the image; it is not loaded by Ruby unless " +
			"LD_PRELOAD points at the matching libjemalloc shared object in the runtime environment. " +
			"This package (e.g. libjemalloc1) is legacy and ships a different .so version than the " +
			"canonical Rails-generator pattern targets — migrate to libjemalloc2 so the standard symlink + " +
			"LD_PRELOAD recipe applies."
	}
	return "Installing a jemalloc package only adds it to the image; it is not loaded by Ruby unless " +
		"LD_PRELOAD points at the jemalloc shared object or MALLOC_CONF carries jemalloc-specific knobs " +
		"(narenas:, background_thread:, dirty_decay_ms:, muzzy_decay_ms:, thp:)."
}

// stageHasJemallocLoadSignal returns true if the effective env for the stage
// shows that jemalloc will be loaded — either via LD_PRELOAD pointing at a
// jemalloc shared object, or via MALLOC_CONF carrying a jemalloc-specific knob.
//
// Only values bound by an `ENV` instruction count: ARG-derived values live
// in EffectiveEnv.Values but are build-time only and absent from the final
// image runtime, so they do not actually load jemalloc at startup.
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
	if envContainsJemallocLDPreload(envBoundValue(sf, "LD_PRELOAD")) {
		return true
	}
	if mallocConfHasJemallocKnob(envBoundValue(sf, "MALLOC_CONF")) {
		return true
	}
	return false
}

// lastJemallocSymlinkRemovalRunEndLine returns the (1-based, multi-line-aware)
// end line of the *last* RUN in the stage whose parsed commands include a
// removal of the canonical /usr/local/lib/libjemalloc.so path (rm, unlink,
// or mv away from it). Returns 0 when no such RUN exists.
func lastJemallocSymlinkRemovalRunEndLine(sf *facts.StageFacts, sm *sourcemap.SourceMap) int {
	if sf == nil || sm == nil {
		return 0
	}
	last := 0
	for _, rf := range sf.Runs {
		if rf == nil || rf.Run == nil {
			continue
		}
		removes := false
		for _, ci := range rf.CommandInfos {
			switch {
			case jemallocSymlinkRemovingCommands[ci.Name]:
				if commandRemovesJemallocSymlink(ci) {
					removes = true
				}
			case ci.Name == "mv":
				if commandRemovesJemallocSymlink(ci) {
					removes = true
				}
			}
			if removes {
				break
			}
		}
		if !removes {
			continue
		}
		locs := rf.Run.Location()
		if len(locs) == 0 {
			continue
		}
		end := sm.ResolveEndLine(locs[len(locs)-1].End.Line)
		if end > last {
			last = end
		}
	}
	return last
}

// lastEnvWriteEndLine returns the (1-based, multi-line-aware) end line of
// the last ENV instruction that wrote `key` in the stage, or 0 when no such
// binding exists. The Bindings map records the final write, so a single
// lookup is sufficient even when multiple ENV instructions touch the key.
func lastEnvWriteEndLine(sf *facts.StageFacts, key string, sm *sourcemap.SourceMap) int {
	if sf == nil || sm == nil {
		return 0
	}
	binding, ok := sf.EffectiveEnv.Bindings[key]
	if !ok || binding.Command == nil {
		return 0
	}
	locs := binding.Command.Location()
	if len(locs) == 0 {
		return 0
	}
	return sm.ResolveEndLine(locs[len(locs)-1].End.Line)
}

// envBoundValue returns the *resolved* value of an env key that was set by
// an `ENV` instruction (or inherited from a parent stage's ENV). Values
// present only in EffectiveEnv.Values via ARG promotion return "" — those
// are build-time only and do not exist in the final image runtime.
//
// The Bindings map confirms the key was actually written by an ENV
// instruction; the resolved (variable-expanded) value comes from
// EffectiveEnv.Values, since binding.Value is the literal right-hand side
// of the ENV instruction (e.g. `$JEMALLOC_PATH`) and would miss valid
// chained patterns like `ENV LD_PRELOAD=$JEMALLOC_PATH`.
func envBoundValue(sf *facts.StageFacts, key string) string {
	if sf == nil {
		return ""
	}
	if _, ok := sf.EffectiveEnv.Bindings[key]; !ok {
		return ""
	}
	return sf.EffectiveEnv.Values[key]
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

// libjemalloc2ProvidingPackages are the apt-family packages that install
// libjemalloc.so.2 — the file the canonical fix symlinks to. libjemalloc1
// is intentionally excluded because it ships libjemalloc.so.1 instead, so
// a generated `ln -sf … libjemalloc.so.2 …` would point at a non-existent
// file and the suggested fix would not actually load jemalloc.
var libjemalloc2ProvidingPackages = map[string]bool{
	"libjemalloc2":    true, // ships /usr/lib/<arch>-linux-gnu/libjemalloc.so.2
	"libjemalloc-dev": true, // depends on libjemalloc2 and adds the unversioned link
}

// installCommandProvidesLibjemalloc2 reports whether the install command
// adds a package that ships /usr/lib/<arch>-linux-gnu/libjemalloc.so.2.
func installCommandProvidesLibjemalloc2(ic shell.InstallCommand) bool {
	if !aptPackageManagers[strings.ToLower(ic.Manager)] {
		return false
	}
	for _, pkg := range ic.Packages {
		stripped := strings.ToLower(shell.StripPackageVersion(pkg.Normalized))
		if libjemalloc2ProvidingPackages[stripped] {
			return true
		}
	}
	return false
}

// buildJemallocPreloadFix returns the canonical Rails-style fix: insert a new
// `RUN ln -sf … && ENV LD_PRELOAD=…` block on the line *after* the install RUN.
// Only emitted for apt-family installs of a package that actually ships
// libjemalloc.so.2 (libjemalloc2 / libjemalloc-dev). For libjemalloc1 the
// canonical fix would link to a non-existent `.so.2` file, so no fix is
// offered there; the violation still fires.
//
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
	if !installCommandProvidesLibjemalloc2(ic) {
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

	// Insert after the install RUN by default, but push past any later
	// instructions that would neutralize the fix:
	//
	//   - `ENV LD_PRELOAD=""` (or any non-jemalloc value) after the
	//     install RUN would otherwise overwrite our ENV write.
	//   - A later `rm /usr/local/lib/libjemalloc.so` (or `mv` away from
	//     it) would otherwise delete the symlink we just created,
	//     leaving LD_PRELOAD pointing at a missing file.
	insertLine := endLine + 1
	for _, key := range []string{"LD_PRELOAD", "MALLOC_CONF"} {
		if line := lastEnvWriteEndLine(sf, key, sm); line > endLine && line+1 > insertLine {
			insertLine = line + 1
		}
	}
	if line := lastJemallocSymlinkRemovalRunEndLine(sf, sm); line > endLine && line+1 > insertLine {
		insertLine = line + 1
	}
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
// RUN that creates the canonical /usr/local/lib/libjemalloc.so file — i.e.
// the exact path our suggested fix points LD_PRELOAD at. Typically this is a
// `ln -s SRC /usr/local/lib/libjemalloc.so` step, but `cp`, `mv`, and
// `install` with the same target are also accepted.
//
// The check inspects parsed command invocations (not raw text) and only
// matches when the LAST non-flag argument of a create/move command resolves
// to /usr/local/lib/libjemalloc.so under path.Clean. Earlier non-flag args
// are intentionally ignored: they are the source(s), and matching them would
// cause `cp /opt/libjemalloc.so /tmp/backup.so` or
// `mv /usr/local/lib/libjemalloc.so /tmp/old.so` to be treated as "the
// symlink exists" — the latter actually removes the canonical file, so
// suppressing the symlink half of the fix would leave LD_PRELOAD pointing at
// a missing file.
//
// References that are not create/move (find, echo, ls, ...) and references to
// libjemalloc.so.2 (the apt-shipped versioned library, not the symlink
// target) are rejected by construction.
func stageReferencesJemallocSymlink(sf *facts.StageFacts) bool {
	if sf == nil {
		return false
	}
	// Walk the stage in source order and track whether the canonical
	// symlink is *currently* present at the end of the stage. A later
	// `rm /usr/local/lib/libjemalloc.so` (or `mv` away from it) undoes
	// an earlier creation, so a single creation early in the stage is
	// not sufficient evidence.
	present := false
	for _, rf := range sf.Runs {
		if rf == nil {
			continue
		}
		for _, ci := range rf.CommandInfos {
			switch {
			case jemallocSymlinkCreatingCommands[ci.Name]:
				if commandTargetsCanonicalSymlinkPath(ci) {
					// Any write to the canonical target redefines what
					// lives there. Set present only when the source is
					// jemalloc; clear it otherwise — e.g.
					// `cp /tmp/libfoo.so /usr/local/lib/libjemalloc.so`
					// overwrites a previously-valid symlink with an
					// unrelated `.so`, so the runtime LD_PRELOAD would
					// load the wrong library.
					if nonTargetArgsReferenceJemalloc(ci.Args) {
						present = true
					} else {
						present = false
					}
				}
				if ci.Name == "mv" && commandRemovesJemallocSymlink(ci) {
					// `mv /usr/local/lib/libjemalloc.so DST` removes the
					// source. The target check above does NOT match this
					// shape (target is DST), so the present flag stays
					// untouched there; this branch handles the removal.
					present = false
				}
			case jemallocSymlinkRemovingCommands[ci.Name]:
				if commandRemovesJemallocSymlink(ci) {
					present = false
				}
			}
		}
	}
	return present
}

// commandTargetsCanonicalSymlinkPath reports whether the command's
// destination — the last non-flag arg under the standard
// `cmd [OPTION...] SRC... DST` form — resolves to the canonical
// /usr/local/lib/libjemalloc.so path. Skips `install -d` (which creates
// a directory at the target, not a file).
func commandTargetsCanonicalSymlinkPath(ci shell.CommandInfo) bool {
	if ci.Name == "install" && (ci.HasFlag("-d") || ci.HasFlag("--directory")) {
		return false
	}
	target := lastNonFlagArg(ci.Args)
	if target == "" {
		return false
	}
	return path.Clean(target) == jemallocCanonicalSymlinkPath
}

// jemallocSymlinkRemovingCommands are command names that, when invoked
// with the canonical path as a non-flag arg, remove the symlink.
var jemallocSymlinkRemovingCommands = map[string]bool{
	"rm":     true,
	"unlink": true,
}

// commandRemovesJemallocSymlink reports whether the parsed command takes
// the canonical /usr/local/lib/libjemalloc.so as one of its non-flag args
// in a position that causes removal:
//
//   - rm /usr/local/lib/libjemalloc.so          → any non-flag arg counts
//   - unlink /usr/local/lib/libjemalloc.so      → any non-flag arg counts
//   - mv /usr/local/lib/libjemalloc.so DST       → only the SOURCE counts;
//     the target (last non-flag arg) means it was created, not removed
//   - mv -t DIR /usr/local/lib/libjemalloc.so    → all args after the -t
//     value are sources, even though they appear after the target
func commandRemovesJemallocSymlink(ci shell.CommandInfo) bool {
	if ci.Name == "mv" {
		sources := mvSourceArgs(ci)
		for _, arg := range sources {
			if path.Clean(arg) == jemallocCanonicalSymlinkPath {
				return true
			}
		}
		return false
	}
	// rm / unlink: any non-flag arg pointing at the canonical path removes it.
	for _, arg := range ci.Args {
		if strings.HasPrefix(arg, "-") {
			continue
		}
		if path.Clean(arg) == jemallocCanonicalSymlinkPath {
			return true
		}
	}
	return false
}

// mvSourceArgs returns the non-flag arguments to a `mv` invocation that act
// as sources (i.e. the files removed by the move). Two syntactic forms are
// supported:
//
//   - `mv [OPTION...] SRC... DST`              → all non-flag args before the
//     final non-flag (the destination) are sources.
//   - `mv [OPTION...] -t DIR SRC...`           → DIR is the destination;
//     all non-flag args other than DIR are sources. The long form
//     `--target-directory[=DIR]` is also recognized.
func mvSourceArgs(ci shell.CommandInfo) []string {
	skipIdx, ok := targetDirectoryArgIndex(ci)
	if ok {
		var sources []string
		for i, arg := range ci.Args {
			if i == skipIdx {
				continue
			}
			if strings.HasPrefix(arg, "-") {
				continue
			}
			sources = append(sources, arg)
		}
		return sources
	}
	// Default form: last non-flag is destination, the rest are sources.
	targetIdx := -1
	for i, arg := range slices.Backward(ci.Args) {
		if !strings.HasPrefix(arg, "-") {
			targetIdx = i
			break
		}
	}
	if targetIdx <= 0 {
		return nil
	}
	var sources []string
	for _, arg := range ci.Args[:targetIdx] {
		if strings.HasPrefix(arg, "-") {
			continue
		}
		sources = append(sources, arg)
	}
	return sources
}

// targetDirectoryArgIndex returns the index of the args entry that supplies
// the directory value to `-t` / `--target-directory` (the *value* token, not
// the flag itself). For `--target-directory=DIR`, `-t=DIR`, and the GNU
// short form `-tDIR` (value attached directly to the flag) the value is
// embedded in the same token and the returned index is -1 (nothing extra to
// skip beyond the flag prefix, which is filtered out by the leading `-`
// check). ok=false means the flag is not present.
func targetDirectoryArgIndex(ci shell.CommandInfo) (int, bool) {
	for i, arg := range ci.Args {
		switch {
		case arg == "-t" || arg == "--target-directory":
			if i+1 < len(ci.Args) && !strings.HasPrefix(ci.Args[i+1], "-") {
				return i + 1, true
			}
		case strings.HasPrefix(arg, "--target-directory="):
			return -1, true
		case strings.HasPrefix(arg, "-t="):
			return -1, true
		// GNU short flag with attached value: `-tDIR` (no `=` separator).
		// Only matches when the character after `-t` is non-empty and not
		// itself a flag separator, to avoid confusing `-t` followed by
		// other clustered short flags.
		case len(arg) > 2 && strings.HasPrefix(arg, "-t") && arg[2] != '-':
			return -1, true
		}
	}
	return 0, false
}

// lastNonFlagArg returns the last argument that does not start with `-`,
// or "" if every arg is a flag.
func lastNonFlagArg(args []string) string {
	for _, arg := range slices.Backward(args) {
		if !strings.HasPrefix(arg, "-") {
			return arg
		}
	}
	return ""
}

// nonTargetArgsReferenceJemalloc reports whether any non-flag arg *other than
// the last one* (the target of a create/move command) references a jemalloc
// shared object. Used to verify that a `cp`/`mv`/`install`/`ln` writing into
// /usr/local/lib/libjemalloc.so is actually copying jemalloc, not an
// unrelated `.so`.
func nonTargetArgsReferenceJemalloc(args []string) bool {
	targetIdx := -1
	for i, arg := range slices.Backward(args) {
		if !strings.HasPrefix(arg, "-") {
			targetIdx = i
			break
		}
	}
	if targetIdx <= 0 {
		return false
	}
	for _, arg := range args[:targetIdx] {
		if strings.HasPrefix(arg, "-") {
			continue
		}
		if strings.Contains(strings.ToLower(arg), "libjemalloc") {
			return true
		}
	}
	return false
}

func init() {
	rules.Register(NewJemallocInstalledButNotPreloadedRule())
}
