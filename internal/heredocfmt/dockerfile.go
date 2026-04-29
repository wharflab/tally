package heredocfmt

import (
	"path"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/command"
	"github.com/moby/buildkit/frontend/dockerfile/instructions"
	"github.com/moby/buildkit/frontend/dockerfile/parser"

	"github.com/wharflab/tally/internal/dockerfile"
	"github.com/wharflab/tally/internal/highlight/extract"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/sourcemap"
)

// DockerfileHeredoc describes a COPY/ADD heredoc body in a Dockerfile.
type DockerfileHeredoc struct {
	Instruction    string
	TargetPath     string
	SourcePath     string
	Content        string
	BodyStartLine  int
	TerminatorLine int
	BodyPrefix     string
}

// RunHeredoc describes a RUN heredoc body whose contents are executed as a script.
type RunHeredoc struct {
	Instruction       string
	Content           string
	StartLine         int
	BodyStartLine     int
	TerminatorLine    int
	BodyPrefix        string
	ShellNameOverride string
}

// CollectDockerfileHeredocs returns supported COPY/ADD heredocs from a parsed Dockerfile.
func CollectDockerfileHeredocs(result *dockerfile.ParseResult) []DockerfileHeredoc {
	if result == nil || result.AST == nil || result.AST.AST == nil {
		return nil
	}

	sm := sourcemap.New(result.Source)
	escapeToken := result.AST.EscapeToken
	if escapeToken == 0 {
		escapeToken = '\\'
	}

	var docs []DockerfileHeredoc
	for _, node := range result.AST.AST.Children {
		if len(node.Heredocs) == 0 || !isCopyOrAddNode(node) {
			continue
		}

		sources, ok := copyAddSources(node)
		if !ok || len(sources.SourceContents) == 0 {
			continue
		}

		spans := heredocBodySpans(node, sm, escapeToken)
		if len(spans) != len(sources.SourceContents) {
			continue
		}

		totalSources := len(sources.SourcePaths) + len(sources.SourceContents)
		for i, src := range sources.SourceContents {
			span := spans[i]
			if span.bodyStartLine <= 0 || span.terminatorLine <= 0 {
				continue
			}

			docs = append(docs, DockerfileHeredoc{
				Instruction:    strings.ToUpper(node.Value),
				TargetPath:     resolveTargetPath(sources.DestPath, src.Path, totalSources),
				SourcePath:     src.Path,
				Content:        src.Data,
				BodyStartLine:  span.bodyStartLine,
				TerminatorLine: span.terminatorLine,
				BodyPrefix:     span.bodyPrefix,
			})
		}
	}
	return docs
}

// CollectRunHeredocs returns RUN heredocs whose body is executed as a shell script.
func CollectRunHeredocs(result *dockerfile.ParseResult) []RunHeredoc {
	if result == nil || result.AST == nil || result.AST.AST == nil {
		return nil
	}

	sm := sourcemap.New(result.Source)
	escapeToken := result.AST.EscapeToken
	if escapeToken == 0 {
		escapeToken = '\\'
	}

	var docs []RunHeredoc
	for _, node := range result.AST.AST.Children {
		if len(node.Heredocs) != 1 || !strings.EqualFold(node.Value, command.Run) {
			continue
		}

		mapping, ok := extract.ExtractRunScript(sm, node, escapeToken)
		if !ok || !mapping.IsHeredoc {
			continue
		}

		spans := heredocBodySpans(node, sm, escapeToken)
		if len(spans) == 0 {
			continue
		}
		span := spans[0]
		if span.bodyStartLine <= 0 || span.terminatorLine <= 0 {
			continue
		}

		docs = append(docs, RunHeredoc{
			Instruction:       strings.ToUpper(node.Value),
			Content:           node.Heredocs[0].Content,
			StartLine:         node.StartLine,
			BodyStartLine:     span.bodyStartLine,
			TerminatorLine:    span.terminatorLine,
			BodyPrefix:        span.bodyPrefix,
			ShellNameOverride: mapping.ShellNameOverride,
		})
	}
	return docs
}

// FormatDockerfileHeredocs builds text edits that pretty-print COPY/ADD heredoc bodies.
func FormatDockerfileHeredocs(file string, result *dockerfile.ParseResult) ([]rules.TextEdit, error) {
	formatter := NewFormatter(file)
	var edits []rules.TextEdit
	for _, doc := range CollectDockerfileHeredocs(result) {
		formatted, _, ok, err := formatter.FormatTarget(doc.TargetPath, doc.Content)
		if err != nil {
			return nil, err
		}
		if !ok && strings.EqualFold(doc.Instruction, command.Copy) {
			formatted, _, ok, err = formatter.FormatShellTarget(doc.TargetPath, doc.Content)
			if err != nil {
				return nil, err
			}
		}
		if !ok || formatted == doc.Content {
			continue
		}

		edits = append(edits, rules.TextEdit{
			Location: rules.NewRangeLocation(file, doc.BodyStartLine, 0, doc.TerminatorLine, 0),
			NewText:  WithBodyPrefix(formatted, doc.BodyPrefix),
		})
	}
	return edits, nil
}

