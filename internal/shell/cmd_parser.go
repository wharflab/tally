//go:build cgo

package shell

import (
	"path"
	"regexp"
	"slices"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
	tsbatch "github.com/wharflab/tree-sitter-batch"
)

var batchLanguage = tsbatch.GetLanguage()

var cmdVariablePattern = regexp.MustCompile(`(?i)(%[a-z_][a-z0-9_]*(?::[^%]+)?%|![a-z_][a-z0-9_]*!|%%~?[a-z]|%~?[0-9])`)

// CmdScriptAnalysis captures the cmd.exe syntax traits that matter for fix
// safety when rewriting a stage to PowerShell.
type CmdScriptAnalysis struct {
	Commands []CommandInfo

	commandByteRanges [][2]uint
	conditionalOps    []cmdConditionalOp

	HasConditionals       bool
	HasExitCommand        bool
	HasPipes              bool
	HasRedirections       bool
	HasControlFlow        bool
	HasVariableReferences bool
}

type cmdConditionalOp struct {
	Text  string
	Start uint
	End   uint
}

// HasBatchOnlySyntax reports whether the script uses cmd.exe shell semantics
// that should block generic rewrites to PowerShell.
func (a *CmdScriptAnalysis) HasBatchOnlySyntax() bool {
	if a == nil {
		return true
	}
	return a.HasConditionals || a.HasPipes || a.HasRedirections || a.HasControlFlow || a.HasVariableReferences
}

func cmdCommandNames(script string) []string {
	analysis := AnalyzeCmdScript(script)
	if analysis == nil {
		return nil
	}

	names := make([]string, 0, len(analysis.Commands))
	for _, cmd := range analysis.Commands {
		names = append(names, cmd.Name)
	}
	return names
}

func findCmdCommands(script string, names ...string) []CommandInfo {
	analysis := AnalyzeCmdScript(script)
	if analysis == nil {
		return nil
	}
	if len(names) == 0 {
		return append([]CommandInfo(nil), analysis.Commands...)
	}

	nameSet := make(map[string]bool, len(names))
	for _, name := range names {
		nameSet[normalizeCmdCommandName(name)] = true
	}

	out := make([]CommandInfo, 0, len(analysis.Commands))
	for _, cmd := range analysis.Commands {
		if nameSet[cmd.Name] {
			out = append(out, cmd)
		}
	}
	return out
}

// AnalyzeCmdScript parses a cmd.exe script and returns the command list plus
// shell-feature signals used by Windows-safe autofixes.
func AnalyzeCmdScript(script string) *CmdScriptAnalysis {
	if strings.TrimSpace(script) == "" || batchLanguage == nil {
		return nil
	}
	if !strings.HasSuffix(script, "\n") {
		script += "\n"
	}

	parser := sitter.NewParser()
	defer parser.Close()

	if err := parser.SetLanguage(batchLanguage); err != nil {
		return nil
	}

	source := []byte(script)
	tree := parser.Parse(source, nil)
	if tree == nil {
		return nil
	}
	defer tree.Close()

	analysis := &CmdScriptAnalysis{}
	walkCmdTree(tree.RootNode(), func(node *sitter.Node) {
		if node == nil {
			return
		}

		switch node.Kind() {
		case tsbatch.NodeCmd:
			if info, ok := cmdCommandInfo(node, source); ok {
				analysis.Commands = append(analysis.Commands, info)
				analysis.commandByteRanges = append(analysis.commandByteRanges, [2]uint{node.StartByte(), node.EndByte()})
				if info.Name == cmdExit {
					analysis.HasExitCommand = true
				}
				if slices.ContainsFunc(info.Args, hasEmbeddedCmdVariableSyntax) {
					analysis.HasVariableReferences = true
				}
			}
		case tsbatch.NodeVariableReference:
			analysis.HasVariableReferences = true
		case tsbatch.NodeCondExec:
			analysis.HasConditionals = true
			if op, ok := cmdConditionalOperator(node, source); ok {
				analysis.conditionalOps = append(analysis.conditionalOps, op)
			}
		case tsbatch.NodePipeStmt:
			analysis.HasPipes = true
		case tsbatch.NodeRedirectStmt, tsbatch.NodeRedirection:
			analysis.HasRedirections = true
		case tsbatch.NodeExitStmt:
			analysis.HasExitCommand = true
			analysis.HasControlFlow = true
		case tsbatch.NodeIfStmt,
			tsbatch.NodeForStmt,
			tsbatch.NodeGotoStmt,
			tsbatch.NodeCallStmt,
			tsbatch.NodeSetlocalStmt,
			tsbatch.NodeEndlocalStmt,
			tsbatch.NodeVariableAssignment,
			tsbatch.NodeParenthesized,
			tsbatch.NodeEchoOff:
			analysis.HasControlFlow = true
		}
	})

	slices.SortFunc(analysis.conditionalOps, func(a, b cmdConditionalOp) int {
		switch {
		case a.Start < b.Start:
			return -1
		case a.Start > b.Start:
			return 1
		default:
			return 0
		}
	})

	if len(analysis.Commands) == 0 && !analysis.HasBatchOnlySyntax() {
		return nil
	}

	return analysis
}

