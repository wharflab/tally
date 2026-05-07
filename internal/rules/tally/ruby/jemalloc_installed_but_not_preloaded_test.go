package ruby

import (
	"strings"
	"testing"

	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/shell"
	"github.com/wharflab/tally/internal/testutil"
)

func TestJemallocInstalledButNotPreloadedRule_Metadata(t *testing.T) {
	t.Parallel()

	meta := NewJemallocInstalledButNotPreloadedRule().Metadata()
	if meta.Code != JemallocInstalledButNotPreloadedRuleCode {
		t.Errorf("Code = %q, want %q", meta.Code, JemallocInstalledButNotPreloadedRuleCode)
	}
	if meta.DefaultSeverity != rules.SeverityWarning {
		t.Errorf("DefaultSeverity = %v, want Warning", meta.DefaultSeverity)
	}
	if meta.Category != "performance" {
		t.Errorf("Category = %q, want %q", meta.Category, "performance")
	}
	if meta.FixPriority != jemallocFixPriority {
		t.Errorf("FixPriority = %d, want %d", meta.FixPriority, jemallocFixPriority)
	}
	if !strings.HasPrefix(meta.DocURL, "https://tally.wharflab.com/rules/tally/ruby/") {
		t.Errorf("DocURL = %q, want tally/ruby/ prefix", meta.DocURL)
	}
}

func TestJemallocInstalledButNotPreloadedRule_Check(t *testing.T) {
	t.Parallel()

	testutil.RunRuleTests(t, NewJemallocInstalledButNotPreloadedRule(), []testutil.RuleTestCase{
		// --- Violations ---
		{
			Name: "apt-get install libjemalloc2 without preload triggers",
			Content: `FROM ruby:3.3-slim
RUN apt-get update && apt-get install -y libjemalloc2
`,
			WantViolations: 1,
		},
		{
			Name: "apt-get versioned libjemalloc2 triggers",
			Content: `FROM ruby:3.3-slim
RUN apt-get install -y libjemalloc2=5.3.0-1
`,
			WantViolations: 1,
		},
		{
			Name: "apt-get install libjemalloc-dev triggers",
			Content: `FROM ruby:3.3-slim
RUN apt-get install -y libjemalloc-dev
`,
			WantViolations: 1,
		},
		{
			Name: "apt-get install libjemalloc1 (legacy) triggers",
			Content: `FROM ruby:3.3-slim
RUN apt-get install -y libjemalloc1
`,
			WantViolations: 1,
		},
		{
			Name: "apk add jemalloc on alpine triggers",
			Content: `FROM ruby:3.3-alpine
RUN apk add --no-cache jemalloc
`,
			WantViolations: 1,
		},
		{
			Name: "dnf install jemalloc on UBI triggers",
			Content: `FROM registry.access.redhat.com/ubi9/ruby-33
RUN dnf install -y jemalloc
`,
			WantViolations: 1,
		},
		{
			Name: "multi-stage final stage violates only once",
			Content: `FROM ruby:3.3-slim AS builder
RUN apt-get install -y libjemalloc2 build-essential

FROM ruby:3.3-slim
RUN apt-get install -y libjemalloc2
`,
			WantViolations: 1,
		},
		// --- Guardrails: no violation ---
		{
			Name: "LD_PRELOAD set to canonical path suppresses",
			Content: `FROM ruby:3.3-slim
RUN apt-get install -y libjemalloc2 \
    && ln -s /usr/lib/$(uname -m)-linux-gnu/libjemalloc.so.2 /usr/local/lib/libjemalloc.so
ENV LD_PRELOAD="/usr/local/lib/libjemalloc.so"
`,
			WantViolations: 0,
		},
		{
			Name: "LD_PRELOAD with raw libjemalloc.so.2 path suppresses",
			Content: `FROM ruby:3.3-slim
RUN apt-get install -y libjemalloc2
ENV LD_PRELOAD=/usr/lib/x86_64-linux-gnu/libjemalloc.so.2
`,
			WantViolations: 0,
		},
		{
			Name: "MALLOC_CONF with narenas knob suppresses",
			Content: `FROM ruby:3.3-slim
RUN apt-get install -y libjemalloc2
ENV MALLOC_CONF="narenas:2,background_thread:true,thp:never"
`,
			WantViolations: 0,
		},
		{
			Name: "MALLOC_CONF with thp knob alone suppresses",
			Content: `FROM ruby:3.3-slim
RUN apt-get install -y libjemalloc2
ENV MALLOC_CONF="thp:never"
`,
			WantViolations: 0,
		},
		// --- No violation: skipped stages ---
		{
			Name: "non-final builder stage skipped",
			Content: `FROM ruby:3.3-slim AS builder
RUN apt-get install -y libjemalloc2 build-essential

FROM ruby:3.3-slim
RUN echo done
`,
			WantViolations: 0,
		},
		{
			Name: "stage named dev skipped",
			Content: `FROM ruby:3.3-slim AS dev
RUN apt-get install -y libjemalloc2
`,
			WantViolations: 0,
		},
		{
			Name: "stage named test skipped",
			Content: `FROM ruby:3.3-slim AS test
RUN apt-get install -y libjemalloc2
`,
			WantViolations: 0,
		},
		// --- No jemalloc install, no violation ---
		{
			Name: "apt-get without jemalloc no violation",
			Content: `FROM ruby:3.3-slim
RUN apt-get install -y curl ca-certificates
`,
			WantViolations: 0,
		},
		{
			Name: "npm package with jemalloc-like name does not count",
			Content: `FROM node:20
RUN npm install jemalloc
`,
			WantViolations: 0,
		},
		// --- Windows base skipped ---
		{
			Name: "windows base image skipped",
			Content: `FROM mcr.microsoft.com/windows/servercore:ltsc2022
RUN apt-get install -y libjemalloc2
`,
			WantViolations: 0,
		},
	})
}

