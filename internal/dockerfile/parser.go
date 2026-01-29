package dockerfile

import (
	"bytes"
	"context"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"
	"github.com/moby/buildkit/frontend/dockerfile/linter"
	"github.com/moby/buildkit/frontend/dockerfile/parser"

	"github.com/tinovyatkin/tally/internal/config"
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

// ParseFile parses a Dockerfile and returns the parse result.
// If cfg is provided, it's used to configure BuildKit's linter (skip disabled rules, etc.).
func ParseFile(_ context.Context, path string, cfg *config.Config) (*ParseResult, error) {
	r, closer, err := openDockerfile(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = closer() }()

	return Parse(r, cfg)
}

// Parse parses a Dockerfile from a reader.
// If cfg is provided, it's used to configure BuildKit's linter (skip disabled rules, etc.).
func Parse(r io.Reader, cfg *config.Config) (*ParseResult, error) {
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

	// Build BuildKit linter config from our config
	lintCfg := buildLinterConfig(cfg, warnFunc)

	// Create BuildKit linter to capture warnings during instruction parsing
	lint := linter.New(lintCfg)

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

// buildLinterConfig creates a BuildKit linter.Config from our config.
// This optimizes BuildKit's linter by:
//   - Setting SkipRules for explicitly excluded BuildKit rules
//   - Setting ExperimentalRules for explicitly included experimental rules
func buildLinterConfig(cfg *config.Config, warnFunc linter.LintWarnFunc) *linter.Config {
	lintCfg := &linter.Config{
		Warn: warnFunc,
	}

	if cfg == nil {
		return lintCfg
	}

	// Check Exclude patterns for buildkit rules
	for _, pattern := range cfg.Rules.Exclude {
		// Handle "buildkit/*" - skip all buildkit rules
		if pattern == "buildkit/*" {
			// Can't skip all at once in BuildKit, but this is rare
			// Individual rules will be filtered by our processor
			continue
		}
		// Handle specific buildkit rule: "buildkit/StageNameCasing"
		if ns, name := parseRuleCode(pattern); ns == "buildkit" && name != "" {
			lintCfg.SkipRules = append(lintCfg.SkipRules, name)
		}
	}

	// Check Include patterns for experimental rules
	for _, pattern := range cfg.Rules.Include {
		// Handle "buildkit/*" - enable all experimental rules
		if pattern == "buildkit/*" {
			// Add known experimental rules
			lintCfg.ExperimentalRules = append(lintCfg.ExperimentalRules, experimentalBuildKitRules...)
			continue
		}
		// Handle specific buildkit rule: "buildkit/InvalidDefinitionDescription"
		if ns, name := parseRuleCode(pattern); ns == "buildkit" && isExperimentalBuildKitRule(name) {
			lintCfg.ExperimentalRules = append(lintCfg.ExperimentalRules, name)
		}
	}

	return lintCfg
}

// experimentalBuildKitRules is the list of known experimental BuildKit linter rules.
// These rules are disabled by default and must be explicitly enabled.
var experimentalBuildKitRules = []string{
	"InvalidDefinitionDescription",
}

// isExperimentalBuildKitRule checks if a rule name is a known experimental BuildKit rule.
func isExperimentalBuildKitRule(name string) bool {
	return slices.Contains(experimentalBuildKitRules, name)
}

// parseRuleCode parses a rule code into namespace and name.
func parseRuleCode(ruleCode string) (string, string) {
	if idx := strings.Index(ruleCode, "/"); idx > 0 {
		return ruleCode[:idx], ruleCode[idx+1:]
	}
	return "", ruleCode
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
