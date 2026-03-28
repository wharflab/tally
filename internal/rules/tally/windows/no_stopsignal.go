package windows

import (
	"bytes"
	"fmt"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/wharflab/tally/internal/rules"
)

// NoStopsignalRuleCode is the full rule code for tally/windows/no-stopsignal.
const NoStopsignalRuleCode = rules.TallyRulePrefix + "windows/no-stopsignal"

// NoStopsignalRule flags STOPSIGNAL instructions in Windows stages.
// Windows containers do not support POSIX signals, so STOPSIGNAL has no effect.
// BuildKit silently accepts the instruction (the platform check is dead code),
// but the resulting image config entry is meaningless at runtime.
type NoStopsignalRule struct{}

// NewNoStopsignalRule creates a new rule instance.
func NewNoStopsignalRule() *NoStopsignalRule { return &NoStopsignalRule{} }

// Metadata returns the rule metadata.
func (r *NoStopsignalRule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            NoStopsignalRuleCode,
		Name:            "No STOPSIGNAL on Windows",
		Description:     "STOPSIGNAL has no effect on Windows containers because they do not support POSIX signals",
		DocURL:          rules.TallyDocURL(NoStopsignalRuleCode),
		DefaultSeverity: rules.SeverityWarning,
		Category:        "correctness",
	}
}

// Check runs the rule against the given input.
func (r *NoStopsignalRule) Check(input rules.LintInput) []rules.Violation {
	stages := windowsStages(input)
	if len(stages) == 0 {
		return nil
	}

	meta := r.Metadata()
	var violations []rules.Violation

	for _, info := range stages {
		if info.Stage == nil {
			continue
		}
		for _, cmd := range info.Stage.Commands {
			stopSig, ok := cmd.(*instructions.StopSignalCommand)
			if !ok {
				continue
			}

			loc := rules.NewLocationFromRanges(input.File, stopSig.Location())
			if loc.IsFileLevel() {
				continue
			}

			msg := fmt.Sprintf(
				"STOPSIGNAL %s has no effect on Windows containers",
				stopSig.Signal,
			)
			detail := "Windows containers do not support POSIX signals. " +
				"BuildKit silently accepts STOPSIGNAL but the instruction is ignored at runtime."

			v := rules.NewViolation(loc, meta.Code, msg, meta.DefaultSeverity).
				WithDocURL(meta.DocURL).
				WithDetail(detail)
			v.StageIndex = info.Index

			if fixes := buildFixes(input.File, input.Source, stopSig); len(fixes) > 0 {
				v = v.WithSuggestedFixes(fixes)
			}

			violations = append(violations, v)
		}
	}

	return violations
}

// buildFixes returns alternative fixes for a STOPSIGNAL instruction on Windows:
//  1. Comment out the line (safe, preferred) — preserves the original as a comment.
//  2. Delete the line (suggestion) — removes the instruction entirely.
func buildFixes(file string, source []byte, cmd *instructions.StopSignalCommand) []*rules.SuggestedFix {
	locs := cmd.Location()
	if len(locs) == 0 {
		return nil
	}

	lineIdx := locs[0].Start.Line - 1 // 0-based
	lines := bytes.Split(source, []byte("\n"))
	if lineIdx < 0 || lineIdx >= len(lines) {
		return nil
	}

	line := string(lines[lineIdx])
	if line == "" {
		return nil
	}

	editLoc := rules.NewRangeLocation(file, locs[0].Start.Line, 0, locs[0].Start.Line, len(line))
	commentedLine := "# [commented out by tally - STOPSIGNAL has no effect on Windows containers]: " + line
	deleteLoc := rules.DeleteLineLocation(file, locs[0].Start.Line, len(line), len(lines))

	return []*rules.SuggestedFix{
		{
			Description: "Comment out STOPSIGNAL (has no effect on Windows)",
			Safety:      rules.FixSafe,
			Priority:    -1, // Must apply before cosmetic fixes on the same line.
			IsPreferred: true,
			Edits:       []rules.TextEdit{{Location: editLoc, NewText: commentedLine}},
		},
		{
			Description: "Delete STOPSIGNAL instruction",
			Safety:      rules.FixSuggestion,
			Priority:    -1,
			Edits:       []rules.TextEdit{{Location: deleteLoc, NewText: ""}},
		},
	}
}

func init() {
	rules.Register(NewNoStopsignalRule())
}
