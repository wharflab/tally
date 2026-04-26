// Package linter provides the shared lint pipeline used by both the CLI and the LSP server.
//
// The pipeline: config discovery → parse → semantic model → rule execution → violation collection.
// Callers use [LintFile] to run the pipeline and then apply their own processor chain
// (via [CLIProcessors] or [LSPProcessors]) to filter and transform the results.
package linter

import (
	"bytes"
	"os"

	"github.com/wharflab/tally/internal/async"
	"github.com/wharflab/tally/internal/config"
	buildcontext "github.com/wharflab/tally/internal/context"
	"github.com/wharflab/tally/internal/directive"
	"github.com/wharflab/tally/internal/dockerfile"
	"github.com/wharflab/tally/internal/facts"
	"github.com/wharflab/tally/internal/invocation"
	"github.com/wharflab/tally/internal/rules"
	_ "github.com/wharflab/tally/internal/rules/all" // Register all rules.
	"github.com/wharflab/tally/internal/rules/buildkit/fixes"
	"github.com/wharflab/tally/internal/semantic"
	"github.com/wharflab/tally/internal/sourcemap"
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

	// ParseResult is a pre-parsed Dockerfile result. If non-nil, LintFile
	// reuses it instead of parsing again. This avoids double-parsing when
	// the caller has already parsed for syntax checks.
	ParseResult *dockerfile.ParseResult

	// Invocation describes the build orchestration context, when present.
	Invocation *invocation.BuildInvocation

	// Channel receives progress and diagnostic output. Nil means silent.
	Channel Channel
}

// Result contains the output of [LintFile].
type Result struct {
	// Violations are raw violations before processor filtering.
	Violations []rules.Violation

	// AsyncPlan contains planned async check requests from AsyncRule implementations.
	// The caller is responsible for executing these (if slow checks are enabled).
	AsyncPlan []async.CheckRequest

	// ParseResult is the parsed Dockerfile (AST, stages, source, BuildKit warnings).
	ParseResult *dockerfile.ParseResult

	// Config is the resolved config (loaded or passed in via Input).
	Config *config.Config
}

// LintFile runs the full lint pipeline for one file.
// It returns raw violations before processor filtering.
//
// Content resolution order:
//  1. input.Content — used as-is when provided.
//  2. input.ParseResult.Source — used when a pre-parsed result is provided
//     without Content, keeping sourcemap.New, directive.Parse, and
//     semantic.NewBuilder in sync with the bytes that were actually parsed.
//  3. os.ReadFile(input.FilePath) — last resort when neither is set.
//
// ParseResult resolution follows the same priority: input.ParseResult is
// reused directly (avoiding a second parse); otherwise the file is parsed
// from the resolved content.
func LintFile(input Input) (*Result, error) {
	cfg := input.Config
	if cfg == nil {
		var err error
		cfg, err = config.Load(input.FilePath)
		if err != nil {
			return nil, err
		}
	}

	content, parseResult, err := resolveParseInput(input, cfg)
	if err != nil {
		return nil, err
	}

	sm := sourcemap.New(content)
	spanIndex := directive.NewInstructionSpanIndexFromAST(parseResult.AST, sm)
	directiveResult := directive.Parse(sm, nil, spanIndex)

	var buildArgs map[string]string
	targetStage := ""
	if input.Invocation != nil {
		buildArgs = invocation.ConcreteBuildArgs(input.Invocation.BuildArgs)
		targetStage = input.Invocation.TargetStage
	}
	sem := semantic.NewBuilder(parseResult, buildArgs, input.FilePath).
		WithTargetStage(targetStage).
		WithShellDirectives(directive.ToSemanticShellDirectives(directiveResult.ShellDirectives)).
		Build()

	invocationCtx := invocation.NewContext(input.Invocation)
	contextFiles := buildContextReader(input, parseResult)
	fileFacts := facts.NewFileFacts(
		input.FilePath,
		parseResult,
		sem,
		directive.ToFactsShellDirectives(directiveResult.ShellDirectives),
		contextFiles,
	)

	enabledRules := EnabledRuleCodes(cfg)

	baseInput := rules.LintInput{
		File:               input.FilePath,
		AST:                parseResult.AST,
		Stages:             parseResult.Stages,
		MetaArgs:           parseResult.MetaArgs,
		Source:             content,
		InvocationContext:  invocationCtx,
		Semantic:           sem,
		Facts:              fileFacts,
		EnabledRules:       enabledRules,
		HeredocMinCommands: heredocMinCommands(cfg),
	}

	violations := make([]rules.Violation, 0, len(rules.All())+len(parseResult.Warnings))

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

	attachInvocation(violations, input.Invocation)

	// Enrich BuildKit violations with auto-fix suggestions.
	fixes.EnrichBuildKitFixes(violations, sem, content)

	// Plan async checks from AsyncRule implementations.
	// Only plan for rules that are enabled (respects --select/--ignore/config).
	var asyncPlan []async.CheckRequest
	for _, rule := range rules.All() {
		ar, ok := rule.(rules.AsyncRule)
		if !ok {
			continue
		}
		code := rule.Metadata().Code
		// Skip rules disabled by Include/Exclude patterns.
		if enabled := cfg.Rules.IsEnabled(code); enabled != nil && !*enabled {
			continue
		}
		// Skip rules with severity "off".
		if sev := cfg.Rules.GetSeverity(code); sev == "off" {
			continue
		}
		ruleInput := baseInput
		ruleInput.Config = cfg.Rules.GetOptions(code)
		for _, req := range ar.PlanAsync(ruleInput) {
			attachInvocationToAsyncRequest(&req, input.Invocation)
			asyncPlan = append(asyncPlan, req)
		}
	}

	return &Result{
		Violations:  violations,
		AsyncPlan:   asyncPlan,
		ParseResult: parseResult,
		Config:      cfg,
	}, nil
}

