package tally

import (
	"fmt"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"
	"github.com/moby/buildkit/frontend/dockerfile/parser"

	"github.com/wharflab/tally/internal/rules"
)

// PreferSystemdSigrtminPlus3RuleCode is the full rule code.
const PreferSystemdSigrtminPlus3RuleCode = rules.TallyRulePrefix + "prefer-systemd-sigrtmin-plus-3"

// PreferSystemdSigrtminPlus3Rule detects stages where PID 1 is clearly
// systemd/init but STOPSIGNAL is missing or not set to SIGRTMIN+3.
//
// systemd requires SIGRTMIN+3 to trigger a clean manager shutdown (analogous
// to "systemctl halt"). Without it the container runtime sends SIGTERM (signal
// 15), which systemd interprets as an isolate-to-rescue-mode request — not a
// clean shutdown.
//
// Cross-rule interaction:
//
//   - tally/prefer-canonical-stopsignal handles RTMIN+3 → SIGRTMIN+3. This
//     rule checks the normalized value, so RTMIN+3 is treated as correct.
//   - tally/no-ungraceful-stopsignal may fire on the same STOPSIGNAL (e.g.
//     SIGKILL on a systemd stage). This rule's fix uses Priority -1 to claim
//     the signal edit range first, replacing with SIGRTMIN+3 instead of
//     SIGTERM.
type PreferSystemdSigrtminPlus3Rule struct{}

// NewPreferSystemdSigrtminPlus3Rule creates a new rule instance.
func NewPreferSystemdSigrtminPlus3Rule() *PreferSystemdSigrtminPlus3Rule {
	return &PreferSystemdSigrtminPlus3Rule{}
}

// Metadata returns the rule metadata.
func (r *PreferSystemdSigrtminPlus3Rule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            PreferSystemdSigrtminPlus3RuleCode,
		Name:            "Prefer SIGRTMIN+3 for systemd/init",
		Description:     "systemd/init containers should use STOPSIGNAL SIGRTMIN+3 for clean shutdown",
		DocURL:          rules.TallyDocURL(PreferSystemdSigrtminPlus3RuleCode),
		DefaultSeverity: rules.SeverityWarning,
		Category:        "correctness",
	}
}

// Check runs the prefer-systemd-sigrtmin-plus-3 rule.
//
// For each stage it determines whether PID 1 is a systemd/init binary (exec
// form only — shell form hides the real PID 1). If so, it verifies that
// STOPSIGNAL is present and set to SIGRTMIN+3.
//
// Windows stages are skipped because STOPSIGNAL has no effect on Windows
// containers.
func (r *PreferSystemdSigrtminPlus3Rule) Check(input rules.LintInput) []rules.Violation {
	meta := r.Metadata()
	sem := input.Semantic
	var violations []rules.Violation

	for stageIdx, stage := range input.Stages {
		if sem != nil {
			if info := sem.StageInfo(stageIdx); info != nil && info.IsWindows() {
				continue
			}
		}

		executable := stageRuntimeExecutable(stage)
		if executable == "" || !isSystemdInit(executable) {
			continue
		}

		// Find the last STOPSIGNAL instruction in this stage.
		var lastStopSig *instructions.StopSignalCommand
		for _, cmd := range stage.Commands {
			if ss, ok := cmd.(*instructions.StopSignalCommand); ok {
				lastStopSig = ss
			}
		}

		if lastStopSig == nil {
			violations = append(violations, r.buildMissingViolation(meta, input, stage))
			continue
		}

		raw := lastStopSig.Signal
		if strings.Contains(raw, "$") {
			continue
		}

		normalized := normalizeSignalName(raw)
		if normalized == signalSIGRTMINPlus3 {
			continue
		}

		violations = append(violations, r.buildWrongSignalViolation(meta, input, lastStopSig, normalized))
	}

	return violations
}

