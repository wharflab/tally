// Package linter provides the shared lint pipeline used by both the CLI and the LSP server.
//
// The pipeline: config discovery → parse → semantic model → rule execution → violation collection.
// Callers use [LintFile] to run the pipeline and then apply their own processor chain
// (via [CLIProcessors] or [LSPProcessors]) to filter and transform the results.
package linter

import (
	"bytes"
	"log"
	"os"

	"github.com/tinovyatkin/tally/internal/config"
	"github.com/tinovyatkin/tally/internal/directive"
	"github.com/tinovyatkin/tally/internal/dockerfile"
	"github.com/tinovyatkin/tally/internal/rules"
	_ "github.com/tinovyatkin/tally/internal/rules/all" // Register all rules.
	"github.com/tinovyatkin/tally/internal/rules/buildkit/fixes"
	"github.com/tinovyatkin/tally/internal/semantic"
	"github.com/tinovyatkin/tally/internal/sourcemap"
)

// Level is a log level for the Channel interface.
type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
)

// Channel receives diagnostic output from the lint/fix pipeline.
// Implementations map to environment-specific UX (LSP notifications, CLI stderr, etc.).
type Channel interface {
	Log(level Level, msg string)
	Progress(title string, pct int) // -1 = indeterminate
	Warn(msg string)
}

// Input configures a single invocation of [LintFile].
type Input struct {
	// FilePath is used for config discovery and violation locations.
	FilePath string

	// Content is the file content to lint. If nil, LintFile reads from FilePath.
	Content []byte

	// Config is the resolved configuration. If nil, LintFile loads from FilePath.
	Config *config.Config

	// BuildContext provides context-aware checks (e.g. .dockerignore).
	// If nil, context-aware checks are skipped.
	BuildContext rules.BuildContext

	// Channel receives progress and diagnostic output. Nil means silent.
	Channel Channel
}

// Result contains the output of [LintFile].
type Result struct {
	// Violations are raw violations before processor filtering.
	Violations []rules.Violation

	// ParseResult is the parsed Dockerfile (AST, stages, source, BuildKit warnings).
	ParseResult *dockerfile.ParseResult

	// Config is the resolved config (loaded or passed in via Input).
	Config *config.Config
}

// LintFile runs the full lint pipeline for one file.
// It returns raw violations before processor filtering.
func LintFile(input Input) (*Result, error) {
	content := input.Content
	if content == nil {
		var err error
		content, err = os.ReadFile(input.FilePath)
		if err != nil {
			return nil, err
		}
	}

	cfg := input.Config
	if cfg == nil {
		var err error
		cfg, err = config.Load(input.FilePath)
		if err != nil {
			log.Printf("linter: config load error for %s: %v", input.FilePath, err)
			cfg = config.Default()
		}
	}

	parseResult, err := dockerfile.Parse(bytes.NewReader(content), cfg)
	if err != nil {
		return nil, err
	}
	// Ensure Source is set (Parse reads from a reader, source is captured internally).
	// If the caller passed Content, the parse result's Source should already match.

	sm := sourcemap.New(content)
	directiveResult := directive.Parse(sm, nil)

	var buildArgs map[string]string
	sem := semantic.NewBuilder(parseResult, buildArgs, input.FilePath).
		WithShellDirectives(directiveResult.ShellDirectives).
		Build()

	enabledRules := EnabledRuleCodes(cfg)

	baseInput := rules.LintInput{
		File:               input.FilePath,
		AST:                parseResult.AST,
		Stages:             parseResult.Stages,
		MetaArgs:           parseResult.MetaArgs,
		Source:             content,
		Semantic:           sem,
		Context:            input.BuildContext,
		EnabledRules:       enabledRules,
		HeredocMinCommands: heredocMinCommands(cfg),
	}

	// Collect construction-time violations from semantic analysis.
	violations := make([]rules.Violation, 0,
		len(sem.ConstructionIssues())+len(rules.All())+len(parseResult.Warnings))

	for _, issue := range sem.ConstructionIssues() {
		violations = append(violations, rules.NewViolation(
			rules.NewLocationFromRange(issue.File, issue.Location),
			issue.Code, issue.Message, issue.Severity,
		).WithDocURL(issue.DocURL))
	}

	// Run all registered rules.
	for _, rule := range rules.All() {
		ruleInput := baseInput
		ruleInput.Config = cfg.Rules.GetOptions(rule.Metadata().Code)
		violations = append(violations, rule.Check(ruleInput)...)
	}

	// Convert BuildKit warnings to violations.
	for _, w := range parseResult.Warnings {
		violations = append(violations, rules.NewViolationFromBuildKitWarning(
			input.FilePath, w.RuleName, w.Description, w.URL, w.Message, w.Location,
		))
	}

	// Enrich BuildKit violations with auto-fix suggestions.
	fixes.EnrichBuildKitFixes(violations, sem, content)

	return &Result{
		Violations:  violations,
		ParseResult: parseResult,
		Config:      cfg,
	}, nil
}
