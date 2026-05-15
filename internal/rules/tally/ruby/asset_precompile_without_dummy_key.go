package ruby

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	rubyfacts "github.com/wharflab/tally/internal/facts/ruby"

	"github.com/wharflab/tally/internal/facts"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/runmount"
	"github.com/wharflab/tally/internal/semantic"
	"github.com/wharflab/tally/internal/shell"
	"github.com/wharflab/tally/internal/sourcemap"
	"github.com/wharflab/tally/internal/stagename"
)

// AssetPrecompileWithoutDummyKeyRuleCode is the full rule code.
const AssetPrecompileWithoutDummyKeyRuleCode = rules.TallyRulePrefix + "ruby/asset-precompile-without-dummy-key"

// Same fix-priority tier as the jemalloc rule so cross-rule edit ordering
// stays predictable when both fire on the same Dockerfile.
const assetPrecompileFixPriority = 88

// dummyKeyVar is the Rails 7.1+ build-time placeholder env var. Setting it to
// any non-empty value tells Rails to skip credential decryption for asset
// compilation.
const dummyKeyVar = "SECRET_KEY_BASE_DUMMY"

// realKeyVar is the Rails secret-key env var. When set to the literal "1" the
// Rails 7.1 release notes accept it as the placeholder contract too — equivalent
// to dummyKeyVar for the purpose of this rule.
const realKeyVar = "SECRET_KEY_BASE"

// railsMasterKeyEnv is the runtime env var Rails uses to decrypt
// credentials.yml.enc. When the same RUN reads this from a BuildKit secret
// mount, the asset-precompile invocation is the supported alternative path
// to SECRET_KEY_BASE_DUMMY.
const railsMasterKeyEnv = "RAILS_MASTER_KEY"

// rails71MajorMinor is the Rails major*100+minor encoding for 7.1 — the
// release that introduced SECRET_KEY_BASE_DUMMY support. Older Rails versions
// don't honor the dummy key constant, so the rule's fix becomes a suggestion
// pointing at secret-mount alternatives instead.
const rails71MajorMinor = 7*100 + 1

// AssetPrecompileWithoutDummyKeyRule flags Rails asset:precompile invocations
// that lack SECRET_KEY_BASE_DUMMY (and don't use the BuildKit secret-mount
// alternative).
type AssetPrecompileWithoutDummyKeyRule struct{}

// NewAssetPrecompileWithoutDummyKeyRule creates the rule.
func NewAssetPrecompileWithoutDummyKeyRule() *AssetPrecompileWithoutDummyKeyRule {
	return &AssetPrecompileWithoutDummyKeyRule{}
}

// Metadata returns the rule metadata.
func (r *AssetPrecompileWithoutDummyKeyRule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            AssetPrecompileWithoutDummyKeyRuleCode,
		Name:            "Set SECRET_KEY_BASE_DUMMY for asset precompile",
		Description:     "Rails assets:precompile runs without SECRET_KEY_BASE_DUMMY=1, which forces RAILS_MASTER_KEY into image history",
		DocURL:          rules.TallyDocURL(AssetPrecompileWithoutDummyKeyRuleCode),
		DefaultSeverity: rules.SeverityWarning,
		Category:        "security",
		FixPriority:     assetPrecompileFixPriority,
	}
}

// Check runs the rule.
func (r *AssetPrecompileWithoutDummyKeyRule) Check(input rules.LintInput) []rules.Violation {
	meta := r.Metadata()

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

		// Skip when the same stage already has SECRET_KEY_BASE_DUMMY
		// (or SECRET_KEY_BASE=1) bound by an ENV before any RUN —
		// those bindings are visible at every RUN as part of EffectiveEnv.

		// Ruby-namespaced rule: only fire on stages that look like a Ruby
		// runtime. A non-Ruby image (Node, Python, ...) running an
		// asset:precompile-named binary for unrelated reasons shouldn't get
		// a Rails-flavored warning.
		if !stageLooksLikeRuby(input.Semantic, stageIdx, stage, sf) {
			continue
		}

		violations = append(violations, r.checkStage(input.File, sf, input.SourceMap(), meta, rubyFacts)...)
	}
	return violations
}

