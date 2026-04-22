package php

import (
	"testing"

	"github.com/wharflab/tally/internal/fix"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/shell"
	"github.com/wharflab/tally/internal/testutil"
)

func TestEnableOpcacheInProductionRule_Metadata(t *testing.T) {
	t.Parallel()

	meta := NewEnableOpcacheInProductionRule().Metadata()
	if meta.Code != EnableOpcacheInProductionRuleCode {
		t.Errorf("Code = %q, want %q", meta.Code, EnableOpcacheInProductionRuleCode)
	}
	if meta.DefaultSeverity != rules.SeverityInfo {
		t.Errorf("DefaultSeverity = %v, want Info", meta.DefaultSeverity)
	}
	if meta.Category != "performance" {
		t.Errorf("Category = %q, want %q", meta.Category, "performance")
	}
}

func TestEnableOpcacheInProductionRule_Check(t *testing.T) {
	t.Parallel()

	testutil.RunRuleTests(t, NewEnableOpcacheInProductionRule(), []testutil.RuleTestCase{
		// --- Violations: final stage is php:*fpm*/*apache* with no OPcache signal ---
		{
			Name: "php-fpm final stage without opcache triggers",
			Content: `FROM php:8.4-fpm
WORKDIR /app
COPY . .
`,
			WantViolations: 1,
		},
		{
			Name: "php-apache final stage without opcache triggers",
			Content: `FROM php:8.4-apache
WORKDIR /app
COPY . .
`,
			WantViolations: 1,
		},
		{
			Name: "fpm variant tag without opcache triggers",
			Content: `FROM php:8.3-fpm-alpine
WORKDIR /app
`,
			WantViolations: 1,
		},
		{
			Name: "multi-stage only final runtime violates",
			Content: `FROM composer:2 AS deps
WORKDIR /app

FROM php:8.4-fpm AS app
COPY --from=deps /app /app
`,
			WantViolations: 1,
		},
		{
			Name: "known derivative image serversideup without opcache triggers",
			Content: `FROM serversideup/php:8.3-fpm-nginx
WORKDIR /app
`,
			WantViolations: 1,
		},
		{
			Name: "generic base with exec-form CMD php-fpm triggers",
			Content: `FROM debian:12-slim
WORKDIR /app
CMD ["php-fpm8.3", "-F"]
`,
			WantViolations: 1,
		},
		{
			Name: "generic base with ENTRYPOINT apache2-foreground triggers",
			Content: `FROM debian:12-slim
WORKDIR /var/www/html
ENTRYPOINT ["apache2-foreground"]
`,
			WantViolations: 1,
		},
		{
			Name: "shell-form CMD php-fpm on generic base triggers",
			Content: `FROM debian:12-slim
WORKDIR /app
CMD php-fpm -F
`,
			WantViolations: 1,
		},
		// --- Compliant: OPcache signal present ---
		{
			Name: "docker-php-ext-install opcache suppresses",
			Content: `FROM php:8.4-fpm
RUN docker-php-ext-install opcache
`,
			WantViolations: 0,
		},
		{
			Name: "docker-php-ext-enable opcache suppresses",
			Content: `FROM php:8.4-apache
RUN docker-php-ext-enable opcache
`,
			WantViolations: 0,
		},
		{
			Name: "docker-php-ext-install with multiple extensions including opcache",
			Content: `FROM php:8.4-fpm
RUN docker-php-ext-install gd opcache intl
`,
			WantViolations: 0,
		},
		{
			Name: "apt-get install php-opcache suppresses",
			Content: `FROM php:8.4-fpm
RUN apt-get install -y php-opcache
`,
			WantViolations: 0,
		},
		{
			Name: "apt-get versioned php8.3-opcache suppresses",
			Content: `FROM php:8.4-fpm
RUN apt-get install -y php8.3-opcache
`,
			WantViolations: 0,
		},
		{
			Name: "apk add php-opcache suppresses",
			Content: `FROM php:8.4-fpm-alpine
RUN apk add --no-cache php83-opcache
`,
			WantViolations: 0,
		},
		{
			Name: "PHP_OPCACHE env signals opcache config",
			Content: `FROM php:8.4-fpm
ENV PHP_OPCACHE_ENABLE=1
`,
			WantViolations: 0,
		},
		{
			Name: "COPY opcache.ini suppresses",
			Content: `FROM php:8.4-fpm
COPY <<EOF /usr/local/etc/php/conf.d/opcache.ini
opcache.enable=1
opcache.memory_consumption=256
EOF
`,
			WantViolations: 0,
		},
		{
			Name: "COPY ini with opcache.enable directive suppresses",
			Content: `FROM php:8.4-fpm
COPY <<EOF /usr/local/etc/php/conf.d/perf.ini
opcache.enable=1
EOF
`,
			WantViolations: 0,
		},
		{
			Name: "commented-out opcache directive does not suppress",
			Content: `FROM php:8.4-fpm
COPY <<EOF /usr/local/etc/php/conf.d/perf.ini
; opcache.enable=1
; zend_extension=opcache
memory_limit=256M
EOF
`,
			WantViolations: 1,
		},
		{
			Name: "ini mentioning opcache only inside a comment does not suppress",
			Content: `FROM php:8.4-fpm
COPY <<EOF /usr/local/etc/php/conf.d/perf.ini
; TODO: review opcache.enable later
memory_limit=256M
EOF
`,
			WantViolations: 1,
		},
		// --- Skipped: not a PHP web runtime image ---
		{
			Name: "php cli image is skipped",
			Content: `FROM php:8.4-cli
RUN composer install --no-dev
`,
			WantViolations: 0,
		},
		{
			Name: "non-php base image is skipped",
			Content: `FROM alpine:3.20
RUN echo done
`,
			WantViolations: 0,
		},
		{
			Name: "untagged php image is skipped",
			Content: `FROM php
RUN echo done
`,
			WantViolations: 0,
		},
		{
			Name: "dev-named php-fpm stage is skipped",
			Content: `FROM php:8.4-fpm AS dev
RUN echo done
`,
			WantViolations: 0,
		},
		{
			Name: "only intermediate stage uses php-fpm, final is different",
			Content: `FROM php:8.4-fpm AS intermediate
RUN echo done

FROM alpine:3.20 AS app
COPY --from=intermediate /usr/local /usr/local
`,
			WantViolations: 0,
		},
		{
			Name: "non-final php-fpm builder stage is not checked, final cli ignored",
			Content: `FROM php:8.4-fpm AS builder
RUN echo done

FROM php:8.4-cli AS app
RUN echo done
`,
			WantViolations: 0,
		},
	})
}

