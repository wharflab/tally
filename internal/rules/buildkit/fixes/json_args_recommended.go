package fixes

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode"

	"github.com/tinovyatkin/tally/internal/rules"
	"github.com/tinovyatkin/tally/internal/shell"
)

func enrichJSONArgsRecommendedFix(v *rules.Violation, source []byte) {
	// We only support converting simple shell-form CMD/ENTRYPOINT to exec-form JSON.
	lineNum := v.Location.Start.Line
	if lineNum <= 0 {
		return
	}

	lineIdx := lineNum - 1
	line := getLine(source, lineIdx)
	if line == nil {
		return
	}

	codeEnd, hasComment := findDockerfileInlineCommentStart(line)

	it := ParseInstruction(line[:codeEnd])
	kw := it.FindKeyword("CMD")
	if kw == nil {
		kw = it.FindKeyword("ENTRYPOINT")
	}
	if kw == nil {
		return
	}

	firstArg := it.TokenAfter(kw)
	if firstArg == nil || firstArg.Start >= codeEnd {
		return
	}

	raw := strings.TrimSpace(string(line[firstArg.Start:codeEnd]))
	args, ok := shell.SplitSimpleCommand(raw, shell.VariantBash)
	if !ok {
		return
	}

	j, err := json.Marshal(args)
	if err != nil {
		// Should be impossible for []string, but keep this for linter satisfaction.
		return
	}
	newText := string(j)
	if hasComment {
		// We replace up to the comment marker (excluding it), so keep a single space
		// before the preserved comment.
		newText += " "
	}

	v.SuggestedFix = &rules.SuggestedFix{
		Description: fmt.Sprintf("Convert %s to exec form JSON array", strings.ToUpper(kw.Value)),
		Safety:      rules.FixSuggestion,
		Edits: []rules.TextEdit{
			{
				Location: rules.NewRangeLocation(v.Location.File, lineNum, firstArg.Start, lineNum, codeEnd),
				NewText:  newText,
			},
		},
		IsPreferred: true,
	}
}

// findDockerfileInlineCommentStart returns the byte offset where an inline comment starts.
// Dockerfile comment parsing is complex; this helper is conservative and only treats `#`
// as a comment start when it is preceded by whitespace and not inside quotes.
//
// It returns (idx, hasComment). When no inline comment is found, idx=len(line).
func findDockerfileInlineCommentStart(line []byte) (int, bool) {
	inSingle := false
	inDouble := false
	escaped := false

	for i := range line {
		ch := line[i]

		if escaped {
			escaped = false
			continue
		}
		if inDouble && ch == '\\' {
			escaped = true
			continue
		}

		switch ch {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
		case '#':
			if inSingle || inDouble {
				continue
			}
			// Only treat as comment start when preceded by whitespace (inline comment).
			if i > 0 && unicode.IsSpace(rune(line[i-1])) {
				return i, true
			}
		}
	}

	return len(line), false
}
