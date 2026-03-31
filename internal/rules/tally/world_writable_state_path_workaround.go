package tally

import (
	"fmt"
	"path"
	"strings"

	"github.com/wharflab/tally/internal/facts"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/shell"
	"github.com/wharflab/tally/internal/sourcemap"
)

// WorldWritableStatePathWorkaroundRuleCode is the full rule code.
const WorldWritableStatePathWorkaroundRuleCode = rules.TallyRulePrefix + "world-writable-state-path-workaround"

// WorldWritableStatePathWorkaroundRule detects chmod 777/a+rwx or mkdir -m 777
// patterns that set world-writable permissions — a common ownership confusion
// workaround that weakens file security instead of fixing ownership properly
// with USER, --chown, or group strategies.
//
// Cross-rule interaction:
//
//   - tally/prefer-copy-chmod: both can fire on the same Dockerfile.
//     prefer-copy-chmod suggests merging COPY + RUN chmod into COPY --chmod;
//     this rule flags the mode value itself as too permissive. Different concerns
//     (structure vs permission value), no fix overlap.
//   - tally/stateful-root-runtime: complementary. That rule flags root + state
//     path; this rule flags world-writable permissions on any path. Both can fire.
//   - tally/copy-after-user-without-chown: same ownership confusion family.
//     No fix overlap (different instructions).
type WorldWritableStatePathWorkaroundRule struct{}

// NewWorldWritableStatePathWorkaroundRule creates a new rule instance.
func NewWorldWritableStatePathWorkaroundRule() *WorldWritableStatePathWorkaroundRule {
	return &WorldWritableStatePathWorkaroundRule{}
}

// Metadata returns the rule metadata.
func (r *WorldWritableStatePathWorkaroundRule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            WorldWritableStatePathWorkaroundRuleCode,
		Name:            "World-Writable State Path Workaround",
		Description:     "chmod 777/a+rwx sets world-writable permissions, a common ownership confusion workaround",
		DocURL:          rules.TallyDocURL(WorldWritableStatePathWorkaroundRuleCode),
		DefaultSeverity: rules.SeverityWarning,
		Category:        "security",
	}
}

// Check runs the world-writable-state-path-workaround rule.
func (r *WorldWritableStatePathWorkaroundRule) Check(input rules.LintInput) []rules.Violation {
	meta := r.Metadata()
	sm := input.SourceMap()
	fileFacts := input.Facts

	var violations []rules.Violation

	for stageIdx := range input.Stages {
		sf := fileFacts.Stage(stageIdx)
		if sf == nil {
			continue
		}

		for _, runFacts := range sf.Runs {
			violations = append(violations,
				r.checkRun(runFacts, input.File, sm, meta)...)
		}
	}

	return violations
}

// checkRun inspects a single RUN instruction for world-writable chmod/mkdir patterns.
func (r *WorldWritableStatePathWorkaroundRule) checkRun(
	runFacts *facts.RunFacts,
	file string,
	sm *sourcemap.SourceMap,
	meta rules.RuleMetadata,
) []rules.Violation {
	if runFacts == nil || !runFacts.UsesShell {
		return r.checkExecForm(runFacts, file, meta)
	}

	script := runFacts.CommandScript
	if script == "" {
		return nil
	}

	variant := runFacts.Shell.Variant

	var violations []rules.Violation

	// Detect world-writable chmod commands.
	for _, cmd := range shell.FindCommands(script, variant, "chmod") {
		info := extractWorldWritableChmod(cmd.Args)
		if info == nil {
			continue
		}

		// Suppression: skip if a chgrp in the same RUN targets the same paths.
		if hasChgrpForPaths(script, variant, info.targets) {
			continue
		}

		loc := rules.NewLocationFromRanges(file, runFacts.Run.Location())
		for _, target := range info.targets {
			v := r.buildViolation(loc, meta, "chmod", info.rawMode, target)
			if fix := r.buildFix(runFacts, sm, info, meta); fix != nil {
				v = v.WithSuggestedFix(fix)
			}
			violations = append(violations, v)
		}
	}

	// Detect world-writable mkdir -m patterns.
	for _, cmd := range shell.FindCommands(script, variant, "mkdir") {
		info := extractWorldWritableMkdir(cmd.Args)
		if info == nil {
			continue
		}

		loc := rules.NewLocationFromRanges(file, runFacts.Run.Location())
		for _, target := range info.targets {
			v := r.buildViolation(loc, meta, "mkdir -m", info.rawMode, target)
			violations = append(violations, v)
		}
	}

	return violations
}

