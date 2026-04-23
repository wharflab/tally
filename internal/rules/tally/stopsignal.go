package tally

import (
	"bytes"
	"path"
	"strconv"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/command"
	"github.com/moby/buildkit/frontend/dockerfile/instructions"
	"github.com/moby/buildkit/frontend/dockerfile/parser"

	"github.com/wharflab/tally/internal/rules"
)

// Canonical signal name constants used by STOPSIGNAL rules for normalization,
// detection, and fix replacement text.
const (
	signalSIGHUP        = "SIGHUP"
	signalSIGINT        = "SIGINT"
	signalSIGQUIT       = "SIGQUIT"
	signalSIGKILL       = "SIGKILL"
	signalSIGTERM       = "SIGTERM"
	signalSIGSTOP       = "SIGSTOP"
	signalSIGWINCH      = "SIGWINCH"
	signalSIGRTMINPlus3 = "SIGRTMIN+3"
)

// numericSignals maps well-known numeric signal values to their canonical names.
// These values are stable on amd64 and arm64; other architectures may differ.
// Includes both ungraceful signals (used for detection) and common graceful
// signals (for consistent normalization in messages and future rules).
var numericSignals = map[int]string{
	1:  signalSIGHUP,
	2:  signalSIGINT,
	3:  signalSIGQUIT,
	9:  signalSIGKILL,
	15: signalSIGTERM,
	19: signalSIGSTOP,
	28: signalSIGWINCH,
}

// stopsignalVisit holds a STOPSIGNAL instruction with its raw and normalized values,
// ready for rule-specific evaluation.
type stopsignalVisit struct {
	cmd        *instructions.StopSignalCommand
	raw        string // original signal token
	normalized string // canonical signal name
}

// visitStopsignals iterates all STOPSIGNAL instructions across stages, skipping
// Windows stages and environment variable references. For each valid instruction,
// it calls fn with the visit context.
func visitStopsignals(input rules.LintInput, fn func(v stopsignalVisit)) {
	var sem = input.Semantic

	for stageIdx, stage := range input.Stages {
		if sem != nil {
			if info := sem.StageInfo(stageIdx); info != nil && info.IsWindows() {
				continue
			}
		}

		for _, cmd := range stage.Commands {
			stopSig, ok := cmd.(*instructions.StopSignalCommand)
			if !ok {
				continue
			}

			raw := stopSig.Signal
			if strings.Contains(raw, "$") {
				continue
			}

			fn(stopsignalVisit{
				cmd:        stopSig,
				raw:        raw,
				normalized: normalizeSignalName(raw),
			})
		}
	}
}

// signalEditLocation returns the Location covering the signal token on a
// STOPSIGNAL source line. Returns nil if the position cannot be determined
// or if the instruction spans multiple physical lines.
func signalEditLocation(file string, source []byte, cmd *instructions.StopSignalCommand) *rules.Location {
	locs := cmd.Location()
	if len(locs) == 0 {
		return nil
	}

	// Reject multi-line STOPSIGNAL spans — the column range calculation
	// assumes a single physical line.
	if locs[0].End.Line != locs[0].Start.Line {
		return nil
	}

	lineIdx := locs[0].Start.Line - 1 // 0-based
	lines := bytes.Split(source, []byte("\n"))
	if lineIdx < 0 || lineIdx >= len(lines) {
		return nil
	}

	line := string(lines[lineIdx])

	startCol, endCol := signalColumnRange(line)
	if startCol < 0 {
		return nil
	}

	loc := rules.NewRangeLocation(file, locs[0].Start.Line, startCol, locs[0].Start.Line, endCol)
	return &loc
}

