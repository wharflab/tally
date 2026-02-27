package shellcheck

import (
	"fmt"
	"sort"
	"strings"

	"github.com/wharflab/tally/internal/rules"
)

const (
	sc1040Code     = 1040
	sc1040RuleCode = rules.ShellcheckRulePrefix + "SC1040"
	sc1040Message  = "When using <<-, you can only indent with tabs."
)

type shellcheckRuleSource uint8

const (
	shellcheckRuleSourceWASM shellcheckRuleSource = iota
	shellcheckRuleSourceNative
)

type nativeShellcheckChecker func(file string, fallbackLoc rules.Location, mapping scriptMapping) []rules.Violation

var shellcheckRuleOwnership = map[int]shellcheckRuleSource{
	sc1040Code: shellcheckRuleSourceNative,
}

var nativeShellcheckCheckers = map[int]nativeShellcheckChecker{
	sc1040Code: checkNativeSC1040,
}

func nativeOwnedShellcheckExcludeCodes() []string {
	owned := nativeOwnedShellcheckCodes()
	if len(owned) == 0 {
		return nil
	}

	exclude := make([]string, 0, len(owned))
	for _, code := range owned {
		exclude = append(exclude, fmt.Sprintf("SC%04d", code))
	}
	return exclude
}

func nativeOwnedShellcheckCodes() []int {
	codes := make([]int, 0, len(shellcheckRuleOwnership))
	for code, source := range shellcheckRuleOwnership {
		if source != shellcheckRuleSourceNative {
			continue
		}
		if nativeShellcheckCheckers[code] == nil {
			continue
		}
		codes = append(codes, code)
	}
	sort.Ints(codes)
	return codes
}

func runNativeShellcheckChecks(file string, fallbackLoc rules.Location, mapping scriptMapping) []rules.Violation {
	if mapping.Script == "" {
		return nil
	}

	codes := nativeOwnedShellcheckCodes()
	violations := make([]rules.Violation, 0)
	for _, code := range codes {
		checker := nativeShellcheckCheckers[code]
		if checker == nil {
			continue
		}
		violations = append(violations, checker(file, fallbackLoc, mapping)...)
	}
	return violations
}

type pendingHereDoc struct {
	dashed bool
	token  string
}

type hereDocEndAnalysis struct {
	terminated  bool
	violationAt int
	fixEnd      int
	fixText     string
}

func checkNativeSC1040(file string, fallbackLoc rules.Location, mapping scriptMapping) []rules.Violation {
	if mapping.Script == "" {
		return nil
	}

	originLine := mapping.OriginStartLine
	if originLine <= 0 {
		originLine = fallbackLoc.Start.Line
	}
	if originLine <= 0 {
		originLine = mapping.FallbackLine
	}
	if originLine <= 0 {
		originLine = 1
	}

	lines := strings.Split(mapping.Script, "\n")
	pending := make([]pendingHereDoc, 0, 2)
	violations := make([]rules.Violation, 0)

	for idx, line := range lines {
		if len(pending) > 0 {
			analysis := analyzeHereDocEndLine(line, pending[0])
			if analysis.violationAt >= 0 {
				dockerLine := originLine + idx
				loc := rules.NewRangeLocation(file, dockerLine, analysis.violationAt, dockerLine, analysis.violationAt)
				v := rules.NewViolation(loc, sc1040RuleCode, sc1040Message, rules.SeverityError).
					WithDocURL(rules.ShellcheckDocURL("SC1040"))
				fix := &rules.SuggestedFix{
					Description: "Normalize <<- heredoc terminator indentation (tabs only)",
					Safety:      rules.FixSafe,
					IsPreferred: true,
					Edits: []rules.TextEdit{{
						Location: rules.NewRangeLocation(file, dockerLine, analysis.violationAt, dockerLine, analysis.fixEnd),
						NewText:  analysis.fixText,
					}},
				}
				violations = append(violations, v.WithSuggestedFix(fix))
			}

			if analysis.terminated {
				pending = pending[1:]
			}
			continue
		}

		pending = append(pending, parseHereDocStarts(line)...)
	}

	return violations
}

