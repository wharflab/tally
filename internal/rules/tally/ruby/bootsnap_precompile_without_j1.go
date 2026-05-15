package ruby

import (
	"regexp"
	"strings"

	"github.com/wharflab/tally/internal/facts"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/semantic"
	"github.com/wharflab/tally/internal/shell"
	"github.com/wharflab/tally/internal/sourcemap"
	"github.com/wharflab/tally/internal/stagename"
)

// BootsnapPrecompileWithoutJ1RuleCode is the full rule code.
const BootsnapPrecompileWithoutJ1RuleCode = rules.TallyRulePrefix + "ruby/bootsnap-precompile-without-j1"

// Default fix priority — same tier as the jemalloc rule so cross-rule edit
// ordering stays predictable when both fire on the same Dockerfile.
const bootsnapFixPriority = 88

// bootsnapPrecompileRE matches a `bootsnap precompile` invocation in a RUN
// script. The pattern is anchored on word boundaries so `mybootsnap precompile`
// or `bootsnap precompiled-foo` do not match. Whitespace between `bootsnap`
// and `precompile` is at least one space/tab, which matches both literal
// `bootsnap precompile` and `bundle exec bootsnap precompile`.
var bootsnapPrecompileRE = regexp.MustCompile(`(?m)\bbootsnap[ \t]+precompile\b`)

// jobsFlagRE matches the parallelism flag in any of its supported forms:
//
//   - `-j 1`    — short flag with space
//   - `-j1`     — short flag with attached value
//   - `-j=1`    — short flag with equals
//   - `--jobs 1`
//   - `--jobs=1`
//
// We only suppress on `1` because that is the documented QEMU-safe value;
// `-j 2` etc. still risk the bug per bootsnap issue #495.
var jobsFlagRE = regexp.MustCompile(`(?:^|[ \t])(?:-j[ \t=]?1\b|--jobs[ \t=]+1\b)`)

// bootsnapPlatformGuardLiterals are the `--platform`-style ARGs whose
// co-occurrence in a script is the suppress signal: when the user wraps
// the bootsnap call in a `BUILDPLATFORM == TARGETPLATFORM` shell check,
// they have explicitly avoided emulated paths and the rule should stand
// down. Matching is heuristic; false negatives are cheaper than false
// positives here.
var bootsnapPlatformGuardLiterals = []string{
	"BUILDPLATFORM",
	"TARGETPLATFORM",
}

// BootsnapPrecompileWithoutJ1Rule flags Ruby/Rails stages that run
// `bootsnap precompile` without `-j 1`, the QEMU-safe parallelism flag.
type BootsnapPrecompileWithoutJ1Rule struct{}

// NewBootsnapPrecompileWithoutJ1Rule creates the rule.
func NewBootsnapPrecompileWithoutJ1Rule() *BootsnapPrecompileWithoutJ1Rule {
	return &BootsnapPrecompileWithoutJ1Rule{}
}

// Metadata returns the rule metadata.
func (r *BootsnapPrecompileWithoutJ1Rule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            BootsnapPrecompileWithoutJ1RuleCode,
		Name:            "bootsnap precompile must use -j 1 for QEMU-safe builds",
		Description:     "`bootsnap precompile` runs without `-j 1`, which crashes under QEMU multi-arch builds",
		DocURL:          rules.TallyDocURL(BootsnapPrecompileWithoutJ1RuleCode),
		DefaultSeverity: rules.SeverityWarning,
		Category:        "correctness",
		FixPriority:     bootsnapFixPriority,
	}
}

