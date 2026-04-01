package hadolint

import (
	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/wharflab/tally/internal/dockerfile"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/shell"
)

// DL3027Rule implements the DL3027 linting rule.
type DL3027Rule struct{}

// NewDL3027Rule creates a new DL3027 rule instance.
func NewDL3027Rule() *DL3027Rule {
	return &DL3027Rule{}
}

// Metadata returns the rule metadata.
func (r *DL3027Rule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            rules.HadolintRulePrefix + "DL3027",
		Name:            "Do not use apt",
		Description:     "Do not use apt as it is meant to be an end-user tool, use apt-get or apt-cache instead",
		DocURL:          rules.HadolintDocURL("DL3027"),
		DefaultSeverity: rules.SeverityWarning,
		Category:        "style",
		IsExperimental:  false,
	}
}

const aptGetReplacement = "apt-get"

// aptCommandMapping maps apt subcommands to their replacement and safety level.
var aptCommandMapping = map[string]struct {
	replacement string
	safety      rules.FixSafety
}{
	// Safe: identical behavior in apt-get
	"install":    {aptGetReplacement, rules.FixSafe},
	"remove":     {aptGetReplacement, rules.FixSafe},
	"update":     {aptGetReplacement, rules.FixSafe},
	"upgrade":    {aptGetReplacement, rules.FixSafe},
	"autoremove": {aptGetReplacement, rules.FixSafe},
	"purge":      {aptGetReplacement, rules.FixSafe},
	"clean":      {aptGetReplacement, rules.FixSafe},
	"autoclean":  {aptGetReplacement, rules.FixSafe},

	// Suggestion: different command family (apt-cache), output may differ
	"search": {"apt-cache", rules.FixSuggestion},
	"show":   {"apt-cache", rules.FixSuggestion},
	"policy": {"apt-cache", rules.FixSuggestion},
}

// Check runs the DL3027 rule.
// It warns when any RUN instruction contains an apt command.
// Skips analysis for stages using non-POSIX shells (e.g., PowerShell).
func (r *DL3027Rule) Check(input rules.LintInput) []rules.Violation {
	meta := r.Metadata()
	sm := input.SourceMap()

	return ScanRunCommandsWithPOSIXShell(
		input,
		func(run *instructions.RunCommand, shellVariant shell.Variant, file string) []rules.Violation {
			runLoc := run.Location()

			var occurrences []shell.CommandOccurrence
			var runStartLine int

			if run.PrependShell {
				// Shell form: parse original source with "RUN " replaced by spaces
				// This preserves column positions for accurate edits on multi-line commands
				script, startLine := dockerfile.RunSourceScript(run, sm)
				if script == "" {
					return nil
				}
				runStartLine = startLine
				occurrences = shell.FindAllCommandOccurrences(script, "apt", shellVariant)
			} else {
				// Exec form: use collapsed command string (JSON array becomes shell-parseable)
				cmdStr := dockerfile.RunCommandString(run)
				occurrences = shell.FindAllCommandOccurrences(cmdStr, "apt", shellVariant)
				// No edits for exec form - positions don't map to source
			}

			if len(occurrences) == 0 {
				return nil
			}

			// Emit one violation per apt occurrence so that each fix is
			// independent.  This lets the fixer skip only the edits that
			// overlap with other rules (e.g., cache-mount cleanup deleting
			// "apt clean") without discarding fixes on unrelated lines.
			var violations []rules.Violation

			for _, occ := range occurrences {
				// Determine replacement suffix based on subcommand.
				suffix := "-get"
				safety := rules.FixSuggestion // Default for unknown subcommands
				if mapping, ok := aptCommandMapping[occ.Subcommand]; ok {
					// Derive the suffix from the replacement: "apt-get" → "-get", "apt-cache" → "-cache".
					suffix = mapping.replacement[len("apt"):]
					safety = mapping.safety
				}

				// Use the specific occurrence location so violations on the same
				// RUN but different lines survive deduplication.
				var occLoc rules.Location
				if run.PrependShell && runStartLine > 0 {
					occLine := runStartLine + occ.Line
					occLoc = rules.NewRangeLocation(file, occLine, occ.StartCol, occLine, occ.EndCol)
				} else {
					occLoc = rules.NewLocationFromRanges(file, runLoc)
				}

				v := rules.NewViolation(
					occLoc,
					meta.Code,
					"do not use apt as it is meant to be an end-user tool, use apt-get or apt-cache instead",
					meta.DefaultSeverity,
				).WithDocURL(meta.DocURL).WithDetail(
					"The apt command is designed for interactive use and has an unstable command-line interface. " +
						"For scripting and automation (like Dockerfiles), use apt-get for package management " +
						"or apt-cache for querying package information.",
				)

				// Only add edits for shell form RUN commands.
				if run.PrependShell {
					editLine := runStartLine + occ.Line
					editEndCol := occ.EndCol

					lineIdx := editLine - 1
					if lineIdx < 0 || lineIdx >= sm.LineCount() {
						violations = append(violations, v)
						continue
					}
					sourceLine := sm.Line(lineIdx)
					if occ.StartCol < 0 || editEndCol > len(sourceLine) ||
						sourceLine[occ.StartCol:editEndCol] != "apt" {
						violations = append(violations, v)
						continue
					}

					// Zero-width insertion right after "apt" to produce "apt-get" / "apt-cache".
					v = v.WithSuggestedFix(&rules.SuggestedFix{
						Description: "Replace 'apt' with 'apt" + suffix + "'",
						Safety:      safety,
						Edits: []rules.TextEdit{{
							Location: rules.NewRangeLocation(file, editLine, editEndCol, editLine, editEndCol),
							NewText:  suffix,
						}},
					})
				}

				violations = append(violations, v)
			}

			return violations
		},
	)
}

// init registers the rule with the default registry.
func init() {
	rules.Register(NewDL3027Rule())
}
