// Package heredoc provides utilities for formatting heredoc RUN instructions.
package heredoc

import (
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/tinovyatkin/tally/internal/runmount"
	"github.com/tinovyatkin/tally/internal/shell"
)

// FormatWithMounts formats commands as a heredoc RUN instruction.
// If mounts are provided, they are included in the RUN instruction.
//
// We always prepend "set -e" to preserve the fail-fast semantics of && chains.
// Without it, heredocs only fail if the LAST command fails - intermediate failures
// are silently ignored. This is different from && chains where any failure stops execution.
//
// When pipefail is true, "set -o pipefail" is also prepended to ensure that
// piped commands fail properly. This is the heredoc equivalent of
// SHELL ["/bin/bash", "-o", "pipefail", "-c"] and avoids needing a separate
// SHELL instruction when DL4006 is enabled alongside prefer-run-heredoc.
//
// See: https://github.com/moby/buildkit/issues/2722
// See: https://github.com/moby/buildkit/issues/4195
func FormatWithMounts(commands []string, mounts []*instructions.Mount, variant shell.Variant, pipefail bool) string {
	var sb strings.Builder
	sb.WriteString("RUN ")
	if len(mounts) > 0 {
		sb.WriteString(runmount.FormatMounts(mounts))
		sb.WriteString(" ")
	}
	sb.WriteString("<<EOF\n")
	sb.WriteString("set -e\n")
	if pipefail {
		sb.WriteString("set -o pipefail\n")
	}
	for _, cmd := range commands {
		// Skip only bare "set -e" since we already added one.
		// Preserve commands like "set -ex" or "set -euo pipefail" to retain
		// additional flags (-x for trace, -u for undefined vars, -o pipefail).
		if shell.SetsErrorFlag(cmd, variant) {
			trimmed := strings.TrimSpace(cmd)
			if trimmed == "set -e" {
				continue
			}
		}
		// Skip bare "set -o pipefail" when we already emit it.
		if pipefail && strings.TrimSpace(cmd) == "set -o pipefail" {
			continue
		}
		sb.WriteString(cmd)
		sb.WriteString("\n")
	}
	sb.WriteString("EOF")
	return sb.String()
}
