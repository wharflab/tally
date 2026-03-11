package shell

import (
	"path"
	"strings"

	gotreesitter "github.com/odvcencio/gotreesitter"

	"github.com/wharflab/tally/internal/powershellast"
)

type powerShellArg struct {
	text string
	node *gotreesitter.Node
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
	lang := powershellast.Language()
	tree, source := powershellast.Parse(script)
	query := powershellast.CommandsQuery()
	if tree == nil || lang == nil || query == nil {
		return nil
	}
	defer tree.Release()

	nameSet := make(map[string]bool, len(names))
	for _, name := range names {
		nameSet[normalizePowerShellCommandName(name)] = true
	}

	var commands []CommandInfo
	cursor := query.Exec(tree.RootNode(), lang, source)
	for {
		match, ok := cursor.Next()
		if !ok {
			break
		}
		nameNode := match.CommandName
		if nameNode == nil {
			continue
		}
		name := normalizePowerShellCommandName(nameNode.Text(source))
		if name == "" {
			continue
		}
		if len(nameSet) > 0 && !nameSet[name] {
			continue
		}

		start := nameNode.StartPoint()
		end := nameNode.EndPoint()
		info := CommandInfo{
			Variant:  VariantPowerShell,
			Name:     name,
			Line:     int(start.Row),
			StartCol: int(start.Column),
			EndCol:   int(end.Column),
		}

		for _, arg := range powerShellCommandArgs(match.CommandElements, source) {
			info.Args = append(info.Args, arg.text)
			if info.Subcommand == "" && !strings.HasPrefix(arg.text, "-") {
				argStart := arg.node.StartPoint()
				argEnd := arg.node.EndPoint()
				info.Subcommand = arg.text
				info.SubcommandLine = int(argStart.Row)
				info.SubcommandStartCol = int(argStart.Column)
				info.SubcommandEndCol = int(argEnd.Column)
			}
		}

		commands = append(commands, info)
	}

	return commands
}

func powerShellCommandArgs(elements *gotreesitter.Node, source []byte) []powerShellArg {
	if elements == nil {
		return nil
	}

	args := make([]powerShellArg, 0, elements.NamedChildCount())
	childCount := elements.NamedChildCount()
	for i := range childCount {
		child := elements.NamedChild(i)
		if child == nil || child.Type(powershellast.Language()) == "command_argument_sep" {
			continue
		}
		text := strings.TrimSpace(child.Text(source))
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
