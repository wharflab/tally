package labels

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/moby/buildkit/frontend/dockerfile/command"
	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/wharflab/tally/internal/facts"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/sourcemap"
)

const dockerfileSourceEntrypointLabel = "com.docker.image.source.entrypoint"

func labelEscapeToken(input rules.LintInput) rune {
	if input.AST == nil {
		return '\\'
	}
	return input.AST.EscapeToken
}

type labelInstructionFixOptions struct {
	CommentDescription string
	DeleteDescription  string
	CommentPrefix      string
	Safety             rules.FixSafety
	Priority           int
}

func buildStandaloneLabelInstructionFixes(
	file string,
	sm *sourcemap.SourceMap,
	pair facts.LabelPairFact,
	escapeToken rune,
	opts labelInstructionFixOptions,
) []*rules.SuggestedFix {
	if sm == nil || pair.Command == nil || len(pair.Command.Labels) != 1 {
		return nil
	}
	locs := pair.Command.Location()
	if len(locs) == 0 {
		return nil
	}

	startLine := locs[0].Start.Line
	endLine := sm.ResolveEndLineWithEscape(locs[0].End.Line, escapeToken)
	if startLine <= 0 || endLine < startLine || endLine > sm.LineCount() {
		return nil
	}

	lastLine := sm.Line(endLine - 1)
	editLoc := rules.NewRangeLocation(file, startLine, 0, endLine, len(lastLine))
	deleteLoc := deleteInstructionLocation(file, sm, startLine, endLine)
	commentedText := commentOutLabelInstruction(sm, startLine, endLine, opts.CommentPrefix)

	return []*rules.SuggestedFix{
		{
			Description: opts.CommentDescription,
			Safety:      opts.Safety,
			Priority:    opts.Priority,
			IsPreferred: true,
			Edits:       []rules.TextEdit{{Location: editLoc, NewText: commentedText}},
		},
		{
			Description: opts.DeleteDescription,
			Safety:      opts.Safety,
			Priority:    opts.Priority,
			Edits:       []rules.TextEdit{{Location: deleteLoc, NewText: ""}},
		},
	}
}

func buildLabelPairRemovalFixes(
	file string,
	sm *sourcemap.SourceMap,
	pair facts.LabelPairFact,
	escapeToken rune,
	opts labelInstructionFixOptions,
) []*rules.SuggestedFix {
	if pair.Command == nil {
		return nil
	}
	if len(pair.Command.Labels) == 1 {
		return buildStandaloneLabelInstructionFixes(file, sm, pair, escapeToken, opts)
	}
	spans := labelPairSourceSpans(sm, pair.Command, escapeToken)
	if pair.PairIndex < 0 || pair.PairIndex >= len(spans) {
		return nil
	}

	deleteEdit, ok := groupedLabelPairDeleteEdit(file, sm, spans, pair.PairIndex)
	if !ok {
		return nil
	}
	commentEdit, ok := groupedLabelPairCommentEdit(file, sm, spans[pair.PairIndex], pair, opts)
	if !ok {
		return nil
	}

	return []*rules.SuggestedFix{
		{
			Description: opts.CommentDescription,
			Safety:      opts.Safety,
			Priority:    opts.Priority,
			IsPreferred: true,
			Edits:       []rules.TextEdit{commentEdit, deleteEdit},
		},
		{
			Description: opts.DeleteDescription,
			Safety:      opts.Safety,
			Priority:    opts.Priority,
			Edits:       []rules.TextEdit{deleteEdit},
		},
	}
}

func commentOutLabelInstruction(sm *sourcemap.SourceMap, startLine, endLine int, prefix string) string {
	lines := make([]string, 0, endLine-startLine+1)
	for lineNum := startLine; lineNum <= endLine; lineNum++ {
		line := sm.Line(lineNum - 1)
		if lineNum == startLine {
			lines = append(lines, prefix+line)
			continue
		}
		lines = append(lines, "# "+line)
	}
	return strings.Join(lines, "\n")
}

