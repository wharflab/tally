//go:build cgo

package shell

import (
	"path"
	"regexp"
	"slices"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
	tsbatch "github.com/wharflab/tree-sitter-batch/bindings/go"
)

var batchLanguage = newBatchLanguage()

var cmdVariablePattern = regexp.MustCompile(`(?i)(%[a-z_][a-z0-9_]*(?::[^%]+)?%|![a-z_][a-z0-9_]*!|%%~?[a-z]|%~?[0-9])`)

func newBatchLanguage() *sitter.Language {
	ptr := tsbatch.Language()
	if ptr == nil {
		return nil
	}
	return sitter.NewLanguage(ptr)
}

// CmdScriptAnalysis captures the cmd.exe syntax traits that matter for fix
// safety when rewriting a stage to PowerShell.
type CmdScriptAnalysis struct {
	Commands []CommandInfo

	HasConditionals       bool
	HasPipes              bool
	HasRedirections       bool
	HasControlFlow        bool
	HasVariableReferences bool
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
		case "cmd":
			if info, ok := cmdCommandInfo(node, source); ok {
				analysis.Commands = append(analysis.Commands, info)
				if slices.ContainsFunc(info.Args, hasEmbeddedCmdVariableSyntax) {
					analysis.HasVariableReferences = true
				}
			}
		case "variable_reference":
			analysis.HasVariableReferences = true
		case "cond_exec":
			analysis.HasConditionals = true
		case "pipe_stmt":
			analysis.HasPipes = true
		case "redirect_stmt", "redirection":
			analysis.HasRedirections = true
		case "if_stmt", "for_stmt", "goto_stmt", "call_stmt", "setlocal_stmt", "endlocal_stmt", "variable_assignment",
			"parenthesized", "echo_off", "exit_stmt":
			analysis.HasControlFlow = true
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
		case "command_name":
			nameNode = child
			hasName = true
		case "argument_list":
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
		Variant:  VariantCmd,
		Name:     name,
		Line:     int(start.Row),
		StartCol: int(start.Column),
		EndCol:   int(end.Column),
	}

	if !hasArgs {
		return info, true
	}

	for _, child := range argChildren[argsStart:] {
		text := strings.TrimSpace(child.Utf8Text(source))
		if text == "" {
			continue
		}
		info.Args = append(info.Args, text)
		if info.Subcommand == "" && !strings.HasPrefix(text, "/") {
			argStart := child.StartPosition()
			argEnd := child.EndPosition()
			info.Subcommand = text
			info.SubcommandLine = int(argStart.Row)
			info.SubcommandStartCol = int(argStart.Column)
			info.SubcommandEndCol = int(argEnd.Column)
		}
	}

	return info, true
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
