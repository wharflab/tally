package fix

import (
	"bytes"
	"context"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/command"
	"github.com/moby/buildkit/frontend/dockerfile/instructions"
	"github.com/moby/buildkit/frontend/dockerfile/parser"

	"github.com/wharflab/tally/internal/dockerfile"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/semantic"
	"github.com/wharflab/tally/internal/shell"
	"github.com/wharflab/tally/internal/sourcemap"
)

// dl4001CleanupResolver drops a tool's install entry and any lingering config
// artifacts after DL4001's sync curl↔wget invocation rewrites run.
//
// Running as a post-sync resolver lets the cleanup see the file after other
// rules (e.g. sort-packages re-ordering the install list) have already applied
// their edits, which is what lets us safely remove the tool without colliding
// with those earlier edits.
type dl4001CleanupResolver struct{}

func (r *dl4001CleanupResolver) ID() string { return rules.DL4001CleanupResolverID }

func (r *dl4001CleanupResolver) Resolve(
	_ context.Context,
	resolveCtx ResolveContext,
	fix *rules.SuggestedFix,
) ([]rules.TextEdit, error) {
	data, ok := fix.ResolverData.(*rules.DL4001CleanupResolveData)
	if !ok || data == nil || data.SourceTool == "" {
		return nil, nil
	}

	parseResult, err := dockerfile.Parse(bytes.NewReader(resolveCtx.Content), nil)
	if err != nil {
		return nil, nil //nolint:nilerr // best-effort cleanup; skip silently on parse errors
	}
	sm := sourcemap.New(resolveCtx.Content)

	ctx := cleanupCtx{
		file:        resolveCtx.FilePath,
		parseResult: parseResult,
		sm:          sm,
		sem:         semantic.NewBuilder(parseResult, nil, resolveCtx.FilePath).Build(),
		sourceTool:  data.SourceTool,
		escapeToken: parseEscapeToken(parseResult),
	}

	// Safety gate: if the Dockerfile still invokes the source tool after the
	// sync pass (commands that fell outside the deterministic subset, AI
	// fallbacks that didn't resolve, etc.), removing the install or config
	// artifacts would turn the build into "tool: command not found". Skip
	// the whole cleanup in that case — the user will see the remaining
	// DL4001 violations and can deal with them manually.
	if r.stillInvokesSourceTool(ctx) {
		return nil, nil
	}

	var edits []rules.TextEdit
	for stageIdx, stage := range parseResult.Stages {
		edits = append(edits, r.installEdits(ctx, stage, stageIdx)...)
	}
	edits = append(edits, r.configArtifactEdits(resolveCtx.FilePath, parseResult, sm, data.SourceTool)...)
	return edits, nil
}

// stillInvokesSourceTool scans every RUN in every stage for a remaining
// invocation of the evicted tool. An invocation is a shell command whose
// basename matches the tool (stripping ".exe" on Windows shells). The scan
// uses the already-parsed AST and the per-stage shell variant so it picks
// up cmd/PowerShell calls too.
func (r *dl4001CleanupResolver) stillInvokesSourceTool(ctx cleanupCtx) bool {
	for stageIdx, stage := range ctx.parseResult.Stages {
		stageInfo := ctx.sem.StageInfo(stageIdx)
		for _, cmd := range stage.Commands {
			run, ok := cmd.(*instructions.RunCommand)
			if !ok {
				continue
			}
			if !run.PrependShell && len(run.Files) == 0 {
				continue
			}
			variant := runShellVariant(run, stageInfo)
			script, _ := resolveRunScript(run, ctx.sm, ctx.escapeToken, variant)
			if script == "" {
				continue
			}
			for _, c := range shell.FindCommands(script, variant, ctx.sourceTool) {
				if c.Name == ctx.sourceTool {
					return true
				}
			}
		}
	}
	return false
}

// cleanupCtx bundles per-Resolve state so helpers don't need long argument lists.
type cleanupCtx struct {
	file        string
	parseResult *dockerfile.ParseResult
	sm          *sourcemap.SourceMap
	sem         *semantic.Model
	sourceTool  string
	escapeToken rune
}

func parseEscapeToken(parseResult *dockerfile.ParseResult) rune {
	if parseResult != nil && parseResult.AST != nil && parseResult.AST.EscapeToken != 0 {
		return parseResult.AST.EscapeToken
	}
	return '\\'
}

