package dockerfile

import (
	"bytes"
	"context"
	"io"
	"os"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"
	"github.com/moby/buildkit/frontend/dockerfile/linter"
	"github.com/moby/buildkit/frontend/dockerfile/parser"
)

// LintWarning captures parameters from BuildKit's linter.LintWarnFunc callback.
// Fields match the callback signature exactly:
//
//	func(rulename, description, url, fmtmsg string, location []parser.Range)
//
// BuildKit doesn't export a struct for this, so we provide one.
// See: github.com/moby/buildkit/frontend/dockerfile/linter.LintWarnFunc
type LintWarning struct {
	RuleName    string
	Description string
	URL         string
	Message     string
	Location    []parser.Range
}

// ParseResult contains the parsed Dockerfile information
type ParseResult struct {
	// AST is the parsed Dockerfile AST from BuildKit
	AST *parser.Result
	// Stages contains the parsed build stages with typed instructions
	Stages []instructions.Stage
	// MetaArgs contains ARG instructions that appear before the first FROM
	MetaArgs []instructions.ArgCommand
	// Source is the raw source content of the Dockerfile
	Source []byte
	// Warnings contains lint warnings from BuildKit's built-in linter
	Warnings []LintWarning
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
	content, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}

	// Parse AST from the buffered content
	ast, err := parser.Parse(bytes.NewReader(content))
	if err != nil {
		return nil, err
	}

	// Collect warnings from BuildKit's linter
	var warnings []LintWarning
	warnFunc := func(rulename, description, url, fmtmsg string, location []parser.Range) {
		warnings = append(warnings, LintWarning{
			RuleName:    rulename,
			Description: description,
			URL:         url,
			Message:     fmtmsg,
			Location:    location,
		})
	}

	// Create BuildKit linter to capture warnings during instruction parsing
	lint := linter.New(&linter.Config{
		Warn: warnFunc,
	})

	// Parse into typed instructions (stages and meta args)
	stages, metaArgs, err := instructions.Parse(ast.AST, lint)
	if err != nil {
		return nil, err
	}

	return &ParseResult{
		AST:      ast,
		Stages:   stages,
		MetaArgs: metaArgs,
		Source:   content,
		Warnings: warnings,
	}, nil
}

// ExtractHeredocFiles extracts virtual file paths from heredoc COPY/ADD commands.
// These are inline files created by heredoc syntax (e.g., COPY <<EOF /app/file.txt)
// that should not be checked against .dockerignore since they don't come from
// the build context.
func ExtractHeredocFiles(stages []instructions.Stage) map[string]bool {
	heredocFiles := make(map[string]bool)

	for _, stage := range stages {
		for _, cmd := range stage.Commands {
			collectHeredocPaths(cmd, heredocFiles)
		}
	}

	return heredocFiles
}

// CollectHeredocPaths extracts heredoc paths from a single COPY/ADD command's
// SourceContents into the provided map. This is useful for per-command filtering.
func CollectHeredocPaths(sourceContents []instructions.SourceContent) map[string]bool {
	paths := make(map[string]bool)
	for _, sc := range sourceContents {
		if sc.Path != "" {
			paths[sc.Path] = true
		}
	}
	return paths
}

// collectHeredocPaths is an internal helper that extracts heredoc paths from a command.
func collectHeredocPaths(cmd instructions.Command, paths map[string]bool) {
	switch c := cmd.(type) {
	case *instructions.CopyCommand:
		for _, sc := range c.SourceContents {
			if sc.Path != "" {
				paths[sc.Path] = true
			}
		}
	case *instructions.AddCommand:
		for _, sc := range c.SourceContents {
			if sc.Path != "" {
				paths[sc.Path] = true
			}
		}
	}
}