func (r *AssetPrecompileWithoutDummyKeyRule) checkStage(
	file string,
	sf *facts.StageFacts,
	sm *sourcemap.SourceMap,
	meta rules.RuleMetadata,
	rubyFacts *rubyfacts.RubyFacts,
) []rules.Violation {
	var violations []rules.Violation
	for _, runFacts := range sf.Runs {
		if runFacts == nil {
			continue
		}
		assetCompiles := findAssetPrecompileCommands(runFacts)
		if len(assetCompiles) == 0 {
			continue
		}
		if runFacts.Env.Values[dummyKeyVar] != "" {
			// Stage-level ENV already provides the placeholder.
			continue
		}
		if envValueIsPlaceholderOne(runFacts.Env.Values[realKeyVar]) {
			// SECRET_KEY_BASE=1 is also accepted as the placeholder contract.
			continue
		}
		// Each RUN can contain multiple asset-compile invocations (rare but
		// possible: chained `... && rake assets:precompile && bin/rails assets:precompile`).
		// We emit one violation per invocation so the suggested fix targets
		// each command precisely.
		for _, ac := range assetCompiles {
			if runScriptHasInlineDummyAssignment(runFacts.SourceScript, ac) {
				continue
			}
			if runHasMasterKeyFromSecret(runFacts) {
				continue
			}
			violations = append(violations, r.violationFor(file, sf, runFacts, ac, sm, meta, rubyFacts))
		}
	}
	return violations
}

func (r *AssetPrecompileWithoutDummyKeyRule) violationFor(
	file string,
	sf *facts.StageFacts,
	runFacts *facts.RunFacts,
	ac assetPrecompileCommand,
	sm *sourcemap.SourceMap,
	meta rules.RuleMetadata,
	rubyFacts *rubyfacts.RubyFacts,
) rules.Violation {
	severity := meta.DefaultSeverity
	rubyVersionInfo := detectRailsVersion(rubyFacts)
	supportsDummyKey := !rubyVersionInfo.detected || rubyVersionInfo.supportsDummyKey
	credentialsObservable := rubyFacts != nil && rubyFacts.HasEncryptedCredentials
	contextWasInspected := rubyFacts != nil &&
		(rubyFacts.Lockfile != nil ||
			rubyFacts.Gemfile != nil ||
			len(rubyFacts.EncryptedCredentialsPaths) > 0)

	if contextWasInspected && !credentialsObservable {
		// No credentials file in this build — the dummy key is just
		// hygiene; the rule still fires but at info severity.
		severity = rules.SeverityInfo
	}

	loc := assetPrecompileLocation(file, runFacts, ac, sm)
	v := rules.NewViolation(loc, meta.Code, meta.Description, severity).
		WithDocURL(meta.DocURL).
		WithDetail(buildDetail(supportsDummyKey, credentialsObservable, contextWasInspected, rubyVersionInfo))

	if fix := buildAssetPrecompileFix(file, runFacts, ac, sm, meta.FixPriority, supportsDummyKey); fix != nil {
		v = v.WithSuggestedFix(fix)
	}

	// Suppress unused warning when sf is later needed.
	_ = sf
	return v
}

// assetPrecompileCommand bundles the parsed shell command that invokes
// assets:precompile together with the variable-prefix style we want our fix
// to emit. The fix builder needs to know whether the caller is Rake-shaped
// (`rake assets:precompile`) or Rails-shaped (`bin/rails assets:precompile`)
// — both honor the inline env-var prefix Bash uses for assignments.
type assetPrecompileCommand struct {
	cmd      shell.CommandInfo
	form     assetPrecompileForm
	flagSpan tokenSpan // span of "assets:precompile" arg, used for diagnostics
}

type assetPrecompileForm int

const (
	formUnknown assetPrecompileForm = iota
	formRails
	formRake
	formBundleExecRake
	formBundleExecRails
)

