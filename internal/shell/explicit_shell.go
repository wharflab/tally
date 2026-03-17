package shell

import (
	"path"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ExplicitShellInvocation describes a shell wrapper explicitly invoked at the
// start of a shell-form instruction body, e.g. "pwsh -Command ..." or
// "cmd /c ...".
type ExplicitShellInvocation struct {
	ShellName string
	Variant   Variant
	Script    string
}

// ParseExplicitShellInvocation detects a leading shell wrapper invocation and
// returns the wrapper shell plus the payload passed to that shell.
func ParseExplicitShellInvocation(script string) (ExplicitShellInvocation, bool) {
	i := SkipShellTokenSpaces(script, 0)
	exeToken, next := NextShellToken(script, i)
	if exeToken == "" {
		return ExplicitShellInvocation{}, false
	}

	shellName := NormalizeShellExecutableName(DropQuotes(exeToken))
	variant := VariantFromShell(shellName)
	if variant == VariantUnknown {
		return ExplicitShellInvocation{}, false
	}

	//nolint:exhaustive // composite mask constants are capability sets, not discrete shell variants
	switch variant {
	case VariantPowerShell:
		return parsePowerShellInvocation(script, next, shellName)
	case VariantCmd:
		return parseCmdInvocation(script, next, shellName)
	case VariantBash, VariantPOSIX, VariantMksh, VariantZsh:
		return parsePOSIXShellInvocation(script, next, shellName, variant)
	default:
		return ExplicitShellInvocation{}, false
	}
}

func parsePowerShellInvocation(script string, next int, shellName string) (ExplicitShellInvocation, bool) {
	for {
		token, end := NextShellToken(script, next)
		if token == "" {
			return ExplicitShellInvocation{}, false
		}

		tokenNorm := strings.ToLower(DropQuotes(token))
		if tokenNorm == "-command" || tokenNorm == "-c" {
			return shellInvocationWithRemainder(script, end, shellName, VariantPowerShell)
		}
		next = end
	}
}

func parseCmdInvocation(script string, next int, shellName string) (ExplicitShellInvocation, bool) {
	for {
		token, end := NextShellToken(script, next)
		if token == "" {
			return ExplicitShellInvocation{}, false
		}

		tokenNorm := strings.ToLower(DropQuotes(token))
		if tokenNorm == "/c" || tokenNorm == "/k" {
			return shellInvocationWithRemainder(script, end, shellName, VariantCmd)
		}
		if !strings.HasPrefix(tokenNorm, "/") {
			return ExplicitShellInvocation{}, false
		}
		next = end
	}
}

func parsePOSIXShellInvocation(
	script string,
	next int,
	shellName string,
	variant Variant,
) (ExplicitShellInvocation, bool) {
	for {
		token, end := NextShellToken(script, next)
		if token == "" {
			return ExplicitShellInvocation{}, false
		}

		tokenNorm := strings.ToLower(DropQuotes(token))
		if tokenNorm == "-c" {
			return shellInvocationWithRemainder(script, end, shellName, variant)
		}
		next = end
	}
}

func shellInvocationWithRemainder(
	script string,
	end int,
	shellName string,
	variant Variant,
) (ExplicitShellInvocation, bool) {
	scriptStart := SkipShellTokenSpaces(script, end)
	if scriptStart >= len(script) {
		return ExplicitShellInvocation{}, false
	}
	return ExplicitShellInvocation{
		ShellName: shellName,
		Variant:   variant,
		Script:    strings.TrimSpace(script[scriptStart:]),
	}, true
}

// NormalizeShellExecutableName canonicalizes a shell executable path/name to
// its lowercase basename without a trailing .exe suffix.
func NormalizeShellExecutableName(exe string) string {
	exe = strings.ToLower(path.Base(strings.ReplaceAll(exe, `\`, "/")))
	return strings.TrimSuffix(exe, ".exe")
}

// NextShellToken returns the next shell-like token starting at or after start.
func NextShellToken(s string, start int) (string, int) {
	i := SkipShellTokenSpaces(s, start)
	if i >= len(s) {
		return "", i
	}

	if s[i] == '"' || s[i] == '\'' {
		quote := s[i]
		j := i + 1
		for j < len(s) {
			if s[j] == '\\' && j+1 < len(s) {
				j += 2
				continue
			}
			if s[j] == quote {
				break
			}
			j++
		}
		if j < len(s) {
			j++
		}
		return s[i:j], j
	}

	j := i
	for j < len(s) {
		r, sz := utf8.DecodeRuneInString(s[j:])
		if unicode.IsSpace(r) {
			break
		}
		j += sz
	}
	return s[i:j], j
}

// SkipShellTokenSpaces advances over whitespace in shell-like command lines.
func SkipShellTokenSpaces(s string, start int) int {
	i := start
	for i < len(s) {
		r, sz := utf8.DecodeRuneInString(s[i:])
		if !unicode.IsSpace(r) {
			break
		}
		i += sz
	}
	return i
}
