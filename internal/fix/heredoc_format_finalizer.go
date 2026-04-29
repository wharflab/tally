package fix

import (
	"bytes"
	"context"

	"github.com/wharflab/tally/internal/dockerfile"
	"github.com/wharflab/tally/internal/heredocfmt"
	"github.com/wharflab/tally/internal/rules"
)

type formattedHeredocsFinalizer struct{}

func (formattedHeredocsFinalizer) RuleCode() string {
	return rules.FormattedHeredocsRuleCode
}

func (formattedHeredocsFinalizer) Description() string {
	return "Pretty-print COPY/ADD heredocs"
}

func (formattedHeredocsFinalizer) Safety() rules.FixSafety {
	return rules.FixSafe
}

func (formattedHeredocsFinalizer) Priority() int {
	return rules.FormattedHeredocsFixPriority
}

func (formattedHeredocsFinalizer) Finalize(
	_ context.Context,
	ctx FinalizeContext,
) ([]rules.TextEdit, error) {
	result, err := dockerfile.Parse(bytes.NewReader(ctx.Content), nil)
	if err != nil {
		return nil, err
	}
	return heredocfmt.FormatDockerfileHeredocs(ctx.FilePath, result)
}

func init() {
	RegisterFinalizer(formattedHeredocsFinalizer{})
}
