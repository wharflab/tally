// Package shell provides shell script parsing utilities for Dockerfile linting.
package shell

import (
	"fmt"
	"path"
	"regexp"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// Command names for file creation detection.
const (
	cmdEcho   = "echo"
	cmdCat    = "cat"
	cmdPrintf = "printf"
	cmdTee    = "tee"
	cmdChmod  = "chmod"
	cmdUmask  = "umask"
	cmdMkdir  = "mkdir"
)

// FileCreationInfo describes a detected file creation pattern in a shell script.
// This is used to coordinate between prefer-copy-heredoc and prefer-run-heredoc rules.
type FileCreationInfo struct {
	// TargetPath is the absolute path to the target file after any caller-supplied
	// path resolution has been applied.
	TargetPath string

	// ResolvedHomePath is true when the original shell target used home expansion
	// (for example "~/.bashrc") and the caller resolved it to an absolute path.
	// Converting such patterns to COPY usually requires an unsafe fix because COPY
	// does not support "~" directly.
	ResolvedHomePath bool

	// Content is the literal content to write.
	Content string

	// ChmodMode is the octal chmod mode (e.g., 0o755, 0o644), or 0 if no chmod.
	ChmodMode uint16

	// RawChmodMode is the original chmod mode string (e.g., "+x", "755", "0755").
	// Preserved so fixes can use the notation the user wrote.
	// Empty when ChmodMode is 0.
	RawChmodMode string

	// IsAppend is true if ALL writes in the chain use >> (append) mode.
	// If true, converting to COPY would lose existing file content.
	// A later > (overwrite) clears this flag since content no longer depends on existing data.
	IsAppend bool

	// HasUnsafeVariables is true if the script uses variables that cannot be
	// converted to COPY heredoc (e.g., shell variables, command substitution).
	HasUnsafeVariables bool

	// PrecedingCommands contains commands before the file creation (for mixed scripts).
	// Empty if file creation is at the start or script is pure file creation.
	PrecedingCommands string

	// RemainingCommands contains commands after the file creation (for mixed scripts).
	// Empty if file creation is at the end or script is pure file creation.
	RemainingCommands string
}

// fileCreationCmd represents a single file creation command in a chain.
type fileCreationCmd struct {
	targetPath       string
	content          string
	isAppend         bool
	resolvedHomePath bool
}

// FileCreationOptions controls optional behavior for file creation detection.
type FileCreationOptions struct {
	// ResolveTargetPath lets callers rewrite literal targets like "~/.bashrc"
	// to an absolute in-image path before matching.
	ResolveTargetPath func(rawTarget string) (resolvedPath string, resolvedHomePath bool, ok bool)

	// InterpretPlainEchoEscapes enables backslash-escape processing for plain
	// echo output (for example \n -> newline) on shells where that behavior is
	// part of the effective runtime semantics.
	InterpretPlainEchoEscapes bool
}

// MultiFileCreationSlot describes a single file-creation sub-block inside a
// chained script. Multiple slots can coexist when a RUN writes to several
// distinct files in one && chain.
type MultiFileCreationSlot struct {
	// Info is the resolved creation for this slot (target, content, chmod, etc.).
	Info FileCreationInfo
	// CmdIndex is the index of the flattened command that produced this slot.
	CmdIndex int
	// EndIndex is the last contributing command index (inclusive).
	EndIndex int
}

// MultiFileCreationInfo describes the set of file creations and interleaved
// non-creation commands found in a single shell script. It is produced by
// DetectFileCreations when the script has one or more distinct target paths.
type MultiFileCreationInfo struct {
	// Slots lists each distinct file creation in chain order.
	Slots []MultiFileCreationSlot

	// Commands is the flattened command list (file creations, chmods, umasks,
	// and "other" commands) in original order. Non-creation commands are
	// preserved verbatim so callers can emit them before/between COPYs.
	Commands []MultiAnalyzedCmd

	// HasUnsafeVariables is true if any command in the script uses unsafe
	// variables (shell vars, command substitution, complex expansions).
	HasUnsafeVariables bool

	// ResolvedHomePath is true if any slot used tilde (~) expansion.
	ResolvedHomePath bool
}

// MultiAnalyzedCmd is a per-command record exposed to callers alongside
// MultiFileCreationInfo. It mirrors the internal analyzedCmd but is part of
// the public API surface for rules that need to know which commands were
// file creations vs other shell commands.
type MultiAnalyzedCmd struct {
	// Kind identifies how this command should be handled.
	Kind MultiCmdKind
	// Text is the original (printed) command text.
	Text string
	// SlotIndex points into MultiFileCreationInfo.Slots for Kind == MultiCmdCreation.
	SlotIndex int
	// MkdirTarget, for Kind == MultiCmdMkdirP, is the absolute directory path
	// that `mkdir -p` would create. Empty for non-mkdir commands.
	MkdirTarget string
	// IsShellStateOnly is true for commands that only mutate the current
	// shell's options and don't cross RUN boundaries (e.g. `set -ex`,
	// `shopt -s nullglob`). Fix builders can drop such commands when they
	// would otherwise be left alone in a RUN that no longer hosts real work.
	IsShellStateOnly bool
}

// MultiCmdKind identifies the role of a command within a MultiFileCreationInfo.
type MultiCmdKind int

const (
	// MultiCmdOther is a generic command we can't absorb into a COPY heredoc.
	MultiCmdOther MultiCmdKind = iota
	// MultiCmdCreation is a file-creation command contributing to Slots.
	MultiCmdCreation
	// MultiCmdChmod is a chmod command (already folded into the matching slot
	// when possible; otherwise treated as "other").
	MultiCmdChmod
	// MultiCmdMkdirP is a `mkdir -p /path` command with a literal absolute path.
	// Callers can elide it when a subsequent COPY creates the same (or a
	// descendant) directory — BuildKit's COPY auto-creates parent directories.
	MultiCmdMkdirP
)

// DetectFileCreation analyzes a shell script for file creation patterns.
// Returns nil if the script is not primarily a file creation operation.
//
// Detected patterns:
//   - echo "content" > /path/to/file
//   - echo "content" >> /path/to/file (append)
//   - cat <<EOF > /path/to/file ... EOF
//   - printf "content" > /path/to/file (limited support)
//   - tee /path/to/file (with heredoc stdin)
//
// Also detects chmod chaining: echo "x" > /file && chmod 0755 /file
//
// The knownVars function is called to check if a variable is a known ARG/ENV.
// If nil, all variables are considered unsafe.
func DetectFileCreation(script string, variant Variant, knownVars func(name string) bool) *FileCreationInfo {
	return DetectFileCreationWithOptions(script, variant, knownVars, FileCreationOptions{})
}

// DetectFileCreationWithOptions analyzes a shell script for file creation
// patterns with optional target-path rewriting and shell-specific echo
// semantics.
func DetectFileCreationWithOptions(
	script string,
	variant Variant,
	knownVars func(name string) bool,
	options FileCreationOptions,
) *FileCreationInfo {
	if !variant.SupportsPOSIXShellAST() {
		return nil
	}

	prog, err := parseScript(script, variant)
	if err != nil {
		return nil
	}

	// Must be a simple script (no complex control flow)
	if !isSimpleScriptFromAST(prog) {
		return nil
	}

	// Analyze the script for file creation patterns
	return analyzeFileCreation(prog, knownVars, options)
}

// DetectFileCreations analyzes a shell script and returns every distinct
// file-creation target found in its && chain. Unlike DetectFileCreation it
// does not collapse mixed scripts into preceding/remaining strings; instead
// it returns the full flattened command list plus one slot per creation.
// Returns nil if the script contains no recognizable file creation.
func DetectFileCreations(
	script string,
	variant Variant,
	knownVars func(name string) bool,
	options FileCreationOptions,
) *MultiFileCreationInfo {
	if !variant.SupportsPOSIXShellAST() {
		return nil
	}

	prog, err := parseScript(script, variant)
	if err != nil {
		return nil
	}

	if !isSimpleScriptFromAST(prog) {
		return nil
	}

	return analyzeFileCreations(prog, knownVars, options)
}

// ChmodInfo describes a standalone chmod command.
type ChmodInfo struct {
	// Mode is the octal mode (e.g., 0o755, 0o644, 0o4755).
	Mode uint16
	// RawMode is the original mode string as written (e.g., "+x", "755", "0755", "u+rwx").
	// Useful for preserving notation in fixes (COPY --chmod supports both octal and symbolic).
	RawMode string
	// Target is the file path being chmod'd.
	Target string
}

// DetectStandaloneChmod checks if a shell script is a standalone chmod command.
// Returns nil if it's not a pure chmod or if the chmod cannot be converted
// (e.g., symbolic mode, recursive chmod, multiple commands).
func DetectStandaloneChmod(script string, variant Variant) *ChmodInfo {
	if !variant.SupportsPOSIXShellAST() {
		return nil
	}

	prog, err := parseScript(script, variant)
	if err != nil {
		return nil
	}

	// Must be exactly one statement
	if len(prog.Stmts) != 1 {
		return nil
	}

	stmt := prog.Stmts[0]
	if stmt.Cmd == nil {
		return nil
	}

	call, ok := stmt.Cmd.(*syntax.CallExpr)
	if !ok || len(call.Args) == 0 {
		return nil
	}

	// Must be chmod command
	if call.Args[0].Lit() != cmdChmod {
		return nil
	}

	mode, rawMode, target, ok := parseChmod(call)
	if !ok {
		return nil
	}

	return &ChmodInfo{Mode: mode, RawMode: rawMode, Target: target}
}

// IsPureFileCreation checks if a shell script is PURELY for creating files.
// Returns true only if every command in the script is for file creation (echo/cat/printf > file)
// or chmod on the created file. Returns false if there are any other commands mixed in.
// This is used by prefer-run-heredoc to yield to prefer-copy-heredoc.
func IsPureFileCreation(script string, variant Variant) bool {
	if !variant.SupportsPOSIXShellAST() {
		return false
	}

	info := DetectFileCreation(script, variant, nil)
	if info == nil || info.HasUnsafeVariables {
		return false
	}
	// Pure means no other commands before or after
	return info.PrecedingCommands == "" && info.RemainingCommands == ""
}

// cmdType represents the type of command in a chain.
type cmdType int

const (
	cmdTypeOther cmdType = iota
	cmdTypeFileCreation
	cmdTypeChmod
	cmdTypeUmask
	cmdTypeMkdirP
)

// analyzedCmd represents a command with its type and original text.
type analyzedCmd struct {
	cmdType      cmdType
	text         string
	creation     *fileCreationCmd // non-nil for cmdTypeFileCreation
	chmodMode    uint16           // non-zero for cmdTypeChmod
	chmodRawMode string           // original mode string for cmdTypeChmod (e.g., "+x", "755")
	chmodTarget  string           // non-empty for cmdTypeChmod
	umaskValue   uint16           // non-zero for cmdTypeUmask (the mask value, e.g., 0o077)
	mkdirTarget  string           // non-empty for cmdTypeMkdirP (absolute literal path)
	hasUnsafe    bool
	// isShellState is true for commands that only mutate the current shell's
	// options (`set`, `shopt`). They have no effect across RUNs — each RUN
	// starts a fresh shell — so fix builders can drop them when they'd end
	// up alone in a leftover RUN buffer.
	isShellState bool
}

// analyzeFileCreation performs detailed analysis of file creation patterns.
// Supports mixed commands by tracking preceding and remaining commands.
func analyzeFileCreation(
	prog *syntax.File,
	knownVars func(name string) bool,
	options FileCreationOptions,
) *FileCreationInfo {
	// Require exactly one top-level statement to avoid ambiguity with separators.
	// Scripts with semicolons (cmd1; cmd2) would be incorrectly rebuilt as && chains.
	// Only && chains within a single statement are supported.
	if len(prog.Stmts) != 1 {
		return nil
	}

	// Collect all commands with their types
	var commands []analyzedCmd
	collectCommands(prog, &commands, knownVars, options)

	if len(commands) == 0 {
		return nil
	}

	// Find contiguous file creation block (including chmod for same file)
	startIdx, endIdx, targetPath := findFileCreationBlock(commands)
	if startIdx == -1 {
		return nil
	}

	// Track most recent umask before file creation block
	var activeUmask uint16
	hasUmask := false
	for i := range startIdx {
		if commands[i].cmdType == cmdTypeUmask {
			activeUmask = commands[i].umaskValue
			hasUmask = true
		}
	}

	// Extract file creation commands and merge content
	var creations []fileCreationCmd
	var chmodMode uint16
	var rawChmodMode string
	hasUnsafeVars := false
	resolvedHomePath := false

	for i := startIdx; i <= endIdx; i++ {
		cmd := commands[i]
		if cmd.hasUnsafe {
			hasUnsafeVars = true
		}
		if cmd.cmdType == cmdTypeFileCreation && cmd.creation != nil {
			creations = append(creations, *cmd.creation)
			if cmd.creation.resolvedHomePath {
				resolvedHomePath = true
			}
		} else if cmd.cmdType == cmdTypeChmod && cmd.chmodTarget == targetPath {
			chmodMode = cmd.chmodMode
			rawChmodMode = cmd.chmodRawMode
		}
	}

	if len(creations) == 0 {
		return nil
	}

	// If no explicit chmod but umask was set, calculate effective mode
	if chmodMode == 0 {
		chmodMode = umaskDerivedChmodMode(activeUmask, hasUmask)
	}

	// Merge content from all creations.
	// Track if all writes are append-only (no overwrite clears content).
	var content strings.Builder
	allAppend := true
	for i, c := range creations {
		if i > 0 && !c.isAppend {
			content.Reset()
		}
		if !c.isAppend {
			allAppend = false
		}
		content.WriteString(c.content)
	}

	// Build preceding / remaining chains. Shell-state-only commands
	// (`set -e`, `shopt`, `trap`) are dropped when *all* commands on the
	// side are state-only, since shell options don't persist across RUN
	// boundaries and preserving them alone would be pure noise. A mixed
	// chain (state + real work) keeps everything.
	preceding := collectNonStateOnlyChain(commands[:startIdx])
	remaining := collectNonStateOnlyChain(commands[endIdx+1:])

	// When umask-derived mode has no explicit raw string, format it
	if chmodMode != 0 && rawChmodMode == "" {
		rawChmodMode = FormatOctalMode(chmodMode)
	}

	return &FileCreationInfo{
		TargetPath:         targetPath,
		ResolvedHomePath:   resolvedHomePath,
		Content:            content.String(),
		ChmodMode:          chmodMode,
		RawChmodMode:       rawChmodMode,
		IsAppend:           allAppend,
		HasUnsafeVariables: hasUnsafeVars,
		PrecedingCommands:  strings.Join(preceding, " && "),
		RemainingCommands:  strings.Join(remaining, " && "),
	}
}

// analyzeFileCreations performs multi-target file-creation analysis.
// Unlike analyzeFileCreation it does not collapse to a single target; instead
// it walks the flattened && chain and reports every distinct creation in
// order, with interleaved non-creation commands preserved verbatim.
func analyzeFileCreations(
	prog *syntax.File,
	knownVars func(name string) bool,
	options FileCreationOptions,
) *MultiFileCreationInfo {
	if len(prog.Stmts) != 1 {
		return nil
	}

	var commands []analyzedCmd
	collectCommands(prog, &commands, knownVars, options)
	if len(commands) == 0 {
		return nil
	}

	// Compute an effective umask at each index for proper mode derivation.
	umaskAt := make([]uint16, len(commands))
	umaskSeen := make([]bool, len(commands))
	var curUmask uint16
	var curSeen bool
	for i, cmd := range commands {
		if cmd.cmdType == cmdTypeUmask {
			curUmask = cmd.umaskValue
			curSeen = true
		}
		umaskAt[i] = curUmask
		umaskSeen[i] = curSeen
	}

	// Find contiguous creation blocks (file creations + chmod to same target).
	// A new block starts whenever the target path differs from the previous
	// creation, or a non-creation/non-matching-chmod command appears.
	type blockRange struct {
		start, end int
		target     string
	}
	var blocks []blockRange
	i := 0
	for i < len(commands) {
		cmd := commands[i]
		if cmd.cmdType != cmdTypeFileCreation || cmd.creation == nil {
			i++
			continue
		}
		target := cmd.creation.targetPath
		start := i
		end := i
		j := i + 1
		for j < len(commands) {
			next := commands[j]
			switch {
			case next.cmdType == cmdTypeFileCreation && next.creation != nil && next.creation.targetPath == target:
				end = j
				j++
				continue
			case next.cmdType == cmdTypeChmod && next.chmodTarget == target:
				end = j
				j++
				continue
			}
			break
		}
		blocks = append(blocks, blockRange{start: start, end: end, target: target})
		i = end + 1
	}

	if len(blocks) == 0 {
		return nil
	}

	// Build slots from blocks.
	info := &MultiFileCreationInfo{}
	// Track which command index belongs to which slot.
	slotIndexByCmd := make(map[int]int, len(commands))
	for _, blk := range blocks {
		slot := buildSlotFromBlock(commands, blk.start, blk.end, blk.target, umaskAt, umaskSeen)
		if slot == nil {
			continue
		}
		slot.CmdIndex = blk.start
		slot.EndIndex = blk.end
		idx := len(info.Slots)
		info.Slots = append(info.Slots, *slot)
		for k := blk.start; k <= blk.end; k++ {
			slotIndexByCmd[k] = idx
		}
	}

	if len(info.Slots) == 0 {
		return nil
	}

	// Emit a flat list of MultiAnalyzedCmd.
	info.Commands = make([]MultiAnalyzedCmd, 0, len(commands))
	for k, cmd := range commands {
		entry := MultiAnalyzedCmd{Text: cmd.text, SlotIndex: -1, IsShellStateOnly: cmd.isShellState}
		if slotIdx, ok := slotIndexByCmd[k]; ok {
			entry.Kind = MultiCmdCreation
			entry.SlotIndex = slotIdx
		} else {
			switch cmd.cmdType {
			case cmdTypeChmod:
				entry.Kind = MultiCmdChmod
			case cmdTypeMkdirP:
				entry.Kind = MultiCmdMkdirP
				entry.MkdirTarget = cmd.mkdirTarget
			case cmdTypeFileCreation:
				// Shouldn't happen: all creations are slot-mapped. Fall through.
				entry.Kind = MultiCmdOther
			default:
				entry.Kind = MultiCmdOther
			}
		}
		if cmd.hasUnsafe {
			info.HasUnsafeVariables = true
		}
		info.Commands = append(info.Commands, entry)
	}
	for _, slot := range info.Slots {
		if slot.Info.HasUnsafeVariables {
			info.HasUnsafeVariables = true
		}
		if slot.Info.ResolvedHomePath {
			info.ResolvedHomePath = true
		}
	}

	return info
}

// buildSlotFromBlock constructs a MultiFileCreationSlot from a contiguous
// block of commands, all targeting the same file path.
func buildSlotFromBlock(
	commands []analyzedCmd,
	start, end int,
	targetPath string,
	umaskAt []uint16,
	umaskSeen []bool,
) *MultiFileCreationSlot {
	var creations []fileCreationCmd
	var chmodMode uint16
	var rawChmodMode string
	hasUnsafeVars := false
	resolvedHomePath := false

	for k := start; k <= end; k++ {
		cmd := commands[k]
		if cmd.hasUnsafe {
			hasUnsafeVars = true
		}
		switch {
		case cmd.cmdType == cmdTypeFileCreation && cmd.creation != nil:
			creations = append(creations, *cmd.creation)
			if cmd.creation.resolvedHomePath {
				resolvedHomePath = true
			}
		case cmd.cmdType == cmdTypeChmod && cmd.chmodTarget == targetPath:
			chmodMode = cmd.chmodMode
			rawChmodMode = cmd.chmodRawMode
		}
	}

	if len(creations) == 0 {
		return nil
	}

	// umask contribution is read from the last umask seen at or before the block start.
	if chmodMode == 0 && start < len(umaskAt) {
		seenIdx := start
		if seenIdx > 0 {
			seenIdx = start - 1
		}
		if seenIdx < len(umaskAt) {
			chmodMode = umaskDerivedChmodMode(umaskAt[seenIdx], umaskSeen[seenIdx])
		}
	}

	var content strings.Builder
	allAppend := true
	for i, c := range creations {
		if i > 0 && !c.isAppend {
			content.Reset()
		}
		if !c.isAppend {
			allAppend = false
		}
		content.WriteString(c.content)
	}

	if chmodMode != 0 && rawChmodMode == "" {
		rawChmodMode = FormatOctalMode(chmodMode)
	}

	return &MultiFileCreationSlot{
		Info: FileCreationInfo{
			TargetPath:         targetPath,
			ResolvedHomePath:   resolvedHomePath,
			Content:            content.String(),
			ChmodMode:          chmodMode,
			RawChmodMode:       rawChmodMode,
			IsAppend:           allAppend,
			HasUnsafeVariables: hasUnsafeVars,
		},
	}
}

// umaskDerivedChmodMode computes the effective chmod mode from a umask value.
// Returns 0 if no umask was set or it matches the default (0o022 → 0o644).
func umaskDerivedChmodMode(activeUmask uint16, hasUmask bool) uint16 {
	if !hasUmask || activeUmask == defaultUmask {
		return 0
	}
	effectiveMode := uint16(0o666) & ^activeUmask
	if effectiveMode == defaultFileMode {
		return 0
	}
	return effectiveMode
}

// collectNonStateOnlyChain returns the command texts of slice, dropping them
// all when every entry is shell-state-only (e.g. `set -e`, `shopt`, `trap`).
// Those commands don't cross RUN boundaries, so preserving them alone in a
// leftover RUN is pure noise. A mix of state-only and real commands keeps
// everything — the `set -e` guard matters when there's real work alongside.
func collectNonStateOnlyChain(slice []analyzedCmd) []string {
	if len(slice) == 0 {
		return nil
	}
	allStateOnly := true
	for _, cmd := range slice {
		if !cmd.isShellState {
			allStateOnly = false
			break
		}
	}
	if allStateOnly {
		return nil
	}
	out := make([]string, 0, len(slice))
	for _, cmd := range slice {
		out = append(out, cmd.text)
	}
	return out
}

// collectCommands flattens && chains and collects all commands with their types.
func collectCommands(
	prog *syntax.File,
	commands *[]analyzedCmd,
	knownVars func(name string) bool,
	options FileCreationOptions,
) {
	for _, stmt := range prog.Stmts {
		collectFromStatement(stmt, commands, knownVars, options)
	}
}

// collectFromStatement recursively collects commands from a statement.
func collectFromStatement(
	stmt *syntax.Stmt,
	commands *[]analyzedCmd,
	knownVars func(name string) bool,
	options FileCreationOptions,
) {
	if stmt == nil || stmt.Cmd == nil {
		return
	}

	switch cmd := stmt.Cmd.(type) {
	case *syntax.BinaryCmd:
		switch cmd.Op {
		case syntax.AndStmt:
			collectFromStatement(cmd.X, commands, knownVars, options)
			collectFromStatement(cmd.Y, commands, knownVars, options)
		case syntax.Pipe, syntax.PipeAll:
			if analyzed, ok := analyzePipeToTee(stmt, cmd, knownVars, options); ok {
				*commands = append(*commands, analyzed)
				return
			}
			*commands = append(*commands, analyzedCmd{
				cmdType: cmdTypeOther,
				text:    stmtToString(stmt),
			})
		default:
			// Other binary ops (||) - treat as single opaque command
			*commands = append(*commands, analyzedCmd{
				cmdType: cmdTypeOther,
				text:    stmtToString(stmt),
			})
		}
	case *syntax.CallExpr:
		analyzed := analyzeCallExpr(stmt, cmd, knownVars, options)
		*commands = append(*commands, analyzed)
	default:
		*commands = append(*commands, analyzedCmd{
			cmdType: cmdTypeOther,
			text:    stmtToString(stmt),
		})
	}
}

// analyzeCallExpr analyzes a call expression and returns its type.
func analyzeCallExpr(
	stmt *syntax.Stmt,
	call *syntax.CallExpr,
	knownVars func(name string) bool,
	options FileCreationOptions,
) analyzedCmd {
	if len(call.Args) == 0 {
		return analyzedCmd{cmdType: cmdTypeOther, text: stmtToString(stmt)}
	}

	cmdName := call.Args[0].Lit()
	text := stmtToString(stmt)

	// Check for umask
	if cmdName == cmdUmask {
		mask, ok := parseUmask(call)
		if ok {
			return analyzedCmd{
				cmdType:    cmdTypeUmask,
				text:       text,
				umaskValue: mask,
			}
		}
		// Unrecognized umask (symbolic mode, etc.) - treat as other
		return analyzedCmd{cmdType: cmdTypeOther, text: text}
	}

	// Check for chmod
	if cmdName == cmdChmod {
		mode, rawMode, target, ok := parseChmod(call)
		if ok {
			return analyzedCmd{
				cmdType:      cmdTypeChmod,
				text:         text,
				chmodMode:    mode,
				chmodRawMode: rawMode,
				chmodTarget:  target,
			}
		}
		return analyzedCmd{cmdType: cmdTypeOther, text: text}
	}

	// Check for tee command (file target is in args, not redirects)
	if cmdName == cmdTee {
		return analyzeTeeCmd(stmt, call, text, options.ResolveTargetPath)
	}

	// Check for mkdir -p (for later absorption by COPY destinations)
	if cmdName == cmdMkdir {
		if target, ok := analyzeMkdirCmd(call); ok {
			return analyzedCmd{
				cmdType:     cmdTypeMkdirP,
				text:        text,
				mkdirTarget: target,
			}
		}
		return analyzedCmd{cmdType: cmdTypeOther, text: text}
	}

	// Check for file creation commands
	if cmdName != cmdEcho && cmdName != cmdCat && cmdName != cmdPrintf {
		return analyzedCmd{
			cmdType:      cmdTypeOther,
			text:         text,
			isShellState: isShellStateOnlyCmd(cmdName),
		}
	}

	// Validate redirects: allow exactly one stdout output redirect and
	// (for cat) an optional heredoc input.
	var outRedir *syntax.Redirect
	for _, redir := range stmt.Redirs {
		switch redir.Op {
		case syntax.RdrOut, syntax.AppOut:
			if redir.N != nil && redir.N.Value != "1" {
				return analyzedCmd{cmdType: cmdTypeOther, text: text}
			}
			if outRedir != nil {
				return analyzedCmd{cmdType: cmdTypeOther, text: text}
			}
			outRedir = redir
		case syntax.Hdoc, syntax.DashHdoc:
			if cmdName != cmdCat {
				return analyzedCmd{cmdType: cmdTypeOther, text: text}
			}
		case syntax.RdrIn, syntax.RdrInOut, syntax.DplIn, syntax.DplOut,
			syntax.RdrClob, syntax.AppClob, syntax.WordHdoc,
			syntax.RdrAll, syntax.RdrAllClob, syntax.AppAll, syntax.AppAllClob:
			// Input redirects and other unsupported redirect types
			return analyzedCmd{cmdType: cmdTypeOther, text: text}
		}
	}
	if outRedir == nil {
		return analyzedCmd{cmdType: cmdTypeOther, text: text}
	}

	rawTargetPath := extractRedirectTarget(outRedir)
	targetPath, resolvedHomePath, ok := resolveFileCreationTarget(rawTargetPath, options.ResolveTargetPath)
	if !ok {
		return analyzedCmd{cmdType: cmdTypeOther, text: text}
	}

	content, unsafe := extractFileContent(stmt, call, knownVars, options)

	return analyzedCmd{
		cmdType: cmdTypeFileCreation,
		text:    text,
		creation: &fileCreationCmd{
			targetPath:       targetPath,
			content:          content,
			isAppend:         outRedir.Op == syntax.AppOut,
			resolvedHomePath: resolvedHomePath,
		},
		hasUnsafe: unsafe,
	}
}

// analyzeTeeCmd handles tee as a file creation command.
// tee writes stdin to a file argument (not a redirect target).
// Supported: tee /file, tee -a /file (append).
// Skipped: multiple output files, unsupported flags, relative paths.
func analyzeTeeCmd(
	stmt *syntax.Stmt,
	call *syntax.CallExpr,
	text string,
	resolveTargetPath func(rawTarget string) (resolvedPath string, resolvedHomePath bool, ok bool),
) analyzedCmd {
	other := analyzedCmd{cmdType: cmdTypeOther, text: text}

	targetPath, isAppend, ok := parseTeeTarget(call)
	if !ok {
		return other
	}

	targetPath, resolvedHomePath, ok := resolveFileCreationTarget(targetPath, resolveTargetPath)
	if !ok {
		return other
	}

	if !teeRedirectsAreCompatible(stmt, true) {
		return other
	}

	// Extract content from heredoc (same as cat — reads stdin)
	content, unsafe := extractCatHeredocContentFromStmt(stmt)

	return analyzedCmd{
		cmdType: cmdTypeFileCreation,
		text:    text,
		creation: &fileCreationCmd{
			targetPath:       targetPath,
			content:          content,
			isAppend:         isAppend,
			resolvedHomePath: resolvedHomePath,
		},
		hasUnsafe: unsafe,
	}
}

// parseTeeTarget parses tee's arguments and returns the single file target and
// whether -a (append) was set. Returns ok=false if the form cannot be safely
// converted to COPY (non-literal args, unknown flags, multiple output files).
func parseTeeTarget(call *syntax.CallExpr) (target string, isAppend bool, ok bool) {
	pastOptions := false
	for i := 1; i < len(call.Args); i++ {
		lit := call.Args[i].Lit()
		if lit == "" {
			return "", false, false
		}

		if lit == "--" {
			pastOptions = true
			continue
		}

		if !pastOptions && strings.HasPrefix(lit, "-") && lit != "-" {
			for _, r := range lit[1:] {
				switch r {
				case 'a':
					isAppend = true
				case 'i', 'p':
					// -i / -p are safe to ignore
				default:
					return "", false, false
				}
			}
			continue
		}

		if target != "" {
			return "", false, false
		}
		target = lit
	}
	return target, isAppend, true
}

// analyzeMkdirCmd parses a `mkdir` call and reports the single absolute
// literal target when the form is `mkdir -p /abs/path` (or `--parents`).
// Quoted paths work because the AST preserves the logical word regardless
// of quoting. Returns ok=false for any form we can't safely absorb:
//   - missing -p / --parents
//   - additional flags (e.g. -m, --mode, -Z, -v)
//   - multiple positional targets
//   - non-literal target (variable, command substitution)
//   - relative path
func analyzeMkdirCmd(call *syntax.CallExpr) (string, bool) {
	if call == nil || len(call.Args) < 3 {
		return "", false
	}
	sawP := false
	var target string
	var targetSet bool
	pastOptions := false
	for i := 1; i < len(call.Args); i++ {
		arg := call.Args[i]
		lit := arg.Lit()

		if !pastOptions && lit == "--" {
			pastOptions = true
			continue
		}

		// Flag tokens are always literal (they start with '-' before any quoting).
		if !pastOptions && lit != "" && strings.HasPrefix(lit, "-") && lit != "-" {
			switch lit {
			case "-p", "--parents":
				sawP = true
			default:
				// Unsupported flags (e.g. -m 0755, --mode=0755, -Z, -v) — skip fix.
				return "", false
			}
			continue
		}

		// Positional: extract the logical word content, which expands quoted
		// literals but still flags variable expansion / command substitution.
		content, unsafeWord := extractWordContent(arg, nil)
		if unsafeWord || content == "" {
			return "", false
		}
		if targetSet {
			return "", false
		}
		target = content
		targetSet = true
	}
	if !sawP || !targetSet {
		return "", false
	}
	if !path.IsAbs(target) {
		return "", false
	}
	return path.Clean(target), true
}

// isShellStateOnlyCmd reports whether a command name is known to only
// affect the current shell's options (e.g. `set -e`, `shopt -s nullglob`,
// `trap -`). Such commands never cross RUN boundaries — each RUN starts a
// fresh shell — so a leftover RUN that contains nothing but these can be
// dropped entirely.
func isShellStateOnlyCmd(name string) bool {
	switch name {
	case "set", "shopt", "trap":
		return true
	}
	return false
}

// teeRedirectsAreCompatible reports whether the redirects on a tee statement
// (or its enclosing pipeline end) can be safely absorbed into a COPY. tee
// echoes to stdout; a redirect to /dev/null is common and harmless, but
// anything else (a real file, stderr) would be a side effect we can't drop.
//
// allowInputHeredoc controls how a heredoc redirect is treated:
//   - true: heredoc is tee's stdin (direct `tee /file <<EOF ... EOF` form),
//     which is legitimate content we can reconstruct.
//   - false: the caller is a `producer | tee /file` pipeline. A heredoc
//     here would override the producer as tee's stdin (last input redirect
//     wins in bash), so we cannot faithfully reconstruct content from the
//     producer — reject the pattern.
func teeRedirectsAreCompatible(stmt *syntax.Stmt, allowInputHeredoc bool) bool {
	seenStdoutRedir := false
	for _, redir := range stmt.Redirs {
		switch redir.Op {
		case syntax.Hdoc, syntax.DashHdoc:
			if !allowInputHeredoc {
				return false
			}
			// Heredoc input — stdin source for direct tee.
		case syntax.RdrOut, syntax.AppOut:
			if seenStdoutRedir {
				return false
			}
			if redir.N != nil && redir.N.Value != "1" {
				return false
			}
			if extractRedirectTarget(redir) != "/dev/null" {
				return false
			}
			seenStdoutRedir = true
		case syntax.RdrIn, syntax.RdrInOut, syntax.DplIn, syntax.DplOut,
			syntax.RdrClob, syntax.AppClob, syntax.WordHdoc,
			syntax.RdrAll, syntax.RdrAllClob, syntax.AppAll, syntax.AppAllClob:
			return false
		}
	}
	return true
}

// analyzePipeToTee recognizes the pattern `producer | tee [-a] /path` and
// converts it to a file-creation analyzed command. The producer can be a
// single echo/cat/printf statement or a brace-group of such statements; their
// output is concatenated and piped into tee.
func analyzePipeToTee(
	stmt *syntax.Stmt,
	bin *syntax.BinaryCmd,
	knownVars func(name string) bool,
	options FileCreationOptions,
) (analyzedCmd, bool) {
	text := stmtToString(stmt)
	other := analyzedCmd{cmdType: cmdTypeOther, text: text}

	if bin.Y == nil || bin.Y.Cmd == nil {
		return other, false
	}
	teeCall, ok := bin.Y.Cmd.(*syntax.CallExpr)
	if !ok || len(teeCall.Args) == 0 || teeCall.Args[0].Lit() != cmdTee {
		return other, false
	}

	target, isAppend, ok := parseTeeTarget(teeCall)
	if !ok || target == "" {
		return other, false
	}

	target, resolvedHomePath, ok := resolveFileCreationTarget(target, options.ResolveTargetPath)
	if !ok {
		return other, false
	}

	// Disallow heredoc on the pipe's tee side: it would override `producer`
	// as tee's actual stdin, making producer content irrelevant.
	if !teeRedirectsAreCompatible(bin.Y, false) {
		return other, false
	}

	// Producer side: echo/cat/printf statements that emit the file's content.
	producers := flattenProducerStmts(bin.X)
	if len(producers) == 0 {
		return other, false
	}

	var content strings.Builder
	hasUnsafe := false
	for _, prod := range producers {
		pc, prodUnsafe, ok := producerStmtContent(prod, knownVars, options)
		if !ok {
			return other, false
		}
		if prodUnsafe {
			hasUnsafe = true
		}
		content.WriteString(pc)
	}

	return analyzedCmd{
		cmdType: cmdTypeFileCreation,
		text:    text,
		creation: &fileCreationCmd{
			targetPath:       target,
			content:          content.String(),
			isAppend:         isAppend,
			resolvedHomePath: resolvedHomePath,
		},
		hasUnsafe: hasUnsafe,
	}, true
}

// flattenProducerStmts returns the sequence of content-producing statements on
// the left side of a pipe. Brace groups (`{ a; b; }`) are flattened to their
// inner statements; a single statement is returned as-is.
func flattenProducerStmts(stmt *syntax.Stmt) []*syntax.Stmt {
	if stmt == nil || stmt.Cmd == nil {
		return nil
	}
	// A brace group can't carry its own redirects — the pipe already sinks
	// the whole group's output.
	if _, isBlock := stmt.Cmd.(*syntax.Block); isBlock {
		if len(stmt.Redirs) > 0 {
			return nil
		}
		return stmt.Cmd.(*syntax.Block).Stmts
	}
	return []*syntax.Stmt{stmt}
}

// producerStmtContent extracts the stdout content a single producer statement
// would emit. Only echo/cat/printf are recognized; any other command type
// makes the producer ineligible for COPY conversion.
func producerStmtContent(
	stmt *syntax.Stmt,
	knownVars func(name string) bool,
	options FileCreationOptions,
) (string, bool, bool) {
	if stmt == nil || stmt.Cmd == nil {
		return "", false, false
	}
	call, ok := stmt.Cmd.(*syntax.CallExpr)
	if !ok || len(call.Args) == 0 {
		return "", false, false
	}
	// Only a heredoc redirect (for cat <<EOF) is acceptable on a producer;
	// any stdout redirect would divert output away from the pipe, breaking
	// the model we're converting to COPY.
	for _, redir := range stmt.Redirs {
		switch redir.Op {
		case syntax.Hdoc, syntax.DashHdoc:
			// OK — handled below for cat.
		default:
			return "", false, false
		}
	}
	name := call.Args[0].Lit()
	switch name {
	case cmdEcho:
		content, unsafe := extractEchoContent(call, knownVars, options.InterpretPlainEchoEscapes)
		return content, unsafe, true
	case cmdPrintf:
		content, unsafe := extractPrintfContent(call, knownVars)
		return content, unsafe, true
	case cmdCat:
		if len(call.Args) > 1 {
			// `cat /some/file` (or `cat -flag`) reads from a source we
			// can't inline at lint time, so the producer's content is
			// statically unknown. Returning ok=false makes the whole
			// pipe fall through as opaque rather than creating a
			// file-creation slot with empty/misleading content.
			return "", false, false
		}
		content, unsafe := extractCatHeredocContentFromStmt(stmt)
		return content, unsafe, true
	}
	return "", false, false
}

func resolveFileCreationTarget(
	rawTargetPath string,
	resolveTargetPath func(rawTarget string) (resolvedPath string, resolvedHomePath bool, ok bool),
) (string, bool, bool) {
	if resolveTargetPath == nil {
		resolveTargetPath = defaultFileCreationTargetPath
	}
	return resolveTargetPath(rawTargetPath)
}

func defaultFileCreationTargetPath(rawTargetPath string) (string, bool, bool) {
	if rawTargetPath == "" || !path.IsAbs(rawTargetPath) {
		return "", false, false
	}
	return path.Clean(rawTargetPath), false, true
}

// findFileCreationBlock finds a contiguous block of file creation commands (+ chmod).
// Returns start index, end index, and target path. Returns -1, -1, "" if not found.
func findFileCreationBlock(commands []analyzedCmd) (int, int, string) {
	// Find first file creation
	startIdx := -1
	var targetPath string

	for i, cmd := range commands {
		if cmd.cmdType == cmdTypeFileCreation && cmd.creation != nil {
			startIdx = i
			targetPath = cmd.creation.targetPath
			break
		}
	}

	if startIdx == -1 {
		return -1, -1, ""
	}

	// Extend to include subsequent file creations to same file and chmod
	endIdx := startIdx
	for i := startIdx + 1; i < len(commands); i++ {
		cmd := commands[i]
		switch cmd.cmdType {
		case cmdTypeFileCreation:
			if cmd.creation != nil && cmd.creation.targetPath == targetPath {
				endIdx = i
			} else {
				return startIdx, endIdx, targetPath // Different file, stop
			}
		case cmdTypeChmod:
			if cmd.chmodTarget == targetPath {
				endIdx = i
			} else {
				return startIdx, endIdx, targetPath // Different target, stop
			}
		case cmdTypeOther, cmdTypeUmask, cmdTypeMkdirP:
			// Other commands, umask, or mkdir after file creation don't
			// affect the file we're building.
			return startIdx, endIdx, targetPath
		}
	}

	return startIdx, endIdx, targetPath
}

// stmtToString converts a statement back to string form.
func stmtToString(stmt *syntax.Stmt) string {
	var buf strings.Builder
	printer := syntax.NewPrinter()
	_ = printer.Print(&buf, stmt)
	return strings.TrimSpace(buf.String())
}

// parseChmod extracts mode, raw mode string, and target from a chmod command.
// Returns (0, "", "", false) if the chmod cannot be converted (e.g., recursive, multiple targets).
// The ok return signals that a valid mode token was parsed (needed because mode 000 is valid).
func parseChmod(call *syntax.CallExpr) (uint16, string, string, bool) {
	if len(call.Args) < 3 {
		return 0, "", "", false
	}

	// Skip chmod itself
	args := call.Args[1:]

	var mode uint16
	var rawMode, target string
	var seenMode, seenTarget bool

	// Look for mode and target, skipping flags
	for _, arg := range args {
		lit := arg.Lit()
		if lit == "" {
			continue
		}

		// Skip flags (including -R for recursive)
		if strings.HasPrefix(lit, "-") {
			if strings.Contains(lit, "R") {
				return 0, "", "", false // Recursive chmod
			}
			continue
		}

		// Check if this is an octal mode
		if IsOctalMode(lit) {
			mode = ParseOctalMode(lit)
			rawMode = lit
			seenMode = true
			continue
		}

		// Check if this is symbolic mode (e.g., +x, u+rwx)
		if IsSymbolicMode(lit) {
			converted := ApplySymbolicMode(lit, defaultFileMode)
			if converted == 0 {
				return 0, "", "", false // Unsupported (e.g., +X, +s, +t)
			}
			mode = converted
			rawMode = lit
			seenMode = true
			continue
		}

		// Must be a target path (only after a mode has been seen)
		if seenMode {
			if seenTarget {
				return 0, "", "", false // Multiple targets
			}
			target = lit
			seenTarget = true
			continue
		}
	}

	return mode, rawMode, target, seenMode && seenTarget
}

// parseUmask extracts the umask value from a umask command.
// Returns (value, true) if successful, (0, false) if not parseable.
// Only supports octal umask values (e.g., "umask 077", "umask 0077").
func parseUmask(call *syntax.CallExpr) (uint16, bool) {
	// umask [mask]
	// We only support octal masks, not symbolic modes
	if len(call.Args) != 2 {
		return 0, false // "umask" alone (print current) or too many args
	}

	maskStr := call.Args[1].Lit()
	if maskStr == "" || !IsOctalMode(maskStr) {
		return 0, false // Symbolic mode or variable
	}

	return ParseOctalMode(maskStr), true
}

// octalModeRegex matches octal chmod modes (3 or 4 digits).
// Supports standard modes (755, 0755) and special modes (1755 sticky, 2755 setgid, 4755 setuid).
var octalModeRegex = regexp.MustCompile(`^[0-7]{3,4}$`)

// IsOctalMode checks if a string is a valid octal chmod mode.
func IsOctalMode(s string) bool {
	return octalModeRegex.MatchString(s)
}

// symbolicModeRegex matches symbolic chmod modes.
var symbolicModeRegex = regexp.MustCompile(`^[ugoa]*[\-+=][rwxXst]+$`)

// defaultFileMode is the typical mode for newly created files (0666 & ~0022 umask).
const defaultFileMode = 0o644

// defaultUmask is the typical umask value (0o022).
const defaultUmask = 0o022

// ApplySymbolicMode converts a symbolic chmod mode to octal, given a base mode.
// Returns 0 if the mode cannot be converted.
// Supports: [ugoa]*[+-=][rwx]+ (not X, s, t which are complex/rare).
func ApplySymbolicMode(symbolic string, baseMode uint16) uint16 {
	if len(symbolic) < 2 {
		return 0
	}

	// Find the operator position
	opIdx := strings.IndexAny(symbolic, "+-=")
	if opIdx == -1 {
		return 0
	}

	who := symbolic[:opIdx]
	op := symbolic[opIdx]
	perms := symbolic[opIdx+1:]

	// Parse who (empty = all)
	var whoMask int
	if who == "" || strings.Contains(who, "a") {
		whoMask = 0o777 // all
	} else {
		if strings.Contains(who, "u") {
			whoMask |= 0o700
		}
		if strings.Contains(who, "g") {
			whoMask |= 0o070
		}
		if strings.Contains(who, "o") {
			whoMask |= 0o007
		}
	}

	// Parse permissions
	var permBits int
	for _, c := range perms {
		switch c {
		case 'r':
			permBits |= 0o444
		case 'w':
			permBits |= 0o222
		case 'x':
			permBits |= 0o111
		case 'X', 's', 't':
			// Not supported - these have complex semantics
			return 0
		}
	}

	// Apply to the who mask
	permBits &= whoMask

	// Apply the operator
	base := int(baseMode)
	var result int
	switch op {
	case '+':
		result = base | permBits
	case '-':
		result = base &^ permBits
	case '=':
		// Clear the who bits first, then set
		result = (base &^ whoMask) | permBits
	default:
		return 0
	}

	// Ensure result is within valid chmod mode range (0o0000 to 0o7777)
	if result < 0 || result > 0o7777 {
		return 0
	}
	return uint16(result)
}

// IsSymbolicMode checks if a string is a symbolic chmod mode.
func IsSymbolicMode(s string) bool {
	return symbolicModeRegex.MatchString(s)
}

// extractRedirectTarget extracts the target path from a redirect.
func extractRedirectTarget(redir *syntax.Redirect) string {
	if redir.Word == nil {
		return ""
	}

	// Only handle literal paths
	return redir.Word.Lit()
}

// extractFileContent extracts the content from a file creation command.
// Returns the content and whether unsafe variables were found.
func extractFileContent(
	stmt *syntax.Stmt,
	call *syntax.CallExpr,
	knownVars func(name string) bool,
	options FileCreationOptions,
) (string, bool) {
	cmdName := call.Args[0].Lit()

	switch cmdName {
	case cmdEcho:
		return extractEchoContent(call, knownVars, options.InterpretPlainEchoEscapes)
	case cmdCat:
		// Only heredoc-only cat is safe (e.g., "cat <<EOF > /file")
		// cat with extra args (e.g., "cat /etc/hosts > /file" or "cat -n <<EOF") is unsafe
		// since we can't determine the content at lint time
		if len(call.Args) > 1 {
			return "", true // Mark as unsafe
		}
		return extractCatHeredocContentFromStmt(stmt)
	case cmdPrintf:
		return extractPrintfContent(call, knownVars)
	}

	return "", false
}

// extractCatHeredocContentFromStmt finds and extracts heredoc content from a cat statement.
func extractCatHeredocContentFromStmt(stmt *syntax.Stmt) (string, bool) {
	// Find the heredoc redirect - reject multiple heredocs as ambiguous
	// (bash uses the last input redirect when multiple are present)
	var hdoc *syntax.Redirect
	for _, redir := range stmt.Redirs {
		if redir.Op == syntax.Hdoc || redir.Op == syntax.DashHdoc {
			if hdoc != nil {
				return "", true // multiple heredocs are ambiguous
			}
			hdoc = redir
		}
	}
	if hdoc != nil {
		return extractCatHeredocContent(hdoc)
	}
	// No heredoc found - cat without heredoc creates empty file
	return "", false
}

// extractEchoContent extracts content from an echo command.
func extractEchoContent(
	call *syntax.CallExpr,
	knownVars func(name string) bool,
	interpretEscapes bool,
) (string, bool) {
	if len(call.Args) == 1 {
		// echo with no args prints a newline
		return "\n", false
	}

	// Check for -e flag (escape sequences) - skip for now
	// Check for -n flag (no newline) - handle specially
	hasNoNewline := false
	hasEscape := false
	startIdx := 1

	for i := 1; i < len(call.Args); i++ {
		lit := call.Args[i].Lit()
		// Not an option: no dash prefix or bare "-" (often represents stdin/stdout)
		if !strings.HasPrefix(lit, "-") || lit == "-" {
			startIdx = i
			break
		}
		// "--" ends option parsing
		if lit == "--" {
			startIdx = i + 1
			break
		}
		// Parse known echo options, reject unknown flags
		for _, r := range strings.TrimPrefix(lit, "-") {
			switch r {
			case 'n':
				hasNoNewline = true
			case 'e', 'E':
				hasEscape = true
			default:
				// Unknown option letter (e.g., -x) - mark unsafe
				return "", true
			}
		}
		startIdx = i + 1
	}

	// Skip -e for now (complex escape handling)
	if hasEscape {
		return "", true
	}

	// echo -n produces no trailing newline; COPY heredoc can't represent that
	if hasNoNewline {
		return "", true
	}

	var content strings.Builder
	hasUnsafe := false

	for i := startIdx; i < len(call.Args); i++ {
		if i > startIdx {
			content.WriteString(" ")
		}

		argContent, unsafe := extractWordContent(call.Args[i], knownVars)
		if unsafe {
			hasUnsafe = true
		}
		content.WriteString(argContent)
	}

	result := content.String()
	if interpretEscapes {
		processed, ok := processPrintfEscapes(result)
		if !ok {
			return "", true
		}
		result = processed
	}
	result += "\n"

	return result, hasUnsafe
}

// isComplexExpansion checks if a ParamExp uses complex expansion features
// (e.g., ${#VAR}, ${VAR:-default}, ${VAR:0:5}, etc.)
// Mirrors the inverse of the unexported ParamExp.simple() upstream.
func isComplexExpansion(p *syntax.ParamExp) bool {
	return p.Flags != nil ||
		p.Excl || p.Length || p.Width || p.IsSet ||
		p.NestedParam != nil || p.Index != nil ||
		len(p.Modifiers) != 0 || p.Slice != nil ||
		p.Repl != nil || p.Names != 0 || p.Exp != nil
}

// extractParamExpContent handles parameter expansion, writing to content and returning unsafe status.
func extractParamExpContent(p *syntax.ParamExp, content *strings.Builder, knownVars func(name string) bool) bool {
	varName := p.Param.Value
	if knownVars != nil && knownVars(varName) {
		// Known ARG/ENV - can be preserved in COPY heredoc
		// Always brace to avoid $VARsuffix ambiguity
		content.WriteString("${")
		content.WriteString(varName)
		content.WriteString("}")
		return isComplexExpansion(p)
	}
	content.WriteString("$")
	content.WriteString(varName)
	return true
}

// extractWordContent extracts the literal content from a word.
func extractWordContent(word *syntax.Word, knownVars func(name string) bool) (string, bool) {
	var content strings.Builder
	hasUnsafe := false

	for _, part := range word.Parts {
		switch p := part.(type) {
		case *syntax.Lit:
			content.WriteString(p.Value)
		case *syntax.SglQuoted:
			content.WriteString(p.Value)
		case *syntax.DblQuoted:
			for _, dpart := range p.Parts {
				switch dp := dpart.(type) {
				case *syntax.Lit:
					content.WriteString(dp.Value)
				case *syntax.ParamExp:
					if extractParamExpContent(dp, &content, knownVars) {
						hasUnsafe = true
					}
				default:
					// Command substitution, arithmetic, etc.
					hasUnsafe = true
				}
			}
		case *syntax.ParamExp:
			if extractParamExpContent(p, &content, knownVars) {
				hasUnsafe = true
			}
		default:
			// Command substitution, arithmetic, etc.
			hasUnsafe = true
		}
	}

	return content.String(), hasUnsafe
}

// extractCatHeredocContent extracts content from a cat heredoc.
func extractCatHeredocContent(redir *syntax.Redirect) (string, bool) {
	if redir.Hdoc == nil {
		return "", false
	}

	// Get heredoc content
	var content strings.Builder
	for _, part := range redir.Hdoc.Parts {
		switch p := part.(type) {
		case *syntax.Lit:
			content.WriteString(p.Value)
		default:
			// Variable expansion in heredoc - mark as potentially unsafe
			return content.String(), true
		}
	}

	result := content.String()
	// <<- (DashHdoc) strips leading tabs from each line
	if redir.Op == syntax.DashHdoc {
		lines := strings.Split(result, "\n")
		for i, line := range lines {
			lines[i] = strings.TrimLeft(line, "\t")
		}
		result = strings.Join(lines, "\n")
	}
	return result, false
}

// extractPrintfContent extracts content from a printf command.
// Handles literal format strings with escape sequences (\n, \t, \\, \r)
// and optional single %s format specifier.
func extractPrintfContent(call *syntax.CallExpr, knownVars func(name string) bool) (string, bool) {
	if len(call.Args) < 2 {
		return "", false
	}

	// Extract format string content (use extractWordContent instead of Lit()
	// because Lit() returns "" for single-quoted words in this parser version).
	format, formatUnsafe := extractWordContent(call.Args[1], knownVars)
	if formatUnsafe {
		return "", true
	}
	if format == "" {
		return "", true // empty format — no content
	}

	// Scan format string for specifiers.
	// Allow: %s (single, string substitution) and %% (literal percent).
	// Reject everything else (%d, %f, %x, etc.).
	hasPercentS := false
	for i := 0; i < len(format); i++ {
		if format[i] != '%' {
			continue
		}
		if i+1 >= len(format) {
			return "", true // trailing %
		}
		switch format[i+1] {
		case 's':
			if hasPercentS {
				return "", true // multiple %s — unsafe
			}
			hasPercentS = true
			i++ // skip 's'
		case '%':
			i++ // skip second '%' — literal percent
		default:
			return "", true // unsupported format specifier
		}
	}

	if hasPercentS {
		if len(call.Args) != 3 {
			return "", true // need exactly 1 argument for %s
		}
		argContent, unsafe := extractWordContent(call.Args[2], knownVars)
		if unsafe {
			return "", true
		}
		// Process escape sequences in format
		processed, ok := processPrintfEscapes(format)
		if !ok {
			return "", true
		}
		// Substitute: %s → argument, %% → %
		processed = strings.Replace(processed, "%s", argContent, 1)
		processed = strings.ReplaceAll(processed, "%%", "%")
		if !strings.HasSuffix(processed, "\n") {
			return "", true
		}
		return processed, false
	}

	// No %s format specifiers
	if len(call.Args) != 2 {
		return "", true // extra args repeat format; unsafe
	}

	// Process printf escape sequences (\n, \t, \\, \r)
	processed, ok := processPrintfEscapes(format)
	if !ok {
		return "", true
	}
	// Handle %% → %
	processed = strings.ReplaceAll(processed, "%%", "%")

	// printf doesn't add trailing newline; COPY heredoc always ends with one
	if !strings.HasSuffix(processed, "\n") {
		return "", true
	}

	return processed, false
}

// processPrintfEscapes interprets printf-style backslash escape sequences.
// Handles: \n (newline), \t (tab), \\ (backslash), \r (carriage return).
// Returns the processed string and true on success.
// Returns ("", false) for unrecognized escape sequences (e.g., \0NNN, \xHH).
func processPrintfEscapes(s string) (string, bool) {
	if !strings.ContainsRune(s, '\\') {
		return s, true // fast path: no escapes
	}

	var buf strings.Builder
	buf.Grow(len(s))

	for i := 0; i < len(s); i++ {
		if s[i] != '\\' {
			buf.WriteByte(s[i])
			continue
		}
		if i+1 >= len(s) {
			return "", false // trailing backslash
		}
		switch s[i+1] {
		case 'n':
			buf.WriteByte('\n')
		case 't':
			buf.WriteByte('\t')
		case '\\':
			buf.WriteByte('\\')
		case 'r':
			buf.WriteByte('\r')
		default:
			return "", false // unsupported escape
		}
		i++ // skip the escaped character
	}

	return buf.String(), true
}

// ParseOctalMode parses an octal mode string (e.g., "755", "0755") to uint16.
// Returns 0 for invalid input.
func ParseOctalMode(s string) uint16 {
	if s == "" {
		return 0
	}
	var mode uint64
	_, err := fmt.Sscanf(s, "%o", &mode)
	if err != nil || mode > 0o7777 {
		return 0
	}
	return uint16(mode)
}

// FormatOctalMode formats a chmod mode as a 4-digit octal string.
// E.g., 0o755 -> "0755", 0o644 -> "0644"
// Returns empty string for 0 (no mode).
func FormatOctalMode(mode uint16) string {
	if mode == 0 {
		return ""
	}
	return fmt.Sprintf("%04o", mode)
}

// MergeChmodModes computes the resulting mode when a chmod operation is applied
// on top of an existing mode string (e.g. from COPY --chmod).
//
// If chmodRaw is an absolute octal mode (e.g. "755"), it overrides existingMode entirely.
// If chmodRaw is symbolic (e.g. "+x"), it is applied on top of existingMode.
// Returns the resulting mode string and true on success, or ("", false) if parsing fails.
func MergeChmodModes(existingMode, chmodRaw string) (string, bool) {
	// Parse the existing base mode
	var base uint16
	switch {
	case IsOctalMode(existingMode):
		base = ParseOctalMode(existingMode)
	case IsSymbolicMode(existingMode):
		base = ApplySymbolicMode(existingMode, defaultFileMode)
	default:
		return "", false
	}
	if base == 0 {
		return "", false
	}

	// Apply the new chmod
	if IsOctalMode(chmodRaw) {
		// Absolute set — new mode replaces existing entirely
		return chmodRaw, true
	}
	if IsSymbolicMode(chmodRaw) {
		result := ApplySymbolicMode(chmodRaw, base)
		if result == 0 {
			return "", false
		}
		return FormatOctalMode(result), true
	}
	return "", false
}
