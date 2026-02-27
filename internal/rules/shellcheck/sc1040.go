package shellcheck

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"mvdan.cc/sh/v3/syntax"

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
	shellcheckRuleSourceGo
)

type nativeShellcheckChecker func(file string, fallbackLoc rules.Location, mapping scriptMapping) []rules.Violation

var shellcheckRuleOwnership = map[int]shellcheckRuleSource{
	sc1040Code: shellcheckRuleSourceGo,
}

var nativeShellcheckCheckers = map[int]nativeShellcheckChecker{
	sc1040Code: checkNativeSC1040,
}

type hereDocTerminator struct {
	dashed bool
	token  string
}

type parsedDashHereDoc struct {
	token          string
	bodyStartLine  int
	terminatorLine int
}

type hereDocEndAnalysis struct {
	terminated  bool
	violationAt int
	spaceRuns   []columnRange
}

type columnRange struct {
	start int
	end   int
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
		if source != shellcheckRuleSourceGo {
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

func checkNativeSC1040(file string, fallbackLoc rules.Location, mapping scriptMapping) []rules.Violation {
	if mapping.Script == "" {
		return nil
	}

	parsedDocs, err := parseDashHereDocs(mapping.Script)
	if err != nil {
		// Parse errors are owned by parse-status handling; do not continue linting.
		return nil
	}
	if len(parsedDocs) == 0 {
		return nil
	}

	originLine := resolveOriginLine(mapping, fallbackLoc)
	lines := strings.Split(mapping.Script, "\n")
	violations := make([]rules.Violation, 0)

	for _, doc := range parsedDocs {
		lineStart := max(doc.bodyStartLine, 1)
		lineEnd := min(doc.terminatorLine, len(lines))
		if lineEnd < lineStart {
			continue
		}

		for scriptLine := lineStart; scriptLine <= lineEnd; scriptLine++ {
			analysis := analyzeHereDocEndLine(lines[scriptLine-1], hereDocTerminator{
				dashed: true,
				token:  doc.token,
			})
			if analysis.violationAt < 0 {
				continue
			}
			dockerLine := originLine + scriptLine - 1
			violations = append(violations, buildSC1040Violation(file, dockerLine, analysis))
		}
	}

	return violations
}

func resolveOriginLine(mapping scriptMapping, fallbackLoc rules.Location) int {
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
	return originLine
}

func buildSC1040Violation(file string, dockerLine int, analysis hereDocEndAnalysis) rules.Violation {
	loc := rules.NewRangeLocation(file, dockerLine, analysis.violationAt, dockerLine, analysis.violationAt)
	v := rules.NewViolation(loc, sc1040RuleCode, sc1040Message, rules.SeverityError).
		WithDocURL(rules.TallyDocURL(sc1040RuleCode))

	edits := make([]rules.TextEdit, 0, len(analysis.spaceRuns))
	for _, run := range analysis.spaceRuns {
		edits = append(edits, rules.TextEdit{
			Location: rules.NewRangeLocation(file, dockerLine, run.start, dockerLine, run.end),
			NewText:  "",
		})
	}

	fix := &rules.SuggestedFix{
		Description: "Normalize <<- heredoc terminator indentation (tabs only)",
		Safety:      rules.FixSafe,
		IsPreferred: true,
		Edits:       edits,
	}
	return v.WithSuggestedFix(fix)
}

func parseDashHereDocs(script string) ([]parsedDashHereDoc, error) {
	parser := syntax.NewParser(
		syntax.Variant(syntax.LangBash),
		syntax.KeepComments(false),
	)
	file, err := parser.Parse(strings.NewReader(script), "")
	if err != nil {
		return nil, err
	}

	docs := make([]parsedDashHereDoc, 0)
	syntax.Walk(file, func(node syntax.Node) bool {
		redir, ok := node.(*syntax.Redirect)
		if !ok || redir.Op.String() != "<<-" || redir.Word == nil || redir.Hdoc == nil {
			return true
		}

		token, ok := decodeHereDocWord(redir.Word)
		if !ok || token == "" {
			return true
		}

		startLine, ok := safeUintToInt(redir.Hdoc.Pos().Line())
		if !ok {
			return true
		}
		endLine, ok := safeUintToInt(redir.Hdoc.End().Line())
		if !ok || startLine <= 0 || endLine < startLine {
			return true
		}

		docs = append(docs, parsedDashHereDoc{
			token:          token,
			bodyStartLine:  startLine,
			terminatorLine: endLine,
		})
		return true
	})

	return docs, nil
}

func decodeHereDocWord(word *syntax.Word) (string, bool) {
	if word == nil {
		return "", false
	}

	var b strings.Builder
	for _, part := range word.Parts {
		switch p := part.(type) {
		case *syntax.Lit:
			b.WriteString(unescapeUnquoted(p.Value))
		case *syntax.SglQuoted:
			b.WriteString(p.Value)
		case *syntax.DblQuoted:
			s, ok := decodeDoubleQuotedWordParts(p.Parts)
			if !ok {
				return "", false
			}
			b.WriteString(s)
		default:
			// Non-literal parts (e.g. ${x}) should not appear in heredoc words.
			return "", false
		}
	}
	return b.String(), true
}

func decodeDoubleQuotedWordParts(parts []syntax.WordPart) (string, bool) {
	var b strings.Builder
	for _, part := range parts {
		switch p := part.(type) {
		case *syntax.Lit:
			b.WriteString(unescapeDoubleQuoted(p.Value))
		default:
			return "", false
		}
	}
	return b.String(), true
}

func unescapeUnquoted(s string) string {
	if !strings.Contains(s, "\\") {
		return s
	}

	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' {
			b.WriteByte(s[i])
			continue
		}
		if i+1 >= len(s) {
			continue
		}
		i++
		// Outside quotes, backslash escapes the next byte.
		if s[i] == '\n' {
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func unescapeDoubleQuoted(s string) string {
	if !strings.Contains(s, "\\") {
		return s
	}

	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '\\' {
			b.WriteByte(c)
			continue
		}
		if i+1 >= len(s) {
			b.WriteByte(c)
			continue
		}
		n := s[i+1]
		switch n {
		case '$', '`', '"', '\\':
			b.WriteByte(n)
			i++
		case '\n':
			i++
		default:
			b.WriteByte('\\')
			b.WriteByte(n)
			i++
		}
	}
	return b.String()
}

func analyzeHereDocEndLine(line string, term hereDocTerminator) hereDocEndAnalysis {
	start, matched := matchHereDocTokenStart(line, term.token)
	if !matched {
		return hereDocEndAnalysis{violationAt: -1}
	}

	afterToken := start + len(term.token)
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
	leaderIsOk := leading == "" || (term.dashed && leadingTabsOnly)

	if leaderIsOk && !hasTrailingSpace && !hasTrailer {
		return hereDocEndAnalysis{terminated: true, violationAt: -1}
	}
	if leaderIsOk && hasTrailingSpace && !hasTrailer {
		return hereDocEndAnalysis{terminated: true, violationAt: -1}
	}

	if term.dashed && !leadingTabsOnly {
		runs := leadingSpaceRuns(leading)
		if len(runs) == 0 {
			return hereDocEndAnalysis{violationAt: -1}
		}
		return hereDocEndAnalysis{
			violationAt: runs[0].start,
			spaceRuns:   runs,
		}
	}

	return hereDocEndAnalysis{violationAt: -1}
}

func leadingSpaceRuns(s string) []columnRange {
	runs := make([]columnRange, 0, 2)
	i := 0
	for i < len(s) {
		if s[i] != ' ' {
			i++
			continue
		}
		start := i
		i++
		for i < len(s) && s[i] == ' ' {
			i++
		}
		runs = append(runs, columnRange{start: start, end: i})
	}
	return runs
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

func safeUintToInt(v uint) (int, bool) {
	if v > uint(math.MaxInt) {
		return 0, false
	}
	return int(v), true
}
