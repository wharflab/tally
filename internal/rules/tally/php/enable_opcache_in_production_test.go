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
		{
			Name: "wrapper ENTRYPOINT + CMD php-fpm still triggers",
			Content: `FROM debian:12-slim
RUN apt-get install -y php8.3-fpm
ENTRYPOINT ["docker-entrypoint.sh"]
CMD ["php-fpm", "-F"]
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
		// --- COPY --from inheritance: final stage inherits opcache from builder ---
		{
			Name: "final stage copies ext dir from builder with opcache no violation",
			Content: `FROM php:5.6-fpm AS builder
RUN docker-php-ext-install opcache

FROM php:5.6-fpm
COPY --from=builder /usr/local/lib/php/extensions/no-debug-non-zts-20131226/ /usr/local/lib/php/extensions/no-debug-non-zts-20131226/
`,
			WantViolations: 0,
		},
		{
			Name: "final stage copies conf.d from builder with opcache no violation",
			Content: `FROM php:8.4-fpm AS builder
RUN docker-php-ext-install opcache

FROM php:8.4-fpm
COPY --from=builder /usr/local/etc/php/conf.d/ /usr/local/etc/php/conf.d/
`,
			WantViolations: 0,
		},
		{
			Name: "final stage copies parent php tree from builder with opcache no violation",
			Content: `FROM php:8.4-fpm AS builder
RUN docker-php-ext-install opcache

FROM php:8.4-fpm
COPY --from=builder /usr/local/ /usr/local/
`,
			WantViolations: 0,
		},
		{
			Name: "final stage copies unrelated files from builder with opcache still violates",
			Content: `FROM php:8.4-fpm AS builder
RUN docker-php-ext-install opcache

FROM php:8.4-fpm
COPY --from=builder /app /app
`,
			WantViolations: 1,
		},
		{
			Name: "final stage copies php conf to inactive destination still violates",
			Content: `FROM php:8.4-fpm AS builder
RUN docker-php-ext-install opcache

FROM php:8.4-fpm
COPY --from=builder /usr/local/etc/php/conf.d/ /tmp/conf.d/
`,
			WantViolations: 1,
		},
		{
			Name: "final stage copies ext dir from builder without opcache still violates",
			Content: `FROM php:8.4-fpm AS builder
RUN docker-php-ext-install gd

FROM php:8.4-fpm
COPY --from=builder /usr/local/lib/php/extensions/no-debug-non-zts-20230831/ /usr/local/lib/php/extensions/no-debug-non-zts-20230831/
`,
			WantViolations: 1,
		},
		{
			Name: "transitive COPY --from chain preserves opcache inheritance",
			Content: `FROM php:8.4-fpm AS base-ext
RUN docker-php-ext-install opcache

FROM php:8.4-fpm AS intermediate
COPY --from=base-ext /usr/local/lib/php/extensions/ /usr/local/lib/php/extensions/

FROM php:8.4-fpm
COPY --from=intermediate /usr/local/lib/php/extensions/ /usr/local/lib/php/extensions/
COPY --from=intermediate /usr/local/etc/php/conf.d/ /usr/local/etc/php/conf.d/
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

	makeIC := func(manager string, names ...string) shell.InstallCommand {
		pkgs := make([]shell.PackageArg, 0, len(names))
		for _, n := range names {
			pkgs = append(pkgs, shell.PackageArg{Normalized: n})
		}
		return shell.InstallCommand{Manager: manager, Packages: pkgs}
	}

	tests := []struct {
		name string
		ic   shell.InstallCommand
		want bool
	}{
		{name: "apt php-opcache", ic: makeIC("apt-get", "php-opcache"), want: true},
		{name: "apt php8.3-opcache", ic: makeIC("apt-get", "php8.3-opcache"), want: true},
		{name: "apt php-opcache with version spec", ic: makeIC("apt-get", "php-opcache=8.3+1ubuntu1"), want: true},
		{name: "apt php-opcache with arch", ic: makeIC("apt-get", "php-opcache:amd64"), want: true},
		{name: "apk php83-opcache", ic: makeIC("apk", "php83-opcache"), want: true},
		{name: "dnf mixed packages", ic: makeIC("dnf", "php-gd", "php-opcache", "php-intl"), want: true},
		{name: "apt no opcache", ic: makeIC("apt-get", "php-gd", "php-intl"), want: false},
		{name: "empty", ic: shell.InstallCommand{}, want: false},
		// Non-OS package managers must not count as an OPcache signal even
		// if the package name contains "opcache".
		{name: "npm fake opcache package", ic: makeIC("npm", "opcache"), want: false},
		{name: "pip fake opcache package", ic: makeIC("pip", "php-opcache"), want: false},
		{name: "composer fake opcache package", ic: makeIC("composer", "php-opcache"), want: false},
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

// When tally/sort-packages is enabled, the fix anchors at the LAST package
// in the install command (not the php*-fpm token itself) so the inserted
// opcache lands at the end of the sorted block. This keeps the composition
// with sort-packages clean: both fixes apply in a single --fix pass and
// produce a fully sorted list with opcache at the correct position.
func TestEnableOpcacheInProductionRule_SuggestedFix_SortPackagesEnabled(t *testing.T) {
	t.Parallel()

	content := "FROM debian:12-slim\nRUN apt-get install -y php8.3-fpm php8.3-cli\nCMD [\"php-fpm8.3\", \"-F\"]\n"
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	input.EnabledRules = []string{
		EnableOpcacheInProductionRuleCode,
		sortPackagesRuleCode,
	}
	violations := NewEnableOpcacheInProductionRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}
	v := violations[0]
	if v.SuggestedFix == nil {
		t.Fatal("expected SuggestedFix")
	}

	// Anchor is php8.3-cli (last package), so opcache is inserted AFTER it,
	// not after php8.3-fpm.
	want := "FROM debian:12-slim\nRUN apt-get install -y php8.3-fpm php8.3-cli php8.3-opcache\nCMD [\"php-fpm8.3\", \"-F\"]\n"
	if got := string(fix.ApplyFix([]byte(content), v.SuggestedFix)); got != want {
		t.Errorf("ApplyFix mismatch:\nwant:\n%q\ngot:\n%q", want, got)
	}
}

// Heredoc RUN: the install command lives in the heredoc body, which
// starts on the line AFTER "RUN <<EOF". Validates that the fix applies
// to the correct Dockerfile line (shell-parser columns are 0-based
// within the heredoc body and need the extra +1 line offset).
func TestEnableOpcacheInProductionRule_SuggestedFix_PackageManagerHeredoc(t *testing.T) {
	t.Parallel()

	content := "FROM debian:12-slim\nRUN <<EOF\napt-get install -y php8.3-fpm\nEOF\nCMD [\"php-fpm8.3\", \"-F\"]\n"
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	violations := NewEnableOpcacheInProductionRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}
	v := violations[0]
	if v.SuggestedFix == nil {
		t.Fatal("expected SuggestedFix for a heredoc RUN installing php*-fpm")
	}

	want := "FROM debian:12-slim\nRUN <<EOF\napt-get install -y php8.3-fpm php8.3-opcache\nEOF\nCMD [\"php-fpm8.3\", \"-F\"]\n"
	if got := string(fix.ApplyFix([]byte(content), v.SuggestedFix)); got != want {
		t.Errorf("ApplyFix mismatch:\nwant:\n%q\ngot:\n%q", want, got)
	}
}

