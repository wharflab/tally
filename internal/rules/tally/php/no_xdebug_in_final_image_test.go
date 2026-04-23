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
			Name: "only exact xdebug install command is reported among same-manager commands",
			Content: `FROM php:8.4-cli
RUN apt-get update && apt-get install -y php-xdebug && apt-get install -y curl
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
		// --- Observable file (COPY heredoc) ---
		{
			Name: "COPY heredoc script installing xdebug triggers violation",
			Content: `FROM php:8.4-fpm AS app
COPY <<EOF /usr/local/bin/setup.sh
#!/bin/bash
pecl install xdebug
docker-php-ext-enable xdebug
EOF
RUN chmod +x /usr/local/bin/setup.sh && /usr/local/bin/setup.sh
`,
			WantViolations: 2,
		},
		{
			Name: "COPY heredoc script without xdebug no violation",
			Content: `FROM php:8.4-fpm AS app
COPY <<EOF /usr/local/bin/setup.sh
#!/bin/bash
docker-php-ext-install gd intl
EOF
RUN chmod +x /usr/local/bin/setup.sh && /usr/local/bin/setup.sh
`,
			WantViolations: 0,
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

func TestNoXdebugInFinalImageRule_SuggestedFix_AptGetSinglePackage(t *testing.T) {
	t.Parallel()

	content := "FROM php:8.4-cli\nRUN apt-get install -y php-xdebug\n"
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	violations := NewNoXdebugInFinalImageRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}

	fix := violations[0].SuggestedFix
	if fix == nil {
		t.Fatal("expected SuggestedFix for standalone apt-get xdebug install")
	}
	if fix.Safety != rules.FixSuggestion {
		t.Errorf("Safety = %v, want FixSuggestion", fix.Safety)
	}
	if fix.Edits[0].NewText != "# RUN apt-get install -y php-xdebug" {
		t.Errorf("NewText = %q, want commented-out line", fix.Edits[0].NewText)
	}
}

func TestNoXdebugInFinalImageRule_SuggestedFix_AptGetUpdateInstallClean(t *testing.T) {
	t.Parallel()

	content := "FROM php:8.4-cli\nRUN apt-get update && apt-get install -y php-xdebug && apt-get clean\n"
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	violations := NewNoXdebugInFinalImageRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}

	fix := violations[0].SuggestedFix
	if fix == nil {
		t.Fatal("expected SuggestedFix for xdebug-only package-manager RUN")
	}
	if fix.Edits[0].NewText != "# RUN apt-get update && apt-get install -y php-xdebug && apt-get clean" {
		t.Errorf("NewText = %q, want commented-out line", fix.Edits[0].NewText)
	}
}

func TestNoXdebugInFinalImageRule_NoFixWhenMixedPackages(t *testing.T) {
	t.Parallel()

	content := "FROM php:8.4-cli\nRUN apt-get install -y php-gd php-xdebug php-intl\n"
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	violations := NewNoXdebugInFinalImageRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}
	if violations[0].SuggestedFix != nil {
		t.Error("expected no SuggestedFix when xdebug is mixed with other packages")
	}
}

func TestNoXdebugInFinalImageRule_NoFixWhenPackageManagerDoesOtherWork(t *testing.T) {
	t.Parallel()

	content := "FROM php:8.4-cli\nRUN apt-get install -y php-xdebug && apt-get remove -y curl\n"
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	violations := NewNoXdebugInFinalImageRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}
	if violations[0].SuggestedFix != nil {
		t.Error("expected no SuggestedFix when the RUN does non-xdebug package-manager work")
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

func TestLooksLikeShellScript(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		path string
		want bool
	}{
		{name: "sh extension", path: "/usr/local/bin/setup.sh", want: true},
		{name: "bash extension", path: "/opt/install.bash", want: true},
		{name: "install in name", path: "/usr/local/bin/install-extensions", want: true},
		{name: "setup in name", path: "/app/setup", want: true},
		{name: "init in name", path: "/docker-entrypoint-init", want: true},
		{name: "entrypoint in name", path: "/usr/local/bin/docker-entrypoint", want: true},
		{name: "start in name", path: "/app/start-server", want: true},
		{name: "php file", path: "/app/index.php", want: false},
		{name: "config file", path: "/etc/php/conf.d/xdebug.ini", want: false},
		{name: "random binary", path: "/usr/bin/composer", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := looksLikeShellScript(tt.path); got != tt.want {
				t.Errorf("looksLikeShellScript(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestContentLooksLikeShellScript(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{name: "bin sh shebang", content: "#!/bin/sh\ndocker-php-ext-install xdebug\n", want: true},
		{name: "bin bash shebang", content: "#!/bin/bash\nset -e\npecl install xdebug\n", want: true},
		{name: "env sh shebang", content: "#!/usr/bin/env sh\necho hello\n", want: true},
		{name: "env bash shebang", content: "#!/usr/bin/env bash\necho hello\n", want: true},
		{name: "no shebang", content: "docker-php-ext-install xdebug\n", want: false},
		{name: "php shebang", content: "#!/usr/bin/php\n<?php echo 1;\n", want: false},
		{name: "empty", content: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := contentLooksLikeShellScript(tt.content); got != tt.want {
				t.Errorf("contentLooksLikeShellScript() = %v, want %v", got, tt.want)
			}
		})
	}
}
