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

	var edits []rules.TextEdit
	for stageIdx, stage := range parseResult.Stages {
		edits = append(edits, r.installEdits(resolveCtx.FilePath, stage, stageIdx, parseResult, sm, data.SourceTool)...)
	}
	edits = append(edits, r.configArtifactEdits(resolveCtx.FilePath, parseResult, sm, data.SourceTool)...)
	return edits, nil
}

// installEdits emits edits that drop data.SourceTool from every install command
// in the stage. When the install has other packages, only the matching package
// token is deleted (plus adjacent whitespace). When the install has just that
// one package, we fall back to deleting the whole install subcommand (including
// a leading "&&" glue) or the whole RUN when the install is the only command.
func (r *dl4001CleanupResolver) installEdits(
	file string,
	stage instructions.Stage,
	stageIdx int,
	parseResult *dockerfile.ParseResult,
	sm *sourcemap.SourceMap,
	sourceTool string,
) []rules.TextEdit {
	nodes := stageASTChildren(stageIdx, parseResult.AST)
	if len(nodes) == 0 {
		return nil
	}
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

		script, startLine := resolveRunScript(run, sm)
		if script == "" {
			continue
		}
		variant := runShellVariant(run)
		installs := shell.FindInstallPackages(script, variant)
		if len(installs) == 0 {
			continue
		}

		firstLine := sm.Line(run.Location()[0].Start.Line - 1)
		cmdStartCol := shell.DockerfileRunCommandStartCol(firstLine)

		runHasOtherPackages := installRunHasOtherPackages(installs, sourceTool)
		for _, ic := range installs {
			hit, _ := findInstallPackageIndex(ic.Packages, sourceTool)
			if hit < 0 {
				continue
			}
			if len(ic.Packages) >= 2 {
				edits = append(edits, rules.TextEdit{
					Location: dl4001PackageDeleteLocation(file, ic.Packages, hit, startLine, cmdStartCol),
					NewText:  "",
				})
				continue
			}

			if !runHasOtherPackages && isRunFullyInstallSubcommand(run, script, variant) {
				edits = append(edits, rules.TextEdit{
					Location: dl4001DeleteInstruction(file, node, sm),
					NewText:  "",
				})
				continue
			}
			// Single-package install inside a multi-step RUN: deleting the
			// package leaves a dangling "apt-get install -y". Leave it for a
			// future pass — the sync rewrites already happened, so emitting
			// nothing here is safe.
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

// envBindsToToolConfig reports whether an ENV instruction binds a tool-specific
// config env var (CURL_HOME, WGETRC, WGETHOSTS, CURLHOME — case-insensitive).
func envBindsToToolConfig(node *parser.Node, sourceTool string) bool {
	keys := extractEnvKeys(node)
	for _, key := range keys {
		k := strings.ToUpper(key)
		switch sourceTool {
		case "curl":
			if k == "CURL_HOME" || k == "CURLHOME" {
				return true
			}
		case "wget":
			if k == "WGETRC" || k == "WGETHOSTS" {
				return true
			}
		}
	}
	return false
}

// extractEnvKeys returns the keys in an ENV instruction, handling both
// "ENV KEY VALUE" (single pair) and "ENV K1=V1 K2=V2" (multiple pairs) forms.
func extractEnvKeys(node *parser.Node) []string {
	var keys []string
	n := node.Next
	for n != nil {
		token := n.Value
		if eq := strings.IndexByte(token, '='); eq > 0 {
			keys = append(keys, token[:eq])
			n = n.Next
			continue
		}
		// KEY VALUE form — collect the key, skip the value.
		keys = append(keys, token)
		if n.Next != nil {
			n = n.Next.Next
		} else {
			n = nil
		}
	}
	return keys
}

// copyTargetsToolConfig reports whether a COPY/ADD instruction writes a known
// config file for the tool (e.g. /.curlrc, /_curlrc, /.wgetrc, /etc/wgetrc).
// Heredoc COPYs end up with the destination as the last non-flag argument.
func copyTargetsToolConfig(node *parser.Node, sourceTool string) bool {
	args := copyArgs(node)
	if len(args) == 0 {
		return false
	}
	dst := args[len(args)-1]
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
		name := pkg.Normalized
		for _, sep := range []string{"==", "=", "@", ":"} {
			if idx := strings.Index(name, sep); idx >= 0 {
				name = name[:idx]
				break
			}
		}
		if strings.EqualFold(name, tool) {
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
			name := pkg.Normalized
			for _, sep := range []string{"==", "=", "@", ":"} {
				if idx := strings.Index(name, sep); idx >= 0 {
					name = name[:idx]
					break
				}
			}
			if !strings.EqualFold(name, sourceTool) {
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
func isRunFullyInstallSubcommand(_ *instructions.RunCommand, script string, variant shell.Variant) bool {
	return shell.CountChainedCommands(script, variant) == 1
}

func resolveRunScript(run *instructions.RunCommand, sm *sourcemap.SourceMap) (string, int) {
	if len(run.Files) > 0 {
		return run.Files[0].Data, 0
	}
	if !run.PrependShell || len(run.Location()) == 0 {
		return strings.Join(run.CmdLine, " "), 0
	}
	startLine := run.Location()[0].Start.Line
	endLine := sm.ResolveEndLine(run.Location()[0].End.Line)
	lines := make([]string, 0, endLine-startLine+1)
	for line := startLine; line <= endLine; line++ {
		lines = append(lines, sm.Line(line-1))
	}
	cmdStartCol := shell.DockerfileRunCommandStartCol(lines[0])
	return shell.ReconstructSourceText(lines, cmdStartCol, '\\'), startLine
}

func runShellVariant(_ *instructions.RunCommand) shell.Variant {
	// The resolver doesn't have stage semantic context; bash is the safe default
	// for apt/apk/dnf/zypper/yum installs we care about.
	return shell.VariantBash
}

func init() {
	RegisterResolver(&dl4001CleanupResolver{})
}
