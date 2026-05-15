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

// gemfileGemHeadRe matches the start of a `gem "name"` declaration in either
// the bare-call form (`gem "name", ...`) or the parenthesized DSL form
// (`gem("name", ...)`). The remainder of the head line is captured for
// option-scanning after joining any trailing continuation lines.
var gemfileGemHeadRe = regexp.MustCompile(`(?m)^\s*gem\s*[\s(]\s*['"]([^'"]+)['"](.*)$`)

// gemfileGroupRe matches a `group :a, :b do` block opener (or its
// parenthesized DSL form `group(:a, :b) do`) and captures the comma-separated
// symbol list (`:a, :b`). The capture is the text between the `group`
// keyword and the trailing ` do` on the same line; surrounding parentheses
// are stripped before the symbol list is parsed.
var gemfileGroupRe = regexp.MustCompile(`(?m)^\s*group\s*[\s(]\s*(.+?)\s*\)?\s+do\b`)

// gitBlockOpenerRe matches the start of a Bundler git/git_source block
// (`git "URL" do`, `git_source(:foo) do`). The pattern is anchored to a
// single line; callers feed it one trimmed line at a time. All gem entries
// inside the block inherit the git source even when they don't carry an
// inline `git:`/`github:` option, so we mark them as git gems.
var gitBlockOpenerRe = regexp.MustCompile(`^(?:git|git_source)\b.*\bdo(?:\s*\|[^|]*\|)?\s*$`)

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

	gitBlockSpans := findGitBlockSpans(text)
	seenGit := map[string]bool{}
	for _, m := range gemfileGemHeadRe.FindAllStringSubmatchIndex(text, -1) {
		// m[0]/m[1] = whole-match span, m[2]/m[3] = name span,
		// m[4]/m[5] = "rest of head line" span.
		name := text[m[2]:m[3]]
		gemStart := m[0]
		rest := joinGemDeclaration(text, m[4], m[5])
		inGitBlock := offsetInsideAnySpan(gemStart, gitBlockSpans)
		if (inGitBlock || hasGitOrGithubOption(rest)) && !seenGit[name] {
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

	return facts
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

// joinGemDeclaration returns the gem-declaration tail starting at restStart,
// extending across continuation lines while the previous logical line ends
// with `,` or `\` (Ruby's argument-continuation tokens). The walk stops at
// the first non-continuing line so unrelated `gem` calls below do not pollute
// the option scan.
func joinGemDeclaration(text string, restStart, restEnd int) string {
	if restStart >= len(text) {
		return ""
	}
	rest := text[restStart:restEnd]
	cursor := restEnd
	for continuesGemDeclaration(rest) && cursor < len(text) {
		// Skip past the trailing newline.
		if cursor < len(text) && text[cursor] == '\n' {
			cursor++
		}
		// Find the next newline (or EOF) and capture the line.
		next := indexByteFrom(text, '\n', cursor)
		if next < 0 {
			next = len(text)
		}
		line := text[cursor:next]
		rest += "\n" + line
		cursor = next
	}
	return rest
}

func indexByteFrom(s string, b byte, start int) int {
	if start >= len(s) {
		return -1
	}
	idx := strings.IndexByte(s[start:], b)
	if idx < 0 {
		return -1
	}
	return start + idx
}

// continuesGemDeclaration reports whether the supplied gem-declaration tail
// is syntactically incomplete and therefore continues onto the next line.
// We strip whitespace and trailing comments first so noise after the comma
// does not interfere.
func continuesGemDeclaration(rest string) bool {
	stripped := strings.TrimRight(stripGemfileLineComment(rest), " \t\r\n")
	if stripped == "" {
		return false
	}
	last := stripped[len(stripped)-1]
	return last == ',' || last == '\\'
}

// findGitBlockSpans returns half-open [start, end) byte ranges covering the
// body of every `git "URL" do ... end` (and `git_source(...) do ... end`)
// block in text. The implementation is line-oriented: any line whose stripped
// form looks like a Ruby block opener (ends in `do` or `do |...|`) increments
// the depth, and any line whose stripped form is a bare `end` decrements it.
// This is good enough for the formatting styles real Gemfiles use.
func findGitBlockSpans(text string) [][2]int {
	type openBlock struct {
		bodyStart int
		isGit     bool
	}

	var (
		spans     [][2]int
		stack     []openBlock
		lineStart int
	)
	for lineStart < len(text) {
		nl := indexByteFrom(text, '\n', lineStart)
		lineEnd := nl
		if lineEnd < 0 {
			lineEnd = len(text)
		}
		line := text[lineStart:lineEnd]
		trimmed := strings.TrimSpace(line)
		switch {
		case isGitBlockOpenerLine(trimmed):
			stack = append(stack, openBlock{bodyStart: lineEnd, isGit: true})
		case isBlockOpenerLine(trimmed):
			stack = append(stack, openBlock{bodyStart: lineEnd, isGit: false})
		case isBlockCloserLine(trimmed):
			if n := len(stack); n > 0 {
				top := stack[n-1]
				stack = stack[:n-1]
				if top.isGit {
					spans = append(spans, [2]int{top.bodyStart, lineStart})
				}
			}
		}
		if nl < 0 {
			break
		}
		lineStart = nl + 1
	}
	// Any unclosed git blocks (malformed Gemfile) extend to EOF.
	for _, top := range stack {
		if top.isGit {
			spans = append(spans, [2]int{top.bodyStart, len(text)})
		}
	}
	return spans
}

func isGitBlockOpenerLine(trimmed string) bool {
	return gitBlockOpenerRe.MatchString(trimmed)
}

func isBlockOpenerLine(trimmed string) bool {
	if trimmed == "" {
		return false
	}
	// We only treat lines whose final non-whitespace token is `do` (or
	// `do |arg|`) as block openers. Inline `do` tokens inside strings or
	// long expressions are out of scope and acceptably rare in Gemfiles.
	return blockOpenerLineRe.MatchString(trimmed)
}

func isBlockCloserLine(trimmed string) bool {
	return trimmed == "end"
}

// offsetInsideAnySpan reports whether offset falls within any of the supplied
// half-open spans.
func offsetInsideAnySpan(offset int, spans [][2]int) bool {
	for _, span := range spans {
		if offset >= span[0] && offset < span[1] {
			return true
		}
	}
	return false
}

// blockOpenerLineRe matches a line whose syntactic tail is a Ruby block
// opener (`... do` or `... do |args|`). Line-anchored so opening tokens
// inside string literals on other lines do not match.
var blockOpenerLineRe = regexp.MustCompile(`\bdo(?:\s*\|[^|]*\|)?\s*$`)

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
