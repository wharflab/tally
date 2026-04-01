//go:build cgo

package shell

import (
	"path"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"

	tspowershell "github.com/wharflab/tally/internal/third_party/tree_sitter_powershell"
)

var powerShellLanguage = newPowerShellLanguage()

func newPowerShellLanguage() *sitter.Language {
	ptr := tspowershell.Language()
	if ptr == nil {
		return nil
	}
	return sitter.NewLanguage(ptr)
}

type powerShellArg struct {
	text string
	node sitter.Node
}

type powerShellStatement struct {
	Text    string
	HasPipe bool
}

type powerShellScriptAnalysis struct {
	Statements []powerShellStatement
	HasComplex bool
}

func powerShellCommandNames(script string) []string {
	cmds := findPowerShellCommands(script)
	names := make([]string, 0, len(cmds))
	for _, cmd := range cmds {
		names = append(names, cmd.Name)
	}
	return names
}

func findPowerShellCommands(script string, names ...string) []CommandInfo {
	parser := sitter.NewParser()
	defer parser.Close()

	if powerShellLanguage == nil {
		return nil
	}
	if err := parser.SetLanguage(powerShellLanguage); err != nil {
		return nil
	}

	source := []byte(script)
	tree := parser.Parse(source, nil)
	if tree == nil {
		return nil
	}
	defer tree.Close()

	nameSet := make(map[string]bool, len(names))
	for _, name := range names {
		nameSet[normalizePowerShellCommandName(name)] = true
	}

	var commands []CommandInfo
	walkPowerShellTree(tree.RootNode(), func(node *sitter.Node) {
		if node == nil || node.Kind() != "command" {
			return
		}

		nameNode := node.ChildByFieldName("command_name")
		if nameNode == nil {
			return
		}

		name := normalizePowerShellCommandName(nameNode.Utf8Text(source))
		if name == "" {
			return
		}
		if len(nameSet) > 0 && !nameSet[name] {
			return
		}

		start := nameNode.StartPosition()
		end := nameNode.EndPosition()
		info := CommandInfo{
			Variant:  VariantPowerShell,
			Name:     name,
			Line:     int(start.Row),
			StartCol: int(start.Column),
			EndCol:   int(end.Column),
		}

		for _, arg := range powerShellCommandArgs(node, source) {
			info.Args = append(info.Args, arg.text)
			if info.Subcommand == "" && !strings.HasPrefix(arg.text, "-") {
				argStart := arg.node.StartPosition()
				argEnd := arg.node.EndPosition()
				info.Subcommand = arg.text
				info.SubcommandLine = int(argStart.Row)
				info.SubcommandStartCol = int(argStart.Column)
				info.SubcommandEndCol = int(argEnd.Column)
			}
		}

		commands = append(commands, info)
	})

	return commands
}

func walkPowerShellTree(node *sitter.Node, visit func(*sitter.Node)) {
	if node == nil {
		return
	}
	visit(node)
	childCount := node.NamedChildCount()
	for i := range childCount {
		walkPowerShellTree(node.NamedChild(i), visit)
	}
}

func walkPowerShellTreeUntil(node *sitter.Node, visit func(*sitter.Node) bool) bool {
	if node == nil {
		return false
	}
	if visit(node) {
		return true
	}
	childCount := node.NamedChildCount()
	for i := range childCount {
		if walkPowerShellTreeUntil(node.NamedChild(i), visit) {
			return true
		}
	}
	return false
}

func powerShellCommandArgs(node *sitter.Node, source []byte) []powerShellArg {
	elements := node.ChildByFieldName("command_elements")
	if elements == nil {
		return nil
	}

	cursor := elements.Walk()
	defer cursor.Close()

	children := elements.NamedChildren(cursor)
	args := make([]powerShellArg, 0, len(children))
	for _, child := range children {
		if child.Kind() == "command_argument_sep" {
			continue
		}
		text := strings.TrimSpace(child.Utf8Text(source))
		if text == "" {
			continue
		}
		args = append(args, powerShellArg{text: text, node: child})
	}
	return args
}