func TestCommandReferencesOpcacheExt(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cmd  shell.CommandInfo
		want bool
	}{
		{
			name: "docker-php-ext-install opcache",
			cmd:  shell.CommandInfo{Name: "docker-php-ext-install", Args: []string{"opcache"}},
			want: true,
		},
		{
			name: "docker-php-ext-enable opcache",
			cmd:  shell.CommandInfo{Name: "docker-php-ext-enable", Args: []string{"opcache"}},
			want: true,
		},
		{
			name: "docker-php-ext-configure opcache",
			cmd:  shell.CommandInfo{Name: "docker-php-ext-configure", Args: []string{"opcache"}},
			want: true,
		},
		{
			name: "pecl install opcache",
			cmd:  shell.CommandInfo{Name: "pecl", Subcommand: "install", Args: []string{"install", "opcache"}},
			want: true,
		},
		{
			name: "pecl uninstall opcache ignored",
			cmd:  shell.CommandInfo{Name: "pecl", Subcommand: "uninstall", Args: []string{"uninstall", "opcache"}},
			want: false,
		},
		{
			name: "apt-get install not matched here (handled via InstallCommands)",
			cmd:  shell.CommandInfo{Name: "apt-get", Subcommand: "install", Args: []string{"install", "-y", "php-opcache"}},
			want: false,
		},
		{
			name: "docker-php-ext-install gd only",
			cmd:  shell.CommandInfo{Name: "docker-php-ext-install", Args: []string{"gd"}},
			want: false,
		},
		{
			name: "unrelated command",
			cmd:  shell.CommandInfo{Name: "echo", Args: []string{"hello"}},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := commandReferencesOpcacheExt(tt.cmd); got != tt.want {
				t.Errorf("commandReferencesOpcacheExt(%v) = %v, want %v", tt.cmd.Name, got, tt.want)
			}
		})
	}
}

