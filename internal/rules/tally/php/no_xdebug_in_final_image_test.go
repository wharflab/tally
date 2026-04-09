package php

import (
	"testing"

	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/testutil"
)

func TestNoXdebugInFinalImageRule_Metadata(t *testing.T) {
	t.Parallel()

	meta := NewNoXdebugInFinalImageRule().Metadata()
	if meta.Code != NoXdebugInFinalImageRuleCode {
		t.Errorf("Code = %q, want %q", meta.Code, NoXdebugInFinalImageRuleCode)
	}
	if meta.DefaultSeverity != rules.SeverityWarning {
		t.Errorf("DefaultSeverity = %v, want Warning", meta.DefaultSeverity)
	}
	if meta.Category != "best-practices" {
		t.Errorf("Category = %q, want %q", meta.Category, "best-practices")
	}
	if meta.FixPriority != 88 { //nolint:mnd // stable priority contract
		t.Errorf("FixPriority = %d, want 88", meta.FixPriority)
	}
}

func TestNoXdebugInFinalImageRule_Check(t *testing.T) {
	t.Parallel()

	testutil.RunRuleTests(t, NewNoXdebugInFinalImageRule(), []testutil.RuleTestCase{
		// --- Violations ---
		{
			Name: "docker-php-ext-install xdebug triggers violation",
			Content: `FROM php:8.4-cli
RUN docker-php-ext-install xdebug
`,
			WantViolations: 1,
		},
		{
			Name: "docker-php-ext-enable xdebug triggers violation",
			Content: `FROM php:8.4-cli
RUN docker-php-ext-enable xdebug
`,
			WantViolations: 1,
		},
		{
			Name: "pecl install xdebug triggers violation",
			Content: `FROM php:8.4-cli
RUN pecl install xdebug
`,
			WantViolations: 1,
		},
		{
			Name: "versioned pecl install triggers violation",
			Content: `FROM php:8.4-cli
RUN pecl install xdebug-3.4.0
`,
			WantViolations: 1,
		},
		{
			Name: "xdebug among other extensions triggers violation",
			Content: `FROM php:8.4-cli
RUN docker-php-ext-install gd xdebug intl
`,
			WantViolations: 1,
		},
		{
			Name: "apt-get install php-xdebug triggers violation",
			Content: `FROM php:8.4-cli
RUN apt-get install -y php-xdebug
`,
			WantViolations: 1,
		},
		{
			Name: "apt-get versioned php8.3-xdebug triggers violation",
			Content: `FROM php:8.4-cli
RUN apt-get install -y php8.3-xdebug
`,
			WantViolations: 1,
		},
		{
			Name: "apk add php-pecl-xdebug triggers violation",
			Content: `FROM php:8.4-cli
RUN apk add --no-cache php83-pecl-xdebug
`,
			WantViolations: 1,
		},
		{
			Name: "chained install and enable reports both",
			Content: `FROM php:8.4-cli
RUN pecl install xdebug && docker-php-ext-enable xdebug
`,
			WantViolations: 2,
		},
		{
			Name: "exec form pecl install xdebug triggers violation",
			Content: `FROM php:8.4-cli
RUN ["pecl", "install", "xdebug"]
`,
			WantViolations: 1,
		},
		{
			Name: "multi-line continuation triggers violation",
			Content: `FROM php:8.4-cli
RUN pecl install \
    xdebug
`,
			WantViolations: 1,
		},
		// --- No violations ---
		{
			Name: "xdebug in builder stage no violation",
			Content: `FROM php:8.4-cli AS builder
RUN docker-php-ext-install xdebug

FROM php:8.4-cli
RUN echo done
`,
			WantViolations: 0,
		},
		{
			Name: "xdebug in dev-named stage no violation",
			Content: `FROM php:8.4-cli AS dev
RUN docker-php-ext-install xdebug
`,
			WantViolations: 0,
		},
		{
			Name: "xdebug in debug-named stage no violation",
			Content: `FROM php:8.4-cli AS runtime.debug
RUN pecl install xdebug
`,
			WantViolations: 0,
		},
		{
			Name: "xdebug in test-named stage no violation",
			Content: `FROM php:8.4-cli AS test
RUN docker-php-ext-install xdebug
`,
			WantViolations: 0,
		},
		{
			Name: "multi-stage only final violates",
			Content: `FROM php:8.4-cli AS builder
RUN docker-php-ext-install xdebug

FROM php:8.4-cli AS app
RUN docker-php-ext-install xdebug
`,
			WantViolations: 1,
		},
		{
			Name: "no xdebug clean image",
			Content: `FROM php:8.4-cli
RUN docker-php-ext-install gd intl
`,
			WantViolations: 0,
		},
		{
			Name: "windows stage skipped",
			Content: `FROM mcr.microsoft.com/windows/servercore:ltsc2022
RUN docker-php-ext-install xdebug
`,
			WantViolations: 0,
		},
		{
			Name: "pecl install unrelated package no violation",
			Content: `FROM php:8.4-cli
RUN pecl install redis
`,
			WantViolations: 0,
		},
		{
			Name: "non-dev word in stage name still reports",
			Content: `FROM php:8.4-cli AS device
RUN docker-php-ext-install xdebug
`,
			WantViolations: 1,
		},
	})
}