func TestJemallocInstalledButNotPreloadedRule_AptFix(t *testing.T) {
	t.Parallel()

	content := "FROM ruby:3.3-slim\nRUN apt-get install -y libjemalloc2\n"
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	violations := NewJemallocInstalledButNotPreloadedRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}

	fix := violations[0].SuggestedFix
	if fix == nil {
		t.Fatal("expected SuggestedFix for apt variant")
	}
	if fix.Safety != rules.FixSuggestion {
		t.Errorf("Safety = %v, want FixSuggestion", fix.Safety)
	}
	if !fix.IsPreferred {
		t.Error("expected IsPreferred = true")
	}
	if fix.Priority != jemallocFixPriority {
		t.Errorf("Priority = %d, want %d", fix.Priority, jemallocFixPriority)
	}
	if len(fix.Edits) != 1 {
		t.Fatalf("expected 1 edit, got %d", len(fix.Edits))
	}
	edit := fix.Edits[0]
	// Zero-width insertion at the line after the install RUN.
	if edit.Location.Start.Line != 3 || edit.Location.End.Line != 3 {
		t.Errorf("edit location = %+v, want line 3", edit.Location)
	}
	if edit.Location.Start.Column != 0 || edit.Location.End.Column != 0 {
		t.Errorf("edit location columns = (%d,%d), want (0,0) (zero-width insert)",
			edit.Location.Start.Column, edit.Location.End.Column)
	}
	if !strings.Contains(edit.NewText, "libjemalloc.so.2") || !strings.Contains(edit.NewText, "LD_PRELOAD") {
		t.Errorf("NewText missing canonical fix elements: %q", edit.NewText)
	}
}

func TestJemallocInstalledButNotPreloadedRule_AptFixAfterMultiLineRun(t *testing.T) {
	t.Parallel()

	content := "FROM ruby:3.3-slim\nRUN apt-get update \\\n    && apt-get install -y libjemalloc2 \\\n    && rm -rf /var/lib/apt/lists/*\n"
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	violations := NewJemallocInstalledButNotPreloadedRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}
	fix := violations[0].SuggestedFix
	if fix == nil {
		t.Fatal("expected SuggestedFix for multi-line apt RUN")
	}
	// The continuation goes from line 2 to line 4; insert anchor must be line 5.
	got := fix.Edits[0].Location.Start.Line
	if got != 5 {
		t.Errorf("insert anchor line = %d, want 5 (after multi-line RUN)", got)
	}
}

func TestJemallocInstalledButNotPreloadedRule_NoFixForApk(t *testing.T) {
	t.Parallel()

	content := "FROM ruby:3.3-alpine\nRUN apk add --no-cache jemalloc\n"
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	violations := NewJemallocInstalledButNotPreloadedRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}
	if violations[0].SuggestedFix != nil {
		t.Error("expected no SuggestedFix for apk variant (path differs from apt)")
	}
}