// installEdits emits edits that drop ctx.sourceTool from every install command
// in the stage. When the install has other packages, only the matching package
// token is deleted (plus adjacent whitespace). When the install has just that
// one package, we fall back to deleting the whole install subcommand (including
// a leading "&&" glue) or the whole RUN when the install is the only command.
func (r *dl4001CleanupResolver) installEdits(
	ctx cleanupCtx,
	stage instructions.Stage,
	stageIdx int,
) []rules.TextEdit {
	nodes := stageASTChildren(stageIdx, ctx.parseResult.AST)
	if len(nodes) == 0 {
		return nil
	}
	stageInfo := ctx.sem.StageInfo(stageIdx)
	var edits []rules.TextEdit

	runIdx := -1
	for _, cmd := range stage.Commands {
		runIdx++
		run, ok := cmd.(*instructions.RunCommand)
		if !ok {
			continue
		}
		if !run.PrependShell && len(run.Files) == 0 {
			continue
		}
		if len(run.Location()) == 0 {
			continue
		}
		nodeIdx := runIdx + 1
		if nodeIdx >= len(nodes) {
			continue
		}
		node := nodes[nodeIdx]

		variant := runShellVariant(run, stageInfo)
		script, startLine := resolveRunScript(run, ctx.sm, ctx.escapeToken, variant)
		if script == "" {
			continue
		}
		installs := shell.FindInstallPackages(script, variant)
		if len(installs) == 0 {
			continue
		}

		// Column adjustment applies only to non-heredoc shell-form RUNs, where
		// the script's line 0 starts after "RUN " plus any flags on the same
		// Dockerfile line. Heredoc bodies start at column 0 of the line after
		// the "RUN <<EOF" opener, so there's no prefix to account for.
		cmdStartCol := 0
		if len(run.Files) == 0 {
			firstLine := ctx.sm.Line(run.Location()[0].Start.Line - 1)
			cmdStartCol = shell.DockerfileRunCommandStartCol(firstLine)
		}

		runHasOtherPackages := installRunHasOtherPackages(installs, ctx.sourceTool)
		for _, ic := range installs {
			hit, _ := findInstallPackageIndex(ic.Packages, ctx.sourceTool)
			if hit < 0 {
				continue
			}
			if len(ic.Packages) >= 2 {
				edits = append(edits, rules.TextEdit{
					Location: dl4001PackageDeleteLocation(ctx.file, ic.Packages, hit, startLine, cmdStartCol),
					NewText:  "",
				})
				continue
			}

			if !runHasOtherPackages && isRunFullyInstallSubcommand(script, variant) {
				edits = append(edits, rules.TextEdit{
					Location: dl4001DeleteInstruction(ctx.file, node, ctx.sm),
					NewText:  "",
				})
				continue
			}
			// Single-package install inside a multi-step RUN (e.g.
			// "apt-get update && apt-get install -y wget && apt-get clean"):
			// produce a narrow delete that drops just the install subcommand
			// and one adjacent && separator, leaving the other commands intact.
			if loc, ok := installSubcommandDeleteLocation(
				ctx.file, script, ic, ctx.sourceTool,
				variant, startLine, cmdStartCol,
			); ok {
				edits = append(edits, rules.TextEdit{Location: loc, NewText: ""})
			}
		}
	}
	return edits
}

// configArtifactEdits removes config artifacts for the evicted tool:
// COPY heredoc inserts for .curlrc/.wgetrc, ENV bindings for config paths,
// and any tally-authored annotation comments that immediately precede them.
func (r *dl4001CleanupResolver) configArtifactEdits(
	file string,
	parseResult *dockerfile.ParseResult,
	sm *sourcemap.SourceMap,
	sourceTool string,
) []rules.TextEdit {
	children := parseResult.AST.AST.Children
	var edits []rules.TextEdit
	for _, node := range children {
		if !nodeIsConfigArtifactForTool(node, sourceTool) {
			continue
		}
		edits = append(edits, rules.TextEdit{
			Location: dl4001DeleteInstruction(file, node, sm),
			NewText:  "",
		})
	}
	return edits
}

