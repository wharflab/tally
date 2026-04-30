package powershell

import (
	"context"
	"errors"
	"testing"

	"github.com/wharflab/tally/internal/psanalyzer"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/testutil"
)

type fakeAnalyzer struct {
	diagnostics []psanalyzer.Diagnostic
	err         error
	scripts     []string
}

func (f *fakeAnalyzer) Analyze(_ context.Context, req psanalyzer.AnalyzeRequest) ([]psanalyzer.Diagnostic, error) {
	f.scripts = append(f.scripts, req.ScriptDefinition)
	return f.diagnostics, f.err
}

func TestRuleSkipsWhenNoPowerShellSnippet(t *testing.T) {
	t.Parallel()

	fake := &fakeAnalyzer{}
	rule := newRuleWithAnalyzer(fake)
	input := testutil.MakeLintInput(t, "Dockerfile", `FROM alpine
RUN echo hi
`)
	input.EnabledRules = []string{PowerShellRuleCode}

	violations := rule.Check(input)
	if len(violations) != 0 {
		t.Fatalf("expected no violations, got %#v", violations)
	}
	if len(fake.scripts) != 0 {
		t.Fatalf("analyzer was called for non-PowerShell Dockerfile: %#v", fake.scripts)
	}
}

func TestRuleChecksPowerShellShellRun(t *testing.T) {
	t.Parallel()

	line, col, endLine, endCol := 1, 5, 1, 15
	fake := &fakeAnalyzer{diagnostics: []psanalyzer.Diagnostic{{
		RuleName:  "PSAvoidUsingWriteHost",
		Severity:  1,
		Line:      &line,
		Column:    &col,
		EndLine:   &endLine,
		EndColumn: &endCol,
		Message:   "Avoid using Write-Host.",
	}}}
	rule := newRuleWithAnalyzer(fake)
	input := testutil.MakeLintInput(t, "Dockerfile", `FROM mcr.microsoft.com/powershell:ubuntu-22.04
SHELL ["pwsh", "-Command"]
RUN Write-Host hi
`)
	input.EnabledRules = []string{PowerShellRuleCode}

	violations := rule.Check(input)
	if len(violations) != 1 {
		t.Fatalf("got %d violations, want 1: %#v", len(violations), violations)
	}
	if got := violations[0].RuleCode; got != rules.PowerShellRulePrefix+"PSAvoidUsingWriteHost" {
		t.Fatalf("RuleCode = %q", got)
	}
	if violations[0].Severity != rules.SeverityWarning {
		t.Fatalf("Severity = %v", violations[0].Severity)
	}
	if violations[0].Location.Start.Line != 3 || violations[0].Location.Start.Column != 4 {
		t.Fatalf("Location = %#v, want line 3 column 4", violations[0].Location)
	}
	if len(fake.scripts) != 1 || fake.scripts[0] == "" {
		t.Fatalf("expected one analyzed script, got %#v", fake.scripts)
	}
}

func TestRuleChecksExplicitPowerShellWrapperLazily(t *testing.T) {
	t.Parallel()

	line, col := 1, 1
	fake := &fakeAnalyzer{diagnostics: []psanalyzer.Diagnostic{{
		RuleName: "PSAvoidUsingWriteHost",
		Severity: 1,
		Line:     &line,
		Column:   &col,
		Message:  "Avoid using Write-Host.",
	}}}
	rule := newRuleWithAnalyzer(fake)
	input := testutil.MakeLintInput(t, "Dockerfile", `FROM alpine
RUN pwsh -Command "Write-Host hi"
`)
	input.EnabledRules = []string{PowerShellRuleCode}

	violations := rule.Check(input)
	if len(violations) != 1 {
		t.Fatalf("got %d violations, want 1: %#v", len(violations), violations)
	}
	if len(fake.scripts) != 1 || fake.scripts[0] != "Write-Host hi" {
		t.Fatalf("analyzed scripts = %#v, want inner PowerShell script only", fake.scripts)
	}
	if violations[0].Location.Start.Line != 2 || violations[0].Location.Start.Column != 19 {
		t.Fatalf("Location = %#v, want wrapper body at line 2 column 19", violations[0].Location)
	}
}

func TestRuleChecksPowerShellHeredocOverride(t *testing.T) {
	t.Parallel()

	line, col := 1, 1
	fake := &fakeAnalyzer{diagnostics: []psanalyzer.Diagnostic{{
		RuleName: "PSAvoidUsingWriteHost",
		Severity: 1,
		Line:     &line,
		Column:   &col,
		Message:  "Avoid using Write-Host.",
	}}}
	rule := newRuleWithAnalyzer(fake)
	input := testutil.MakeLintInput(t, "Dockerfile", `FROM alpine
RUN <<EOF pwsh
Write-Host hi
EOF
`)
	input.EnabledRules = []string{PowerShellRuleCode}

	violations := rule.Check(input)
	if len(violations) != 1 {
		t.Fatalf("got %d violations, want 1: %#v", len(violations), violations)
	}
	if len(fake.scripts) != 1 || fake.scripts[0] != "Write-Host hi" {
		t.Fatalf("analyzed scripts = %#v, want heredoc body", fake.scripts)
	}
	if violations[0].Location.Start.Line != 3 || violations[0].Location.Start.Column != 0 {
		t.Fatalf("Location = %#v, want heredoc body at line 3 column 0", violations[0].Location)
	}
}

func TestRuleReportsAnalyzerFailureOnPowerShellSnippet(t *testing.T) {
	t.Parallel()

	fake := &fakeAnalyzer{err: errors.New("module not available")}
	rule := newRuleWithAnalyzer(fake)
	input := testutil.MakeLintInput(t, "Dockerfile", `FROM mcr.microsoft.com/powershell:ubuntu-22.04
SHELL ["pwsh", "-Command"]
RUN Write-Host hi
`)
	input.EnabledRules = []string{PowerShellRuleCode}

	violations := rule.Check(input)
	if len(violations) != 1 {
		t.Fatalf("got %d violations, want 1: %#v", len(violations), violations)
	}
	if violations[0].RuleCode != metaFailureRuleCode {
		t.Fatalf("RuleCode = %q, want %q", violations[0].RuleCode, metaFailureRuleCode)
	}
	if violations[0].Location.Start.Line != 3 {
		t.Fatalf("Location = %#v, want fallback RUN line", violations[0].Location)
	}
}
