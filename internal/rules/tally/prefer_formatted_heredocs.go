// Package tally implements tally-specific linting rules for Dockerfiles.
package tally

import (
	"fmt"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/command"
	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/wharflab/tally/internal/dockerfile"
	"github.com/wharflab/tally/internal/heredocfmt"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/rules/configutil"
	"github.com/wharflab/tally/internal/shell"
)

// PreferFormattedHeredocsRule implements heredoc body pretty-printing.
type PreferFormattedHeredocsRule struct {
	schema map[string]any
}

// NewPreferFormattedHeredocsRule creates a new prefer-formatted-heredocs rule instance.
func NewPreferFormattedHeredocsRule() *PreferFormattedHeredocsRule {
	schema, err := configutil.RuleSchema(rules.FormattedHeredocsRuleCode)
	if err != nil {
		panic(err)
	}
	return &PreferFormattedHeredocsRule{schema: schema}
}

// Metadata returns the rule metadata.
func (r *PreferFormattedHeredocsRule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            rules.FormattedHeredocsRuleCode,
		Name:            "Prefer formatted heredocs",
		Description:     "Pretty-print typed heredocs and shell heredocs",
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
	meta := r.Metadata()
	parseResult := &dockerfile.ParseResult{
		AST:    input.AST,
		Source: input.Source,
	}
	formatter := heredocfmt.NewFormatter(input.File)
	var violations []rules.Violation

	for _, doc := range heredocfmt.CollectDockerfileHeredocs(parseResult) {
		formatted, kind, ok, err := formatter.FormatTarget(doc.TargetPath, doc.Content)
		if err != nil {
			continue
		}
		if kind != "" {
			if !ok || formatted == doc.Content {
				continue
			}

			loc := rules.NewRangeLocation(input.File, doc.BodyStartLine, 0, doc.TerminatorLine, 0)
			message := fmt.Sprintf(
				"%s heredoc for %s should be pretty-printed as %s",
				doc.Instruction,
				doc.TargetPath,
				strings.ToUpper(string(kind)),
			)
			v := rules.NewViolation(loc, meta.Code, message, meta.DefaultSeverity).
				WithDocURL(meta.DocURL).
				WithSuggestedFix(&rules.SuggestedFix{
					Description: "Pretty-print heredoc body",
					Safety:      rules.FixSafe,
					Priority:    meta.FixPriority,
					Edits: []rules.TextEdit{
						{
							Location: loc,
							NewText:  heredocfmt.WithBodyPrefix(formatted, doc.BodyPrefix),
						},
					},
					IsPreferred: true,
				})
			violations = append(violations, v)
			continue
		}

		if !strings.EqualFold(doc.Instruction, command.Copy) {
			continue
		}
		formatted, _, ok, err = formatter.FormatShellTarget(doc.TargetPath, doc.Content)
		if err != nil || !ok || formatted == doc.Content {
			continue
		}

		loc := rules.NewRangeLocation(input.File, doc.BodyStartLine, 0, doc.TerminatorLine, 0)
		message := doc.Instruction + " heredoc for " + doc.TargetPath + " should be pretty-printed as a shell script"
		v := rules.NewViolation(loc, meta.Code, message, meta.DefaultSeverity).
			WithDocURL(meta.DocURL).
			WithSuggestedFix(&rules.SuggestedFix{
				Description: "Pretty-print COPY shell heredoc body",
				Safety:      rules.FixSafe,
				Priority:    meta.FixPriority,
				Edits: []rules.TextEdit{
					{
						Location: loc,
						NewText:  heredocfmt.WithBodyPrefix(formatted, doc.BodyPrefix),
					},
				},
				IsPreferred: true,
			})
		violations = append(violations, v)
	}

	for _, doc := range heredocfmt.CollectRunHeredocs(parseResult) {
		variant := runHeredocShellVariant(input, doc)
		if !variant.SupportsPOSIXShellAST() {
			continue
		}

		formatted, ok, err := formatter.FormatShell(doc.Content, variant)
		if err != nil || !ok || formatted == doc.Content {
			continue
		}

		loc := rules.NewRangeLocation(input.File, doc.BodyStartLine, 0, doc.TerminatorLine, 0)
		message := doc.Instruction + " heredoc should be pretty-printed as a shell script"
		v := rules.NewViolation(loc, meta.Code, message, meta.DefaultSeverity).
			WithDocURL(meta.DocURL).
			WithSuggestedFix(&rules.SuggestedFix{
				Description: "Pretty-print RUN heredoc body",
				Safety:      rules.FixSafe,
				Priority:    meta.FixPriority,
				Edits: []rules.TextEdit{
					{
						Location: loc,
						NewText:  heredocfmt.WithBodyPrefix(formatted, doc.BodyPrefix),
					},
				},
				IsPreferred: true,
			})
		violations = append(violations, v)
	}

	return violations
}

func runHeredocShellVariant(input rules.LintInput, doc heredocfmt.RunHeredoc) shell.Variant {
	if name, ok := shellFromHeredocShebang(doc.Content); ok {
		return shell.VariantFromShell(name)
	}
	if doc.ShellNameOverride != "" {
		return shell.VariantFromShell(doc.ShellNameOverride)
	}
	if input.Semantic == nil {
		return shell.VariantBash
	}

	stageIdx := stageIndexAtLine(input.Stages, doc.StartLine)
	if stageIdx < 0 {
		return shell.VariantBash
	}
	info := input.Semantic.StageInfo(stageIdx)
	if info == nil {
		return shell.VariantBash
	}
	return info.ShellVariantAtLine(doc.StartLine)
}

func shellFromHeredocShebang(content string) (string, bool) {
	firstLine, _, _ := strings.Cut(content, "\n")
	if name, ok := shell.ShellFromShebang(firstLine); ok {
		return name, true
	}
	if strings.HasPrefix(strings.TrimSpace(firstLine), "#!") {
		return "", true
	}
	return "", false
}

func stageIndexAtLine(stages []instructions.Stage, line int) int {
	stageIdx := -1
	for i, stage := range stages {
		if len(stage.Location) > 0 && stage.Location[0].Start.Line <= line {
			stageIdx = i
		}
	}
	return stageIdx
}

func init() {
	rules.Register(NewPreferFormattedHeredocsRule())
}