// Check runs the rule.
func (r *BootsnapPrecompileWithoutJ1Rule) Check(input rules.LintInput) []rules.Violation {
	meta := r.Metadata()

	// Context refinement: if Gemfile.lock is observable AND it does NOT list
	// bootsnap as a dependency, suppress the rule entirely. Some Dockerfiles
	// copy generic templates that include `bootsnap precompile` for projects
	// that have removed bootsnap from their Gemfile.
	if input.Facts != nil {
		if rf := input.Facts.RubyFacts(); rf != nil && rf.Lockfile != nil {
			if _, hasBootsnap := rf.Lockfile.Specs["bootsnap"]; !hasBootsnap {
				return nil
			}
		}
	}

	var violations []rules.Violation
	for stageIdx, stage := range input.Stages {
		if stagename.LooksLikeDev(stage.Name) {
			continue
		}

		var sf *facts.StageFacts
		if input.Facts != nil {
			sf = input.Facts.Stage(stageIdx)
		}
		if sf == nil {
			continue
		}
		if sf.BaseImageOS == semantic.BaseImageOSWindows {
			continue
		}

		// Ruby-namespaced rule: only fire on stages that look like a Ruby
		// runtime. A non-Ruby image running a tool called `bootsnap` for
		// unrelated reasons should not trip this Rails-flavored warning.
		if !stageLooksLikeRuby(input.Semantic, stageIdx, stage, sf) {
			continue
		}

		violations = append(violations, r.checkStage(input.File, sf, input.SourceMap(), meta)...)
	}
	return violations
}

func (r *BootsnapPrecompileWithoutJ1Rule) checkStage(
	file string,
	sf *facts.StageFacts,
	sm *sourcemap.SourceMap,
	meta rules.RuleMetadata,
) []rules.Violation {
	var violations []rules.Violation
	for _, runFacts := range sf.Runs {
		if runFacts == nil || runFacts.Run == nil {
			continue
		}
		script := runFacts.SourceScript
		if script == "" {
			continue
		}

		// Skip scripts that gate bootsnap on BUILDPLATFORM == TARGETPLATFORM.
		// The check only suppresses when both ARGs are present in the script
		// alongside the bootsnap call; either alone is not strong evidence.
		if scriptIsPlatformGuarded(script) {
			continue
		}

		matches := bootsnapPrecompileRE.FindAllStringIndex(script, -1)
		if len(matches) == 0 {
			continue
		}

		for _, match := range matches {
			matchStart, matchEnd := match[0], match[1]
			if invocationHasJobsOne(script, matchEnd) {
				continue
			}

			loc := bootsnapViolationLocation(file, runFacts, sm, matchStart, matchEnd)
			fix := buildBootsnapFix(file, runFacts, sm, matchEnd, meta.FixPriority)

			v := rules.NewViolation(loc, meta.Code, meta.Description, meta.DefaultSeverity).
				WithDocURL(meta.DocURL).
				WithDetail(
					"`bootsnap precompile` defaults to host CPU parallelism, which crashes under " +
						"QEMU multi-arch builds (see https://github.com/Shopify/bootsnap/issues/495). " +
						"The Rails generator template carries `-j 1` for exactly this reason.",
				)
			if fix != nil {
				v = v.WithSuggestedFix(fix)
			}
			violations = append(violations, v)
		}
	}
	return violations
}

// scriptIsPlatformGuarded reports whether the run script references both
// BUILDPLATFORM and TARGETPLATFORM, which is the canonical shape of a
// `[ "$BUILDPLATFORM" = "$TARGETPLATFORM" ]` (or equivalent) guard. This is
// a heuristic — the user may have explicitly opted out of emulated paths
// for the bootsnap call, in which case the rule should stand down.
func scriptIsPlatformGuarded(script string) bool {
	for _, lit := range bootsnapPlatformGuardLiterals {
		if !strings.Contains(script, lit) {
			return false
		}
	}
	return true
}

// invocationHasJobsOne reports whether the bootsnap precompile invocation
// starting at endIdx (the byte offset just past the `precompile` token)
// already carries a `-j 1`-equivalent flag in the same shell command. The
// search window stops at the first `&&`, `||`, `;`, `|`, or newline so a
// later command's `-j 1` does not accidentally suppress the violation.
func invocationHasJobsOne(script string, endIdx int) bool {
	if endIdx >= len(script) {
		return false
	}
	tail := script[endIdx:]
	stop := commandEndOffset(tail)
	if stop > 0 {
		tail = tail[:stop]
	}
	return jobsFlagRE.MatchString(tail)
}