func deleteInstructionLocation(file string, sm *sourcemap.SourceMap, startLine, endLine int) rules.Location {
	lastLine := sm.Line(endLine - 1)
	if endLine < sm.LineCount() {
		return rules.NewRangeLocation(file, startLine, 0, endLine+1, 0)
	}
	return rules.NewRangeLocation(file, startLine, 0, endLine, len(lastLine))
}

type sourcePosition struct {
	line int
	col  int
}

type logicalByte struct {
	b   byte
	pos sourcePosition
}

type labelWordSpan struct {
	text  string
	start sourcePosition
	end   sourcePosition
}

func labelPairSourceSpans(
	sm *sourcemap.SourceMap,
	cmd *instructions.LabelCommand,
	escapeToken rune,
) []labelWordSpan {
	if sm == nil || cmd == nil {
		return nil
	}
	locs := cmd.Location()
	if len(locs) == 0 {
		return nil
	}

	startLine := locs[0].Start.Line
	endLine := sm.ResolveEndLineWithEscape(locs[0].End.Line, escapeToken)
	logical := labelInstructionLogicalArgs(sm, startLine, endLine, escapeToken)
	words := parseLabelWordsWithSpans(logical, escapeToken)
	if len(words) != len(cmd.Labels) {
		return nil
	}
	for idx, word := range words {
		parts := strings.SplitN(word.text, "=", 2)
		if len(parts) != 2 {
			return nil
		}
		if parts[0] != cmd.Labels[idx].Key || parts[1] != cmd.Labels[idx].Value {
			return nil
		}
	}
	return words
}

func labelInstructionLogicalArgs(
	sm *sourcemap.SourceMap,
	startLine int,
	endLine int,
	escapeToken rune,
) []logicalByte {
	if startLine <= 0 || endLine < startLine || endLine > sm.LineCount() {
		return nil
	}
	firstLine := sm.Line(startLine - 1)
	argsStart := labelInstructionArgsStart(firstLine)
	if argsStart < 0 {
		return nil
	}

	var out []logicalByte
	for lineNum := startLine; lineNum <= endLine; lineNum++ {
		line := sm.Line(lineNum - 1)
		startCol := 0
		if lineNum == startLine {
			startCol = argsStart
		}
		endCol := len(line)
		if lineNum < endLine {
			if cut, ok := continuationCutColumn(line, escapeToken); ok {
				endCol = cut
			}
		}
		if startCol > endCol || startCol > len(line) {
			continue
		}
		for col := startCol; col < endCol; col++ {
			out = append(out, logicalByte{
				b:   line[col],
				pos: sourcePosition{line: lineNum, col: col},
			})
		}
	}
	return out
}

func labelInstructionArgsStart(line string) int {
	col := 0
	for col < len(line) && (line[col] == ' ' || line[col] == '\t') {
		col++
	}
	if len(line[col:]) < len(command.Label) || !strings.EqualFold(line[col:col+len(command.Label)], command.Label) {
		return -1
	}
	return col + len(command.Label)
}

func continuationCutColumn(line string, escapeToken rune) (int, bool) {
	trimmedEnd := strings.TrimRight(line, " \t")
	if trimmedEnd == "" {
		return len(line), false
	}
	escape := string(escapeToken)
	if !strings.HasSuffix(trimmedEnd, escape) {
		return len(line), false
	}
	cut := len(trimmedEnd) - len(escape)
	if cut > 0 && strings.HasSuffix(trimmedEnd[:cut], escape) {
		return len(line), false
	}
	return cut, true
}