// canParsePowerShell reports whether the PowerShell tree-sitter grammar can
// parse the script without errors. A clean parse means valid PowerShell syntax
// (e.g., [ClassName]::Method), not a JSON exec-form attempt.
func canParsePowerShell(script string) bool {
	p := sitter.NewParser()
	defer p.Close()

	if powerShellLanguage == nil {
		return false
	}
	if err := p.SetLanguage(powerShellLanguage); err != nil {
		return false
	}

	tree := p.Parse([]byte(script), nil)
	if tree == nil {
		return false
	}
	defer tree.Close()

	return !tree.RootNode().HasError()
}

func analyzePowerShellScript(script string) *powerShellScriptAnalysis {
	if strings.TrimSpace(script) == "" || powerShellLanguage == nil {
		return nil
	}

	parser := sitter.NewParser()
	defer parser.Close()

	if err := parser.SetLanguage(powerShellLanguage); err != nil {
		return nil
	}

	source := []byte(script)
	tree := parser.Parse(source, nil)
	if tree == nil {
		return nil
	}
	defer tree.Close()

	root := tree.RootNode()
	if root.HasError() {
		return nil
	}

	analysis := &powerShellScriptAnalysis{}
	collectPowerShellStatements(root, source, analysis, "", false)
	if len(analysis.Statements) == 0 && !analysis.HasComplex {
		return nil
	}
	return analysis
}

func hasPowerShellFlowControl(script, keyword string) bool {
	if strings.TrimSpace(script) == "" || powerShellLanguage == nil {
		return false
	}

	parser := sitter.NewParser()
	defer parser.Close()

	if err := parser.SetLanguage(powerShellLanguage); err != nil {
		return false
	}

	source := []byte(script)
	tree := parser.Parse(source, nil)
	if tree == nil {
		return false
	}
	defer tree.Close()

	root := tree.RootNode()
	if root.HasError() {
		return false
	}

	return walkPowerShellTreeUntil(root, func(node *sitter.Node) bool {
		if node == nil || node.Kind() != "flow_control_statement" {
			return false
		}

		text := strings.TrimSpace(node.Utf8Text(source))
		if text == "" {
			return false
		}

		first := strings.Fields(text)[0]
		return strings.EqualFold(first, keyword)
	})
}

func collectPowerShellStatements(
	node *sitter.Node,
	source []byte,
	analysis *powerShellScriptAnalysis,
	parentKind string,
	inComplex bool,
) {
	if node == nil || !node.IsNamed() {
		return
	}

	kind := node.Kind()
	if isPowerShellComplexKind(kind) {
		analysis.HasComplex = true
		inComplex = true
	}

	if kind == "pipeline" && !inComplex && isTopLevelPowerShellPipelineParent(parentKind) {
		text := strings.TrimSpace(node.Utf8Text(source))
		if text != "" {
			analysis.Statements = append(analysis.Statements, powerShellStatement{
				Text:    text,
				HasPipe: hasPowerShellPipelineOperator(node),
			})
		}
		return
	}

	childCount := node.NamedChildCount()
	for i := range childCount {
		collectPowerShellStatements(node.NamedChild(i), source, analysis, kind, inComplex)
	}
}

func hasPowerShellPipelineOperator(node *sitter.Node) bool {
	if node == nil || node.Kind() != "pipeline" {
		return false
	}

	return node.NamedChildCount() > 1
}

func isTopLevelPowerShellPipelineParent(kind string) bool {
	switch kind {
	case "program", "statement_list", "script_block_body":
		return true
	default:
		return false
	}
}

func isPowerShellComplexKind(kind string) bool {
	switch kind {
	case "if_statement",
		"switch_statement",
		"foreach_statement",
		"for_statement",
		"while_statement",
		"function_statement",
		"trap_statement",
		"try_statement",
		"data_statement",
		"parallel_statement",
		"class_statement",
		"script_block",
		"script_block_expression":
		return true
	default:
		return false
	}
}

func normalizePowerShellCommandName(name string) string {
	name = strings.TrimSpace(DropQuotes(name))
	if name == "" {
		return ""
	}
	name = strings.ReplaceAll(name, "''", "")
	name = strings.ReplaceAll(name, `""`, "")
	name = strings.ReplaceAll(name, "`", "")
	name = strings.ToLower(path.Base(strings.ReplaceAll(name, `\`, "/")))
	return strings.TrimSuffix(name, ".exe")
}