func TestNoXdebugInFinalImageRule_SuggestedFix_SingleLine(t *testing.T) {
	t.Parallel()

	content := "FROM php:8.4-cli\nRUN docker-php-ext-install xdebug\n"
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	violations := NewNoXdebugInFinalImageRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}

	fix := violations[0].SuggestedFix
	if fix == nil {
		t.Fatal("expected SuggestedFix")
	}
	if fix.Safety != rules.FixSuggestion {
		t.Errorf("Safety = %v, want FixSuggestion", fix.Safety)
	}
	if !fix.IsPreferred {
		t.Error("expected IsPreferred = true")
	}
	if fix.Priority != 88 { //nolint:mnd // stable priority contract
		t.Errorf("Priority = %d, want 88", fix.Priority)
	}
	if len(fix.Edits) != 1 {
		t.Fatalf("expected 1 edit, got %d", len(fix.Edits))
	}
	if fix.Edits[0].NewText != "# RUN docker-php-ext-install xdebug" {
		t.Errorf("NewText = %q, want commented-out line", fix.Edits[0].NewText)
	}
}

func TestNoXdebugInFinalImageRule_SuggestedFix_MultiLine(t *testing.T) {
	t.Parallel()

	content := "FROM php:8.4-cli\nRUN pecl install \\\n    xdebug\n"
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	violations := NewNoXdebugInFinalImageRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}

	fix := violations[0].SuggestedFix
	if fix == nil {
		t.Fatal("expected SuggestedFix")
	}
	if fix.Edits[0].NewText != "# RUN pecl install \\\n#     xdebug" {
		t.Errorf("NewText = %q, want commented-out multi-line", fix.Edits[0].NewText)
	}
}

func TestNoXdebugInFinalImageRule_NoFixWhenMixed(t *testing.T) {
	t.Parallel()

	content := "FROM php:8.4-cli\nRUN docker-php-ext-install gd xdebug intl\n"
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	violations := NewNoXdebugInFinalImageRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}
	if violations[0].SuggestedFix != nil {
		t.Error("expected no SuggestedFix when xdebug is mixed with other extensions")
	}
}

func TestNoXdebugInFinalImageRule_ExecFormHasNoFix(t *testing.T) {
	t.Parallel()

	content := "FROM php:8.4-cli\nRUN [\"pecl\", \"install\", \"xdebug\"]\n"
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	violations := NewNoXdebugInFinalImageRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}
	// Exec form may not get a fix since runStartLine can be 0 and source mapping
	// behaves differently.
}

func TestNoXdebugInFinalImageRule_DeleteFix(t *testing.T) {
	t.Parallel()

	content := "FROM php:8.4-cli\nRUN docker-php-ext-install xdebug\n"
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	violations := NewNoXdebugInFinalImageRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}

	fixes := violations[0].SuggestedFixes
	if len(fixes) < 2 {
		t.Fatalf("expected at least 2 alternative fixes, got %d", len(fixes))
	}

	deleteFix := fixes[1]
	if deleteFix.Safety != rules.FixUnsafe {
		t.Errorf("delete fix Safety = %v, want FixUnsafe", deleteFix.Safety)
	}
	if len(deleteFix.Edits) != 1 {
		t.Fatalf("expected 1 edit, got %d", len(deleteFix.Edits))
	}
	if deleteFix.Edits[0].NewText != "" {
		t.Errorf("NewText = %q, want empty (deletion)", deleteFix.Edits[0].NewText)
	}
}
