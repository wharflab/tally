package tally

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/command"
	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/wharflab/tally/internal/rules"
)

// NoUngracefulStopsignalRuleCode is the full rule code.
const NoUngracefulStopsignalRuleCode = rules.TallyRulePrefix + "no-ungraceful-stopsignal"

// ungracefulSignals maps normalized signal names that defeat the purpose of STOPSIGNAL.
var ungracefulSignals = map[string]string{
	"SIGKILL": "cannot be caught or ignored; the container gets no chance to clean up",
	"SIGSTOP": "suspends the process instead of stopping it; the container will not terminate",
}

// numericSignals maps well-known numeric signal values to their canonical names.
// These values are stable on amd64 and arm64; other architectures may differ.
// Includes both ungraceful signals (used for detection) and common graceful
// signals (for consistent normalization in messages and future rules).
var numericSignals = map[int]string{
	1:  "SIGHUP",
	2:  "SIGINT",
	3:  "SIGQUIT",
	9:  "SIGKILL",
	15: "SIGTERM",
	19: "SIGSTOP",
	28: "SIGWINCH",
}

// NoUngracefulStopsignalRule detects STOPSIGNAL values that defeat the purpose
// of graceful container shutdown, specifically SIGKILL and SIGSTOP.
type NoUngracefulStopsignalRule struct{}

// NewNoUngracefulStopsignalRule creates a new rule instance.
func NewNoUngracefulStopsignalRule() *NoUngracefulStopsignalRule {
	return &NoUngracefulStopsignalRule{}
}

// Metadata returns the rule metadata.
func (r *NoUngracefulStopsignalRule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            NoUngracefulStopsignalRuleCode,
		Name:            "No Ungraceful STOPSIGNAL",
		Description:     "STOPSIGNAL should not use signals that prevent graceful shutdown",
		DocURL:          rules.TallyDocURL(NoUngracefulStopsignalRuleCode),
		DefaultSeverity: rules.SeverityWarning,
		Category:        "correctness",
	}
}

// Check runs the no-ungraceful-stopsignal rule.
func (r *NoUngracefulStopsignalRule) Check(input rules.LintInput) []rules.Violation {
	meta := r.Metadata()
	var violations []rules.Violation

	for _, stage := range input.Stages {
		for _, cmd := range stage.Commands {
			stopSig, ok := cmd.(*instructions.StopSignalCommand)
			if !ok {
				continue
			}

			raw := stopSig.Signal

			// Skip environment variable references — can't statically determine.
			if strings.Contains(raw, "$") {
				continue
			}

			normalized := normalizeSignalName(raw)
			reason, isUngraceful := ungracefulSignals[normalized]
			if !isUngraceful {
				continue
			}

			loc := rules.NewLocationFromRanges(input.File, stopSig.Location())

			msg := fmt.Sprintf(
				"STOPSIGNAL %s is not a graceful stop signal: %s",
				normalized, reason,
			)

			v := rules.NewViolation(loc, meta.Code, msg, meta.DefaultSeverity).
				WithDocURL(meta.DocURL).
				WithDetail("Replace with a signal that allows graceful shutdown (e.g. SIGTERM)")

			if fix := buildStopsignalFix(input.File, input.Source, stopSig); fix != nil {
				v = v.WithSuggestedFix(fix)
			}

			violations = append(violations, v)
		}
	}

	return violations
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

// buildStopsignalFix creates a TextEdit that replaces the ungraceful signal
// with SIGTERM. Returns nil if the source position cannot be determined.
func buildStopsignalFix(file string, source []byte, cmd *instructions.StopSignalCommand) *rules.SuggestedFix {
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

	startCol, endCol := signalColumnRange(line)
	if startCol < 0 {
		return nil
	}

	editLoc := rules.NewRangeLocation(file, locs[0].Start.Line, startCol, locs[0].Start.Line, endCol)

	return &rules.SuggestedFix{
		Description: "Replace with SIGTERM for graceful shutdown",
		Safety:      rules.FixSuggestion,
		Edits: []rules.TextEdit{
			{
				Location: editLoc,
				NewText:  "SIGTERM",
			},
		},
		IsPreferred: true,
	}
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
	end := len(strings.TrimRight(line, " \t"))
	if i >= end {
		return -1, -1
	}

	return i, end
}

func init() {
	rules.Register(NewNoUngracefulStopsignalRule())
}
