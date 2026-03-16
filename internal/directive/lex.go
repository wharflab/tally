package directive

import "regexp"

type CommentTokenKind int

const (
	CommentTokenKeyword CommentTokenKind = iota
	CommentTokenOperator
	CommentTokenValue
	CommentTokenRule
)

type CommentToken struct {
	StartByte int
	EndByte   int
	Kind      CommentTokenKind
}

var (
	syntaxLexPattern = regexp.MustCompile(
		`(?i)#\s*(syntax)\s*(=)\s*(\S(?:.*\S)?)\s*$`)
	escapeLexPattern = regexp.MustCompile(
		`(?i)#\s*(escape)\s*(=)\s*(\S(?:.*\S)?)\s*$`)
	tallyIgnoreLexPattern = regexp.MustCompile(
		`(?i)#\s*(tally)\s+((global)\s+)?(ignore)\s*(=)\s*([A-Za-z0-9_,\s/.-]+?)(?:;(reason)\s*(=)\s*(.*))?$`)
	hadolintIgnoreLexPattern = regexp.MustCompile(
		`(?i)#\s*(hadolint)\s+((global)\s+)?(ignore)\s*(=)\s*([A-Za-z0-9_,\s/.-]+?)(?:;(reason)\s*(=)\s*(.*))?$`)
	buildxLexPattern = regexp.MustCompile(
		`(?i)#\s*(check)\s*(=)\s*(skip)\s*(=)\s*([A-Za-z0-9_,\s/.-]+?)(?:;(reason)\s*(=)\s*(.*))?$`)
	tallyShellLexPattern = regexp.MustCompile(
		`(?i)#\s*(tally)\s+(shell)\s*(=)\s*([A-Za-z0-9_./-]+)\s*$`)
	hadolintShellLexPattern = regexp.MustCompile(
		`(?i)#\s*(hadolint)\s+(shell)\s*(=)\s*([A-Za-z0-9_./-]+)\s*$`)
	ruleListTokenPattern = regexp.MustCompile(`[A-Za-z0-9./_-]+`)
)

// LexComment tokenizes recognized directive comments into semantic pieces.
// Input text must include the leading # and should already be left-trimmed.
func LexComment(text string) []CommentToken {
	if tokens := lexKeywordValueComment(text, syntaxLexPattern); tokens != nil {
		return tokens
	}
	if tokens := lexKeywordValueComment(text, escapeLexPattern); tokens != nil {
		return tokens
	}
	if tokens := lexIgnoreComment(text, tallyIgnoreLexPattern); tokens != nil {
		return tokens
	}
	if tokens := lexIgnoreComment(text, hadolintIgnoreLexPattern); tokens != nil {
		return tokens
	}
	if tokens := lexBuildxComment(text); tokens != nil {
		return tokens
	}
	if tokens := lexShellComment(text, tallyShellLexPattern); tokens != nil {
		return tokens
	}
	if tokens := lexShellComment(text, hadolintShellLexPattern); tokens != nil {
		return tokens
	}
	return nil
}

func lexKeywordValueComment(text string, pattern *regexp.Regexp) []CommentToken {
	matches := pattern.FindStringSubmatchIndex(text)
	if matches == nil {
		return nil
	}

	return []CommentToken{
		{StartByte: matches[2], EndByte: matches[3], Kind: CommentTokenKeyword},
		{StartByte: matches[4], EndByte: matches[5], Kind: CommentTokenOperator},
		{StartByte: matches[6], EndByte: matches[7], Kind: CommentTokenValue},
	}
}

func lexIgnoreComment(text string, pattern *regexp.Regexp) []CommentToken {
	matches := pattern.FindStringSubmatchIndex(text)
	if matches == nil {
		return nil
	}

	tokens := make([]CommentToken, 0, 8)
	tokens = append(tokens, CommentToken{
		StartByte: matches[2],
		EndByte:   matches[3],
		Kind:      CommentTokenKeyword,
	})
	if matches[6] >= 0 && matches[7] >= 0 {
		tokens = append(tokens, CommentToken{
			StartByte: matches[6],
			EndByte:   matches[7],
			Kind:      CommentTokenKeyword,
		})
	}
	tokens = append(tokens,
		CommentToken{StartByte: matches[8], EndByte: matches[9], Kind: CommentTokenKeyword},
		CommentToken{StartByte: matches[10], EndByte: matches[11], Kind: CommentTokenOperator},
	)
	tokens = append(tokens, lexRuleList(text, matches[12], matches[13])...)
	if matches[14] >= 0 && matches[15] >= 0 {
		tokens = append(tokens,
			CommentToken{StartByte: matches[14], EndByte: matches[15], Kind: CommentTokenKeyword},
			CommentToken{StartByte: matches[16], EndByte: matches[17], Kind: CommentTokenOperator},
			CommentToken{StartByte: matches[18], EndByte: matches[19], Kind: CommentTokenValue},
		)
	}
	return tokens
}

func lexBuildxComment(text string) []CommentToken {
	matches := buildxLexPattern.FindStringSubmatchIndex(text)
	if matches == nil {
		return nil
	}

	tokens := make([]CommentToken, 0, 8)
	tokens = append(tokens,
		CommentToken{StartByte: matches[2], EndByte: matches[3], Kind: CommentTokenKeyword},
		CommentToken{StartByte: matches[4], EndByte: matches[5], Kind: CommentTokenOperator},
		CommentToken{StartByte: matches[6], EndByte: matches[7], Kind: CommentTokenKeyword},
		CommentToken{StartByte: matches[8], EndByte: matches[9], Kind: CommentTokenOperator},
	)
	tokens = append(tokens, lexRuleList(text, matches[10], matches[11])...)
	if matches[12] >= 0 && matches[13] >= 0 {
		tokens = append(tokens,
			CommentToken{StartByte: matches[12], EndByte: matches[13], Kind: CommentTokenKeyword},
			CommentToken{StartByte: matches[14], EndByte: matches[15], Kind: CommentTokenOperator},
			CommentToken{StartByte: matches[16], EndByte: matches[17], Kind: CommentTokenValue},
		)
	}
	return tokens
}

func lexShellComment(text string, pattern *regexp.Regexp) []CommentToken {
	matches := pattern.FindStringSubmatchIndex(text)
	if matches == nil {
		return nil
	}

	return []CommentToken{
		{StartByte: matches[2], EndByte: matches[3], Kind: CommentTokenKeyword},
		{StartByte: matches[4], EndByte: matches[5], Kind: CommentTokenKeyword},
		{StartByte: matches[6], EndByte: matches[7], Kind: CommentTokenOperator},
		{StartByte: matches[8], EndByte: matches[9], Kind: CommentTokenValue},
	}
}

func lexRuleList(text string, start, end int) []CommentToken {
	if start < 0 || end <= start {
		return nil
	}
	matches := ruleListTokenPattern.FindAllStringIndex(text[start:end], -1)
	tokens := make([]CommentToken, 0, len(matches))
	for _, match := range matches {
		tokens = append(tokens, CommentToken{
			StartByte: start + match[0],
			EndByte:   start + match[1],
			Kind:      CommentTokenRule,
		})
	}
	return tokens
}