type tokenSpan struct {
	line     int // 0-based line within the script
	startCol int // 0-based column
	endCol   int // 0-based exclusive column
}

// findAssetPrecompileCommands returns every assets:precompile invocation in
// the RUN, normalized to a small set of supported shapes. Order matches
// source order.
func findAssetPrecompileCommands(runFacts *facts.RunFacts) []assetPrecompileCommand {
	if runFacts == nil {
		return nil
	}
	var out []assetPrecompileCommand
	for _, ci := range runFacts.CommandInfos {
		ac, ok := classifyAssetPrecompile(ci)
		if !ok {
			continue
		}
		out = append(out, ac)
	}
	return out
}

// classifyAssetPrecompile tests whether a parsed command invokes
// assets:precompile in one of the four supported shapes. Returns the
// classified command on match.
func classifyAssetPrecompile(ci shell.CommandInfo) (assetPrecompileCommand, bool) {
	switch ci.Name {
	case "rails":
		if argsHaveAssetsPrecompile(ci.Args, 0) {
			span := assetsPrecompileTokenSpan(ci, "assets:precompile")
			return assetPrecompileCommand{cmd: ci, form: formRails, flagSpan: span}, true
		}
	case "rake":
		if argsHaveAssetsPrecompile(ci.Args, 0) {
			span := assetsPrecompileTokenSpan(ci, "assets:precompile")
			return assetPrecompileCommand{cmd: ci, form: formRake, flagSpan: span}, true
		}
	case "bundle":
		// Look for `bundle exec <rake|rails> ... assets:precompile ...`.
		// We tolerate intermediate flags between `exec` and the wrapped
		// command, but the shapes the design doc cares about both have
		// `bundle exec <tool>` directly.
		if len(ci.Args) < 2 {
			return assetPrecompileCommand{}, false
		}
		if ci.Args[0] != "exec" {
			return assetPrecompileCommand{}, false
		}
		wrapped := ci.Args[1]
		var form assetPrecompileForm
		switch {
		case wrapped == "rake":
			form = formBundleExecRake
		case wrapped == "rails", strings.HasSuffix(wrapped, "/rails"):
			form = formBundleExecRails
		default:
			return assetPrecompileCommand{}, false
		}
		if !argsHaveAssetsPrecompile(ci.Args, 2) {
			return assetPrecompileCommand{}, false
		}
		span := assetsPrecompileTokenSpan(ci, "assets:precompile")
		return assetPrecompileCommand{cmd: ci, form: form, flagSpan: span}, true
	}
	return assetPrecompileCommand{}, false
}

// argsHaveAssetsPrecompile reports whether the args slice contains the
// literal "assets:precompile" task name at or after fromIndex. Tasks with
// trailing arguments (`assets:precompile RAILS_ENV=production`) are also
// matched on the leading token.
func argsHaveAssetsPrecompile(args []string, fromIndex int) bool {
	if fromIndex < 0 {
		fromIndex = 0
	}
	for i := fromIndex; i < len(args); i++ {
		if args[i] == "assets:precompile" {
			return true
		}
	}
	return false
}

// assetsPrecompileTokenSpan returns the script-relative span of the
// assets:precompile arg in the parsed command. Returns a zero span when the
// parser did not preserve arg ranges or the token is not present.
func assetsPrecompileTokenSpan(ci shell.CommandInfo, token string) tokenSpan {
	for i, arg := range ci.Args {
		if arg != token {
			continue
		}
		if i >= len(ci.ArgRanges) {
			return tokenSpan{}
		}
		r := ci.ArgRanges[i]
		return tokenSpan{line: r.Line, startCol: r.StartCol, endCol: r.EndCol}
	}
	return tokenSpan{}
}

// envValueIsPlaceholderOne reports whether an env value is the literal "1"
// (any optional surrounding whitespace ignored). This is the form Rails 7.1
// release notes call out as accepted alongside SECRET_KEY_BASE_DUMMY=1.
func envValueIsPlaceholderOne(v string) bool {
	return strings.TrimSpace(v) == "1"
}