func TestInstallCommandInstallsOpcache(t *testing.T) {
	t.Parallel()

	makeIC := func(names ...string) shell.InstallCommand {
		pkgs := make([]shell.PackageArg, 0, len(names))
		for _, n := range names {
			pkgs = append(pkgs, shell.PackageArg{Normalized: n})
		}
		return shell.InstallCommand{Packages: pkgs}
	}

	tests := []struct {
		name string
		ic   shell.InstallCommand
		want bool
	}{
		{name: "php-opcache", ic: makeIC("php-opcache"), want: true},
		{name: "php8.3-opcache", ic: makeIC("php8.3-opcache"), want: true},
		{name: "php-opcache with version spec", ic: makeIC("php-opcache=8.3+1ubuntu1"), want: true},
		{name: "php-opcache with arch", ic: makeIC("php-opcache:amd64"), want: true},
		{name: "alpine pecl variant", ic: makeIC("php83-opcache"), want: true},
		{name: "mixed packages including opcache", ic: makeIC("php-gd", "php-opcache", "php-intl"), want: true},
		{name: "no opcache package", ic: makeIC("php-gd", "php-intl"), want: false},
		{name: "empty", ic: shell.InstallCommand{}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := installCommandInstallsOpcache(tt.ic); got != tt.want {
				t.Errorf("installCommandInstallsOpcache(%v) = %v, want %v", tt.ic.Packages, got, tt.want)
			}
		})
	}
}

func TestEnableOpcacheInProductionRule_SuggestedFix_OfficialPHPFPM(t *testing.T) {
	t.Parallel()

	content := "FROM php:8.4-fpm\nWORKDIR /app\n"
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	violations := NewEnableOpcacheInProductionRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}

	sfix := violations[0].SuggestedFix
	if sfix == nil {
		t.Fatal("expected SuggestedFix for official php:*-fpm image")
	}
	if sfix.Safety != rules.FixSuggestion {
		t.Errorf("Safety = %v, want FixSuggestion", sfix.Safety)
	}
	if !sfix.IsPreferred {
		t.Error("expected IsPreferred = true")
	}
	if len(sfix.Edits) != 1 {
		t.Fatalf("expected 1 edit, got %d", len(sfix.Edits))
	}
	edit := sfix.Edits[0]
	if edit.NewText != "RUN docker-php-ext-install opcache\n" {
		t.Errorf("NewText = %q, want RUN docker-php-ext-install opcache\\n", edit.NewText)
	}
	// Inserted on line 2 (right after the FROM on line 1).
	if edit.Location.Start.Line != 2 || edit.Location.Start.Column != 0 {
		t.Errorf(
			"edit start = %d:%d, want 2:0",
			edit.Location.Start.Line,
			edit.Location.Start.Column,
		)
	}
	if edit.Location.End != edit.Location.Start {
		t.Errorf("expected zero-width insertion, got %v -> %v", edit.Location.Start, edit.Location.End)
	}

	want := "FROM php:8.4-fpm\nRUN docker-php-ext-install opcache\nWORKDIR /app\n"
	if got := string(fix.ApplyFix([]byte(content), sfix)); got != want {
		t.Errorf("ApplyFix mismatch:\nwant:\n%q\ngot:\n%q", want, got)
	}
}

func TestEnableOpcacheInProductionRule_NoFixForDerivativeImage(t *testing.T) {
	t.Parallel()

	content := "FROM serversideup/php:8.3-fpm-nginx\nWORKDIR /app\n"
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	violations := NewEnableOpcacheInProductionRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}
	if violations[0].SuggestedFix != nil {
		t.Error("expected no SuggestedFix for derivative image (package name is distro-specific)")
	}
}

func TestEnableOpcacheInProductionRule_NoFixForGenericBaseWithPHPFPMCmd(t *testing.T) {
	t.Parallel()

	// CMD alone, no apt/apk install in view: no distro-specific fix possible.
	content := "FROM debian:12-slim\nWORKDIR /app\nCMD [\"php-fpm8.3\", \"-F\"]\n"
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	violations := NewEnableOpcacheInProductionRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}
	if violations[0].SuggestedFix != nil {
		t.Error("expected no SuggestedFix when no install command is visible")
	}
}

