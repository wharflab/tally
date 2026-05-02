package powershell

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/wharflab/tally/internal/config"
	fixpkg "github.com/wharflab/tally/internal/fix"
	"github.com/wharflab/tally/internal/psanalyzer"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/testutil"
)

type fakeAnalyzer struct {
	diagnostics []psanalyzer.Diagnostic
	err         error
	scripts     []string
	requests    []psanalyzer.AnalyzeRequest
}

func (f *fakeAnalyzer) Analyze(_ context.Context, req psanalyzer.AnalyzeRequest) ([]psanalyzer.Diagnostic, error) {
	f.scripts = append(f.scripts, req.ScriptDefinition)
	f.requests = append(f.requests, req)
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

func TestRuleSkipsWhenSlowChecksDisabled(t *testing.T) {
	t.Parallel()

	fake := &fakeAnalyzer{diagnostics: []psanalyzer.Diagnostic{{
		RuleName: "PSAvoidUsingWriteHost",
		Severity: 1,
		Message:  "Avoid using Write-Host.",
	}}}
	rule := newRuleWithAnalyzer(fake)
	input := testutil.MakeLintInput(t, "Dockerfile", `FROM mcr.microsoft.com/powershell:ubuntu-22.04
SHELL ["pwsh", "-Command"]
RUN Write-Host hi
`)
	input.EnabledRules = []string{PowerShellRuleCode}
	input.SlowChecksEnabled = false

	violations := rule.Check(input)
	if len(violations) != 0 {
		t.Fatalf("expected no violations when slow checks are disabled, got %#v", violations)
	}
	if len(fake.scripts) != 0 {
		t.Fatalf("analyzer was called when slow checks are disabled: %#v", fake.scripts)
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
	if got, want := violations[0].DocURL, rules.PowerShellDiagnosticDocURL("PSAvoidUsingWriteHost"); got != want {
		t.Fatalf("DocURL = %q, want %q", got, want)
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

func TestRuleAttachesPowerShellSuggestedFix(t *testing.T) {
	t.Parallel()

	line, col, endLine, endCol := 1, 5, 1, 8
	fake := &fakeAnalyzer{diagnostics: []psanalyzer.Diagnostic{{
		RuleName:  "PSAvoidUsingCmdletAliases",
		Severity:  1,
		Line:      &line,
		Column:    &col,
		EndLine:   &endLine,
		EndColumn: &endCol,
		Message:   "'gci' is an alias of 'Get-ChildItem'.",
		SuggestedCorrections: []psanalyzer.SuggestedCorrection{{
			Description: "Replace gci with Get-ChildItem",
			Line:        1,
			Column:      5,
			EndLine:     1,
			EndColumn:   8,
			Text:        "Get-ChildItem",
		}},
	}}}
	rule := newRuleWithAnalyzer(fake)
	content := `FROM mcr.microsoft.com/powershell:ubuntu-22.04
SHELL ["pwsh", "-Command"]
RUN gci
`
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	input.EnabledRules = []string{PowerShellRuleCode}

	violations := rule.Check(input)
	if len(violations) != 1 {
		t.Fatalf("got %d violations, want 1: %#v", len(violations), violations)
	}
	gotFix := violations[0].SuggestedFix
	if gotFix == nil {
		t.Fatal("expected PowerShell suggested correction to be exposed as a suggested fix")
	}
	if gotFix.Description != "Replace gci with Get-ChildItem" {
		t.Fatalf("fix description = %q", gotFix.Description)
	}
	if gotFix.Safety != rules.FixSuggestion {
		t.Fatalf("fix safety = %v, want %v", gotFix.Safety, rules.FixSuggestion)
	}
	if !gotFix.IsPreferred {
		t.Fatalf("expected single PowerShell fix to be preferred")
	}
	if len(gotFix.Edits) != 1 {
		t.Fatalf("fix edits = %#v, want one edit", gotFix.Edits)
	}
	edit := gotFix.Edits[0]
	if edit.Location.Start.Line != 3 || edit.Location.Start.Column != 4 ||
		edit.Location.End.Line != 3 || edit.Location.End.Column != 7 {
		t.Fatalf("edit location = %#v, want RUN body range", edit.Location)
	}

	got := string(fixpkg.ApplyFix([]byte(content), gotFix))
	want := `FROM mcr.microsoft.com/powershell:ubuntu-22.04
SHELL ["pwsh", "-Command"]
RUN Get-ChildItem
`
	if got != want {
		t.Fatalf("applied fix mismatch:\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestRuleSkipsPowerShellSuggestedFixWithoutPreciseMapping(t *testing.T) {
	t.Parallel()

	line, col, endLine, endCol := 1, 1, 1, 4
	fake := &fakeAnalyzer{diagnostics: []psanalyzer.Diagnostic{{
		RuleName:  "PSAvoidUsingCmdletAliases",
		Severity:  1,
		Line:      &line,
		Column:    &col,
		EndLine:   &endLine,
		EndColumn: &endCol,
		Message:   "'gci' is an alias of 'Get-ChildItem'.",
		SuggestedCorrections: []psanalyzer.SuggestedCorrection{{
			Description: "Replace gci with Get-ChildItem",
			Line:        1,
			Column:      1,
			EndLine:     1,
			EndColumn:   4,
			Text:        "Get-ChildItem",
		}},
	}}}
	rule := newRuleWithAnalyzer(fake)
	input := testutil.MakeLintInput(t, "Dockerfile", `FROM alpine
RUN ["pwsh", "-NoProfile", "-Command", "gci"]
`)
	input.EnabledRules = []string{PowerShellRuleCode}

	violations := rule.Check(input)
	if len(violations) != 1 {
		t.Fatalf("got %d violations, want 1: %#v", len(violations), violations)
	}
	if violations[0].SuggestedFix != nil {
		t.Fatalf("expected no suggested fix for approximate exec-form mapping, got %#v", violations[0].SuggestedFix)
	}
}

func TestRulePassesPowerShellRuleSettings(t *testing.T) {
	t.Parallel()

	fake := &fakeAnalyzer{}
	rule := newRuleWithAnalyzer(fake)
	input := testutil.MakeLintInput(t, "Dockerfile", `FROM mcr.microsoft.com/powershell:ubuntu-22.04
SHELL ["pwsh", "-Command"]
RUN [System.Management.Automation.SemanticVersion]'1.18.0-rc1'
`)
	input.EnabledRules = []string{PowerShellRuleCode}
	input.Config = &config.RulesConfig{
		Include: []string{"powershell/PSUseCompatibleTypes"},
		Exclude: []string{"powershell/PSAvoidUsingWriteHost"},
		Powershell: map[string]config.RuleConfig{
			"PSUseCompatibleTypes": {
				Options: map[string]any{
					"Enable": true,
					"TargetProfiles": []string{
						"ubuntu_x64_18.04_6.1.3_x64_4.0.30319.42000_core",
					},
				},
			},
			"PSAvoidUsingWriteHost": {
				Severity: config.SeverityOffValue,
				Options:  map[string]any{"Enable": true},
			},
			"PowerShell": {
				Options: map[string]any{"Ignored": true},
			},
		},
	}

	violations := rule.Check(input)
	if len(violations) != 0 {
		t.Fatalf("expected no violations from fake analyzer, got %#v", violations)
	}
	if len(fake.requests) != 1 {
		t.Fatalf("got %d analyzer requests, want 1", len(fake.requests))
	}

	settings := fake.requests[0].Settings
	if len(settings.IncludeRules) != 0 {
		t.Fatalf("IncludeRules = %#v, want none so tally include semantics do not restrict PSScriptAnalyzer", settings.IncludeRules)
	}
	if !slices.Equal(settings.ExcludeRules, []string{"PSAvoidUsingWriteHost"}) {
		t.Fatalf("ExcludeRules = %#v", settings.ExcludeRules)
	}
	ruleSettings := settings.Rules["PSUseCompatibleTypes"]
	if len(ruleSettings) != 2 {
		t.Fatalf("PSUseCompatibleTypes settings = %#v", ruleSettings)
	}
	if ruleSettings["Enable"] != true {
		t.Fatalf("Enable = %#v, want true", ruleSettings["Enable"])
	}
	profiles, ok := ruleSettings["TargetProfiles"].([]string)
	if !ok || !slices.Equal(profiles, []string{"ubuntu_x64_18.04_6.1.3_x64_4.0.30319.42000_core"}) {
		t.Fatalf("TargetProfiles = %#v", ruleSettings["TargetProfiles"])
	}
	if _, ok := settings.Rules["PSAvoidUsingWriteHost"]; ok {
		t.Fatalf("disabled rule settings should not be forwarded: %#v", settings.Rules["PSAvoidUsingWriteHost"])
	}
	if _, ok := settings.Rules["PowerShell"]; ok {
		t.Fatalf("engine settings should not be forwarded as analyzer rule settings")
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

func TestRuleChecksExecFormPowerShellCommandRun(t *testing.T) {
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
RUN ["pwsh", "-NoProfile", "-Command", "Write-Host", "hi"]
`)
	input.EnabledRules = []string{PowerShellRuleCode}

	violations := rule.Check(input)
	if len(violations) != 1 {
		t.Fatalf("got %d violations, want 1: %#v", len(violations), violations)
	}
	if len(fake.scripts) != 1 || fake.scripts[0] != "Write-Host hi" {
		t.Fatalf("analyzed scripts = %#v, want exec-form PowerShell command body", fake.scripts)
	}
	if violations[0].Location.Start.Line != 2 || violations[0].Location.Start.Column != 0 {
		t.Fatalf("Location = %#v, want exec-form RUN line", violations[0].Location)
	}
}

func TestRuleRejectsWindowsPowerShellWrapper(t *testing.T) {
	t.Parallel()

	fake := &fakeAnalyzer{}
	rule := newRuleWithAnalyzer(fake)
	input := testutil.MakeLintInput(t, "Dockerfile", `FROM alpine
RUN powershell -Command "Write-Host hi"
RUN powershell.exe -Command "Write-Host bye"
`)
	input.EnabledRules = []string{PowerShellRuleCode}

	violations := rule.Check(input)
	if len(violations) != 0 {
		t.Fatalf("expected no violations, got %#v", violations)
	}
	if len(fake.scripts) != 0 {
		t.Fatalf("analyzer was called for Windows PowerShell wrapper: %#v", fake.scripts)
	}
}

func TestRuleRejectsExecFormWindowsPowerShell(t *testing.T) {
	t.Parallel()

	fake := &fakeAnalyzer{}
	rule := newRuleWithAnalyzer(fake)
	input := testutil.MakeLintInput(t, "Dockerfile", `FROM alpine
RUN ["powershell", "-Command", "Write-Host hi"]
RUN ["powershell.exe", "-Command", "Write-Host bye"]
`)
	input.EnabledRules = []string{PowerShellRuleCode}

	violations := rule.Check(input)
	if len(violations) != 0 {
		t.Fatalf("expected no violations, got %#v", violations)
	}
	if len(fake.scripts) != 0 {
		t.Fatalf("analyzer was called for exec-form Windows PowerShell wrapper: %#v", fake.scripts)
	}
}

func TestRuleSkipsPowerShellFileInvocation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		dockerfile string
	}{
		{
			name: "shell form",
			dockerfile: `FROM alpine
RUN pwsh ./build.ps1 -Foo Bar
`,
		},
		{
			name: "exec form",
			dockerfile: `FROM alpine
RUN ["pwsh", "-File", "./build.ps1"]
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			fake := &fakeAnalyzer{}
			rule := newRuleWithAnalyzer(fake)
			input := testutil.MakeLintInput(t, "Dockerfile", tt.dockerfile)
			input.EnabledRules = []string{PowerShellRuleCode}

			violations := rule.Check(input)
			if len(violations) != 0 {
				t.Fatalf("expected no violations, got %#v", violations)
			}
			if len(fake.scripts) != 0 {
				t.Fatalf("analyzer was called for PowerShell file invocation: %#v", fake.scripts)
			}
		})
	}
}

func TestRuleReportsAnalyzerFailureAtExplicitInvocationLine(t *testing.T) {
	t.Parallel()

	fake := &fakeAnalyzer{err: errors.New("module not available")}
	rule := newRuleWithAnalyzer(fake)
	input := testutil.MakeLintInput(t, "Dockerfile", `FROM alpine
RUN pwsh \
    -Command "Write-Host hi"
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
		t.Fatalf("Location = %#v, want explicit invocation command line", violations[0].Location)
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

func TestRulePreservesPowerShellHeredocBackslashContinuation(t *testing.T) {
	t.Parallel()

	fake := &fakeAnalyzer{}
	rule := newRuleWithAnalyzer(fake)
	input := testutil.MakeLintInput(t, "Dockerfile", `FROM alpine
RUN <<EOF pwsh
Write-Host foo \
Write-Host bar
EOF
`)
	input.EnabledRules = []string{PowerShellRuleCode}

	violations := rule.Check(input)
	if len(violations) != 0 {
		t.Fatalf("expected no violations, got %#v", violations)
	}
	if len(fake.scripts) != 1 {
		t.Fatalf("analyzed scripts = %#v, want one heredoc body", fake.scripts)
	}
	want := "Write-Host foo \\\nWrite-Host bar"
	if fake.scripts[0] != want {
		t.Fatalf("analyzed script = %q, want %q", fake.scripts[0], want)
	}
}

func TestRuleChecksOnbuildPowerShellRun(t *testing.T) {
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
	input := testutil.MakeLintInput(t, "Dockerfile", `FROM mcr.microsoft.com/powershell:ubuntu-22.04
SHELL ["pwsh", "-Command"]
ONBUILD RUN Write-Host hi
`)
	input.EnabledRules = []string{PowerShellRuleCode}

	violations := rule.Check(input)
	if len(violations) != 1 {
		t.Fatalf("got %d violations, want 1: %#v", len(violations), violations)
	}
	if len(fake.scripts) != 1 || strings.TrimSpace(fake.scripts[0]) != "Write-Host hi" {
		t.Fatalf("analyzed scripts = %#v, want ONBUILD PowerShell RUN script", fake.scripts)
	}
	if violations[0].Location.Start.Line != 3 {
		t.Fatalf("Location = %#v, want ONBUILD RUN line", violations[0].Location)
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