// normalizeSignalName normalizes a raw STOPSIGNAL token to its canonical form.
//
// Normalization steps:
//  1. Strip surrounding double quotes ("SIGKILL" -> SIGKILL)
//  2. Convert numeric values to signal names (9 -> SIGKILL)
//  3. Add SIG prefix if missing (KILL -> SIGKILL)
//  4. Uppercase
func normalizeSignalName(raw string) string {
	s := strings.TrimSpace(raw)

	// Strip surrounding quotes.
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		s = s[1 : len(s)-1]
	}

	s = strings.TrimSpace(s)
	s = strings.ToUpper(s)

	// Try numeric conversion.
	if num, err := strconv.Atoi(s); err == nil {
		if name, ok := numericSignals[num]; ok {
			return name
		}
		// Unknown numeric signal — return as-is.
		return s
	}

	// Add SIG prefix if missing and not already present.
	if !strings.HasPrefix(s, "SIG") {
		s = "SIG" + s
	}

	return s
}

// signalColumnRange finds the 0-based [start, end) column range of the signal
// token in a STOPSIGNAL source line such as "STOPSIGNAL SIGKILL".
// Returns (-1, -1) if not found.
func signalColumnRange(line string) (int, int) {
	upper := strings.ToUpper(line)
	prefix := strings.ToUpper(command.StopSignal)

	idx := strings.Index(upper, prefix)
	if idx < 0 {
		return -1, -1
	}

	// Scan past "STOPSIGNAL" and any whitespace.
	i := idx + len(prefix)
	for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
		i++
	}

	// The remaining text up to the end of the line (trimmed) is the signal token.
	end := len(strings.TrimRight(line, " \t\r"))
	if i >= end {
		return -1, -1
	}

	return i, end
}

// systemdInitPaths lists absolute paths recognized as systemd/init PID 1 binaries.
var systemdInitPaths = map[string]bool{
	"/sbin/init":               true,
	"/usr/sbin/init":           true,
	"/lib/systemd/systemd":     true,
	"/usr/lib/systemd/systemd": true,
}

// isSystemdInit returns true if the executable path matches a known
// systemd/init binary (full path match or bare "systemd" name).
func isSystemdInit(executable string) bool {
	if systemdInitPaths[executable] {
		return true
	}
	return path.Base(executable) == "systemd"
}

// isNginxOrOpenResty returns true if the executable's base name is "nginx"
// or "openresty". Matching on the base name covers absolute paths such as
// /usr/sbin/nginx and /usr/local/openresty/nginx/sbin/nginx.
func isNginxOrOpenResty(executable string) bool {
	base := path.Base(executable)
	return base == "nginx" || base == "openresty"
}

// daemonStopsignalRule describes a daemon-specific STOPSIGNAL rule so that the
// generic check/fix scaffolding can be shared. It captures the behavior that
// varies between daemons: how to recognize the PID 1 binary, what the target
// signal is, and how to build the violation messages and fixes.
type daemonStopsignalRule struct {
	meta           rules.RuleMetadata
	isDaemon       func(executable string) bool
	targetSignal   string
	wrongFix       daemonStopsignalFixSpec
	missingFix     daemonStopsignalFixSpec
	missingMessage string
	missingDetail  string
	wrongDetail    string
	wrongMessage   func(normalized string) string
	insertText     string
}

// daemonStopsignalFixSpec captures the fix-kind metadata (safety level plus
// description) for one of a daemon rule's two fix paths.
type daemonStopsignalFixSpec struct {
	Description string
	Safety      rules.FixSafety
}

// checkDaemonStopsignal runs the shared STOPSIGNAL check loop for a
// daemon-specific rule. It iterates stages, identifies the PID 1 binary via
// stageRuntimeExecutable, and delegates to the daemon-specific predicates and
// templates for message/fix construction.
func checkDaemonStopsignal(input rules.LintInput, spec daemonStopsignalRule) []rules.Violation {
	sem := input.Semantic
	var violations []rules.Violation

	for stageIdx, stage := range input.Stages {
		if sem != nil {
			if info := sem.StageInfo(stageIdx); info != nil && info.IsWindows() {
				continue
			}
		}

		executable := stageRuntimeExecutable(stage)
		if executable == "" || !spec.isDaemon(executable) {
			continue
		}

		var lastStopSig *instructions.StopSignalCommand
		for _, cmd := range stage.Commands {
			if ss, ok := cmd.(*instructions.StopSignalCommand); ok {
				lastStopSig = ss
			}
		}

		if lastStopSig == nil {
			violations = append(violations, buildMissingStopsignalViolation(input, spec, stage))
			continue
		}

		raw := lastStopSig.Signal
		if strings.Contains(raw, "$") {
			continue
		}

		normalized := normalizeSignalName(raw)
		if normalized == spec.targetSignal {
			continue
		}

		violations = append(violations, buildWrongStopsignalViolation(input, spec, lastStopSig, normalized))
	}

	return violations
}

