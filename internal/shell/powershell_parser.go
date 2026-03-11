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