// runHasMasterKeyFromSecret returns true when the RUN consumes
// RAILS_MASTER_KEY through a BuildKit secret mount. Two equivalent shapes are
// recognized:
//
//   - --mount=type=secret,id=rails_master_key,env=RAILS_MASTER_KEY
//     (BuildKit's env-injecting secret mount, no shell read needed).
//   - --mount=type=secret,id=rails_master_key + a shell expression that
//     reads the secret file (e.g. `RAILS_MASTER_KEY="$(cat /run/secrets/rails_master_key)"`).
//
// Both are the alternative compliant path called out in the design doc.
func runHasMasterKeyFromSecret(runFacts *facts.RunFacts) bool {
	if runFacts == nil || runFacts.Run == nil {
		return false
	}
	mounts := runmount.GetMounts(runFacts.Run)
	hasSecretMount := false
	for _, m := range mounts {
		if m == nil || m.Type != instructions.MountTypeSecret {
			continue
		}
		// env=RAILS_MASTER_KEY case: BuildKit injects the env var directly.
		if m.Env != nil && *m.Env == railsMasterKeyEnv {
			return true
		}
		hasSecretMount = true
	}
	if !hasSecretMount {
		return false
	}
	// Otherwise look for an explicit read of the secret file inside the
	// shell script. Match RAILS_MASTER_KEY="$(cat /run/secrets/...)" and
	// the equivalent forms tally's corpus uses.
	return masterKeyFromSecretFileRe.MatchString(runFacts.SourceScript)
}

// masterKeyFromSecretFileRe matches a typical bash assignment that pulls
// RAILS_MASTER_KEY out of a BuildKit secret mount file. The pattern is
// intentionally loose around whitespace and quoting; false positives only
// suppress the rule, which is the safe direction for "the user is wiring
// the master key from a real secret".
var masterKeyFromSecretFileRe = regexp.MustCompile(
	`RAILS_MASTER_KEY\s*=\s*["']?\$\(\s*cat\s+["']?/run/secrets/[A-Za-z0-9_./-]+`,
)

// runScriptHasInlineDummyAssignment reports whether the RUN script's
// preface preceding the asset-compile command sets the dummy key in a way
// that actually applies to the command:
//
//  1. As an inline assignment-prefix attached directly to the precompile
//     command itself (`SECRET_KEY_BASE_DUMMY=1 bin/rails assets:precompile`).
//     POSIX `VAR=value cmd` semantics: the assignment is scoped to that
//     single command's environment.
//  2. As an `export` statement earlier in the same shell — those persist for
//     every command that follows in the same shell, so the precompile
//     inherits the setting.
//
// Plain assignments without `export` (e.g. `SECRET_KEY_BASE_DUMMY=1 echo ok &&
// bin/rails ...`) are NOT counted: those scope only to the single command
// they prefix (`echo ok`), not to the subsequent `bin/rails`.
func runScriptHasInlineDummyAssignment(script string, ac assetPrecompileCommand) bool {
	if script == "" {
		return false
	}
	cutoff := assetPrecompileScriptOffset(script, ac)
	if cutoff < 0 {
		// Couldn't locate the command inside the reconstructed script —
		// fall back to scanning the whole script. The risk is a placeholder
		// later in the script suppressing the violation, which is the safe
		// direction for this rule.
		cutoff = len(script)
	}
	preface := script[:cutoff]

	// Case 1: assignment-prefix immediately before the command. The prefix
	// is the run of `VAR=value` tokens that ends at the command itself, with
	// no `&&`/`;`/`|`/`\n` in between. We anchor the scan at the cutoff and
	// walk backwards to find the closest preceding shell separator; the
	// assignment must live in that final segment.
	finalSegment := preface
	if sep := lastShellSeparatorOffset(preface); sep >= 0 {
		finalSegment = preface[sep+1:]
	}
	if dummyAssignmentRe.MatchString(finalSegment) {
		return true
	}
	if realKeyEqualsOneRe.MatchString(finalSegment) {
		return true
	}

	// Case 2: `export SECRET_KEY_BASE_DUMMY=...` (or `SECRET_KEY_BASE=1`)
	// anywhere in the preface. Exported values persist across separators
	// so they affect any subsequent command in the same shell.
	if exportedDummyAssignmentRe.MatchString(preface) {
		return true
	}
	if exportedRealKeyEqualsOneRe.MatchString(preface) {
		return true
	}
	return false
}

