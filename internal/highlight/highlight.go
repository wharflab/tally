package highlight

import (
	"bytes"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/command"
	"github.com/moby/buildkit/frontend/dockerfile/instructions"
	"github.com/moby/buildkit/frontend/dockerfile/parser"

	"github.com/wharflab/tally/internal/directive"
	dfparse "github.com/wharflab/tally/internal/dockerfile"
	"github.com/wharflab/tally/internal/highlight/core"
	highlightdockerfile "github.com/wharflab/tally/internal/highlight/dockerfile"
	"github.com/wharflab/tally/internal/highlight/extract"
	highlightshell "github.com/wharflab/tally/internal/highlight/shell"
	"github.com/wharflab/tally/internal/semantic"
	"github.com/wharflab/tally/internal/shell"
	"github.com/wharflab/tally/internal/sourcemap"
)

type Document struct {
	File      string
	SourceMap *sourcemap.SourceMap
	Tokens    []core.Token
	byLine    map[int][]core.Token
}

func Analyze(file string, source []byte) *Document {
	sm := sourcemap.New(source)
	doc := &Document{
		File:      file,
		SourceMap: sm,
	}

	var tokens []core.Token

	parseResult, err := dfparse.Parse(bytes.NewReader(source), nil)
	if err == nil && parseResult != nil && parseResult.AST != nil && parseResult.AST.AST != nil {
		root := parseResult.AST.AST
		tokens = append(tokens, highlightdockerfile.Tokenize(sm, root, parseResult.AST.EscapeToken)...)

		directives := directive.Parse(sm, nil, nil).ShellDirectives
		sem := semantic.NewBuilder(parseResult, nil, file).WithShellDirectives(directives).Build()
		tokens = append(tokens, shellTokens(parseResult, sem, sm, directives)...)
	} else {
		tokens = append(tokens, highlightdockerfile.Tokenize(sm, nil, '\\')...)
	}

	doc.Tokens = core.Normalize(sm, tokens)
	doc.byLine = core.ByLine(doc.Tokens)
	return doc
}

func (d *Document) LineTokens(line int) []core.Token {
	if d == nil || d.byLine == nil {
		return nil
	}
	return d.byLine[line]
}

func shellTokens(
	parseResult *dfparse.ParseResult,
	sem *semantic.Model,
	sm *sourcemap.SourceMap,
	directives []directive.ShellDirective,
) []core.Token {
	if parseResult == nil || parseResult.AST == nil || parseResult.AST.AST == nil {
		return nil
	}

	nodesByStartLine := nodeIndex(parseResult.AST.AST)
	var out []core.Token

	for stageIdx, stage := range parseResult.Stages {
		stageInfo := (*semantic.StageInfo)(nil)
		if sem != nil {
			stageInfo = sem.StageInfo(stageIdx)
		}
		shellName := extract.InitialShellNameForStage(stage, directives, stageInfo)

		for _, cmd := range stage.Commands {
			startLine := extract.CommandStartLine(cmd.Location())
			if shellCmd, ok := cmd.(*instructions.ShellCommand); ok {
				if len(shellCmd.Shell) > 0 {
					shellName = shellCmd.Shell[0]
				}
				continue
			}

			node := nodesByStartLine[startLine]
			if node == nil {
				continue
			}
			out = append(out, tokensForCommand(cmd, node, sm, parseResult.AST.EscapeToken, shellName)...)
		}
	}
	return out
}

func tokensForCommand(
	cmd instructions.Command,
	node *parser.Node,
	sm *sourcemap.SourceMap,
	escapeToken rune,
	shellName string,
) []core.Token {
	mapping, ok := shellMappingForCommand(cmd, node, sm, escapeToken)
	if !ok {
		return nil
	}
	variant := effectiveShellVariant(shellName, mapping)
	mapping.Script = extract.NormalizeContinuation(mapping.Script, escapeToken, continuationRune(variant))
	return remapShellTokens(mapping, variant)
}

// continuationRune returns the native line-continuation character for a shell variant.
func continuationRune(variant shell.Variant) rune {
	switch variant { //nolint:exhaustive // POSIX variants all use backslash
	case shell.VariantPowerShell:
		return '`'
	case shell.VariantCmd:
		return '^'
	default:
		return '\\'
	}
}

func shellMappingForCommand(
	cmd instructions.Command,
	node *parser.Node,
	sm *sourcemap.SourceMap,
	escapeToken rune,
) (extract.Mapping, bool) {
	switch c := cmd.(type) {
	case *instructions.RunCommand:
		if !c.PrependShell {
			return extract.Mapping{}, false
		}
		return extract.ExtractRunScript(sm, node, escapeToken)
	case *instructions.CmdCommand:
		if !c.PrependShell {
			return extract.Mapping{}, false
		}
		return extract.ExtractShellFormScript(sm, node, escapeToken, command.Cmd)
	case *instructions.EntrypointCommand:
		if !c.PrependShell {
			return extract.Mapping{}, false
		}
		return extract.ExtractShellFormScript(sm, node, escapeToken, command.Entrypoint)
	case *instructions.HealthCheckCommand:
		if hcShellVariant(c) == "" {
			return extract.Mapping{}, false
		}
		return extract.ExtractHealthcheckCmdShellScript(sm, node, escapeToken)
	default:
		return extract.Mapping{}, false
	}
}

func nodeIndex(root *parser.Node) map[int]*parser.Node {
	if root == nil {
		return nil
	}
	out := make(map[int]*parser.Node)
	for _, node := range root.Children {
		if node == nil || node.StartLine <= 0 {
			continue
		}
		out[node.StartLine] = node
	}
	return out
}

func effectiveShellVariant(shellName string, mapping extract.Mapping) shell.Variant {
	if mapping.ShellNameOverride != "" {
		return shell.VariantFromShell(mapping.ShellNameOverride)
	}
	if mapping.IsHeredoc {
		firstLine, _, _ := strings.Cut(mapping.Script, "\n")
		if name, ok := shell.ShellFromShebang(firstLine); ok {
			return shell.VariantFromShell(name)
		}
	}
	return shell.VariantFromShell(shellName)
}

func remapShellTokens(mapping extract.Mapping, variant shell.Variant) []core.Token {
	tokens := highlightshell.Tokenize(mapping.Script, variant)
	for i := range tokens {
		tokens[i].Line += mapping.OriginStartLine - 1
	}
	return tokens
}

func hcShellVariant(cmd *instructions.HealthCheckCommand) string {
	if cmd == nil || cmd.Health == nil || len(cmd.Health.Test) == 0 {
		return ""
	}
	if cmd.Health.Test[0] != "CMD-SHELL" {
		return ""
	}
	return "CMD-SHELL"
}