// checkExecForm handles exec-form RUN like ["chmod", "777", "/app"].
func (r *WorldWritableStatePathWorkaroundRule) checkExecForm(
	runFacts *facts.RunFacts,
	file string,
	meta rules.RuleMetadata,
) []rules.Violation {
	if runFacts == nil || runFacts.Run == nil {
		return nil
	}

	cmdLine := runFacts.Run.CmdLine
	if len(cmdLine) < 3 {
		return nil
	}

	cmdName := cmdLine[0]
	if cmdName != "chmod" {
		return nil
	}

	info := extractWorldWritableChmod(cmdLine[1:])
	if info == nil {
		return nil
	}

	loc := rules.NewLocationFromRanges(file, runFacts.Run.Location())
	var violations []rules.Violation
	for _, target := range info.targets {
		v := r.buildViolation(loc, meta, "chmod", info.rawMode, target)
		violations = append(violations, v)
	}
	return violations
}

// buildViolation constructs a violation with a contextual message.
func (r *WorldWritableStatePathWorkaroundRule) buildViolation(
	loc rules.Location,
	meta rules.RuleMetadata,
	cmdDesc, rawMode, target string,
) rules.Violation {
	var msg string
	if isStatePath(target) {
		msg = fmt.Sprintf(
			"%s %s on state path %s sets world-writable permissions (ownership confusion workaround)",
			cmdDesc, rawMode, target,
		)
	} else {
		msg = fmt.Sprintf(
			"%s %s on %s sets world-writable permissions",
			cmdDesc, rawMode, target,
		)
	}

	return rules.NewViolation(loc, meta.Code, msg, meta.DefaultSeverity).
		WithDocURL(meta.DocURL)
}

// worldWritableInfo holds a detected world-writable chmod/mkdir.
type worldWritableInfo struct {
	rawMode     string   // original mode string (e.g., "777", "a+rwx")
	parsedMode  uint16   // parsed octal mode
	targets     []string // target paths
	isRecursive bool     // -R flag present (chmod only)
}

// extractWorldWritableChmod parses chmod arguments for world-writable modes.
// args should NOT include the "chmod" command name itself.
func extractWorldWritableChmod(args []string) *worldWritableInfo {
	var (
		rawMode     string
		parsedMode  uint16
		seenMode    bool
		isRecursive bool
		targets     []string
	)

	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			if strings.Contains(arg, "R") {
				isRecursive = true
			}
			continue
		}

		if !seenMode {
			rawMode, parsedMode, seenMode = parseWorldWritableMode(arg)
			if !seenMode {
				return nil // first non-flag arg must be the mode
			}
			continue
		}

		// Remaining non-flag args are target paths.
		targets = append(targets, arg)
	}

	if !seenMode || len(targets) == 0 {
		return nil
	}

	if !isWorldWritable(parsedMode) {
		return nil
	}

	return &worldWritableInfo{
		rawMode:     rawMode,
		parsedMode:  parsedMode,
		targets:     targets,
		isRecursive: isRecursive,
	}
}