// commandEndOffset returns the byte offset of the first command separator in
// the script tail (i.e. the end of the current shell command). Returns -1
// when no separator is found, in which case the whole tail is one command.
//
// Single `&` (background spawn) is intentionally NOT a separator: it forks
// the running command, but `-j 1` could still legitimately apply to the
// bootsnap call before it. Conservatively keep scanning past it.
func commandEndOffset(tail string) int {
	for i := range len(tail) {
		switch tail[i] {
		case '\n', ';':
			return i
		case '|':
			// Both `|` (pipe) and `||` (or-list) end the current command.
			return i
		case '&':
			if i+1 < len(tail) && tail[i+1] == '&' {
				return i
			}
		}
	}
	return -1
}

// bootsnapViolationLocation returns the source location of the
// `bootsnap precompile` token inside the offending RUN. The script is the
// reconstructed shell text relative to the RUN's first line (after the
// `RUN [<flags>]` prefix has been stripped), so a 0-line match must be
// adjusted by the Dockerfile-relative column offset of the run script.
func bootsnapViolationLocation(
	file string,
	runFacts *facts.RunFacts,
	sm *sourcemap.SourceMap,
	matchStart, matchEnd int,
) rules.Location {
	if runFacts == nil || runFacts.Run == nil {
		return rules.NewFileLocation(file)
	}
	runRanges := runFacts.Run.Location()
	if len(runRanges) == 0 {
		return rules.NewFileLocation(file)
	}
	runStartLine := runRanges[0].Start.Line

	startLineOff, startCol := sourcemap.ByteToLineCol(runFacts.SourceScript, matchStart)
	endLineOff, endCol := sourcemap.ByteToLineCol(runFacts.SourceScript, matchEnd)

	startLine := runStartLine + startLineOff
	endLine := runStartLine + endLineOff

	// First-line offsets are shell-relative; translate to Dockerfile columns
	// by adding the byte offset where the shell command begins on that line.
	if startLineOff == 0 && sm != nil {
		offset := runScriptStartColumn(sm, runStartLine)
		startCol += offset
	}
	if endLineOff == 0 && sm != nil {
		offset := runScriptStartColumn(sm, runStartLine)
		endCol += offset
	}
	return rules.NewRangeLocation(file, startLine, startCol, endLine, endCol)
}

// runScriptStartColumn returns the Dockerfile-relative byte column where the
// shell text begins on the RUN's first line, or 0 if the source map cannot
// resolve the line.
func runScriptStartColumn(sm *sourcemap.SourceMap, runStartLine int) int {
	line := sm.Line(runStartLine - 1)
	if line == "" {
		return 0
	}
	return shell.DockerfileRunCommandStartCol(line)
}

// buildBootsnapFix returns a narrow text-edit fix that inserts ` -j 1`
// directly after the `bootsnap precompile` token. FixSafe — the edit does
// not change the structure of the command, only the parallelism flag,
// which is the documented Rails-generator wording.
func buildBootsnapFix(
	file string,
	runFacts *facts.RunFacts,
	sm *sourcemap.SourceMap,
	matchEnd int,
	priority int,
) *rules.SuggestedFix {
	if runFacts == nil || runFacts.Run == nil || sm == nil {
		return nil
	}
	runRanges := runFacts.Run.Location()
	if len(runRanges) == 0 {
		return nil
	}
	runStartLine := runRanges[0].Start.Line

	endLineOff, endCol := sourcemap.ByteToLineCol(runFacts.SourceScript, matchEnd)
	insertLine := runStartLine + endLineOff
	insertCol := endCol
	if endLineOff == 0 {
		insertCol += runScriptStartColumn(sm, runStartLine)
	}
	return &rules.SuggestedFix{
		Description: "Insert `-j 1` after `bootsnap precompile` to avoid the QEMU multi-arch crash",
		Safety:      rules.FixSafe,
		Priority:    priority,
		IsPreferred: true,
		Edits: []rules.TextEdit{{
			Location: rules.NewRangeLocation(file, insertLine, insertCol, insertLine, insertCol),
			NewText:  " -j 1",
		}},
	}
}

func init() {
	rules.Register(NewBootsnapPrecompileWithoutJ1Rule())
}