func walkCmdTree(node *sitter.Node, visit func(*sitter.Node)) {
	if node == nil {
		return
	}
	visit(node)
	childCount := node.NamedChildCount()
	for i := range childCount {
		walkCmdTree(node.NamedChild(i), visit)
	}
}

func cmdCommandInfo(node *sitter.Node, source []byte) (CommandInfo, bool) {
	var (
		nameNode sitter.Node
		argsNode sitter.Node
		hasName  bool
		hasArgs  bool
	)

	cursor := node.Walk()
	defer cursor.Close()

	for _, child := range node.NamedChildren(cursor) {
		switch child.Kind() {
		case tsbatch.NodeCommandName:
			nameNode = child
			hasName = true
		case tsbatch.NodeArgumentList:
			argsNode = child
			hasArgs = true
		}
	}

	if !hasName {
		return CommandInfo{}, false
	}

	rawName := strings.TrimSpace(nameNode.Utf8Text(source))
	start := nameNode.StartPosition()
	end := nameNode.EndPosition()

	var argChildren []sitter.Node
	if hasArgs {
		argCursor := argsNode.Walk()
		defer argCursor.Close()
		argChildren = argsNode.NamedChildren(argCursor)
	}

	argsStart := 0
	if repairedName, repairedEnd, ok := repairDriveQualifiedCmdName(rawName, argChildren, source); ok {
		rawName = repairedName
		end = repairedEnd
		argsStart = 1
	}

	name := normalizeCmdCommandName(rawName)
	if name == "" {
		return CommandInfo{}, false
	}

	info := CommandInfo{
		SourceKind:      CommandSourceKindDirect,
		Variant:         VariantCmd,
		Name:            name,
		HasExeSuffix:    hasExeSuffix(rawName),
		Line:            int(start.Row),
		StartCol:        int(start.Column),
		EndCol:          int(end.Column),
		CommandEndLine:  int(end.Row),
		CommandEndCol:   int(end.Column),
		HasCommandRange: true,
	}

	if !hasArgs {
		return info, true
	}

	for _, child := range argChildren[argsStart:] {
		text := strings.TrimSpace(child.Utf8Text(source))
		if text == "" {
			continue
		}
		isLiteral := !hasEmbeddedCmdVariableSyntax(text)
		argStart := child.StartPosition()
		argEnd := child.EndPosition()
		info.Args = append(info.Args, text)
		info.ArgLiteral = append(info.ArgLiteral, isLiteral)
		info.ArgRanges = append(info.ArgRanges, ArgRange{
			Line:     int(argStart.Row),
			StartCol: int(argStart.Column),
			EndCol:   int(argEnd.Column),
		})
		info.CommandEndLine = int(argEnd.Row)
		info.CommandEndCol = int(argEnd.Column)
		if info.Subcommand == "" && !strings.HasPrefix(text, "/") {
			info.Subcommand = text
			info.SubcommandLine = int(argStart.Row)
			info.SubcommandStartCol = int(argStart.Column)
			info.SubcommandEndCol = int(argEnd.Column)
		}
	}

	return info, true
}

func cmdConditionalOperator(node *sitter.Node, source []byte) (cmdConditionalOp, bool) {
	cursor := node.Walk()
	defer cursor.Close()

	for _, child := range node.Children(cursor) {
		if child.IsNamed() {
			continue
		}
		text := strings.TrimSpace(child.Utf8Text(source))
		if text == "&&" || text == "||" {
			return cmdConditionalOp{
				Text:  text,
				Start: child.StartByte(),
				End:   child.EndByte(),
			}, true
		}
	}

	return cmdConditionalOp{}, false
}

func repairDriveQualifiedCmdName(name string, args []sitter.Node, source []byte) (string, sitter.Point, bool) {
	if len(name) != 1 || !isASCIIAlpha(name[0]) || len(args) == 0 {
		return "", sitter.Point{}, false
	}

	firstArg := strings.TrimSpace(args[0].Utf8Text(source))
	if !strings.HasPrefix(firstArg, `:\`) && !strings.HasPrefix(firstArg, `:/`) {
		return "", sitter.Point{}, false
	}

	return name + firstArg, args[0].EndPosition(), true
}

func isASCIIAlpha(b byte) bool {
	return ('a' <= b && b <= 'z') || ('A' <= b && b <= 'Z')
}

func normalizeCmdCommandName(name string) string {
	name = strings.TrimSpace(DropQuotes(name))
	if name == "" {
		return ""
	}
	name = strings.ToLower(path.Base(strings.ReplaceAll(name, `\`, "/")))
	return strings.TrimSuffix(name, ".exe")
}

func hasEmbeddedCmdVariableSyntax(text string) bool {
	return cmdVariablePattern.MatchString(text)
}
