package dockerfile

import (
	"bufio"
	"context"
	"io"
	"os"

	"github.com/moby/buildkit/frontend/dockerfile/parser"
)

// ParseResult contains the parsed Dockerfile information
type ParseResult struct {
	// TotalLines is the total number of lines in the Dockerfile
	TotalLines int
	// AST is the parsed Dockerfile AST from BuildKit
	AST *parser.Result
}

// openDockerfile opens a Dockerfile path for reading.
// If path is "-", returns os.Stdin and a no-op closer.
// Otherwise, opens the file and returns it with its Close method.
func openDockerfile(path string) (io.Reader, func() error, error) {
	if path == "-" {
		return os.Stdin, func() error { return nil }, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	return f, f.Close, nil
}

// ParseFile parses a Dockerfile and returns the parse result
func ParseFile(_ context.Context, path string) (*ParseResult, error) {
	r, closer, err := openDockerfile(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = closer() }()

	return Parse(r)
}

// Parse parses a Dockerfile from a reader
func Parse(r io.Reader) (*ParseResult, error) {
	// Count lines first - we need to read the content twice
	// Use a TeeReader to count lines while parsing
	var lines int
	countingReader := &lineCountingReader{r: r, lines: &lines}

	ast, err := parser.Parse(countingReader)
	if err != nil {
		return nil, err
	}

	return &ParseResult{
		TotalLines: lines,
		AST:        ast,
	}, nil
}

// lineCountingReader wraps a reader and counts lines
type lineCountingReader struct {
	r            io.Reader
	lines        *int
	lastByte     byte
	eofProcessed bool
}

func (l *lineCountingReader) Read(p []byte) (int, error) {
	n, err := l.r.Read(p)
	for i := range n {
		if p[i] == '\n' {
			*l.lines++
		}
		l.lastByte = p[i]
	}
	// Count the last line if it doesn't end with newline
	// Only do this once when we first hit EOF
	if err == io.EOF && !l.eofProcessed {
		l.eofProcessed = true
		if l.lastByte != 0 && l.lastByte != '\n' {
			*l.lines++
		}
	}
	return n, err
}

// CountLines counts the number of lines in a file
func CountLines(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	lines := 0
	for scanner.Scan() {
		lines++
	}
	return lines, scanner.Err()
}