func TestEnableOpcacheInProductionRule_SuggestedFix_PackageManagerDebian(t *testing.T) {
	t.Parallel()

	content := "FROM debian:12-slim\nRUN apt-get install -y php8.3-fpm\nCMD [\"php-fpm8.3\", \"-F\"]\n"
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	violations := NewEnableOpcacheInProductionRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}
	v := violations[0]
	if v.SuggestedFix == nil {
		t.Fatal("expected SuggestedFix when a php*-fpm package is visible in an install command")
	}
	if v.SuggestedFix.Safety != rules.FixSuggestion {
		t.Errorf("Safety = %v, want FixSuggestion", v.SuggestedFix.Safety)
	}
	if len(v.SuggestedFix.Edits) != 1 {
		t.Fatalf("expected 1 edit, got %d", len(v.SuggestedFix.Edits))
	}
	if v.SuggestedFix.Edits[0].NewText != " php8.3-opcache" {
		t.Errorf("NewText = %q, want %q", v.SuggestedFix.Edits[0].NewText, " php8.3-opcache")
	}

	want := "FROM debian:12-slim\nRUN apt-get install -y php8.3-fpm php8.3-opcache\nCMD [\"php-fpm8.3\", \"-F\"]\n"
	if got := string(fix.ApplyFix([]byte(content), v.SuggestedFix)); got != want {
		t.Errorf("ApplyFix mismatch:\nwant:\n%q\ngot:\n%q", want, got)
	}
}

func TestEnableOpcacheInProductionRule_SuggestedFix_PackageManagerAlpine(t *testing.T) {
	t.Parallel()

	content := "FROM alpine:3.20\nRUN apk add --no-cache php83 php83-fpm\nCMD [\"php-fpm83\", \"-F\"]\n"
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	violations := NewEnableOpcacheInProductionRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}
	v := violations[0]
	if v.SuggestedFix == nil {
		t.Fatal("expected SuggestedFix for apk add php83-fpm")
	}
	if v.SuggestedFix.Edits[0].NewText != " php83-opcache" {
		t.Errorf("NewText = %q, want %q", v.SuggestedFix.Edits[0].NewText, " php83-opcache")
	}

	want := "FROM alpine:3.20\nRUN apk add --no-cache php83 php83-fpm php83-opcache\nCMD [\"php-fpm83\", \"-F\"]\n"
	if got := string(fix.ApplyFix([]byte(content), v.SuggestedFix)); got != want {
		t.Errorf("ApplyFix mismatch:\nwant:\n%q\ngot:\n%q", want, got)
	}
}

func TestEnableOpcacheInProductionRule_SuggestedFix_PackageManagerRHEL(t *testing.T) {
	t.Parallel()

	content := "FROM redhat/ubi9\nRUN dnf install -y php-fpm\nCMD [\"php-fpm\", \"-F\"]\n"
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	violations := NewEnableOpcacheInProductionRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}
	v := violations[0]
	if v.SuggestedFix == nil {
		t.Fatal("expected SuggestedFix for dnf install php-fpm")
	}
	if v.SuggestedFix.Edits[0].NewText != " php-opcache" {
		t.Errorf("NewText = %q, want %q", v.SuggestedFix.Edits[0].NewText, " php-opcache")
	}

	want := "FROM redhat/ubi9\nRUN dnf install -y php-fpm php-opcache\nCMD [\"php-fpm\", \"-F\"]\n"
	if got := string(fix.ApplyFix([]byte(content), v.SuggestedFix)); got != want {
		t.Errorf("ApplyFix mismatch:\nwant:\n%q\ngot:\n%q", want, got)
	}
}

func TestEnableOpcacheInProductionRule_NoFixWhenMultiplePHPFPMPackages(t *testing.T) {
	t.Parallel()

	// Two different php*-fpm packages: ambiguous, no fix.
	content := "FROM debian:12-slim\nRUN apt-get install -y php8.3-fpm php8.2-fpm\nCMD [\"php-fpm8.3\", \"-F\"]\n"
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	violations := NewEnableOpcacheInProductionRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}
	if violations[0].SuggestedFix != nil {
		t.Error("expected no SuggestedFix when multiple php*-fpm packages are present")
	}
}

func TestDerivePHPOpcachePackage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   string
		want string
	}{
		{"php8.3-fpm", "php8.3-opcache"},
		{"php7.4-fpm", "php7.4-opcache"},
		{"php83-fpm", "php83-opcache"},
		{"php-fpm", "php-opcache"},
		{"php83-php-fpm", "php83-php-opcache"},
		{"PHP8.3-FPM", "php8.3-opcache"},              // case-insensitive
		{"php8.3-fpm=8.3+1ubuntu1", "php8.3-opcache"}, // version stripped
		{"php8.3-fpm:amd64", "php8.3-opcache"},        // arch stripped
		{"php8.3-cli", ""},                            // CLI is not fpm
		{"nginx", ""},
		{"", ""},
	}
	for _, tt := range tests {
		if got := derivePHPOpcachePackage(tt.in, ""); got != tt.want {
			t.Errorf("derivePHPOpcachePackage(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