func TestJemallocInstalledButNotPreloadedRule_ViolationLocation(t *testing.T) {
	t.Parallel()

	content := "FROM ruby:3.3-slim\nRUN apt-get install -y libjemalloc2\n"
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	violations := NewJemallocInstalledButNotPreloadedRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}
	loc := violations[0].Location
	if loc.Start.Line != 2 {
		t.Errorf("violation line = %d, want 2 (the install RUN)", loc.Start.Line)
	}
	// The location should point to the package token, not to column 0 of RUN.
	if loc.Start.Column == 0 {
		t.Errorf("violation column = 0, expected non-zero (anchored on package token)")
	}
}

// --- Helper-level tests for coverage ---

func TestIsJemallocPackage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		want bool
	}{
		{name: "libjemalloc2", want: true},
		{name: "libjemalloc1", want: true},
		{name: "libjemalloc-dev", want: true},
		{name: "jemalloc", want: true},
		{name: "jemalloc-dev", want: true},
		{name: "libjemalloc2=5.3.0-1", want: true},
		{name: "libjemalloc2:amd64", want: true},
		{name: "LIBJEMALLOC2", want: true},
		{name: "libfoo", want: false},
		{name: "ruby", want: false},
		{name: "", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := isJemallocPackage(tt.name); got != tt.want {
				t.Errorf("isJemallocPackage(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

func TestInstallCommandInstallsJemalloc(t *testing.T) {
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
		{name: "apt libjemalloc2", ic: makeIC("apt-get", "libjemalloc2"), want: true},
		{name: "apk jemalloc", ic: makeIC("apk", "jemalloc"), want: true},
		{name: "dnf jemalloc", ic: makeIC("dnf", "jemalloc"), want: true},
		{name: "yum jemalloc", ic: makeIC("yum", "jemalloc"), want: true},
		{name: "zypper jemalloc", ic: makeIC("zypper", "jemalloc"), want: true},
		{name: "microdnf jemalloc", ic: makeIC("microdnf", "jemalloc"), want: true},
		{name: "apt-get mixed", ic: makeIC("apt-get", "curl", "libjemalloc2"), want: true},
		{name: "apt no jemalloc", ic: makeIC("apt-get", "curl", "git"), want: false},
		{name: "npm jemalloc-named pkg ignored", ic: makeIC("npm", "jemalloc"), want: false},
		{name: "pip jemalloc-named pkg ignored", ic: makeIC("pip", "jemalloc"), want: false},
		{name: "empty", ic: shell.InstallCommand{}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := installCommandInstallsJemalloc(tt.ic); got != tt.want {
				t.Errorf("installCommandInstallsJemalloc(%v) = %v, want %v", tt.ic.Packages, got, tt.want)
			}
		})
	}
}

func TestEnvContainsJemallocLDPreload(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{name: "canonical path", value: "/usr/local/lib/libjemalloc.so", want: true},
		{name: "debian multiarch path", value: "/usr/lib/x86_64-linux-gnu/libjemalloc.so.2", want: true},
		{name: "uppercase path", value: "/USR/LIB/libJemalloc.so", want: true},
		{name: "non-jemalloc preload", value: "/usr/lib/libfoo.so", want: false},
		{name: "empty", value: "", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := envContainsJemallocLDPreload(tt.value); got != tt.want {
				t.Errorf("envContainsJemallocLDPreload(%q) = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}

func TestMallocConfHasJemallocKnob(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{name: "narenas only", value: "narenas:2", want: true},
		{name: "full mastodon-style", value: "narenas:2,background_thread:true,thp:never,dirty_decay_ms:1000,muzzy_decay_ms:0", want: true},
		{name: "thp alone", value: "thp:never", want: true},
		{name: "uppercased knob (case-insensitive)", value: "NARENAS:2", want: true},
		{name: "mixed case knob", value: "Background_Thread:true", want: true},
		{name: "no jemalloc keys", value: "M_MMAP_MAX=0", want: false},
		{name: "empty", value: "", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := mallocConfHasJemallocKnob(tt.value); got != tt.want {
				t.Errorf("mallocConfHasJemallocKnob(%q) = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}

func TestJemallocViolationDetail_AptVsOther(t *testing.T) {
	t.Parallel()

	apt := jemallocViolationDetail("apt-get")
	if !strings.Contains(apt, "ln -s /usr/lib/$(uname -m)") {
		t.Errorf("apt detail missing canonical ln command: %q", apt)
	}
	apk := jemallocViolationDetail("apk")
	if strings.Contains(apk, "ln -s /usr/lib/$(uname -m)") {
		t.Errorf("non-apt detail should not suggest a debian-multiarch ln command: %q", apk)
	}
}
