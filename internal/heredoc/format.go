// Package heredoc provides utilities for formatting heredoc RUN instructions.
package heredoc

import (
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/wharflab/tally/internal/runmount"
	"github.com/wharflab/tally/internal/shell"
)

// FormatWithMounts formats commands as a heredoc RUN instruction.
// If mounts are provided, they are included in the RUN instruction.
//
// For POSIX shells we prepend "set -e" (and optionally "set -o pipefail") to
// preserve the fail-fast semantics of && chains. For PowerShell we prepend
// "$ErrorActionPreference = 'Stop'" and add explicit inter-command guards. For
// cmd.exe we keep the original && chain semantics in a single parenthesized
// command block; Docker-validated WCOW heredoc bodies do not reliably execute
// multi-line cmd chains even with caret continuations.
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
	for _, line := range heredocBodyLines(commands, variant, pipefail) {
		sb.WriteString(line)
		sb.WriteString("\n")
	}
	sb.WriteString("EOF")
	return sb.String()
}

func heredocBodyLines(commands []string, variant shell.Variant, pipefail bool) []string {
	switch variant {
	case shell.VariantPowerShell:
		return powerShellBodyLines(commands)
	case shell.VariantCmd:
		return cmdBodyLines(commands)
	default:
		return posixBodyLines(commands, variant, pipefail)
	}
}

func posixBodyLines(commands []string, variant shell.Variant, pipefail bool) []string {
	lines := []string{"set -e"}
	if pipefail {
		lines = append(lines, "set -o pipefail")
	}
	for _, cmd := range commands {
		trimmed := strings.TrimSpace(cmd)
		if shell.SetsErrorFlag(cmd, variant) && trimmed == "set -e" {
			continue
		}
		if pipefail && trimmed == "set -o pipefail" {
			continue
		}
		lines = append(lines, cmd)
	}
	return lines
}

func powerShellBodyLines(commands []string) []string {
	lines := make([]string, 0, len(commands)*2+1)
	hasStopPrelude := false

	for _, cmd := range commands {
		trimmed := strings.TrimSpace(cmd)
		if trimmed == "" {
			continue
		}
		if strings.EqualFold(trimmed, "$ErrorActionPreference = 'Stop'") ||
			strings.EqualFold(trimmed, `$ErrorActionPreference = "Stop"`) {
			hasStopPrelude = true
		}
	}

	if !hasStopPrelude {
		lines = append(lines, "$ErrorActionPreference = 'Stop'")
	}

	guard := "if (-not $?) { if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }; exit 1 }"
	lastIdx := -1
	for i, cmd := range commands {
		if strings.TrimSpace(cmd) != "" {
			lastIdx = i
		}
	}

	for i, cmd := range commands {
		trimmed := strings.TrimSpace(cmd)
		if trimmed == "" {
			continue
		}
		if !hasStopPrelude &&
			(strings.EqualFold(trimmed, "$ErrorActionPreference = 'Stop'") ||
				strings.EqualFold(trimmed, `$ErrorActionPreference = "Stop"`)) {
			continue
		}
		lines = append(lines, cmd)
		if i != lastIdx {
			lines = append(lines, guard)
		}
	}

	return lines
}

func cmdBodyLines(commands []string) []string {
	filtered := make([]string, 0, len(commands))
	for _, cmd := range commands {
		if strings.TrimSpace(cmd) != "" {
			filtered = append(filtered, cmd)
		}
	}
	if len(filtered) == 0 {
		return nil
	}
	if len(filtered) == 1 {
		return filtered
	}

	// Docker-validated on WCOW: cmd.exe heredoc bodies only execute reliably when
	// the command list is kept on a single logical line. Multi-line bodies run
	// the first line but do not continue the chain, even when using caret
	// continuations or line-by-line guards. Keep the heredoc body as one grouped
	// && list so it still reads like a single block of work.
	return []string{"(" + strings.Join(filtered, " && ") + ")"}
}