func analyzeHereDocEndLine(line string, pending pendingHereDoc) hereDocEndAnalysis {
	start, matched := matchHereDocTokenStart(line, pending.token)
	if !matched {
		return hereDocEndAnalysis{violationAt: -1}
	}

	afterToken := start + len(pending.token)
	trailingEnd := afterToken
	for trailingEnd < len(line) && isLineWhitespace(line[trailingEnd]) {
		trailingEnd++
	}

	leading := line[:start]
	trailingSpace := line[afterToken:trailingEnd]
	trailer := line[trailingEnd:]

	hasTrailingSpace := trailingSpace != ""
	hasTrailer := trailer != ""
	leadingTabsOnly := allTabs(leading)
	leaderIsOk := leading == "" || (pending.dashed && leadingTabsOnly)

	if leaderIsOk && !hasTrailingSpace && !hasTrailer {
		return hereDocEndAnalysis{terminated: true, violationAt: -1}
	}

	if leaderIsOk && hasTrailingSpace && !hasTrailer {
		return hereDocEndAnalysis{terminated: true, violationAt: -1}
	}

	if pending.dashed && !leadingTabsOnly {
		return hereDocEndAnalysis{
			violationAt: 0,
			fixEnd:      start,
			fixText:     strings.ReplaceAll(leading, " ", ""),
		}
	}

	return hereDocEndAnalysis{violationAt: -1}
}

func matchHereDocTokenStart(line, token string) (int, bool) {
	if token == "" || len(line) < len(token) {
		return 0, false
	}

	maxStart := len(line) - len(token)
	for i := 0; i <= maxStart; i++ {
		if i > 0 && !isLineWhitespace(line[i-1]) {
			break
		}
		if strings.HasPrefix(line[i:], token) {
			return i, true
		}
	}
	return 0, false
}

func parseHereDocStarts(line string) []pendingHereDoc {
	starts := make([]pendingHereDoc, 0, 1)

	for i := 0; i+1 < len(line); {
		switch line[i] {
		case '\\':
			if i+1 < len(line) {
				i += 2
				continue
			}
			i++
			continue
		case '\'', '"', '`':
			i = skipQuoted(line, i)
			continue
		case '#':
			return starts
		}

		if line[i] != '<' || line[i+1] != '<' {
			i++
			continue
		}

		j := i + 2
		dashed := false
		if j < len(line) && line[j] == '-' {
			dashed = true
			j++
		}

		for j < len(line) && isLineWhitespace(line[j]) {
			j++
		}

		token, consumed := readHereDocToken(line[j:])
		if consumed == 0 {
			i += 2
			continue
		}
		decoded := unquoteHereDocToken(token)
		if decoded != "" {
			starts = append(starts, pendingHereDoc{dashed: dashed, token: decoded})
		}
		i = j + consumed
	}

	return starts
}

func readHereDocToken(s string) (string, int) {
	if s == "" {
		return "", 0
	}

	var b strings.Builder
	i := 0
	for i < len(s) {
		c := s[i]
		if isHereDocTokenTerminator(c) {
			break
		}

		if c == '\\' {
			b.WriteByte(c)
			i++
			if i < len(s) {
				b.WriteByte(s[i])
				i++
			}
			continue
		}

		if c == '\'' || c == '"' {
			quote := c
			b.WriteByte(c)
			i++
			for i < len(s) {
				d := s[i]
				b.WriteByte(d)
				i++
				if quote == '"' && d == '\\' && i < len(s) {
					b.WriteByte(s[i])
					i++
					continue
				}
				if d == quote {
					break
				}
			}
			continue
		}

		b.WriteByte(c)
		i++
	}

	token := b.String()
	if token == "" {
		return "", 0
	}
	return token, i
}

func unquoteHereDocToken(token string) string {
	if len(token) >= 2 {
		if (token[0] == '\'' && token[len(token)-1] == '\'') || (token[0] == '"' && token[len(token)-1] == '"') {
			return token[1 : len(token)-1]
		}
	}
	if strings.Contains(token, "\\") {
		return strings.ReplaceAll(token, "\\", "")
	}
	return token
}

func skipQuoted(line string, start int) int {
	quote := line[start]
	i := start + 1
	for i < len(line) {
		if quote == '"' && line[i] == '\\' {
			i += 2
			continue
		}
		if line[i] == quote {
			return i + 1
		}
		i++
	}
	return len(line)
}

func isHereDocTokenTerminator(c byte) bool {
	if isLineWhitespace(c) {
		return true
	}
	switch c {
	case ';', '|', '&', '<', '>', '(', ')':
		return true
	default:
		return false
	}
}

func isLineWhitespace(c byte) bool {
	return c == ' ' || c == '\t'
}

func allTabs(s string) bool {
	for i := range len(s) {
		if s[i] != '\t' {
			return false
		}
	}
	return true
}
