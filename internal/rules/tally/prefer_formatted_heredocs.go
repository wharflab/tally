// Package tally implements tally-specific linting rules for Dockerfiles.
package tally

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/moby/buildkit/frontend/dockerfile/command"

	"github.com/wharflab/tally/internal/dockerfile"
	"github.com/wharflab/tally/internal/heredocfmt"
	"github.com/wharflab/tally/internal/psanalyzer"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/rules/configutil"
)

const powerShellFormatTimeout = 5 * time.Minute

// PreferFormattedHeredocsRule implements heredoc body pretty-printing.
type PreferFormattedHeredocsRule struct {
	schema              map[string]any
	powerShellFormatter heredocfmt.PowerShellFormatter
}

// NewPreferFormattedHeredocsRule creates a new prefer-formatted-heredocs rule instance.
func NewPreferFormattedHeredocsRule() *PreferFormattedHeredocsRule {
	return newPreferFormattedHeredocsRuleWithPowerShellFormatter(psanalyzer.SharedRunner())
}

func newPreferFormattedHeredocsRuleWithPowerShellFormatter(
	formatter heredocfmt.PowerShellFormatter,
) *PreferFormattedHeredocsRule {
	schema, err := configutil.RuleSchema(rules.FormattedHeredocsRuleCode)
	if err != nil {
		panic(err)
	}
	return &PreferFormattedHeredocsRule{schema: schema, powerShellFormatter: formatter}
}

// Metadata returns the rule metadata.
func (r *PreferFormattedHeredocsRule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            rules.FormattedHeredocsRuleCode,
		Name:            "Prefer formatted heredocs",
		Description:     "Pretty-print typed heredocs, shell heredocs, and PowerShell heredocs",
		DocURL:          rules.TallyDocURL(rules.FormattedHeredocsRuleCode),
		DefaultSeverity: rules.SeverityStyle,
		Category:        "style",
		IsExperimental:  false,
		FixPriority:     rules.FormattedHeredocsFixPriority,
	}
}

// Schema returns the JSON Schema for this rule's configuration.
func (r *PreferFormattedHeredocsRule) Schema() map[string]any {
	return r.schema
}

// DefaultConfig returns the default configuration for this rule.
func (r *PreferFormattedHeredocsRule) DefaultConfig() any {
	return nil
}

// ValidateConfig validates the configuration against the rule's JSON Schema.
func (r *PreferFormattedHeredocsRule) ValidateConfig(config any) error {
	return configutil.ValidateRuleOptions(rules.FormattedHeredocsRuleCode, config)
}

// Check runs the prefer-formatted-heredocs rule.
func (r *PreferFormattedHeredocsRule) Check(input rules.LintInput) []rules.Violation {
	return r.CheckContext(context.Background(), input)
}

// CheckContext runs the prefer-formatted-heredocs rule with caller cancellation.
func (r *PreferFormattedHeredocsRule) CheckContext(ctx context.Context, input rules.LintInput) []rules.Violation {
	meta := r.Metadata()
	parseResult := &dockerfile.ParseResult{
		AST:    input.AST,
		Source: input.Source,
	}
	formatter := heredocfmt.NewFormatter(input.File)
	var violations []rules.Violation

	for _, doc := range heredocfmt.CollectDockerfileHeredocs(parseResult) {
		if v, ok := r.checkDockerfileHeredoc(ctx, input, formatter, meta, doc); ok {
			violations = append(violations, v)
		}
	}

	for _, doc := range heredocfmt.CollectRunHeredocs(parseResult) {
		if v, ok := r.checkRunHeredoc(ctx, input, formatter, meta, doc); ok {
			violations = append(violations, v)
		}
	}

	return violations
}

func (r *PreferFormattedHeredocsRule) checkDockerfileHeredoc(
	ctx context.Context,
	input rules.LintInput,
	formatter *heredocfmt.Formatter,
	meta rules.RuleMetadata,
	doc heredocfmt.DockerfileHeredoc,
) (rules.Violation, bool) {
	formatted, kind, ok, err := formatter.FormatTarget(doc.TargetPath, doc.Content)
	if err != nil {
		return rules.Violation{}, false
	}
	if kind != "" {
		return formattedTypedHeredocViolation(input.File, meta, doc, formatted, kind, ok)
	}
	if strings.EqualFold(doc.Instruction, command.Copy) {
		return r.checkCopyScriptHeredoc(ctx, input, formatter, meta, doc)
	}
	if strings.EqualFold(doc.Instruction, command.Add) {
		return r.checkPowerShellScriptHeredoc(ctx, input, formatter, meta, doc)
	}
	return rules.Violation{}, false
}

func formattedTypedHeredocViolation(
	file string,
	meta rules.RuleMetadata,
	doc heredocfmt.DockerfileHeredoc,
	formatted string,
	kind heredocfmt.Kind,
	ok bool,
) (rules.Violation, bool) {
	if !ok || formatted == doc.Content {
		return rules.Violation{}, false
	}
	loc := heredocBodyLocation(file, doc.BodyStartLine, doc.TerminatorLine)
	message := fmt.Sprintf(
		"%s heredoc for %s should be pretty-printed as %s",
		doc.Instruction,
		doc.TargetPath,
		strings.ToUpper(string(kind)),
	)
	return formattedHeredocViolation(meta, loc, message, "Pretty-print heredoc body", formatted, doc.BodyPrefix), true
}