// lastShellSeparatorOffset returns the byte offset of the last shell
// command separator in the input (the position of the separator character
// itself), or -1 if none. Recognized separators: `;`, `&`, `&&`, `|`,
// `||`, `\n`. Quoting is not modeled here — a separator inside `"..."`
// would still match — but `RUN` scripts in the corpus that we're trying
// to match against (Rails-generator-style) don't put separators inside
// quoted strings, so this is acceptable.
func lastShellSeparatorOffset(s string) int {
	last := -1
	for i := range len(s) {
		switch s[i] {
		case ';', '&', '|', '\n':
			last = i
		}
	}
	return last
}

// dummyAssignmentRe matches SECRET_KEY_BASE_DUMMY=<non-empty>, where the
// value can be quoted or bare and may appear after `export`.
//
// Forms it accepts (each on the LHS of the regex):
//
//	SECRET_KEY_BASE_DUMMY=1
//	export SECRET_KEY_BASE_DUMMY=1
//	SECRET_KEY_BASE_DUMMY="any-string"
//	SECRET_KEY_BASE_DUMMY='any-string'
var dummyAssignmentRe = regexp.MustCompile(
	`(?:^|[\s;&|(])` + regexp.QuoteMeta(dummyKeyVar) + `=(?:"[^"]+"|'[^']+'|\S+)`,
)

// realKeyEqualsOneRe matches SECRET_KEY_BASE=1 and the quoted variants
// (single- or double-quoted "1"). The Rails 7.1 release notes describe
// SECRET_KEY_BASE=1 as also acceptable as the placeholder contract.
var realKeyEqualsOneRe = regexp.MustCompile(
	`(?:^|[\s;&|(])` + regexp.QuoteMeta(realKeyVar) + `=(?:1|"1"|'1')(?:\s|$|[;&|)])`,
)

// exportedDummyAssignmentRe matches `export SECRET_KEY_BASE_DUMMY=...`. Unlike
// the bare assignment form, an exported value persists across separators in
// the same shell, so it counts as compliant for any subsequent command.
var exportedDummyAssignmentRe = regexp.MustCompile(
	`(?:^|[\s;&|(])export\s+` + regexp.QuoteMeta(dummyKeyVar) + `=(?:"[^"]+"|'[^']+'|\S+)`,
)

// exportedRealKeyEqualsOneRe matches `export SECRET_KEY_BASE=1`. Same scoping
// rationale as exportedDummyAssignmentRe.
var exportedRealKeyEqualsOneRe = regexp.MustCompile(
	`(?:^|[\s;&|(])export\s+` + regexp.QuoteMeta(realKeyVar) + `=(?:1|"1"|'1')(?:\s|$|[;&|)])`,
)

// assetPrecompileScriptOffset returns the byte offset within the
// reconstructed script where the assets:precompile invocation starts, or -1
// when the parser didn't preserve enough position info to locate it.
func assetPrecompileScriptOffset(script string, ac assetPrecompileCommand) int {
	if !ac.cmd.HasCommandRange {
		return -1
	}
	return scriptOffsetForLineCol(script, ac.cmd.Line, ac.cmd.StartCol)
}

// scriptOffsetForLineCol converts a 0-based (line, column) coordinate inside
// the reconstructed script to a byte offset. Columns are byte offsets from
// the start of their line. Returns -1 when the requested line is out of
// range.
func scriptOffsetForLineCol(script string, line, col int) int {
	if line < 0 || col < 0 {
		return -1
	}
	currentLine := 0
	offset := 0
	for offset < len(script) {
		if currentLine == line {
			if col > len(script)-offset {
				return -1
			}
			return offset + col
		}
		nl := strings.IndexByte(script[offset:], '\n')
		if nl < 0 {
			return -1
		}
		offset += nl + 1
		currentLine++
	}
	return -1
}