// buildWrongSignalViolation creates a violation for a STOPSIGNAL that is
// present but set to the wrong value.
func (r *PreferSystemdSigrtminPlus3Rule) buildWrongSignalViolation(
	meta rules.RuleMetadata,
	input rules.LintInput,
	cmd *instructions.StopSignalCommand,
	normalized string,
) rules.Violation {
	loc := rules.NewLocationFromRanges(input.File, cmd.Location())

	msg := fmt.Sprintf(
		"STOPSIGNAL %s should be SIGRTMIN+3 for systemd/init containers",
		normalized,
	)

	v := rules.NewViolation(loc, meta.Code, msg, meta.DefaultSeverity).
		WithDocURL(meta.DocURL).
		WithDetail("systemd requires SIGRTMIN+3 to trigger a clean manager shutdown, analogous to systemctl halt")

	if editLoc := signalEditLocation(input.File, input.Source, cmd); editLoc != nil {
		v = v.WithSuggestedFix(&rules.SuggestedFix{
			Description: "Replace with SIGRTMIN+3 for clean systemd shutdown",
			Safety:      rules.FixSafe,
			Priority:    -1,
			IsPreferred: true,
			Edits: []rules.TextEdit{
				{Location: *editLoc, NewText: signalSIGRTMINPlus3},
			},
		})
	}

	return v
}

// buildMissingViolation creates a violation when no STOPSIGNAL is present in a
// systemd/init stage. The fix inserts STOPSIGNAL SIGRTMIN+3 before the
// ENTRYPOINT or CMD that defines the init process.
func (r *PreferSystemdSigrtminPlus3Rule) buildMissingViolation(
	meta rules.RuleMetadata,
	input rules.LintInput,
	stage instructions.Stage,
) rules.Violation {
	// Find the last ENTRYPOINT or CMD to determine the insertion point
	// and the violation location. Track them separately so ENTRYPOINT
	// always takes precedence, matching stageRuntimeExecutable semantics.
	var lastEntrypointLoc, lastCmdLoc []parser.Range
	for _, cmd := range stage.Commands {
		switch c := cmd.(type) {
		case *instructions.EntrypointCommand:
			lastEntrypointLoc = c.Location()
		case *instructions.CmdCommand:
			lastCmdLoc = c.Location()
		}
	}

	var runtimeLoc []parser.Range
	if lastEntrypointLoc != nil {
		runtimeLoc = lastEntrypointLoc
	} else {
		runtimeLoc = lastCmdLoc
	}

	var loc rules.Location
	if len(runtimeLoc) > 0 {
		loc = rules.NewLocationFromRanges(input.File, runtimeLoc)
	} else {
		loc = rules.NewLocationFromRanges(input.File, stage.Location)
	}

	v := rules.NewViolation(loc, meta.Code,
		"systemd/init container is missing STOPSIGNAL SIGRTMIN+3",
		meta.DefaultSeverity,
	).
		WithDocURL(meta.DocURL).
		WithDetail(
			"Without STOPSIGNAL SIGRTMIN+3, the container runtime sends SIGTERM" +
				" which systemd interprets as an isolate-to-rescue-mode request, not a clean shutdown",
		)

	// Insert STOPSIGNAL before the runtime instruction.
	if len(runtimeLoc) > 0 {
		insertLine := runtimeLoc[0].Start.Line
		v = v.WithSuggestedFix(&rules.SuggestedFix{
			Description: "Add STOPSIGNAL SIGRTMIN+3 for clean systemd shutdown",
			Safety:      rules.FixSafe,
			Priority:    -1,
			IsPreferred: true,
			Edits: []rules.TextEdit{
				{
					Location: rules.NewRangeLocation(input.File, insertLine, 0, insertLine, 0),
					NewText:  "# [tally] SIGRTMIN+3 is the graceful shutdown signal for systemd/init\nSTOPSIGNAL SIGRTMIN+3\n",
				},
			},
		})
	}

	return v
}

func init() {
	rules.Register(NewPreferSystemdSigrtminPlus3Rule())
}