type heredocBodySpan struct {
	bodyStartLine  int
	terminatorLine int
	bodyPrefix     string
}

func isCopyOrAddNode(node *parser.Node) bool {
	if node == nil {
		return false
	}
	switch strings.ToLower(node.Value) {
	case command.Copy, command.Add:
		return true
	default:
		return false
	}
}

func copyAddSources(node *parser.Node) (*instructions.SourcesAndDest, bool) {
	inst, err := instructions.ParseInstruction(node)
	if err != nil {
		return nil, false
	}
	switch cmd := inst.(type) {
	case *instructions.CopyCommand:
		return &cmd.SourcesAndDest, true
	case *instructions.AddCommand:
		return &cmd.SourcesAndDest, true
	default:
		return nil, false
	}
}

func heredocBodySpans(node *parser.Node, sm *sourcemap.SourceMap, escapeToken rune) []heredocBodySpan {
	if node == nil || sm == nil || len(node.Heredocs) == 0 {
		return nil
	}

	bodyStart := instructionEndLine(node, sm, escapeToken) + 1
	spans := make([]heredocBodySpan, 0, len(node.Heredocs))
	for _, heredoc := range node.Heredocs {
		terminator := findHeredocTerminator(sm, bodyStart, node.EndLine, heredoc)
		if terminator <= 0 {
			return nil
		}
		spans = append(spans, heredocBodySpan{
			bodyStartLine:  bodyStart,
			terminatorLine: terminator,
			bodyPrefix:     heredocBodyPrefix(sm, bodyStart, terminator, heredoc.Chomp),
		})
		bodyStart = terminator + 1
	}
	return spans
}

func instructionEndLine(node *parser.Node, sm *sourcemap.SourceMap, escapeToken rune) int {
	end := node.StartLine
	escape := string(escapeToken)
	for lineNum := node.StartLine; lineNum <= sm.LineCount(); lineNum++ {
		line := strings.TrimRight(sm.Line(lineNum-1), " \t")
		end = lineNum
		if !strings.HasSuffix(line, escape) {
			return end
		}
	}
	return end
}

func findHeredocTerminator(
	sm *sourcemap.SourceMap,
	startLine int,
	endLine int,
	heredoc parser.Heredoc,
) int {
	if endLine <= 0 || endLine > sm.LineCount() {
		endLine = sm.LineCount()
	}
	for lineNum := startLine; lineNum <= endLine; lineNum++ {
		line := sm.Line(lineNum - 1)
		if heredoc.Chomp {
			line = strings.TrimLeft(line, "\t")
		}
		if line == heredoc.Name {
			return lineNum
		}
	}
	return 0
}

func heredocBodyPrefix(sm *sourcemap.SourceMap, startLine, terminatorLine int, chomp bool) string {
	if !chomp {
		return ""
	}

	var common string
	found := false
	for lineNum := startLine; lineNum < terminatorLine; lineNum++ {
		line := sm.Line(lineNum - 1)
		if strings.Trim(line, "\t") == "" {
			continue
		}
		tabs := leadingTabs(line)
		if !found {
			common = tabs
			found = true
			continue
		}
		common = commonTabPrefix(common, tabs)
	}
	return common
}

func leadingTabs(s string) string {
	i := 0
	for i < len(s) && s[i] == '\t' {
		i++
	}
	return s[:i]
}

func commonTabPrefix(a, b string) string {
	n := min(len(a), len(b))
	i := 0
	for i < n && a[i] == '\t' && b[i] == '\t' {
		i++
	}
	return a[:i]
}

func resolveTargetPath(dest, sourcePath string, totalSources int) string {
	if totalSources <= 1 {
		if _, ok := SupportedKind(dest); ok {
			return dest
		}
		if looksDirectory(dest) {
			return path.Join(filepathToSlash(dest), sourcePath)
		}
		return dest
	}
	return path.Join(filepathToSlash(dest), sourcePath)
}

func looksDirectory(p string) bool {
	p = filepathToSlash(strings.TrimSpace(p))
	return strings.HasSuffix(p, "/") || p == "." || p == ""
}

func filepathToSlash(p string) string {
	return strings.ReplaceAll(p, `\`, `/`)
}

// WithBodyPrefix re-applies the leading tab prefix used by <<- heredoc bodies.
func WithBodyPrefix(formatted, prefix string) string {
	if prefix == "" {
		return formatted
	}
	body := strings.TrimSuffix(formatted, "\n")
	if body == "" {
		return ""
	}
	lines := strings.Split(body, "\n")
	var b strings.Builder
	for _, line := range lines {
		b.WriteString(prefix)
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}
