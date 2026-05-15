package ruby

import (
	"regexp"
	"strings"
)

// GemfileFacts is the typed projection of a Gemfile that complements
// LockfileFacts. All fields are zero-valued when the Gemfile is unobservable
// or fails to parse.
type GemfileFacts struct {
	// RubyConstraint holds the value passed to the top-level ruby "..."
	// directive, when present. Quotes are stripped.
	RubyConstraint string

	// Sources lists every URL passed to a top-level source "..." directive,
	// preserving Gemfile order.
	Sources []string

	// GitGems lists gem entries that pin to a git: or github: source. Order
	// matches first appearance.
	GitGems []string

	// HasDevGroup is true when the Gemfile contains a `group :development do`
	// block (multi-group declarations like `group :development, :test do`
	// also count).
	HasDevGroup bool

	// HasTestGroup is true when the Gemfile contains a `group :test do` block
	// (multi-group declarations like `group :development, :test do` also count).
	HasTestGroup bool
}

// gemfileRubyRe matches a top-level `ruby "X"` or `ruby 'X'` directive.
// Patch-level constraints (`ruby "3.3.0", patchlevel: ...`) are accepted; only
// the leading version literal is captured.
var gemfileRubyRe = regexp.MustCompile(`(?m)^\s*ruby\s+['"]([^'"]+)['"]`)

// gemfileSourceRe matches a top-level `source "URL"` or `source 'URL'`
// directive. Anchored at the start of a line to ignore `source` calls inside
// gem option hashes.
var gemfileSourceRe = regexp.MustCompile(`(?m)^\s*source\s+['"]([^'"]+)['"]`)

// gemfileGemRe matches a `gem "name"` line and captures the full remainder
// (everything after the gem name). The remainder is then scanned for git: or
// github: options.
var gemfileGemRe = regexp.MustCompile(`(?m)^\s*gem\s+['"]([^'"]+)['"](.*)$`)

// gemfileGroupRe matches a `group :a, :b do` block opener and captures the
// comma-separated symbol list (`:a, :b`). The capture is the text between
// `group` and the final ` do` on the same line.
var gemfileGroupRe = regexp.MustCompile(`(?m)^\s*group\s+(.+?)\s+do\b`)

// gitOptionRe and githubOptionRe detect whether a gem entry's option list
// pins it to a git URL. Both `git: "..."`/`github: "..."` (Ruby 1.9 hash
// syntax) and `:git => "..."`/`:github => "..."` (legacy syntax) are matched.
var (
	gitOptionRe    = regexp.MustCompile(`(?:^|[\s,{])(?::git\s*=>|git:)\s*['"]`)
	githubOptionRe = regexp.MustCompile(`(?:^|[\s,{])(?::github\s*=>|github:)\s*['"]`)
)

// ParseGemfile parses Gemfile content into typed facts. Returns nil for empty
// input or input that yields no recognizable directives.
func ParseGemfile(content []byte) *GemfileFacts {
	if len(content) == 0 {
		return nil
	}
	text := stripGemfileComments(string(content))
	if strings.TrimSpace(text) == "" {
		return nil
	}

	facts := &GemfileFacts{}

	if m := gemfileRubyRe.FindStringSubmatch(text); m != nil {
		facts.RubyConstraint = m[1]
	}

	for _, m := range gemfileSourceRe.FindAllStringSubmatch(text, -1) {
		facts.Sources = append(facts.Sources, m[1])
	}

	seenGit := map[string]bool{}
	for _, m := range gemfileGemRe.FindAllStringSubmatch(text, -1) {
		name := m[1]
		rest := m[2]
		if hasGitOrGithubOption(rest) && !seenGit[name] {
			seenGit[name] = true
			facts.GitGems = append(facts.GitGems, name)
		}
	}

	for _, m := range gemfileGroupRe.FindAllStringSubmatch(text, -1) {
		symbols := parseGroupSymbols(m[1])
		for _, sym := range symbols {
			switch sym {
			case "development":
				facts.HasDevGroup = true
			case "test":
				facts.HasTestGroup = true
			}
		}
	}

	if !looksLikeGemfile(facts) {
		return nil
	}
	return facts
}

func looksLikeGemfile(facts *GemfileFacts) bool {
	return facts.RubyConstraint != "" ||
		len(facts.Sources) > 0 ||
		len(facts.GitGems) > 0 ||
		facts.HasDevGroup ||
		facts.HasTestGroup
}

// stripGemfileComments removes `#` comments from each line, preserving `#`
// characters that appear inside single- or double-quoted string literals.
// The implementation is intentionally simple: it does not understand Ruby
// string interpolation or heredocs, but it is good enough for the directive
// shapes the rules above care about.
func stripGemfileComments(text string) string {
	var sb strings.Builder
	sb.Grow(len(text))

	for line := range strings.SplitAfterSeq(text, "\n") {
		sb.WriteString(stripGemfileLineComment(line))
	}
	return sb.String()
}

func stripGemfileLineComment(line string) string {
	inSingle := false
	inDouble := false
	escape := false

	for i := range len(line) {
		ch := line[i]
		if escape {
			escape = false
			continue
		}
		switch ch {
		case '\\':
			if inSingle || inDouble {
				escape = true
			}
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
		case '#':
			if !inSingle && !inDouble {
				// Preserve the trailing newline, if any.
				if strings.HasSuffix(line, "\n") {
					return line[:i] + "\n"
				}
				return line[:i]
			}
		}
	}
	return line
}

// hasGitOrGithubOption checks the trailing part of a gem entry for git: or
// github: options.
func hasGitOrGithubOption(rest string) bool {
	if rest == "" {
		return false
	}
	return gitOptionRe.MatchString(rest) || githubOptionRe.MatchString(rest)
}

// parseGroupSymbols extracts the list of group symbols from the captured
// portion of `group :a, :b do`. Whitespace and the leading colon are stripped.
func parseGroupSymbols(raw string) []string {
	out := make([]string, 0, 2)
	for part := range strings.SplitSeq(raw, ",") {
		sym := strings.TrimSpace(part)
		sym = strings.TrimPrefix(sym, ":")
		if sym == "" {
			continue
		}
		out = append(out, sym)
	}
	return out
}
