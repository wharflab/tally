package ruby

import (
	"strings"
)

// ParseRubyVersionFile parses a `.ruby-version` file's content. The format
// accepted by chruby/rbenv/asdf is one of:
//
//	3.3.5
//	ruby-3.3.5
//
// Optional surrounding whitespace and a trailing newline are tolerated.
// Returns "" when no version is found.
func ParseRubyVersionFile(content []byte) string {
	if len(content) == 0 {
		return ""
	}
	for line := range strings.SplitSeq(string(content), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		// "ruby-3.3.5" -> "3.3.5".
		trimmed = strings.TrimPrefix(trimmed, "ruby-")
		return trimmed
	}
	return ""
}

// ParseToolVersionsFile parses a `.tool-versions` (asdf format) file. Each
// non-blank line lists `<tool> <version> [version2 ...]`. We return the first
// version listed for the `ruby` tool, or "" if none is found.
func ParseToolVersionsFile(content []byte) string {
	if len(content) == 0 {
		return ""
	}
	for line := range strings.SplitSeq(string(content), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		fields := strings.Fields(trimmed)
		if len(fields) < 2 {
			continue
		}
		if !strings.EqualFold(fields[0], "ruby") {
			continue
		}
		return fields[1]
	}
	return ""
}
