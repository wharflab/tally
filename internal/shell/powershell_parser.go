//go:build cgo

package shell

import (
	"path"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
	tspowershell "github.com/wharflab/tree-sitter-powershell"
)

var powerShellLanguage = tspowershell.GetLanguage()

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
		if node == nil || node.Kind() != tspowershell.NodeCommand {
			return
		}

		nameNode := node.ChildByFieldName(tspowershell.FieldCommandName)
		if nameNode == nil {
			return
		}

		rawName := nameNode.Utf8Text(source)
		name := normalizePowerShellCommandName(rawName)
		if name == "" {
			return
		}
		if len(nameSet) > 0 && !nameSet[name] {
			return
		}

		start := nameNode.StartPosition()
		end := nameNode.EndPosition()
		info := CommandInfo{
			SourceKind:      CommandSourceKindDirect,
			Variant:         VariantPowerShell,
			Name:            name,
			HasExeSuffix:    hasExeSuffix(rawName),
			Line:            int(start.Row),
			StartCol:        int(start.Column),
			EndCol:          int(end.Column),
			CommandEndLine:  int(end.Row),
			CommandEndCol:   int(end.Column),
			HasCommandRange: true,
		}

		for _, arg := range powerShellCommandArgs(node, source) {
			argStart := arg.node.StartPosition()
			argEnd := arg.node.EndPosition()
			info.Args = append(info.Args, arg.text)
			info.ArgLiteral = append(info.ArgLiteral, isPlainPowerShellLiteralArg(arg.text))
			info.ArgRanges = append(info.ArgRanges, ArgRange{
				Line:     int(argStart.Row),
				StartCol: int(argStart.Column),
				EndCol:   int(argEnd.Column),
			})
			info.CommandEndLine = int(argEnd.Row)
			info.CommandEndCol = int(argEnd.Column)
			if info.Subcommand == "" && !strings.HasPrefix(arg.text, "-") {
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
	elements := node.ChildByFieldName(tspowershell.FieldCommandElements)
	if elements == nil {
		return nil
	}

	cursor := elements.Walk()
	defer cursor.Close()

	children := elements.NamedChildren(cursor)
	args := make([]powerShellArg, 0, len(children))
	for _, child := range children {
		if child.Kind() == tspowershell.NodeCommandArgumentSep {
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

func isPlainPowerShellLiteralArg(text string) bool {
	raw := strings.TrimSpace(text)
	if raw == "" {
		return false
	}
	if len(raw) >= 2 && raw[0] == '\'' && raw[len(raw)-1] == '\'' {
		return true
	}
	if len(raw) >= 2 && raw[0] == '"' && raw[len(raw)-1] == '"' {
		body := raw[1 : len(raw)-1]
		return !strings.Contains(body, "$") && !strings.Contains(body, "`")
	}
	if strings.HasPrefix(raw, "@") {
		return false
	}
	return !strings.Contains(raw, "$") && !strings.Contains(raw, "`")
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

// CanParsePowerShellScript reports whether the PowerShell tree-sitter grammar
// can parse the script without errors.
func CanParsePowerShellScript(script string) bool {
	return canParsePowerShell(script)
}

// PowerShellAssignment returns the variable name and right-hand value for a
// simple top-level PowerShell assignment expression like
// "$ErrorActionPreference = 'Stop'".
func PowerShellAssignment(script string) (string, string, bool) {
	if strings.TrimSpace(script) == "" || powerShellLanguage == nil {
		return "", "", false
	}

	parser := sitter.NewParser()
	defer parser.Close()

	if err := parser.SetLanguage(powerShellLanguage); err != nil {
		return "", "", false
	}

	source := []byte(script)
	tree := parser.Parse(source, nil)
	if tree == nil {
		return "", "", false
	}
	defer tree.Close()

	root := tree.RootNode()
	if root.HasError() {
		return "", "", false
	}

	pipeline := singleTopLevelPowerShellPipeline(root)
	if pipeline == nil || pipeline.NamedChildCount() != 1 {
		return "", "", false
	}

	assign := pipeline.NamedChild(0)
	if assign == nil || assign.Kind() != tspowershell.NodeAssignmentExpression {
		return "", "", false
	}

	cursor := assign.Walk()
	defer cursor.Close()

	children := assign.NamedChildren(cursor)
	if len(children) != 3 {
		return "", "", false
	}
	if children[0].Kind() != tspowershell.NodeLeftAssignmentExpression || children[2].Kind() != tspowershell.NodePipeline {
		return "", "", false
	}

	name := strings.TrimSpace(children[0].Utf8Text(source))
	value := strings.TrimSpace(children[2].Utf8Text(source))
	if name == "" || value == "" {
		return "", "", false
	}

	return name, value, true
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

func singleTopLevelPowerShellPipeline(root *sitter.Node) *sitter.Node {
	if root == nil || root.Kind() != tspowershell.NodeProgram || root.NamedChildCount() != 1 {
		return nil
	}

	stmtList := root.NamedChild(0)
	if stmtList == nil {
		return nil
	}

	switch stmtList.Kind() {
	case tspowershell.NodePipeline:
		return stmtList
	case tspowershell.NodeStatementList, tspowershell.NodeScriptBlockBody:
		if stmtList.NamedChildCount() != 1 {
			return nil
		}
		pipeline := stmtList.NamedChild(0)
		if pipeline == nil || pipeline.Kind() != tspowershell.NodePipeline {
			return nil
		}
		return pipeline
	default:
		return nil
	}
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
		if node == nil || node.Kind() != tspowershell.NodeFlowControlStatement {
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

	if isTopLevelPowerShellStatement(parentKind, kind) {
		text := strings.TrimSpace(node.Utf8Text(source))
		if text != "" {
			stmt := powerShellStatement{Text: text}
			if kind == tspowershell.NodePipeline {
				stmt.HasPipe = hasPowerShellPipelineOperator(node)
			}
			analysis.Statements = append(analysis.Statements, stmt)
		}
		return
	}

	childCount := node.NamedChildCount()
	for i := range childCount {
		collectPowerShellStatements(node.NamedChild(i), source, analysis, kind, inComplex)
	}
}

func isTopLevelPowerShellStatement(parentKind, kind string) bool {
	if !isTopLevelPowerShellPipelineParent(parentKind) {
		return false
	}

	switch kind {
	case tspowershell.NodeStatementList, tspowershell.NodeScriptBlockBody, tspowershell.NodeEmptyStatement, tspowershell.NodeComment:
		return false
	default:
		return true
	}
}

func hasPowerShellPipelineOperator(node *sitter.Node) bool {
	if node == nil || node.Kind() != tspowershell.NodePipeline {
		return false
	}

	// The grammar may wrap piped commands in a pipeline_chain node
	// (single child of the pipeline), or place commands as direct children.
	if node.NamedChildCount() > 1 {
		return true
	}
	if node.NamedChildCount() == 1 {
		child := node.NamedChild(0)
		return child != nil && child.Kind() == tspowershell.NodePipelineChain && child.NamedChildCount() > 1
	}
	return false
}

func isTopLevelPowerShellPipelineParent(kind string) bool {
	switch kind {
	case tspowershell.NodeProgram, tspowershell.NodeStatementList, tspowershell.NodeScriptBlockBody:
		return true
	default:
		return false
	}
}

func isPowerShellComplexKind(kind string) bool {
	switch kind {
	case tspowershell.NodeIfStatement,
		tspowershell.NodeSwitchStatement,
		tspowershell.NodeForeachStatement,
		tspowershell.NodeForStatement,
		tspowershell.NodeWhileStatement,
		tspowershell.NodeFlowControlStatement,
		tspowershell.NodeFunctionStatement,
		tspowershell.NodeTrapStatement,
		tspowershell.NodeTryStatement,
		tspowershell.NodeDataStatement,
		tspowershell.NodeParallelStatement,
		tspowershell.NodeClassStatement,
		tspowershell.NodeScriptBlock,
		tspowershell.NodeScriptBlockExpression:
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