func nodeIsConfigArtifactForTool(node *parser.Node, sourceTool string) bool {
	if node == nil {
		return false
	}
	switch strings.ToLower(node.Value) {
	case command.Env:
		return envBindsToToolConfig(node, sourceTool)
	case command.Copy, command.Add:
		return copyTargetsToolConfig(node, sourceTool)
	}
	return false
}

// envBindsToToolConfig reports whether an ENV instruction is dedicated to the
// tool-specific config env vars (CURL_HOME, CURLHOME, WGETRC, WGETHOSTS). It
// returns true only when EVERY key in the instruction is a tool-config key;
// an ENV that mixes tool config with unrelated bindings is left alone so we
// don't drop the caller's data. Surgical intra-ENV editing (keeping some
// pairs, deleting others) is intentionally out of scope for this resolver —
// the risk of mis-quoting or splitting line-continued ENV instructions
// outweighs the cosmetic win of removing just the tool key.
func envBindsToToolConfig(node *parser.Node, sourceTool string) bool {
	keys := extractEnvKeys(node)
	if len(keys) == 0 {
		return false
	}
	for _, key := range keys {
		if !isToolConfigEnvKey(key, sourceTool) {
			return false
		}
	}
	return true
}

func isToolConfigEnvKey(key, sourceTool string) bool {
	k := strings.ToUpper(key)
	switch sourceTool {
	case "curl":
		return k == "CURL_HOME" || k == "CURLHOME"
	case "wget":
		return k == "WGETRC" || k == "WGETHOSTS"
	}
	return false
}

// extractEnvKeys returns the keys in an ENV instruction. BuildKit's parser
// represents ENV as a chain of (key, value, separator) triples for both
// "ENV KEY=VALUE" and legacy "ENV KEY VALUE" forms (the trailing separator
// node is "=" in the first form, empty in the second).
func extractEnvKeys(node *parser.Node) []string {
	var keys []string
	n := node.Next
	for n != nil {
		keys = append(keys, n.Value)
		// Advance past value and separator nodes if present.
		for step := 0; step < 2 && n.Next != nil; step++ {
			n = n.Next
		}
		n = n.Next
	}
	return keys
}

// copyTargetsToolConfig reports whether a COPY/ADD instruction writes a known
// config file for the tool. Heredoc COPYs end up with the destination as the
// last non-flag argument. Two destination shapes count as a match:
//   - a literal path whose suffix is the tool's config filename
//     (e.g. `/etc/.wgetrc`, `/root/.curlrc`, or `${CURL_HOME}/.curlrc`
//     where the ${VAR} prefix is stripped);
//   - a bare env-var reference to the tool's config env var itself
//     (e.g. `${WGETRC}` or `${CURL_HOME}`) — the final path is guaranteed
//     to be the tool's config file by construction.
func copyTargetsToolConfig(node *parser.Node, sourceTool string) bool {
	args := copyArgs(node)
	if len(args) == 0 {
		return false
	}
	dst := args[len(args)-1]

	if name, ok := bareEnvVarName(dst); ok && isToolConfigEnvKey(name, sourceTool) {
		return true
	}

	dst = stripEnvReference(dst)
	dstLower := strings.ToLower(dst)
	switch sourceTool {
	case "curl":
		return strings.HasSuffix(dstLower, "/.curlrc") ||
			strings.HasSuffix(dstLower, "/_curlrc")
	case "wget":
		return strings.HasSuffix(dstLower, "/.wgetrc") ||
			strings.HasSuffix(dstLower, "/etc/wgetrc")
	}
	return false
}

// bareEnvVarName returns the variable name for a destination that is entirely
// an env-var reference with no literal path component (e.g. "${WGETRC}",
// "$CURL_HOME"). Returns false for empty strings, literal paths, or mixed
// forms like "${CURL_HOME}/.curlrc" — those are handled by stripEnvReference
// + suffix matching instead.
func bareEnvVarName(s string) (string, bool) {
	if !strings.HasPrefix(s, "$") {
		return "", false
	}
	inner := s[1:]
	if strings.HasPrefix(inner, "{") && strings.HasSuffix(inner, "}") {
		inner = inner[1 : len(inner)-1]
	}
	if inner == "" || strings.ContainsAny(inner, "/\\${}") {
		return "", false
	}
	return inner, true
}

