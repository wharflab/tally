// Package fixes provides auto-fix functionality for BuildKit rules.
package fixes

import (
	"strings"
)

// TokenType identifies the kind of token.
type TokenType int

const (
	// TokenWhitespace represents spaces and tabs.
	TokenWhitespace TokenType = iota
	// TokenKeyword represents instruction names and keywords like AS.
	TokenKeyword
	// TokenFlag represents flags like --from=value or --platform=linux/amd64.
	TokenFlag
	// TokenArgument represents regular arguments (image names, paths, etc).
	TokenArgument
)

// Token represents a parsed token from a Dockerfile instruction line.
type Token struct {
	Type  TokenType
	Value string // The raw token text
	Start int    // Byte offset from line start (inclusive)
	End   int    // Byte offset from line start (exclusive)
}

// Tokenizer parses Dockerfile instruction lines into tokens.
// It tracks byte offsets for precise source location mapping.
type Tokenizer struct {
	input []byte
	pos   int // current position
}

// NewTokenizer creates a tokenizer for an instruction line.
func NewTokenizer(line []byte) *Tokenizer {
	return &Tokenizer{input: line, pos: 0}
}

// Tokenize parses the entire line into tokens.
func (t *Tokenizer) Tokenize() []Token {
	var tokens []Token
	for !t.atEnd() {
		tok := t.nextToken()
		if tok != nil {
			tokens = append(tokens, *tok)
		}
	}
	return tokens
}

// TokenizeLine is a convenience function to tokenize a line.
func TokenizeLine(line []byte) []Token {
	return NewTokenizer(line).Tokenize()
}

// atEnd returns true if we've reached the end of input.
func (t *Tokenizer) atEnd() bool {
	return t.pos >= len(t.input)
}

// peek returns the current byte without advancing.
func (t *Tokenizer) peek() byte {
	if t.atEnd() {
		return 0
	}
	return t.input[t.pos]
}

// advance moves forward by one byte.
func (t *Tokenizer) advance() {
	if t.atEnd() {
		return
	}
	t.pos++
}

// nextToken extracts the next token from input.
func (t *Tokenizer) nextToken() *Token {
	if t.atEnd() {
		return nil
	}

	ch := t.peek()

	// Whitespace
	if ch == ' ' || ch == '\t' {
		return t.scanWhitespace()
	}

	// Flag (--name or --name=value)
	if t.pos+1 < len(t.input) && ch == '-' && t.input[t.pos+1] == '-' {
		return t.scanFlag()
	}

	// Quoted string
	if ch == '"' || ch == '\'' {
		return t.scanQuoted(ch)
	}

	// Regular word (keyword or argument)
	return t.scanWord()
}

// scanWhitespace consumes whitespace characters.
func (t *Tokenizer) scanWhitespace() *Token {
	start := t.pos
	for !t.atEnd() {
		ch := t.peek()
		if ch != ' ' && ch != '\t' {
			break
		}
		t.advance()
	}
	return &Token{
		Type:  TokenWhitespace,
		Value: string(t.input[start:t.pos]),
		Start: start,
		End:   t.pos,
	}
}

// scanFlag consumes a flag token (--name or --name=value).
func (t *Tokenizer) scanFlag() *Token {
	start := t.pos
	t.advance() // first '-'
	t.advance() // second '-'

	// Scan flag name
	for !t.atEnd() {
		ch := t.peek()
		if ch == '=' || ch == ' ' || ch == '\t' {
			break
		}
		t.advance()
	}

	// Check for =value
	if !t.atEnd() && t.peek() == '=' {
		t.advance() // consume '='
		t.scanFlagValue()
	}

	return &Token{
		Type:  TokenFlag,
		Value: string(t.input[start:t.pos]),
		Start: start,
		End:   t.pos,
	}
}

// scanFlagValue consumes the value part of a flag after '='.
func (t *Tokenizer) scanFlagValue() {
	if t.atEnd() {
		return
	}

	ch := t.peek()

	// Quoted value
	if ch == '"' || ch == '\'' {
		t.scanQuotedContent(ch)
		return
	}

	// Unquoted value
	for !t.atEnd() {
		ch := t.peek()
		if ch == ' ' || ch == '\t' {
			break
		}
		t.advance()
	}
}

// scanQuoted consumes a quoted string as a single argument token.
func (t *Tokenizer) scanQuoted(quote byte) *Token {
	start := t.pos
	t.scanQuotedContent(quote)
	return &Token{
		Type:  TokenArgument,
		Value: string(t.input[start:t.pos]),
		Start: start,
		End:   t.pos,
	}
}

