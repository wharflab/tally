package shellcheck

import (
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/command"
)

func blankLeadingKeywordOnly(lines []string, keyword string) []string {
	if len(lines) == 0 {
		return lines
	}
	line0, _, ok := blankLeadingKeyword(lines[0], keyword)
	if !ok {
		return lines
	}
	out := slicesClone(lines)
	out[0] = line0
	return out
}

func blankRunLeadingFlags(lines []string, escapeToken rune) []string {
	if len(lines) == 0 {
		return lines
	}
	line0, after, ok := blankLeadingKeyword(lines[0], command.Run)
	if !ok {
		return lines
	}
	out := slicesClone(lines)
	out[0] = line0
	return blankDockerFlagsUntilNonFlag(out, 0, after, escapeToken)
}

func blankOnbuildRunLeadingFlags(lines []string, escapeToken rune) []string {
	if len(lines) == 0 {
		return lines
	}
	line0, after, ok := blankLeadingKeyword(lines[0], command.Onbuild)
	if !ok {
		return lines
	}
	out := slicesClone(lines)
	out[0] = line0

	// Find and blank the next token, which should be RUN.
	var runLineIdx, runStart, runEnd int
	found := false
	for li := range out {
		line := out[li]
		contIdx := continuationIndex(line, escapeToken)
		i := 0
		if li == 0 {
			i = after
		}
		for {
			i = skipSpaces(line, i)
			if i >= len(line) {
				break
			}
			start := i
			for i < len(line) && !isSpace(line[i]) {
				i++
			}
			end := i
			tok := line[start:end]
			if start == contIdx && tok == string(escapeToken) {
				// Only a continuation marker on this line; continue searching.
				break
			}
			if strings.EqualFold(tok, command.Run) {
				runLineIdx, runStart, runEnd = li, start, end
				found = true
			}
			// Stop at the first real token.
			break
		}
		if found {
			break
		}
	}
	if !found {
		return out
	}

	contIdx := continuationIndex(out[runLineIdx], escapeToken)
	out[runLineIdx] = blankRange(out[runLineIdx], runStart, runEnd, contIdx)
	return blankDockerFlagsUntilNonFlag(out, runLineIdx, runEnd, escapeToken)
}

func blankHealthcheckCmdShellLeading(lines []string, escapeToken rune) ([]string, bool) {
	if len(lines) == 0 {
		return nil, false
	}

	line0, after, ok := blankLeadingKeyword(lines[0], command.Healthcheck)
	if !ok {
		return nil, false
	}
	out := slicesClone(lines)
	out[0] = line0

	out, stopWord, found := blankDockerFlagsUntilStopWord(out, 0, after, escapeToken, []string{command.Cmd, "NONE"}, true)
	if !found {
		return nil, false
	}
	if strings.EqualFold(stopWord, "NONE") {
		return nil, false
	}
	return out, true
}

func blankLeadingKeyword(line, keyword string) (string, int, bool) {
	i := firstNonSpaceTab(line)
	if i < 0 {
		return line, 0, false
	}
	rest := line[i:]
	if len(rest) < len(keyword) || !strings.EqualFold(rest[:len(keyword)], keyword) {
		return line, 0, false
	}
	after := i + len(keyword)
	// Require a boundary (whitespace or end).
	if after < len(line) && !isSpace(line[after]) {
		return line, 0, false
	}

	contIdx := continuationIndex(line, '\\') // keyword is never the continuation token
	return blankRange(line, i, after, contIdx), after, true
}

func blankDockerFlagsUntilNonFlag(lines []string, startLineIdx, startCol int, escapeToken rune) []string {
	out, _, _ := blankDockerFlagsUntilFirstNonFlag(lines, startLineIdx, startCol, escapeToken)
	return out
}

type tokenPos struct {
	lineIdx int
	start   int
	end     int
	tok     string
	contIdx int
}

func blankDockerFlagsUntilFirstNonFlag(lines []string, startLineIdx, startCol int, escapeToken rune) ([]string, tokenPos, bool) {
	out := slicesClone(lines)

	for li := startLineIdx; li < len(out); li++ {
		line := out[li]
		contIdx := continuationIndex(line, escapeToken)

		i := 0
		if li == startLineIdx {
			i = startCol
		}
		for {
			i = skipSpaces(line, i)
			if i >= len(line) {
				break
			}
			start := i
			for i < len(line) && !isSpace(line[i]) {
				i++
			}
			end := i
			tok := line[start:end]

			// Ignore a bare continuation marker (e.g. "RUN \\").
			if start == contIdx && tok == string(escapeToken) {
				break
			}

			if strings.HasPrefix(tok, "--") {
				line = blankRange(line, start, end, contIdx)
				out[li] = line
				continue
			}

			// First non-flag token: script begins here.
			return out, tokenPos{
				lineIdx: li,
				start:   start,
				end:     end,
				tok:     tok,
				contIdx: contIdx,
			}, true
		}
	}

	return out, tokenPos{}, false
}

func blankDockerFlagsUntilStopWord(
	lines []string,
	startLineIdx, startCol int,
	escapeToken rune,
	stopWords []string,
	blankStopWord bool,
) ([]string, string, bool) {
	out, tok, found := blankDockerFlagsUntilFirstNonFlag(lines, startLineIdx, startCol, escapeToken)
	if !found {
		return out, "", false
	}

	for _, sw := range stopWords {
		if strings.EqualFold(tok.tok, sw) {
			if blankStopWord {
				out[tok.lineIdx] = blankRange(out[tok.lineIdx], tok.start, tok.end, tok.contIdx)
			}
			return out, tok.tok, true
		}
	}

	// Unexpected token before stop word.
	return out, "", false
}

func continuationIndex(line string, escapeToken rune) int {
	trimmed := strings.TrimRight(line, " \t")
	if trimmed == "" {
		return -1
	}
	last := len(trimmed) - 1
	if rune(trimmed[last]) != escapeToken {
		return -1
	}
	return last
}

func blankRange(line string, start, end, preserveIdx int) string {
	if start < 0 {
		start = 0
	}
	if end > len(line) {
		end = len(line)
	}
	if start >= end {
		return line
	}
	b := []byte(line)
	for i := start; i < end; i++ {
		if i == preserveIdx {
			continue
		}
		b[i] = ' '
	}
	return string(b)
}

func firstNonSpaceTab(line string) int {
	for i := range len(line) {
		if line[i] != ' ' && line[i] != '\t' {
			return i
		}
	}
	return -1
}

func skipSpaces(line string, i int) int {
	for i < len(line) && isSpace(line[i]) {
		i++
	}
	return i
}

func isSpace(b byte) bool {
	return b == ' ' || b == '\t'
}