// buildWrongStopsignalViolation emits a violation for a STOPSIGNAL set to a
// signal other than the daemon's preferred value.
func buildWrongStopsignalViolation(
	input rules.LintInput,
	spec daemonStopsignalRule,
	cmd *instructions.StopSignalCommand,
	normalized string,
) rules.Violation {
	loc := rules.NewLocationFromRanges(input.File, cmd.Location())

	v := rules.NewViolation(loc, spec.meta.Code, spec.wrongMessage(normalized), spec.meta.DefaultSeverity).
		WithDocURL(spec.meta.DocURL).
		WithDetail(spec.wrongDetail)

	if editLoc := signalEditLocation(input.File, input.Source, cmd); editLoc != nil {
		v = v.WithSuggestedFix(&rules.SuggestedFix{
			Description: spec.wrongFix.Description,
			Safety:      spec.wrongFix.Safety,
			Priority:    -1,
			IsPreferred: true,
			Edits: []rules.TextEdit{
				{Location: *editLoc, NewText: spec.targetSignal},
			},
		})
	}

	return v
}

// buildMissingStopsignalViolation emits a violation for a daemon stage that
// has no STOPSIGNAL at all, inserting the daemon's preferred value before the
// ENTRYPOINT or CMD that defines PID 1.
func buildMissingStopsignalViolation(
	input rules.LintInput,
	spec daemonStopsignalRule,
	stage instructions.Stage,
) rules.Violation {
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

	v := rules.NewViolation(loc, spec.meta.Code, spec.missingMessage, spec.meta.DefaultSeverity).
		WithDocURL(spec.meta.DocURL).
		WithDetail(spec.missingDetail)

	if len(runtimeLoc) > 0 {
		insertLine := runtimeLoc[0].Start.Line
		v = v.WithSuggestedFix(&rules.SuggestedFix{
			Description: spec.missingFix.Description,
			Safety:      spec.missingFix.Safety,
			Priority:    -1,
			IsPreferred: true,
			Edits: []rules.TextEdit{
				{
					Location: rules.NewRangeLocation(input.File, insertLine, 0, insertLine, 0),
					NewText:  spec.insertText,
				},
			},
		})
	}

	return v
}

// stageRuntimeExecutable returns the effective PID 1 executable for a build
// stage by examining ENTRYPOINT and CMD instructions. Returns an empty string
// when the runtime process cannot be determined (shell form, no ENTRYPOINT/CMD,
// or empty command line).
//
// Docker semantics: if ENTRYPOINT is set it defines PID 1 (CMD becomes
// arguments); if only CMD is set then CMD defines PID 1.
func stageRuntimeExecutable(stage instructions.Stage) string {
	var lastEntrypoint *instructions.EntrypointCommand
	var lastCmd *instructions.CmdCommand

	for _, cmd := range stage.Commands {
		switch c := cmd.(type) {
		case *instructions.EntrypointCommand:
			lastEntrypoint = c
		case *instructions.CmdCommand:
			lastCmd = c
		}
	}

	if lastEntrypoint != nil {
		if lastEntrypoint.PrependShell || len(lastEntrypoint.CmdLine) == 0 {
			return ""
		}
		return lastEntrypoint.CmdLine[0]
	}

	if lastCmd != nil {
		if lastCmd.PrependShell || len(lastCmd.CmdLine) == 0 {
			return ""
		}
		return lastCmd.CmdLine[0]
	}

	return ""
}