// assetPrecompileLocation returns the violation's reported source location:
// prefer the line of the assets:precompile token itself, falling back to the
// command name span, then the RUN's first line.
func assetPrecompileLocation(
	file string,
	runFacts *facts.RunFacts,
	ac assetPrecompileCommand,
	sm *sourcemap.SourceMap,
) rules.Location {
	if runFacts == nil || runFacts.Run == nil {
		return rules.NewFileLocation(file)
	}
	runRanges := runFacts.Run.Location()
	if len(runRanges) == 0 {
		return rules.NewFileLocation(file)
	}

	// Prefer the assets:precompile token span when the parser preserved it.
	if ac.flagSpan.endCol > ac.flagSpan.startCol {
		line := runRanges[0].Start.Line + ac.flagSpan.line
		startCol, endCol := ac.flagSpan.startCol, ac.flagSpan.endCol
		if ac.flagSpan.line == 0 && sm != nil {
			offset := shell.DockerfileRunCommandStartCol(sm.Line(line - 1))
			startCol += offset
			endCol += offset
		}
		return rules.NewRangeLocation(file, line, startCol, line, endCol)
	}

	// Fallback: command-name span.
	if ac.cmd.HasCommandRange {
		line := runRanges[0].Start.Line + ac.cmd.Line
		startCol, endCol := ac.cmd.StartCol, ac.cmd.EndCol
		if ac.cmd.Line == 0 && sm != nil {
			offset := shell.DockerfileRunCommandStartCol(sm.Line(line - 1))
			startCol += offset
			endCol += offset
		}
		return rules.NewRangeLocation(file, line, startCol, line, endCol)
	}
	return rules.NewLocationFromRanges(file, runRanges)
}

// detectRailsVersion returns the project's Rails major.minor when an
// observable Gemfile.lock supplies it. Falls back to "unknown" so the rule
// keeps its default fix wording when there's nothing to gate on.
func detectRailsVersion(rubyFacts *rubyfacts.RubyFacts) railsVersionInfo {
	info := railsVersionInfo{supportsDummyKey: true}
	if rubyFacts == nil || rubyFacts.Lockfile == nil {
		return info
	}
	version := rubyFacts.Lockfile.Specs["rails"]
	if version == "" {
		return info
	}
	major, minor, ok := parseMajorMinor(version)
	if !ok {
		return info
	}
	info.detected = true
	info.major = major
	info.minor = minor
	info.versionString = version
	info.supportsDummyKey = (major*100 + minor) >= rails71MajorMinor
	return info
}

// railsVersionInfo packages the rule's view of the project's Rails version.
type railsVersionInfo struct {
	detected         bool
	major            int
	minor            int
	versionString    string
	supportsDummyKey bool
}

// parseMajorMinor parses "M", "M.N", "M.N.P", "M.N.P.preX" into (M, N).
// Returns ok=false for empty or non-numeric M.
func parseMajorMinor(version string) (int, int, bool) {
	parts := strings.SplitN(version, ".", 3)
	if len(parts) == 0 {
		return 0, 0, false
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, false
	}
	minor := 0
	if len(parts) >= 2 {
		// Strip suffixes like "1.beta1" -> "1".
		minorRaw := parts[1]
		minorClean := strings.Builder{}
		for _, r := range minorRaw {
			if r >= '0' && r <= '9' {
				minorClean.WriteRune(r)
				continue
			}
			break
		}
		if minorClean.Len() > 0 {
			n, err := strconv.Atoi(minorClean.String())
			if err == nil {
				minor = n
			}
		}
	}
	return major, minor, true
}