// copyArgs returns the non-flag arguments for a COPY/ADD node.
func copyArgs(node *parser.Node) []string {
	var args []string
	for n := node.Next; n != nil; n = n.Next {
		if strings.HasPrefix(n.Value, "--") {
			continue
		}
		args = append(args, n.Value)
	}
	return args
}

// stripEnvReference trims ${VAR} prefixes so "${CURL_HOME}/.curlrc" is
// treated as "/.curlrc" for suffix matching.
func stripEnvReference(s string) string {
	for strings.HasPrefix(s, "$") {
		end := strings.IndexAny(s, "/\\")
		if end < 0 {
			return s
		}
		s = s[end:]
	}
	return s
}

// dl4001DeleteInstruction returns a location that deletes the full span of
// node (including any trailing newline) plus its preceding tally-authored
// annotation comment, if present.
func dl4001DeleteInstruction(
	file string,
	node *parser.Node,
	sm *sourcemap.SourceMap,
) rules.Location {
	startLine := sm.EffectiveStartLine(node.StartLine, node.PrevComment)
	endLine := sm.ResolveEndLine(node.EndLine)
	return rules.NewRangeLocation(file, startLine, 0, endLine+1, 0)
}

// installSubcommandDeleteLocation returns a narrow delete range covering one
// install subcommand and its adjacent "&&"/"||"/";" glue inside a chained RUN
// (e.g. removes "apt-get install -y wget && " from
// "apt-get update && apt-get install -y wget && apt-get clean").
//
// The returned range is anchored at the install's CommandInfo span (from
// shell.FindCommands, which already gives us start/end positions); the start
// or end is then extended to absorb one adjacent separator so we don't leave
// a dangling "&&" behind. Trailing separator is preferred (keeps the chain's
// first segment canonical); leading separator is consumed only when the
// install is the last statement in the chain.
//
// ic is the already-identified InstallCommand whose subcommand matches a
// recognized install verb ("install"/"add"/"require"/"i"); it's used to
// correlate the right CommandInfo when a script contains multiple manager
// invocations (e.g. apt-get update vs apt-get install).
func installSubcommandDeleteLocation(
	file, script string, ic shell.InstallCommand, tool string,
	variant shell.Variant,
	startLine, cmdStartCol int,
) (rules.Location, bool) {
	cmds := shell.FindCommands(script, variant, ic.Manager)
	for _, c := range cmds {
		if !c.HasCommandRange {
			continue
		}
		// Correlate by subcommand + presence of the target package token.
		if c.Subcommand != ic.Subcommand || !commandMentionsTool(c, tool) {
			continue
		}
		// Script-relative command span.
		startScriptLine, startScriptCol := c.Line, c.StartCol
		endScriptLine, endScriptCol := c.CommandEndLine, c.CommandEndCol

		// Extend end to eat one trailing && / || / ; separator (preferred).
		extended := false
		if end, ok := nextSeparatorAfter(script, endScriptLine, endScriptCol); ok {
			endScriptLine, endScriptCol = end.line, end.col
			extended = true
		}
		// Otherwise, try a preceding separator so we don't leave "&& <removed>".
		if !extended {
			if start, ok := prevSeparatorBefore(script, startScriptLine, startScriptCol); ok {
				startScriptLine, startScriptCol = start.line, start.col
			}
		}

		docStartLine := startLine + startScriptLine
		docEndLine := startLine + endScriptLine
		docStartCol := startScriptCol
		docEndCol := endScriptCol
		if startScriptLine == 0 {
			docStartCol += cmdStartCol
		}
		if endScriptLine == 0 {
			docEndCol += cmdStartCol
		}
		return rules.NewRangeLocation(file, docStartLine, docStartCol, docEndLine, docEndCol), true
	}
	return rules.Location{}, false
}

// commandMentionsTool reports whether a CommandInfo names the evicted tool
// as a positional argument (stripping version suffixes). Used to distinguish
// the "apt-get install -y wget" invocation from a sibling "apt-get update".
func commandMentionsTool(cmd shell.CommandInfo, tool string) bool {
	for _, arg := range cmd.Args {
		if strings.HasPrefix(arg, "-") {
			continue
		}
		if strings.EqualFold(shell.StripPackageVersion(arg), tool) {
			return true
		}
	}
	return false
}

