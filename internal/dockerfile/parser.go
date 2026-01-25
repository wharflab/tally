package dockerfile

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"os"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/parser"
)

// ParseResult contains the parsed Dockerfile information
type ParseResult struct {
	// TotalLines is the total number of lines in the Dockerfile
	TotalLines int
	// BlankLines is the number of blank (empty or whitespace-only) lines
	BlankLines int
	// CommentLines is the number of comment lines (starting with #)
	CommentLines int
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
	// Read the entire content to count lines by category
	content, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}

	// Count lines by category
	stats := countLines(content)

	// Parse from the buffered content
	ast, err := parser.Parse(bytes.NewReader(content))
	if err != nil {
		return nil, err
	}

	return &ParseResult{
		TotalLines:   stats.total,
		BlankLines:   stats.blank,
		CommentLines: stats.comments,
		AST:          ast,
	}, nil
}

// lineStats contains counts of different line types.
type lineStats struct {
	total    int
	blank    int
	comments int
}

// countLines counts total, blank, and comment lines in content.
func countLines(content []byte) lineStats {
	var stats lineStats
	scanner := bufio.NewScanner(bytes.NewReader(content))

	for scanner.Scan() {
		stats.total++
		line := strings.TrimSpace(scanner.Text())

		if line == "" {
			stats.blank++
		} else if strings.HasPrefix(line, "#") {
			stats.comments++
		}
	}

	return stats
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
