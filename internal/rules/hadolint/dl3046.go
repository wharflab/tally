package hadolint

import (
	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/wharflab/tally/internal/dockerfile"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/shell"
)

// DL3046Rule implements the DL3046 linting rule.
// It warns when useradd is used with a high UID (>99999) without the -l flag,
// which causes excessively large images due to lastlog/faillog entries.
type DL3046Rule struct{}

// NewDL3046Rule creates a new DL3046 rule instance.
func NewDL3046Rule() *DL3046Rule {
	return &DL3046Rule{}
}

// Metadata returns the rule metadata.
func (r *DL3046Rule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            rules.HadolintRulePrefix + "DL3046",
		Name:            "useradd without -l and high UID",
		Description:     "`useradd` without flag `-l` and high UID will result in excessively large Image",
		DocURL:          rules.HadolintDocURL("DL3046"),
		DefaultSeverity: rules.SeverityWarning,
		Category:        "performance",
		IsExperimental:  false,
	}
}

// Check runs the DL3046 rule.
// It warns when any RUN instruction contains useradd with a high UID but without the -l flag.
func (r *DL3046Rule) Check(input rules.LintInput) []rules.Violation {
	meta := r.Metadata()

	return ScanRunCommandsWithPOSIXShell(
		input,
		func(run *instructions.RunCommand, shellVariant shell.Variant, file string) []rules.Violation {
			return r.checkUseraddCommands(run, shellVariant, file, meta, input)
		},
	)
}

// checkUseraddCommands inspects a RUN instruction for useradd commands with high UIDs missing -l flag.
func (r *DL3046Rule) checkUseraddCommands(
	run *instructions.RunCommand,
	shellVariant shell.Variant,
	file string,
	meta rules.RuleMetadata,
	input rules.LintInput,
) []rules.Violation {
	cmdStr := dockerfile.RunCommandString(run)
	useraddCmds := shell.FindCommands(cmdStr, shellVariant, "useradd")
	if len(useraddCmds) == 0 {
		return nil
	}

	var results []rules.Violation
	for i := range useraddCmds {
		if !isHighUIDWithoutNoLogInit(&useraddCmds[i]) {
			continue
		}

		violation := r.createViolation(run, file, meta)
		if fix := r.createAutoFix(run, file, input); fix != nil {
			violation = violation.WithSuggestedFix(fix)
		}
		results = append(results, violation)
	}
	return results
}

// isHighUIDWithoutNoLogInit checks if a useradd command has a high UID (>99999) without -l flag.
func isHighUIDWithoutNoLogInit(cmd *shell.CommandInfo) bool {
	// Already has the flag that prevents the issue
	if cmd.HasAnyFlag("-l", "--no-log-init") {
		return false
	}
	// No UID specified, so no issue
	if !cmd.HasAnyFlag("-u", "--uid") {
		return false
	}
	// Get UID value - check both short and long flag forms
	uidValue := cmd.GetArgValue("-u")
	if uidValue == "" {
		uidValue = cmd.GetArgValue("--uid")
	}
	// High UID = more than 5 digits (>99999)
	return len(uidValue) > 5
}

// createViolation builds the violation for a problematic useradd command.
func (r *DL3046Rule) createViolation(run *instructions.RunCommand, file string, meta rules.RuleMetadata) rules.Violation {
	return rules.NewViolation(
		rules.NewLocationFromRanges(file, run.Location()),
		meta.Code,
		"`useradd` without flag `-l` and high UID will result in excessively large Image",
		meta.DefaultSeverity,
	).WithDocURL(meta.DocURL).WithDetail(
		"When useradd creates a user with a high UID (>99999), it writes entries to " +
			"/var/log/lastlog and /var/log/faillog. These files are sparse but can bloat " +
			"the image layer. Use the -l (--no-log-init) flag to skip these log entries.",
	)
}

// createAutoFix generates a fix that inserts -l flag after useradd (shell form only).
func (r *DL3046Rule) createAutoFix(
	run *instructions.RunCommand,
	file string,
	input rules.LintInput,
) *rules.SuggestedFix {
	if !run.PrependShell {
		return nil
	}

	sm := input.SourceMap()
	sourceScript, scriptStartLine := getRunSourceScript(run, sm)
	if sourceScript == "" {
		return nil
	}

	// Recalculate command position from original source for accurate fix placement
	useraddCmds := shell.FindCommands(sourceScript, shell.VariantBash, "useradd")
	if len(useraddCmds) == 0 {
		return nil
	}

	// Find matching command by checking if it has same problematic pattern
	for i := range useraddCmds {
		if !isHighUIDWithoutNoLogInit(&useraddCmds[i]) {
			continue
		}
		targetLine := scriptStartLine + useraddCmds[i].Line
		insertCol := useraddCmds[i].EndCol

		lineIdx := targetLine - 1
		if lineIdx < 0 || lineIdx >= sm.LineCount() {
			continue
		}
		sourceLine := sm.Line(lineIdx)
		if insertCol < 0 || insertCol > len(sourceLine) {
			continue
		}

		return &rules.SuggestedFix{
			Description: "Add -l flag to useradd",
			Safety:      rules.FixSafe,
			Edits: []rules.TextEdit{{
				Location: rules.NewRangeLocation(file, targetLine, insertCol, targetLine, insertCol),
				NewText:  " -l",
			}},
		}
	}
	return nil
}

// init registers the rule with the default registry.
func init() {
	rules.Register(NewDL3046Rule())
}
