package hadolint

import (
	"slices"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/wharflab/tally/internal/dockerfile"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/shell"
)

// DL3047Rule implements the DL3047 linting rule.
// It warns when wget is used without a progress indicator flag,
// which causes excessively bloated build logs for large downloads.
//
// Cross-rule interactions:
//   - tally/prefer-add-unpack: also matches wget/curl download-then-extract
//     patterns and suggests using ADD instead. DL3047 is complementary — it
//     only advises on progress-bar usage and does not suggest replacing wget.
//   - hadolint/DL4001: warns when both wget and curl are present in the same
//     image. DL3047 fires independently per wget invocation and its auto-fix
//     (--progress=dot:giga) does not affect DL4001 findings.
type DL3047Rule struct{}

// NewDL3047Rule creates a new DL3047 rule instance.
func NewDL3047Rule() *DL3047Rule {
	return &DL3047Rule{}
}

// Metadata returns the rule metadata.
func (r *DL3047Rule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            rules.HadolintRulePrefix + "DL3047",
		Name:            "Avoid wget without progress bar",
		Description:     "Avoid use of wget without progress bar. Use `wget --progress=dot:giga <url>` or consider using `-q` or `-nv`",
		DocURL:          "https://github.com/hadolint/hadolint/wiki/DL3047",
		DefaultSeverity: rules.SeverityInfo,
		Category:        "best-practice",
		IsExperimental:  false,
		// FixPriority 96 ensures prefer-add-unpack (priority 95) applies first.
		// When wget|tar is replaced by ADD --unpack, the progress-bar fix becomes
		// moot and is harmlessly skipped. For standalone wget the fix still applies.
		FixPriority: 96,
	}
}

// wgetHasProgressSuppression checks whether a wget command already has flags
// that either control progress output or suppress it entirely.
func wgetHasProgressSuppression(cmd *shell.CommandInfo) bool {
	// --progress controls the progress indicator directly
	if cmd.HasFlag("--progress") {
		return true
	}

	// Quiet flags suppress all output including progress
	if cmd.HasAnyFlag("-q", "--quiet") {
		return true
	}

	// Output file flags redirect log output, so progress doesn't bloat build logs
	if cmd.HasAnyFlag("-o", "--output-file", "-a", "--append-output") {
		return true
	}

	// --no-verbose / -nv suppress progress output
	if cmd.HasFlag("--no-verbose") {
		return true
	}
	// -nv is a special combined short form that means --no-verbose
	// It's not a combination of -n and -v flags, so check it as a literal arg
	if slices.Contains(cmd.Args, "-nv") {
		return true
	}

	return false
}

// Check runs the DL3047 rule.
// It warns when any RUN instruction contains wget without progress-related flags.
func (r *DL3047Rule) Check(input rules.LintInput) []rules.Violation {
	meta := r.Metadata()
	sm := input.SourceMap()

	return ScanRunCommandsWithPOSIXShell(
		input,
		func(run *instructions.RunCommand, shellVariant shell.Variant, file string) []rules.Violation {
			var cmds []shell.CommandInfo
			var runStartLine int

			if run.PrependShell {
				// Shell form: parse original source preserving column positions.
				script, startLine := getRunSourceScript(run, sm)
				if script == "" {
					return nil
				}
				runStartLine = startLine
				cmds = shell.FindCommands(script, shellVariant, "wget")
			} else {
				// Exec form: reconstruct a shell string from the JSON array.
				// NOTE: Upstream Hadolint DL3047 skips exec-form because it only
				// parses shell AST, which exec-form lacks. We intentionally extend
				// coverage to exec-form since RUN ["wget", ...] still produces
				// bloated logs. No auto-fix is offered (positions don't map to source).
				cmdStr := dockerfile.RunCommandString(run)
				cmds = shell.FindCommands(cmdStr, shellVariant, "wget")
			}

			var violations []rules.Violation
			for _, cmd := range cmds {
				if wgetHasProgressSuppression(&cmd) {
					continue
				}

				loc := rules.NewLocationFromRanges(file, run.Location())
				v := rules.NewViolation(
					loc,
					meta.Code,
					"wget without progress bar will bloat build logs; use `wget --progress=dot:giga`, `-q`, or `-nv`",
					meta.DefaultSeverity,
				).WithDocURL(meta.DocURL).WithDetail(
					"When downloading large files, wget's default progress output produces " +
						"excessive log lines in Docker builds. Use --progress=dot:giga for a " +
						"compact progress indicator, or -q/--quiet/-nv/--no-verbose to suppress output entirely.",
				)

				// Add auto-fix: insert --progress=dot:giga after "wget"
				if run.PrependShell {
					editLine := runStartLine + cmd.Line
					insertCol := cmd.EndCol // Right after "wget"

					lineIdx := editLine - 1
					if lineIdx >= 0 && lineIdx < sm.LineCount() {
						sourceLine := sm.Line(lineIdx)
						if insertCol >= 0 && insertCol <= len(sourceLine) {
							v = v.WithSuggestedFix(&rules.SuggestedFix{
								Description: "Add --progress=dot:giga to wget",
								Safety:      rules.FixSafe,
								Priority:    meta.FixPriority,
								Edits: []rules.TextEdit{{
									Location: rules.NewRangeLocation(file, editLine, insertCol, editLine, insertCol),
									NewText:  " --progress=dot:giga",
								}},
							})
						}
					}
				}

				violations = append(violations, v)
			}

			return violations
		},
	)
}

// init registers the rule with the default registry.
func init() {
	rules.Register(NewDL3047Rule())
}