//nolint:gocognit,funlen // Mirrors BuildKit's parseWords state machine while tracking source spans.
func parseLabelWordsWithSpans(logical []logicalByte, escapeToken rune) []labelWordSpan {
	const (
		inSpaces = iota
		inWord
		inQuote
	)

	var words []labelWordSpan
	phase := inSpaces
	quote := rune(0)
	blankOK := false
	var builder strings.Builder
	var start sourcePosition
	var end sourcePosition
	haveStart := false
	s := logicalBytesString(logical)

	appendCurrent := func() {
		if blankOK || builder.Len() > 0 {
			words = append(words, labelWordSpan{text: builder.String(), start: start, end: end})
		}
		builder.Reset()
		blankOK = false
		haveStart = false
	}
	writeRune := func(pos int, ch rune, width int) {
		if !haveStart {
			start = logical[pos].pos
			haveStart = true
		}
		builder.WriteRune(ch)
		last := logical[pos+width-1].pos
		end = sourcePosition{line: last.line, col: last.col + 1}
	}

	for pos := 0; pos <= len(s); {
		var ch rune
		var width int
		if pos != len(s) {
			ch, width = utf8.DecodeRuneInString(s[pos:])
		}

		if phase == inSpaces {
			if pos == len(s) {
				break
			}
			if unicode.IsSpace(ch) {
				pos += width
				continue
			}
			phase = inWord
		}
		if (phase == inWord || phase == inQuote) && pos == len(s) {
			appendCurrent()
			break
		}
		if phase == inWord {
			if unicode.IsSpace(ch) {
				phase = inSpaces
				appendCurrent()
				pos += width
				continue
			}
			if ch == '\'' || ch == '"' {
				quote = ch
				blankOK = true
				phase = inQuote
			}
			if ch == escapeToken {
				if pos+width == len(s) {
					pos += width
					continue
				}
				writeRune(pos, ch, width)
				pos += width
				ch, width = utf8.DecodeRuneInString(s[pos:])
			}
			writeRune(pos, ch, width)
			pos += width
			continue
		}
		if phase == inQuote {
			if ch == quote {
				phase = inWord
			}
			if ch == escapeToken && quote != '\'' {
				if pos+width == len(s) {
					phase = inWord
					pos += width
					continue
				}
				pos += width
				writeRune(pos-width, ch, width)
				ch, width = utf8.DecodeRuneInString(s[pos:])
			}
			writeRune(pos, ch, width)
			pos += width
		}
	}
	return words
}

func logicalBytesString(logical []logicalByte) string {
	var builder strings.Builder
	builder.Grow(len(logical))
	for _, b := range logical {
		builder.WriteByte(b.b)
	}
	return builder.String()
}

func groupedLabelPairDeleteEdit(
	file string,
	sm *sourcemap.SourceMap,
	spans []labelWordSpan,
	pairIndex int,
) (rules.TextEdit, bool) {
	if pairIndex < 0 || pairIndex >= len(spans) || len(spans) < 2 {
		return rules.TextEdit{}, false
	}

	start := spans[pairIndex].start
	end := spans[pairIndex].end
	if pairIndex == 0 {
		end = spans[1].start
	} else {
		start = spans[pairIndex-1].end
		if pairIndex == len(spans)-1 {
			end = extendPositionThroughHorizontalWhitespace(sm, end)
		}
	}

	return rules.TextEdit{
		Location: rules.NewRangeLocation(file, start.line, start.col, end.line, end.col),
		NewText:  "",
	}, true
}

func extendPositionThroughHorizontalWhitespace(sm *sourcemap.SourceMap, pos sourcePosition) sourcePosition {
	line := sm.Line(pos.line - 1)
	for pos.col < len(line) && (line[pos.col] == ' ' || line[pos.col] == '\t') {
		pos.col++
	}
	return pos
}

func groupedLabelPairCommentEdit(
	file string,
	sm *sourcemap.SourceMap,
	span labelWordSpan,
	pair facts.LabelPairFact,
	opts labelInstructionFixOptions,
) (rules.TextEdit, bool) {
	if span.start.line != span.end.line || strings.ContainsAny(span.text, "\r\n") {
		return rules.TextEdit{}, false
	}
	locs := pair.Command.Location()
	if len(locs) == 0 {
		return rules.TextEdit{}, false
	}
	startLine := locs[0].Start.Line
	indent := leadingHorizontalWhitespace(sm.Line(startLine - 1))
	return rules.TextEdit{
		Location: rules.NewRangeLocation(file, startLine, 0, startLine, 0),
		NewText:  fmt.Sprintf("%s%sLABEL %s\n", indent, opts.CommentPrefix, span.text),
	}, true
}

func leadingHorizontalWhitespace(line string) string {
	col := 0
	for col < len(line) && (line[col] == ' ' || line[col] == '\t') {
		col++
	}
	return line[:col]
}