// buildDetail composes the human-readable detail string for the violation,
// taking the observable build context into account.
func buildDetail(
	supportsDummyKey bool,
	credentialsObservable bool,
	contextWasInspected bool,
	versionInfo railsVersionInfo,
) string {
	if !supportsDummyKey {
		return fmt.Sprintf(
			"Rails %s predates SECRET_KEY_BASE_DUMMY (added in Rails 7.1). "+
				"Pass RAILS_MASTER_KEY through a BuildKit secret mount "+
				"(`--mount=type=secret,id=rails_master_key,env=RAILS_MASTER_KEY`) "+
				"or set SECRET_KEY_BASE to a random ephemeral value for this build "+
				"so neither secret ends up in image history.",
			versionInfo.versionString,
		)
	}
	if contextWasInspected && !credentialsObservable {
		return "This build context does not appear to ship a Rails encrypted-credentials file " +
			"(`config/credentials.yml.enc` or `config/credentials/<env>.yml.enc`), so the dummy " +
			"key is just hygiene — but still recommended for image-cache reproducibility. " +
			"Prepend `SECRET_KEY_BASE_DUMMY=1` to the asset-precompile command so future " +
			"credential rollouts don't bake `RAILS_MASTER_KEY` into image history."
	}
	return "Running `assets:precompile` without `SECRET_KEY_BASE_DUMMY=1` forces Rails to " +
		"decrypt credentials at build time, which pushes users toward passing `RAILS_MASTER_KEY` " +
		"via `ARG`/`ENV` — both end up in image history. " +
		"Prepend `SECRET_KEY_BASE_DUMMY=1` to the command (Rails 7.1+ honors it as the build-time " +
		"placeholder) or consume the master key through `RUN --mount=type=secret`."
}

// buildAssetPrecompileFix builds the suggested fix. For Rails 7.1+ projects
// (the default when the linter has no observable Rails version) the fix
// inserts `SECRET_KEY_BASE_DUMMY=1 ` immediately before the asset-compile
// command at FixSafe — exactly the patch the Rails 7.1 release notes
// recommend. For Rails < 7.1 we drop the safety down to FixSuggestion and
// suggest the BuildKit secret-mount path instead.
func buildAssetPrecompileFix(
	file string,
	runFacts *facts.RunFacts,
	ac assetPrecompileCommand,
	sm *sourcemap.SourceMap,
	priority int,
	supportsDummyKey bool,
) *rules.SuggestedFix {
	if runFacts == nil || runFacts.Run == nil || sm == nil {
		return nil
	}
	if !ac.cmd.HasCommandRange {
		return nil
	}
	runRanges := runFacts.Run.Location()
	if len(runRanges) == 0 {
		return nil
	}

	// For Rails < 7.1, the dummy key constant doesn't work. Surface the
	// secret-mount alternative as a structural suggestion instead of a
	// drop-in textual fix — the user must decide which CI secret to wire.
	if !supportsDummyKey {
		return &rules.SuggestedFix{
			Description: "Pass RAILS_MASTER_KEY via a BuildKit secret mount (Rails < 7.1 does not honor SECRET_KEY_BASE_DUMMY)",
			Safety:      rules.FixSuggestion,
			Priority:    priority,
		}
	}

	line := runRanges[0].Start.Line + ac.cmd.Line
	startCol := ac.cmd.StartCol
	if ac.cmd.Line == 0 {
		offset := shell.DockerfileRunCommandStartCol(sm.Line(line - 1))
		startCol += offset
	}
	if startCol < 0 {
		return nil
	}
	return &rules.SuggestedFix{
		Description: "Prepend SECRET_KEY_BASE_DUMMY=1 to the assets:precompile command so the build never needs RAILS_MASTER_KEY",
		Safety:      rules.FixSafe,
		Priority:    priority,
		IsPreferred: true,
		Edits: []rules.TextEdit{{
			Location: rules.NewRangeLocation(file, line, startCol, line, startCol),
			NewText:  dummyKeyVar + "=1 ",
		}},
	}
}

func init() {
	rules.Register(NewAssetPrecompileWithoutDummyKeyRule())
}
