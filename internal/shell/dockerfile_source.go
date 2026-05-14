package shell

import (
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/command"
)

// DockerfileRunCommandStartCol returns the byte offset where the shell command
// begins in the first line of a RUN instruction, after RUN and any leading flags.
func DockerfileRunCommandStartCol(firstLine string) int {
	trimmed := strings.TrimLeft(firstLine, " \t")
	offset := len(firstLine) - len(trimmed)

	upper := strings.ToUpper(trimmed)
	if strings.HasPrefix(upper, strings.ToUpper(command.Run)) {
		offset += len(command.Run)
	}

	rest := firstLine[offset:]
	trimmed = strings.TrimLeft(rest, " \t")
	offset += len(rest) - len(trimmed)

	for strings.HasPrefix(firstLine[offset:], "--") {
		offset = SkipDockerfileFlagValue(firstLine, offset, false)
		for offset < len(firstLine) && (firstLine[offset] == ' ' || firstLine[offset] == '\t') {
			offset++
		}
	}

	return offset
}

// SkipDockerfileFlagValue advances past a Dockerfile flag token starting at the
// provided offset. When stopAtLineContinuation is true, a trailing backslash is
// treated as the end of the token instead of part of the token.
func SkipDockerfileFlagValue(line string, offset int, stopAtLineContinuation bool) int {
	for offset < len(line) {
		switch line[offset] {
		case '"':
			offset++
			for offset < len(line) && line[offset] != '"' {
				offset++
			}
			if offset < len(line) {
				offset++
			}
		case ' ', '\t':
			return offset
		case '\\':
			if stopAtLineContinuation {
				return offset
			}
			offset++
		default:
			offset++
		}
	}
	return offset
}

// BridgeDockerfileCommentContinuations replaces Dockerfile comment-only lines
// inside a continued shell-form instruction with a synthetic continuation line.
// Dockerfile comments are removed before the shell sees the instruction; keeping
// them as shell comments can make a valid Dockerfile look like an invalid shell
// script when the preceding line ends with a continuation marker.
func BridgeDockerfileCommentContinuations(lines []string, escapeToken, target rune) []string {
	if len(lines) == 0 || escapeToken == 0 {
		return lines
	}
	if target == 0 {
		target = '\\'
	}

	var out []string
	continued := false
	for i, line := range lines {
		if isDockerfileCommentLine(line) {
			if continued {
				if out == nil {
					out = append([]string(nil), lines...)
				}
				out[i] = dockerfileContinuationBridgeLine(line, target)
			}
			continue
		}
		continued = dockerfileLineContinues(line, escapeToken)
	}
	if out == nil {
		return lines
	}
	return out
}

func isDockerfileCommentLine(line string) bool {
	trimmed := strings.TrimLeft(line, " \t")
	return strings.HasPrefix(trimmed, "#")
}

func dockerfileLineContinues(line string, escapeToken rune) bool {
	trimmed := strings.TrimRight(line, " \t")
	return trimmed != "" && strings.HasSuffix(trimmed, string(escapeToken))
}

func dockerfileContinuationBridgeLine(line string, target rune) string {
	i := 0
	for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
		i++
	}
	return line[:i] + string(target)
}