// extractWorldWritableMkdir parses mkdir arguments for -m/--mode with
// world-writable values.
func extractWorldWritableMkdir(args []string) *worldWritableInfo {
	var (
		rawMode    string
		parsedMode uint16
		seenMode   bool
		targets    []string
	)

	for i := 0; i < len(args); i++ {
		arg := args[i]

		// --mode=777 form
		if value, found := strings.CutPrefix(arg, "--mode="); found {
			rawMode, parsedMode, seenMode = parseWorldWritableMode(value)
			continue
		}

		// --mode 777 form
		if arg == "--mode" && i+1 < len(args) {
			i++
			rawMode, parsedMode, seenMode = parseWorldWritableMode(args[i])
			continue
		}

		// Handle combined short flags like -pm 777 or standalone -m 777
		if strings.HasPrefix(arg, "-") && !strings.HasPrefix(arg, "--") {
			flagChars := strings.TrimPrefix(arg, "-")
			if strings.Contains(flagChars, "m") {
				// -m is the last char → next arg is the mode value
				if strings.HasSuffix(flagChars, "m") && i+1 < len(args) {
					i++
					rawMode, parsedMode, seenMode = parseWorldWritableMode(args[i])
				}
				// -m<value> form (e.g., -m777) — the value is embedded
				if idx := strings.Index(flagChars, "m"); idx < len(flagChars)-1 {
					rawMode, parsedMode, seenMode = parseWorldWritableMode(flagChars[idx+1:])
				}
			}
			continue
		}

		// Non-flag arg → directory path
		if !strings.HasPrefix(arg, "-") {
			targets = append(targets, arg)
		}
	}

	if !seenMode || len(targets) == 0 || !isWorldWritable(parsedMode) {
		return nil
	}

	return &worldWritableInfo{
		rawMode:    rawMode,
		parsedMode: parsedMode,
		targets:    targets,
	}
}

// parseWorldWritableMode parses a mode string and returns the raw mode,
// parsed octal mode, and whether parsing succeeded.
func parseWorldWritableMode(s string) (string, uint16, bool) {
	switch {
	case shell.IsOctalMode(s):
		mode := shell.ParseOctalMode(s)
		return s, mode, true
	case shell.IsSymbolicMode(s):
		// Apply symbolic mode to default file mode (0o644) and also to 0o000
		// to handle both additive (+w) and absolute (=rwx) cases.
		fromDefault := shell.ApplySymbolicMode(s, 0o644)
		fromZero := shell.ApplySymbolicMode(s, 0o000)
		// Use whichever produces the more permissive result.
		mode := fromDefault | fromZero
		if mode == 0 {
			return "", 0, false // unsupported symbolic mode (X, s, t)
		}
		return s, mode, true
	default:
		// Unrecognized mode format (e.g., g=u). Not parseable → skip.
		return "", 0, false
	}
}

// isWorldWritable checks if a mode has the others-write bit set.
func isWorldWritable(mode uint16) bool {
	return mode&0o002 != 0
}

// suggestTighterMode computes a tighter replacement by clearing the others-write bit.
func suggestTighterMode(rawMode string, parsedMode uint16) string {
	tighter := parsedMode &^ 0o002
	if shell.IsOctalMode(rawMode) {
		// Preserve the user's digit count: 3 digits (777) → 3 digits, 4 digits (0777) → 4 digits.
		if len(rawMode) == 4 {
			return fmt.Sprintf("%04o", tighter)
		}
		return fmt.Sprintf("%03o", tighter)
	}
	// For symbolic modes, suggest the octal equivalent.
	return fmt.Sprintf("%03o", tighter)
}