func resolveParseInput(input Input, cfg *config.Config) ([]byte, *dockerfile.ParseResult, error) {
	content := input.Content
	if input.ParseResult != nil {
		parseResult := input.ParseResult
		// Populate content from the pre-parsed result so that sourcemap.New,
		// directive.Parse, and semantic.NewBuilder all operate on the same
		// bytes that were parsed — not a separate re-read from disk.
		if content == nil {
			content = parseResult.Source
		}
		return content, parseResult, nil
	}

	if content == nil {
		var err error
		content, err = os.ReadFile(input.FilePath)
		if err != nil {
			return nil, nil, err
		}
	}
	parseResult, err := dockerfile.Parse(bytes.NewReader(content), cfg)
	if err != nil {
		return nil, nil, err
	}
	return content, parseResult, nil
}

func buildContextReader(input Input, parseResult *dockerfile.ParseResult) facts.ContextFileReader {
	if input.Invocation == nil {
		return nil
	}

	if input.Invocation.ContextRef.Kind != invocation.ContextKindDir || input.Invocation.ContextRef.Value == "" {
		return nil
	}

	local, err := buildcontext.New(input.Invocation.ContextRef.Value, input.FilePath,
		buildcontext.WithHeredocFiles(dockerfile.ExtractHeredocFiles(parseResult.Stages)))
	if err != nil {
		if input.Channel != nil {
			input.Channel.Warn(
				"failed to create build context for " + invocation.LabelForSource(&input.Invocation.Source) + ": " + err.Error(),
			)
		}
		return nil
	}

	return local
}

func attachInvocation(violations []rules.Violation, inv *invocation.BuildInvocation) {
	if inv == nil {
		return
	}
	exposeSource := inv.Source.Kind != invocation.KindDockerfile
	for i := range violations {
		violations[i].InvocationKey = inv.Key
		if exposeSource {
			source := inv.Source
			violations[i].Invocation = &source
		}
	}
}

func attachInvocationToAsyncRequest(req *async.CheckRequest, inv *invocation.BuildInvocation) {
	if req == nil || inv == nil {
		return
	}
	req.InvocationKey = inv.Key
	if req.Handler == nil {
		return
	}
	exposeSource := inv.Source.Kind != invocation.KindDockerfile
	source := inv.Source
	req.Handler = invocationAwareHandler{
		inner:        req.Handler,
		key:          inv.Key,
		src:          source,
		exposeSource: exposeSource,
	}
}

type invocationAwareHandler struct {
	inner        async.ResultHandler
	key          string
	src          invocation.InvocationSource
	exposeSource bool
}

func (h invocationAwareHandler) OnSuccess(resolved any) []any {
	if h.inner == nil {
		return nil
	}
	results := h.inner.OnSuccess(resolved)
	if results == nil {
		return nil
	}
	for i, result := range results {
		switch v := result.(type) {
		case rules.Violation:
			v.InvocationKey = h.key
			if h.exposeSource {
				source := h.src
				v.Invocation = &source
			}
			results[i] = v
		case async.CompletedCheck:
			if v.InvocationKey == "" {
				v.InvocationKey = h.key
			}
			results[i] = v
		}
	}
	return results
}
