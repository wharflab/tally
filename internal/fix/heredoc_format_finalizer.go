package fix

import (
	"bytes"
	"context"

	"github.com/wharflab/tally/internal/directive"
	"github.com/wharflab/tally/internal/dockerfile"
	"github.com/wharflab/tally/internal/heredocfmt"
	"github.com/wharflab/tally/internal/psanalyzer"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/semantic"
	"github.com/wharflab/tally/internal/sourcemap"
)

type formattedHeredocsFinalizer struct {
	powerShellFormatter heredocfmt.PowerShellFormatter
}

func (formattedHeredocsFinalizer) RuleCode() string {
	return rules.FormattedHeredocsRuleCode
}

func (formattedHeredocsFinalizer) Description() string {
	return "Pretty-print Dockerfile heredocs"
}

func (formattedHeredocsFinalizer) Safety() rules.FixSafety {
	return rules.FixSafe
}

func (formattedHeredocsFinalizer) Priority() int {
	return rules.FormattedHeredocsFixPriority
}

func (f formattedHeredocsFinalizer) Finalize(
	finalizeCtx context.Context,
	ctx FinalizeContext,
) ([]rules.TextEdit, error) {
	result, err := dockerfile.Parse(bytes.NewReader(ctx.Content), nil)
	if err != nil {
		return nil, err
	}
	sem := semanticModelForFinalizer(ctx.FilePath, result)
	powerShellFormatter := f.powerShellFormatter
	if !ctx.SlowChecksEnabled {
		powerShellFormatter = nil
	}
	return heredocfmt.FormatDockerfileHeredocsWithPowerShell(
		finalizeCtx,
		ctx.FilePath,
		result,
		sem,
		powerShellFormatter,
	)
}

func init() {
	RegisterFinalizer(formattedHeredocsFinalizer{powerShellFormatter: psanalyzer.SharedRunner()})
}

func semanticModelForFinalizer(file string, result *dockerfile.ParseResult) *semantic.Model {
	if result == nil {
		return semantic.NewModel(nil, nil, file)
	}
	sm := sourcemap.New(result.Source)
	spanIndex := directive.NewInstructionSpanIndexFromAST(result.AST, sm)
	directiveResult := directive.Parse(sm, nil, spanIndex)
	return semantic.NewBuilder(result, nil, file).
		WithShellDirectives(directive.ToSemanticShellDirectives(directiveResult.ShellDirectives)).
		Build()
}