func (r *PreferFormattedHeredocsRule) checkCopyScriptHeredoc(
	ctx context.Context,
	input rules.LintInput,
	formatter *heredocfmt.Formatter,
	meta rules.RuleMetadata,
	doc heredocfmt.DockerfileHeredoc,
) (rules.Violation, bool) {
	formatted, _, ok, err := formatter.FormatShellTarget(doc.TargetPath, doc.Content)
	if err != nil {
		return rules.Violation{}, false
	}
	if !ok {
		return r.checkPowerShellScriptHeredoc(ctx, input, formatter, meta, doc)
	}
	if formatted == doc.Content {
		return rules.Violation{}, false
	}

	loc := heredocBodyLocation(input.File, doc.BodyStartLine, doc.TerminatorLine)
	message := doc.Instruction + " heredoc for " + doc.TargetPath + " should be pretty-printed as a shell script"
	return formattedHeredocViolation(meta, loc, message, "Pretty-print COPY shell heredoc body", formatted, doc.BodyPrefix), true
}

func (r *PreferFormattedHeredocsRule) checkPowerShellScriptHeredoc(
	ctx context.Context,
	input rules.LintInput,
	formatter *heredocfmt.Formatter,
	meta rules.RuleMetadata,
	doc heredocfmt.DockerfileHeredoc,
) (rules.Violation, bool) {
	if !input.SlowChecksEnabled {
		return rules.Violation{}, false
	}
	formatted, ok, err := r.formatPowerShellTarget(ctx, formatter, doc.TargetPath, doc.Content)
	if err != nil || !ok || formatted == doc.Content {
		return rules.Violation{}, false
	}

	loc := heredocBodyLocation(input.File, doc.BodyStartLine, doc.TerminatorLine)
	message := doc.Instruction + " heredoc for " + doc.TargetPath + " should be pretty-printed as PowerShell"
	description := "Pretty-print " + doc.Instruction + " PowerShell heredoc body"
	return formattedHeredocViolation(meta, loc, message, description, formatted, doc.BodyPrefix), true
}

func (r *PreferFormattedHeredocsRule) checkRunHeredoc(
	ctx context.Context,
	input rules.LintInput,
	formatter *heredocfmt.Formatter,
	meta rules.RuleMetadata,
	doc heredocfmt.RunHeredoc,
) (rules.Violation, bool) {
	variant := heredocfmt.RunHeredocShellVariant(input.Stages, input.Semantic, doc)
	if variant.IsPowerShell() {
		if !input.SlowChecksEnabled {
			return rules.Violation{}, false
		}
		formatted, ok, err := r.formatPowerShell(ctx, formatter, doc.Content)
		if err != nil || !ok || formatted == doc.Content {
			return rules.Violation{}, false
		}
		loc := heredocBodyLocation(input.File, doc.BodyStartLine, doc.TerminatorLine)
		message := doc.Instruction + " heredoc should be pretty-printed as PowerShell"
		return formattedHeredocViolation(
			meta,
			loc,
			message,
			"Pretty-print RUN PowerShell heredoc body",
			formatted,
			doc.BodyPrefix,
		), true
	}
	if !variant.SupportsPOSIXShellAST() {
		return rules.Violation{}, false
	}

	formatted, ok, err := formatter.FormatShell(doc.Content, variant)
	if err != nil || !ok || formatted == doc.Content {
		return rules.Violation{}, false
	}
	loc := heredocBodyLocation(input.File, doc.BodyStartLine, doc.TerminatorLine)
	message := doc.Instruction + " heredoc should be pretty-printed as a shell script"
	return formattedHeredocViolation(meta, loc, message, "Pretty-print RUN heredoc body", formatted, doc.BodyPrefix), true
}

func heredocBodyLocation(file string, bodyStartLine, terminatorLine int) rules.Location {
	return rules.NewRangeLocation(file, bodyStartLine, 0, terminatorLine, 0)
}

func formattedHeredocViolation(
	meta rules.RuleMetadata,
	loc rules.Location,
	message string,
	description string,
	formatted string,
	bodyPrefix string,
) rules.Violation {
	return rules.NewViolation(loc, meta.Code, message, meta.DefaultSeverity).
		WithDocURL(meta.DocURL).
		WithSuggestedFix(&rules.SuggestedFix{
			Description: description,
			Safety:      rules.FixSafe,
			Priority:    meta.FixPriority,
			Edits: []rules.TextEdit{
				{
					Location: loc,
					NewText:  heredocfmt.WithBodyPrefix(formatted, bodyPrefix),
				},
			},
			IsPreferred: true,
		})
}

func (r *PreferFormattedHeredocsRule) formatPowerShellTarget(
	ctx context.Context,
	formatter *heredocfmt.Formatter,
	target string,
	content string,
) (string, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, powerShellFormatTimeout)
	defer cancel()
	return formatter.FormatPowerShellTarget(ctx, r.powerShellFormatter, target, content)
}

func (r *PreferFormattedHeredocsRule) formatPowerShell(
	ctx context.Context,
	formatter *heredocfmt.Formatter,
	content string,
) (string, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, powerShellFormatTimeout)
	defer cancel()
	return formatter.FormatPowerShell(ctx, r.powerShellFormatter, content)
}

func init() {
	rules.Register(NewPreferFormattedHeredocsRule())
}