// scriptPos is a 0-based (line, col) script-relative position.
type scriptPos struct{ line, col int }

// nextSeparatorAfter walks script bytes starting at (line, col) and, if the
// next non-whitespace run begins with "&&", "||", or ";", returns the
// position just after the separator (and any trailing whitespace up to the
// next command token). Returns false if no separator follows.
func nextSeparatorAfter(script string, line, col int) (scriptPos, bool) {
	offset := scriptOffset(script, line, col)
	if offset < 0 {
		return scriptPos{}, false
	}
	// Skip whitespace.
	wsEnd := offset
	for wsEnd < len(script) && (script[wsEnd] == ' ' || script[wsEnd] == '\t') {
		wsEnd++
	}
	sepLen := 0
	switch {
	case strings.HasPrefix(script[wsEnd:], "&&"):
		sepLen = 2
	case strings.HasPrefix(script[wsEnd:], "||"):
		sepLen = 2
	case wsEnd < len(script) && script[wsEnd] == ';':
		sepLen = 1
	default:
		return scriptPos{}, false
	}
	// Consume any whitespace following the separator so the remaining chain
	// stays flush with how the script was formatted.
	postEnd := wsEnd + sepLen
	for postEnd < len(script) && (script[postEnd] == ' ' || script[postEnd] == '\t') {
		postEnd++
	}
	endLine, endCol := scriptLineCol(script, postEnd)
	return scriptPos{line: endLine, col: endCol}, true
}

// prevSeparatorBefore walks script bytes backward from (line, col) and, if
// the preceding non-whitespace run ends with "&&", "||", or ";", returns the
// position just before that separator's leading whitespace.
func prevSeparatorBefore(script string, line, col int) (scriptPos, bool) {
	offset := scriptOffset(script, line, col)
	if offset < 0 {
		return scriptPos{}, false
	}
	i := offset
	// Walk back over whitespace.
	for i > 0 && (script[i-1] == ' ' || script[i-1] == '\t') {
		i--
	}
	sepLen := 0
	switch {
	case i >= 2 && script[i-2:i] == "&&":
		sepLen = 2
	case i >= 2 && script[i-2:i] == "||":
		sepLen = 2
	case i >= 1 && script[i-1] == ';':
		sepLen = 1
	default:
		return scriptPos{}, false
	}
	// Consume whitespace before the separator too.
	j := i - sepLen
	for j > 0 && (script[j-1] == ' ' || script[j-1] == '\t') {
		j--
	}
	startLine, startCol := scriptLineCol(script, j)
	return scriptPos{line: startLine, col: startCol}, true
}

// scriptOffset converts a 0-based (line, col) pair into a byte offset into
// script, or -1 when the position is out of range.
func scriptOffset(script string, line, col int) int {
	offset := 0
	curLine := 0
	for offset < len(script) && curLine < line {
		if script[offset] == '\n' {
			curLine++
		}
		offset++
	}
	if curLine != line {
		return -1
	}
	// Advance within the line.
	remaining := col
	for remaining > 0 && offset < len(script) && script[offset] != '\n' {
		offset++
		remaining--
	}
	if remaining > 0 {
		return -1
	}
	return offset
}

// scriptLineCol converts a byte offset back to a 0-based (line, col) pair.
func scriptLineCol(script string, offset int) (int, int) {
	if offset > len(script) {
		offset = len(script)
	}
	line := 0
	lineStart := 0
	for i := range offset {
		if script[i] == '\n' {
			line++
			lineStart = i + 1
		}
	}
	return line, offset - lineStart
}

// dl4001PackageDeleteLocation computes the span to delete for a single package
// token inside an install command. Kept in sync with the helper in the DL4001
// rule (which uses the same logic for the sync per-invocation rewrites).
func dl4001PackageDeleteLocation(
	file string,
	pkgs []shell.PackageArg,
	i, startLine, cmdStartCol int,
) rules.Location {
	pkg := pkgs[i]
	docStartLine := startLine + pkg.Line
	docEndLine := startLine + pkg.Line
	docStartCol := pkg.StartCol
	docEndCol := pkg.EndCol
	if pkg.Line == 0 {
		docStartCol += cmdStartCol
		docEndCol += cmdStartCol
	}
	if i == 0 && i+1 < len(pkgs) {
		next := pkgs[i+1]
		if next.Line == pkg.Line && next.StartCol > pkg.EndCol {
			docEndCol = next.StartCol
			if pkg.Line == 0 {
				docEndCol += cmdStartCol
			}
		}
	} else if i > 0 && docStartCol > 0 {
		docStartCol--
	}
	return rules.NewRangeLocation(file, docStartLine, docStartCol, docEndLine, docEndCol)
}