// scanQuotedContent consumes characters until closing quote.
// Handles escaped characters.
func (t *Tokenizer) scanQuotedContent(quote byte) {
	t.advance() // opening quote
	for !t.atEnd() {
		ch := t.peek()
		if ch == '\\' && t.pos+1 < len(t.input) {
			// Escape sequence
			t.advance() // backslash
			t.advance() // escaped char
			continue
		}
		if ch == quote {
			t.advance() // closing quote
			break
		}
		t.advance()
	}
}

// scanWord consumes an unquoted word (keyword or argument).
func (t *Tokenizer) scanWord() *Token {
	start := t.pos
	for !t.atEnd() {
		ch := t.peek()
		if ch == ' ' || ch == '\t' || ch == '"' || ch == '\'' {
			break
		}
		t.advance()
	}

	value := string(t.input[start:t.pos])
	tokType := TokenArgument

	// Check if this is a keyword
	upper := strings.ToUpper(value)
	if isDockerfileKeyword(upper) {
		tokType = TokenKeyword
	}

	return &Token{
		Type:  tokType,
		Value: value,
		Start: start,
		End:   t.pos,
	}
}

// isDockerfileKeyword returns true if the word is a Dockerfile keyword.
func isDockerfileKeyword(word string) bool {
	switch word {
	case "FROM", "AS", "RUN", "CMD", "LABEL", "MAINTAINER", "EXPOSE", "ENV",
		"ADD", "COPY", "ENTRYPOINT", "VOLUME", "USER", "WORKDIR", "ARG",
		"ONBUILD", "STOPSIGNAL", "HEALTHCHECK", "SHELL":
		return true
	}
	return false
}

// InstructionTokens provides convenient access to parsed instruction tokens.
type InstructionTokens struct {
	tokens []Token
	line   []byte
}

// ParseInstruction tokenizes a line and returns a structured accessor.
func ParseInstruction(line []byte) *InstructionTokens {
	return &InstructionTokens{
		tokens: TokenizeLine(line),
		line:   line,
	}
}

// FindKeyword finds a specific keyword token (case-insensitive).
func (it *InstructionTokens) FindKeyword(keyword string) *Token {
	for i := range it.tokens {
		if it.tokens[i].Type == TokenKeyword && strings.EqualFold(it.tokens[i].Value, keyword) {
			return &it.tokens[i]
		}
	}
	return nil
}

// FindFlag finds a flag by name (e.g., "from" finds --from=value).
// Returns the full flag token including the value.
func (it *InstructionTokens) FindFlag(name string) *Token {
	prefix := "--" + strings.ToLower(name)
	for i := range it.tokens {
		if it.tokens[i].Type != TokenFlag {
			continue
		}
		flagLower := strings.ToLower(it.tokens[i].Value)
		// Match --name or --name=...
		if flagLower == prefix || strings.HasPrefix(flagLower, prefix+"=") {
			return &it.tokens[i]
		}
	}
	return nil
}

// FlagValue extracts just the value part from a flag token.
// For --from=builder, returns (valueStart, valueEnd, "builder").
// Returns (-1, -1, "") if flag has no value.
func (it *InstructionTokens) FlagValue(flagToken *Token) (int, int, string) {
	if flagToken == nil || flagToken.Type != TokenFlag {
		return -1, -1, ""
	}

	eqIdx := strings.Index(flagToken.Value, "=")
	if eqIdx == -1 {
		return -1, -1, ""
	}

	valueStart := flagToken.Start + eqIdx + 1
	valueEnd := flagToken.End

	rawValue := flagToken.Value[eqIdx+1:]
	// Strip quotes if present
	if len(rawValue) >= 2 {
		if (rawValue[0] == '"' && rawValue[len(rawValue)-1] == '"') ||
			(rawValue[0] == '\'' && rawValue[len(rawValue)-1] == '\'') {
			valueStart++
			valueEnd--
			rawValue = rawValue[1 : len(rawValue)-1]
		}
	}

	return valueStart, valueEnd, rawValue
}

// TokenAfter returns the next non-whitespace token after the given token.
func (it *InstructionTokens) TokenAfter(tok *Token) *Token {
	if tok == nil {
		return nil
	}
	found := false
	for i := range it.tokens {
		if found && it.tokens[i].Type != TokenWhitespace {
			return &it.tokens[i]
		}
		if it.tokens[i].Start == tok.Start && it.tokens[i].End == tok.End {
			found = true
		}
	}
	return nil
}

// Arguments returns all argument tokens (non-keyword, non-flag, non-whitespace).
func (it *InstructionTokens) Arguments() []Token {
	var args []Token
	for _, tok := range it.tokens {
		if tok.Type == TokenArgument {
			args = append(args, tok)
		}
	}
	return args
}