// buildFix creates a FixSuggestion that replaces the world-writable mode token
// with a tighter mode. Only generated for octal modes (symbolic edits are too
// varied for reliable auto-replacement).
func (r *WorldWritableStatePathWorkaroundRule) buildFix(
	runFacts *facts.RunFacts,
	sm *sourcemap.SourceMap,
	info *worldWritableInfo,
	meta rules.RuleMetadata,
) *rules.SuggestedFix {
	if info.isRecursive {
		return nil // recursive chmod is too complex for safe suggestion
	}
	if !shell.IsOctalMode(info.rawMode) {
		return nil // only offer fix for octal modes
	}

	tighter := suggestTighterMode(info.rawMode, info.parsedMode)

	// Locate the mode token in the source to build a precise TextEdit.
	runLoc := runFacts.Run.Location()
	if len(runLoc) == 0 {
		return nil
	}

	startLine := runLoc[0].Start.Line // 1-based
	endLine := sm.ResolveEndLine(startLine)

	// Search for the mode token in the source lines of this RUN instruction.
	for lineNum := startLine; lineNum <= endLine; lineNum++ {
		lineText := sm.Line(lineNum - 1) // Line() is 0-based
		col := findModeToken(lineText, info.rawMode)
		if col < 0 {
			continue
		}

		edit := rules.TextEdit{
			Location: rules.NewRangeLocation(
				"", // file is filled by fixer
				lineNum, col,
				lineNum, col+len(info.rawMode),
			),
			NewText: tighter,
		}

		return &rules.SuggestedFix{
			Description: fmt.Sprintf("Replace %s with %s to remove world-writable permission", info.rawMode, tighter),
			Edits:       []rules.TextEdit{edit},
			Safety:      rules.FixSuggestion,
			IsPreferred: true,
			Priority:    meta.FixPriority,
		}
	}

	return nil
}

// findModeToken finds the column offset of a chmod mode token in a line.
// Returns -1 if not found. Ensures the match is surrounded by whitespace or
// line boundaries to avoid matching partial tokens.
func findModeToken(line, mode string) int {
	offset := 0
	for {
		idx := strings.Index(line[offset:], mode)
		if idx < 0 {
			return -1
		}
		col := offset + idx

		// Check boundaries: must be preceded by space/tab/start and followed by space/tab/end.
		before := col == 0 || line[col-1] == ' ' || line[col-1] == '\t'
		after := col+len(mode) >= len(line) ||
			line[col+len(mode)] == ' ' || line[col+len(mode)] == '\t' ||
			line[col+len(mode)] == '\\' || line[col+len(mode)] == '&' ||
			line[col+len(mode)] == ';' || line[col+len(mode)] == '\n'
		if before && after {
			return col
		}

		offset = col + len(mode)
		if offset >= len(line) {
			return -1
		}
	}
}

// hasChgrpForPaths checks whether the script contains a chgrp command targeting
// any of the given paths. This is the OpenShift suppression: chgrp 0 + chmod g+rwx
// is a valid pattern for arbitrary-UID containers.
func hasChgrpForPaths(script string, variant shell.Variant, targets []string) bool {
	if !strings.Contains(script, "chgrp") {
		return false
	}

	chgrpCmds := shell.FindCommands(script, variant, "chgrp")
	for _, cmd := range chgrpCmds {
		chgrpPaths := extractChgrpTargets(cmd.Args)
		for _, chgrpPath := range chgrpPaths {
			for _, target := range targets {
				if pathCovers(chgrpPath, target) {
					return true
				}
			}
		}
	}

	return false
}

// extractChgrpTargets extracts file paths from chgrp arguments.
// chgrp [options] GROUP FILE...
func extractChgrpTargets(args []string) []string {
	var (
		seenGroup bool
		paths     []string
	)
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			continue
		}
		if !seenGroup {
			seenGroup = true // first non-flag is the group
			continue
		}
		paths = append(paths, arg)
	}
	return paths
}

// pathCovers checks if chgrpPath covers targetPath (exact or parent).
func pathCovers(chgrpPath, targetPath string) bool {
	chgrpPath = path.Clean(chgrpPath)
	targetPath = path.Clean(targetPath)
	return targetPath == chgrpPath || strings.HasPrefix(targetPath, chgrpPath+"/")
}

func init() {
	rules.Register(NewWorldWritableStatePathWorkaroundRule())
}
