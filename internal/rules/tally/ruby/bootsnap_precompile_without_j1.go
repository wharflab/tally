package ruby

import (
	"regexp"

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
		// Exec-form RUN (`RUN ["foo", "bar"]`) is not a shell script, so the
		// SourceScript-relative offsets we use to compute the fix position
		// would be wrong. The exec form also does not invoke a shell at all,
		// so a literal `bootsnap precompile` argv is a niche edge case
		// (typically users wrap with `sh -c`). Skip cleanly.
		if !runFacts.UsesShell {
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

// scriptIsPlatformGuarded reports whether the run script gates the bootsnap
// call behind a `BUILDPLATFORM == TARGETPLATFORM`-style check. The canonical
// shapes we recognize:
//
//	if [ "$BUILDPLATFORM" = "$TARGETPLATFORM" ]; then ... bootsnap precompile ...
//	test "$BUILDPLATFORM" = "$TARGETPLATFORM" && ... bootsnap precompile ...
//	[ "$BUILDPLATFORM" = "$TARGETPLATFORM" ] && bootsnap precompile ...
//
// The check requires a comparison of the two ARGs (either `=` or `==`) on
// the same line; mere references to the variables alone are not sufficient.
// This is heuristic — false negatives (real guards we miss) are cheaper than
// false positives (suppressing a legitimate violation).
func scriptIsPlatformGuarded(script string) bool {
	return platformGuardRE.MatchString(script)
}

// platformGuardRE matches a `BUILDPLATFORM`/`TARGETPLATFORM` comparison in
// either direction, regardless of `$VAR` vs `${VAR}` form, with optional
// quotes around either side. The two variables must appear in the same
// comparison expression — otherwise the check is too loose. Recognized
// comparison operators: `=`, `==`, `!=`.
var platformGuardRE = regexp.MustCompile(
	`(?:` +
		`\$\{?BUILDPLATFORM\}?"?\s*(?:!=|==?)\s*"?\$\{?TARGETPLATFORM` +
		`|` +
		`\$\{?TARGETPLATFORM\}?"?\s*(?:!=|==?)\s*"?\$\{?BUILDPLATFORM` +
		`)`,
)

// invocationHasJobsOne reports whether the bootsnap precompile invocation
// starting at endIdx (the byte offset just past the `precompile` token)
// already carries a `-j 1`-equivalent flag in the same shell command. The
// search window stops at the first command separator (`&`, `&&`, `||`, `;`,
// `|`, or unescaped newline) so a later command's `-j 1` does not
// accidentally suppress the violation.
//
// The scanner respects shell quoting (single, double, backtick) and
// backslash escapes so a `;` or `&` inside `"..."`/`'...'` does not end the
// command. It does not parse heredocs, parameter expansion, or
// command substitution; those edge cases are rare in
// `RUN bundle exec bootsnap precompile -j 1` shapes.
func invocationHasJobsOne(script string, endIdx int) bool {
	if endIdx >= len(script) {
		return false
	}
	tail := script[endIdx:]
	stop := commandEndOffset(tail)
	if stop >= 0 {
		tail = tail[:stop]
	}
	return jobsFlagRE.MatchString(tail)
}

// commandEndOffset returns the byte offset of the first command separator in
// the script tail (i.e. the end of the current shell command). Returns -1
// when no separator is found, in which case the whole tail is one command.
// A separator at offset 0 returns 0 (caller's `stop >= 0` check handles the
// empty-window case correctly).
//
// Quoted strings (single, double, backtick) and backslash escapes are
// respected so that `echo ";"` and `echo "&&"` do not terminate the command.
// Backslash followed by newline is the Dockerfile-style line continuation
// and does NOT terminate the command. A bare newline does.
//
// Recognized terminators:
//   - `;`            — sequence
//   - `|`            — pipe (also handles `||`)
//   - `&` / `&&`     — background spawn / and-list (both end the current
//     command for our purposes; `-j 1` cannot apply across either)
//   - unescaped `\n` — newline-as-separator
func commandEndOffset(tail string) int {
	const (
		stateNormal = iota
		stateSingleQuote
		stateDoubleQuote
		stateBacktick
	)
	state := stateNormal
	for i := 0; i < len(tail); i++ {
		c := tail[i]
		switch state {
		case stateSingleQuote:
			// Single-quoted strings: only `'` ends the quote, no escapes.
			if c == '\'' {
				state = stateNormal
			}
			continue
		case stateDoubleQuote:
			if c == '\\' && i+1 < len(tail) {
				i++ // skip the escaped character
				continue
			}
			if c == '"' {
				state = stateNormal
			}
			continue
		case stateBacktick:
			if c == '\\' && i+1 < len(tail) {
				i++
				continue
			}
			if c == '`' {
				state = stateNormal
			}
			continue
		}
		// Normal state.
		switch c {
		case '\\':
			// Backslash-newline is a line continuation, NOT a separator.
			// Any other escaped char is consumed.
			if i+1 < len(tail) {
				i++
			}
		case '\'':
			state = stateSingleQuote
		case '"':
			state = stateDoubleQuote
		case '`':
			state = stateBacktick
		case '\n', ';':
			return i
		case '|':
			return i
		case '&':
			return i
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