// Multi-line RUN with backslash continuation: the php*-fpm package lives on
// line 1 of the reconstructed script (not the RUN line). Validates that
// DockerfileRunCommandStartCol is only added on line 0, not on continuation
// lines where the reconstructed source preserves the original indentation.
func TestEnableOpcacheInProductionRule_SuggestedFix_PackageManagerContinuation(t *testing.T) {
	t.Parallel()

	content := "FROM debian:12-slim\nRUN apt-get install -y \\\n    php8.3-fpm\nCMD [\"php-fpm8.3\", \"-F\"]\n"
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	violations := NewEnableOpcacheInProductionRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}
	v := violations[0]
	if v.SuggestedFix == nil {
		t.Fatal("expected SuggestedFix for apt-get install across a continuation line")
	}

	want := "FROM debian:12-slim\nRUN apt-get install -y \\\n    php8.3-fpm php8.3-opcache\nCMD [\"php-fpm8.3\", \"-F\"]\n"
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
		in      string
		manager string
		want    string
	}{
		{in: "php8.3-fpm", manager: "apt-get", want: "php8.3-opcache"},
		{in: "php7.4-fpm", manager: "apt", want: "php7.4-opcache"},
		{in: "php83-fpm", manager: "apk", want: "php83-opcache"},
		{in: "php-fpm", manager: "dnf", want: "php-opcache"},
		{in: "php-fpm", manager: "yum", want: "php-opcache"},
		{in: "php-fpm", manager: "microdnf", want: "php-opcache"},
		{in: "php83-php-fpm", manager: "dnf", want: "php83-php-opcache"},
		{in: "PHP8.3-FPM", manager: "apt-get", want: "php8.3-opcache"}, // case-insensitive
		{in: "php8.3-fpm=8.3+1ubuntu1", manager: "apt-get", want: "php8.3-opcache"},
		{in: "php8.3-fpm:amd64", manager: "apt-get", want: "php8.3-opcache"},
		{in: "php8.3-cli", manager: "apt-get", want: ""}, // CLI is not fpm
		{in: "nginx", manager: "apt-get", want: ""},
		{in: "", manager: "apt-get", want: ""},
		// Non-OS package managers must not receive an OS opcache derivation,
		// even if the package happens to look like php-fpm.
		{in: "php8.3-fpm", manager: "npm", want: ""},
		{in: "php-fpm", manager: "pip", want: ""},
		{in: "php-fpm", manager: "composer", want: ""},
		{in: "php-fpm", manager: "", want: ""},
	}
	for _, tt := range tests {
		if got := derivePHPOpcachePackage(tt.in, tt.manager); got != tt.want {
			t.Errorf("derivePHPOpcachePackage(%q, %q) = %q, want %q", tt.in, tt.manager, got, tt.want)
		}
	}
}
