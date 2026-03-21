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