// findInstallPackageIndex returns the index (or -1) of the first package
// token whose normalized name matches tool, stripping common version suffixes.
func findInstallPackageIndex(pkgs []shell.PackageArg, tool string) (int, shell.PackageArg) {
	for i, pkg := range pkgs {
		if strings.EqualFold(shell.StripPackageVersion(pkg.Normalized), tool) {
			return i, pkg
		}
	}
	return -1, shell.PackageArg{}
}

// installRunHasOtherPackages reports whether any install command in the same
// RUN has a package other than the tool being evicted.
func installRunHasOtherPackages(installs []shell.InstallCommand, sourceTool string) bool {
	for _, ic := range installs {
		for _, pkg := range ic.Packages {
			if !strings.EqualFold(shell.StripPackageVersion(pkg.Normalized), sourceTool) {
				return true
			}
		}
	}
	return false
}

// isRunFullyInstallSubcommand reports whether the RUN body consists solely of
// a single install command for the evicted tool, so deleting the whole RUN
// is safe. Conservative: any additional shell command (update, cleanup, etc.)
// returns false so we don't drop side effects.
func isRunFullyInstallSubcommand(script string, variant shell.Variant) bool {
	return shell.CountChainedCommands(script, variant) == 1
}

func resolveRunScript(
	run *instructions.RunCommand,
	sm *sourcemap.SourceMap,
	escapeToken rune,
	variant shell.Variant,
) (string, int) {
	if len(run.Files) > 0 {
		// Heredoc RUN: the body starts on the line AFTER the "RUN <<EOF"
		// opener. BuildKit reports the opener as Location()[0] and the first
		// body line as Location()[1], so prefer the latter when available
		// and fall back to opener+1 for hand-rolled test inputs.
		startLine := 0
		if loc := run.Location(); len(loc) > 1 {
			startLine = loc[1].Start.Line
		} else if len(loc) > 0 {
			startLine = loc[0].Start.Line + 1
		}
		return run.Files[0].Data, startLine
	}
	if !run.PrependShell || len(run.Location()) == 0 {
		return strings.Join(run.CmdLine, " "), 0
	}
	startLine := run.Location()[0].Start.Line
	endLine := sm.ResolveEndLineWithEscape(run.Location()[0].End.Line, escapeToken)
	lines := make([]string, 0, endLine-startLine+1)
	for line := startLine; line <= endLine; line++ {
		lines = append(lines, sm.Line(line-1))
	}
	cmdStartCol := shell.DockerfileRunCommandStartCol(lines[0])
	// Use the shell's native continuation token so variant-specific parsers
	// (tree-sitter PowerShell/cmd, mvdan.cc/sh POSIX) handle line
	// continuations correctly. A backtick-escaped "RUN ... `\n" on a
	// PowerShell stage must keep backtick continuations so the PowerShell
	// parser sees the whole chain as one command.
	return shell.ReconstructSourceTextForVariant(lines, cmdStartCol, escapeToken, variant), startLine
}

// runShellVariant returns the effective shell variant for a RUN instruction
// at its position in the stage, accounting for any SHELL instructions that
// appeared before it. This matters for FindInstallPackages and CountChainedCommands,
// which dispatch to shell-specific grammar (PowerShell, cmd) when appropriate.
// Falls back to bash when stage info isn't available (e.g. malformed semantic model).
func runShellVariant(run *instructions.RunCommand, stageInfo *semantic.StageInfo) shell.Variant {
	if stageInfo == nil {
		return shell.VariantBash
	}
	if locs := run.Location(); len(locs) > 0 && locs[0].Start.Line > 0 {
		return stageInfo.ShellVariantAtLine(locs[0].Start.Line)
	}
	return stageInfo.ShellSetting.Variant
}

func init() {
	RegisterResolver(&dl4001CleanupResolver{})
}
