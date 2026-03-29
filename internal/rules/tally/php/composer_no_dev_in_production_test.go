package php

import (
	"testing"

	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/testutil"
)

func TestComposerNoDevInProductionRule_Metadata(t *testing.T) {
	t.Parallel()

	meta := NewComposerNoDevInProductionRule().Metadata()
	if meta.Code != ComposerNoDevInProductionRuleCode {
		t.Errorf("Code = %q, want %q", meta.Code, ComposerNoDevInProductionRuleCode)
	}
	if meta.DefaultSeverity != rules.SeverityWarning {
		t.Errorf("DefaultSeverity = %v, want Warning", meta.DefaultSeverity)
	}
	if meta.Category != "security" {
		t.Errorf("Category = %q, want %q", meta.Category, "security")
	}
	if meta.FixPriority != 88 { //nolint:mnd // stable priority contract
		t.Errorf("FixPriority = %d, want 88", meta.FixPriority)
	}
}

func TestComposerNoDevInProductionRule_Check(t *testing.T) {
	t.Parallel()

	testutil.RunRuleTests(t, NewComposerNoDevInProductionRule(), []testutil.RuleTestCase{
		{
			Name: "shell form composer install triggers violation",
			Content: `FROM php:8.4-cli
RUN composer install
`,
			WantViolations: 1,
		},
		{
			Name: "composer install with no-dev is allowed",
			Content: `FROM php:8.4-cli
RUN composer install --no-dev
`,
			WantViolations: 0,
		},
		{
			Name: "COMPOSER_NO_DEV env suppresses violation",
			Content: `FROM php:8.4-cli
ENV COMPOSER_NO_DEV=1
RUN composer install
`,
			WantViolations: 0,
		},
		{
			Name: "truthy COMPOSER_NO_DEV env suppresses violation",
			Content: `FROM php:8.4-cli
ENV COMPOSER_NO_DEV=true
RUN composer install
`,
			WantViolations: 0,
		},
		{
			Name: "falsey COMPOSER_NO_DEV env still violates",
			Content: `FROM php:8.4-cli
ENV COMPOSER_NO_DEV=0
RUN composer install
`,
			WantViolations: 1,
		},
		{
			Name: "dev stage is skipped",
			Content: `FROM php:8.4-cli AS dev
RUN composer install
`,
			WantViolations: 0,
		},
		{
			Name: "testing token stage is skipped",
			Content: `FROM php:8.4-cli AS runtime_testing
RUN composer install
`,
			WantViolations: 0,
		},
		{
			Name: "non-dev stage token still reports",
			Content: `FROM php:8.4-cli AS device
RUN composer install
`,
			WantViolations: 1,
		},
		{
			Name: "composer update does not trigger",
			Content: `FROM php:8.4-cli
RUN composer update
`,
			WantViolations: 0,
		},
		{
			Name: "exec form still reports",
			Content: `FROM php:8.4-cli
RUN ["composer", "install"]
`,
			WantViolations: 1,
		},
		{
			Name: "windows stage skipped",
			Content: `FROM mcr.microsoft.com/windows/servercore:ltsc2022
RUN composer install
`,
			WantViolations: 0,
		},
		{
			Name: "prod and dev stages only report prod",
			Content: `FROM php:8.4-cli AS dev
RUN composer install

FROM php:8.4-cli AS app
RUN composer install
`,
			WantViolations: 1,
		},
	})
}

func TestComposerNoDevInProductionRule_SuggestedFix(t *testing.T) {
	t.Parallel()

	content := "FROM php:8.4-cli\nRUN composer install\n"
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	violations := NewComposerNoDevInProductionRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}

	fix := violations[0].SuggestedFix
	if fix == nil {
		t.Fatal("expected SuggestedFix")
	}
	if fix.Safety != rules.FixSuggestion {
		t.Errorf("Safety = %v, want %v", fix.Safety, rules.FixSuggestion)
	}
	if fix.Priority != 88 { //nolint:mnd // stable priority contract
		t.Errorf("Priority = %d, want 88", fix.Priority)
	}
	if len(fix.Edits) != 1 {
		t.Fatalf("expected 1 edit, got %d", len(fix.Edits))
	}

	edit := fix.Edits[0]
	if edit.Location.Start.Line != 2 || edit.Location.Start.Column != 20 {
		t.Errorf(
			"edit start = %d:%d, want 2:20",
			edit.Location.Start.Line,
			edit.Location.Start.Column,
		)
	}
	if edit.NewText != " --no-dev" {
		t.Errorf("NewText = %q, want %q", edit.NewText, " --no-dev")
	}
}

func TestComposerNoDevInProductionRule_ExecFormHasNoFix(t *testing.T) {
	t.Parallel()

	content := "FROM php:8.4-cli\nRUN [\"composer\", \"install\"]\n"
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	violations := NewComposerNoDevInProductionRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}
	if violations[0].SuggestedFix != nil {
		t.Fatal("expected no SuggestedFix for exec-form RUN")
	}
}
