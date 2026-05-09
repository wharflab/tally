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
			Name: "dnf install jemalloc-devel on UBI triggers",
			Content: `FROM registry.access.redhat.com/ubi9/ruby-33
RUN dnf install -y jemalloc-devel
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
		// Regression: ENV chaining (LD_PRELOAD=$JEMALLOC_PATH) must resolve
		// through EffectiveEnv.Values, not the literal binding.Value, so
		// the load signal is recognized.
		{
			Name: "chained ENV LD_PRELOAD via $JEMALLOC_PATH suppresses",
			Content: `FROM ruby:3.3-slim
RUN apt-get install -y libjemalloc2
ENV JEMALLOC_PATH=/usr/local/lib/libjemalloc.so
ENV LD_PRELOAD=$JEMALLOC_PATH
`,
			WantViolations: 0,
		},
		{
			Name: "chained ENV MALLOC_CONF via $MALLOC_TUNING suppresses",
			Content: `FROM ruby:3.3-slim
RUN apt-get install -y libjemalloc2
ENV MALLOC_TUNING=narenas:2,background_thread:true,thp:never
ENV MALLOC_CONF=$MALLOC_TUNING
`,
			WantViolations: 0,
		},
		// Regression: ARG values are build-time only and absent from the
		// final image runtime, so an ARG-only LD_PRELOAD must NOT suppress
		// the violation — the running process still uses glibc malloc.
		{
			Name: "ARG-only LD_PRELOAD does not suppress",
			Content: `FROM ruby:3.3-slim
ARG LD_PRELOAD=/usr/local/lib/libjemalloc.so
RUN apt-get install -y libjemalloc2
`,
			WantViolations: 1,
		},
		{
			Name: "ARG-only MALLOC_CONF does not suppress",
			Content: `FROM ruby:3.3-slim
ARG MALLOC_CONF=narenas:2,background_thread:true
RUN apt-get install -y libjemalloc2
`,
			WantViolations: 1,
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
		// --- Non-Ruby stage is out of scope for this Ruby-namespaced rule ---
		{
			Name: "python image installing libjemalloc2 is not a Ruby concern",
			Content: `FROM python:3.12-slim
RUN apt-get install -y libjemalloc2
`,
			WantViolations: 0,
		},
		{
			Name: "node image installing libjemalloc2 is not a Ruby concern",
			Content: `FROM node:20
RUN apt-get install -y libjemalloc2
`,
			WantViolations: 0,
		},
		// --- Ruby signal via env var (no ruby:* base) ---
		{
			Name: "RAILS_ENV signals a Ruby stage even on debian base",
			Content: `FROM debian:12-slim
ENV RAILS_ENV=production
RUN apt-get install -y libjemalloc2
`,
			WantViolations: 1,
		},
		// Regression: an ARG-promoted RAILS_ENV is build-time only and must
		// NOT classify a non-Ruby stage as Ruby — the rule should stay out
		// of scope rather than warn on a debian image that just happens to
		// declare a Rails-flavored ARG.
		{
			Name: "ARG-only RAILS_ENV does not classify debian as Ruby",
			Content: `FROM debian:12-slim
ARG RAILS_ENV=production
RUN apt-get install -y libjemalloc2
`,
			WantViolations: 0,
		},
		{
			Name: "ARG-only BUNDLE_PATH does not classify alpine as Ruby",
			Content: `FROM alpine:3.20
ARG BUNDLE_PATH=/vendor/bundle
RUN apk add --no-cache jemalloc
`,
			WantViolations: 0,
		},
		// --- Ruby signal via runtime command ---
		{
			Name: "puma CMD signals a Ruby stage on debian base",
			Content: `FROM debian:12-slim
RUN apt-get install -y libjemalloc2
CMD ["puma", "-C", "config/puma.rb"]
`,
			WantViolations: 1,
		},
		// --- Ruby derivative image ---
		{
			Name: "phusion passenger-ruby base counts as Ruby",
			Content: `FROM phusion/passenger-ruby33:latest
RUN apt-get install -y libjemalloc2
`,
			WantViolations: 1,
		},
		// --- Multi-stage FROM <prev-stage>: must walk ancestry to find Ruby base ---
		{
			Name: "final stage FROM builder where builder is ruby:* fires",
			Content: `FROM ruby:3.3-slim AS builder
RUN echo build steps

FROM builder
RUN apt-get install -y libjemalloc2
`,
			WantViolations: 1,
		},
		{
			Name: "multi-hop FROM ancestry still resolves to ruby base",
			Content: `FROM ruby:3.3-slim AS base
FROM base AS deps
FROM deps
RUN apt-get install -y libjemalloc2
`,
			WantViolations: 1,
		},
		// --- ARG-templated FROM: must expand against meta ARGs ---
		{
			Name: "ARG-templated ruby image with default value resolves",
			Content: `ARG RUBY_IMAGE=ruby:3.3-slim
FROM ${RUBY_IMAGE}
RUN apt-get install -y libjemalloc2
`,
			WantViolations: 1,
		},
		{
			Name: "ARG-templated rails image resolves to non-default",
			Content: `ARG BASE_IMAGE=ruby:3.3-slim
FROM $BASE_IMAGE
RUN apt-get install -y libjemalloc2
`,
			WantViolations: 1,
		},
		{
			// Without expansion the rule used to skip this stage entirely;
			// classification must still fire on the templated-but-non-Ruby
			// case only when the resolved name is Ruby.
			Name: "ARG-templated non-Ruby base does not fire",
			Content: `ARG BASE_IMAGE=python:3.12-slim
FROM ${BASE_IMAGE}
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
	// Use idempotent `ln -sf` so a re-run on already-linked layouts replaces
	// the symlink rather than failing the build with `File exists`.
	if !strings.Contains(edit.NewText, "ln -sf ") {
		t.Errorf("NewText must use idempotent `ln -sf`, got %q", edit.NewText)
	}
}

// Regression: when the stage already creates the libjemalloc.so symlink and
// only forgot to set LD_PRELOAD, the fix must NOT add a second `ln -s`. Adding
// it would make `--fix-unsafe` rebuilds fail with `File exists`.
func TestJemallocInstalledButNotPreloadedRule_FixSkipsSymlinkWhenAlreadyPresent(t *testing.T) {
	t.Parallel()

	content := "FROM ruby:3.3-slim\n" +
		"RUN apt-get install -y libjemalloc2 \\\n" +
		"    && ln -s /usr/lib/$(uname -m)-linux-gnu/libjemalloc.so.2 /usr/local/lib/libjemalloc.so\n"
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	violations := NewJemallocInstalledButNotPreloadedRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}
	fix := violations[0].SuggestedFix
	if fix == nil {
		t.Fatal("expected SuggestedFix when stage already symlinked but missing LD_PRELOAD")
	}
	if len(fix.Edits) != 1 {
		t.Fatalf("expected 1 edit, got %d", len(fix.Edits))
	}
	got := fix.Edits[0].NewText
	if strings.Contains(got, "ln -s") {
		t.Errorf("fix added a redundant ln -s when the stage already has one: %q", got)
	}
	if !strings.Contains(got, `ENV LD_PRELOAD="/usr/local/lib/libjemalloc.so"`) {
		t.Errorf("fix must still emit the missing ENV LD_PRELOAD line: %q", got)
	}
}

func TestStageReferencesJemallocSymlink(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{
			name: "stage with ln -s libjemalloc.so",
			content: "FROM ruby:3.3-slim\n" +
				"RUN apt-get install -y libjemalloc2 \\\n" +
				"    && ln -s /usr/lib/x86_64-linux-gnu/libjemalloc.so.2 /usr/local/lib/libjemalloc.so\n",
			want: true,
		},
		{
			name: "stage without symlink",
			content: "FROM ruby:3.3-slim\n" +
				"RUN apt-get install -y libjemalloc2\n",
			want: false,
		},
		{
			name: "package name libjemalloc2 alone does not count",
			content: "FROM ruby:3.3-slim\n" +
				"RUN apt-get install -y libjemalloc2 libjemalloc-dev\n",
			want: false,
		},
		{
			// Regression: a script that only echoes/finds the path must not
			// suppress the symlink half of the fix.
			name: "find/echo references do not count",
			content: "FROM ruby:3.3-slim\n" +
				"RUN apt-get install -y libjemalloc2\n" +
				"RUN find / -name 'libjemalloc.so*' && echo 'see libjemalloc.so docs'\n",
			want: false,
		},
		{
			// Regression: a reference to libjemalloc.so.2 (the apt-shipped
			// versioned library) is NOT the symlink target — basename is
			// "libjemalloc.so.2", not "libjemalloc.so".
			name: "reference to libjemalloc.so.2 only does not count",
			content: "FROM ruby:3.3-slim\n" +
				"RUN apt-get install -y libjemalloc2\n" +
				"RUN ls -l /usr/lib/x86_64-linux-gnu/libjemalloc.so.2\n",
			want: false,
		},
		{
			name: "cp creating libjemalloc.so counts",
			content: "FROM ruby:3.3-slim\n" +
				"RUN apt-get install -y libjemalloc2 \\\n" +
				"    && cp /usr/lib/x86_64-linux-gnu/libjemalloc.so.2 /usr/local/lib/libjemalloc.so\n",
			want: true,
		},
		{
			// Regression: cp where libjemalloc.so is the SOURCE, not the
			// target. The canonical file is not created here; the fix must
			// keep emitting the ln -sf step.
			name: "cp with libjemalloc.so as source not target",
			content: "FROM ruby:3.3-slim\n" +
				"RUN apt-get install -y libjemalloc2 \\\n" +
				"    && cp /opt/libjemalloc.so /tmp/backup.so\n",
			want: false,
		},
		{
			// Regression: mv from the canonical target REMOVES the file
			// rather than creating it. Counting this would leave LD_PRELOAD
			// pointing at a missing file.
			name: "mv from canonical target removes the file",
			content: "FROM ruby:3.3-slim\n" +
				"RUN apt-get install -y libjemalloc2 \\\n" +
				"    && mv /usr/local/lib/libjemalloc.so /tmp/old.so\n",
			want: false,
		},
		{
			// Symlink TARGET written with redundant `..` segments still
			// resolves to the canonical path under path.Clean.
			name: "ln target with .. segments resolves to canonical",
			content: "FROM ruby:3.3-slim\n" +
				"RUN apt-get install -y libjemalloc2 \\\n" +
				"    && ln -s /usr/lib/x86_64-linux-gnu/libjemalloc.so.2 /usr/local/lib/../lib/libjemalloc.so\n",
			want: true,
		},
		{
			// `ln -s SRC` form (no explicit target) creates the symlink in
			// the current directory with the basename of SRC. Without a
			// canonical target, we conservatively don't suppress the fix.
			name: "ln without explicit target does not count",
			content: "FROM ruby:3.3-slim\n" +
				"RUN apt-get install -y libjemalloc2 \\\n" +
				"    && ln -s /usr/lib/x86_64-linux-gnu/libjemalloc.so.2\n",
			want: false,
		},
		{
			// Regression: `install -d /path` creates a directory at /path,
			// not the shared object. LD_PRELOAD pointing at a directory
			// does not load jemalloc.
			name: "install -d at canonical path does not count",
			content: "FROM ruby:3.3-slim\n" +
				"RUN apt-get install -y libjemalloc2 \\\n" +
				"    && install -d /usr/local/lib/libjemalloc.so\n",
			want: false,
		},
		{
			// Long-form --directory must also disqualify the install
			// command from counting as "symlink already created".
			name: "install --directory at canonical path does not count",
			content: "FROM ruby:3.3-slim\n" +
				"RUN apt-get install -y libjemalloc2 \\\n" +
				"    && install --directory /usr/local/lib/libjemalloc.so\n",
			want: false,
		},
		{
			// `install` without -d copies the source file to the target,
			// so it does materialize the canonical libjemalloc.so file.
			name: "install copy mode at canonical target counts",
			content: "FROM ruby:3.3-slim\n" +
				"RUN apt-get install -y libjemalloc2 \\\n" +
				"    && install -m 644 /usr/lib/x86_64-linux-gnu/libjemalloc.so.2 /usr/local/lib/libjemalloc.so\n",
			want: true,
		},
		{
			// Regression: a later `rm` removes the canonical file. The
			// symlink is not present at the end of the stage, so the
			// fix must still emit the ln -sf step.
			name: "later rm of canonical path undoes earlier ln",
			content: "FROM ruby:3.3-slim\n" +
				"RUN apt-get install -y libjemalloc2 \\\n" +
				"    && ln -s /usr/lib/x86_64-linux-gnu/libjemalloc.so.2 /usr/local/lib/libjemalloc.so\n" +
				"RUN rm /usr/local/lib/libjemalloc.so\n",
			want: false,
		},
		{
			// Regression: rm -f same path also undoes the symlink.
			name: "later rm -f of canonical path undoes earlier ln",
			content: "FROM ruby:3.3-slim\n" +
				"RUN apt-get install -y libjemalloc2 \\\n" +
				"    && ln -s /usr/lib/x86_64-linux-gnu/libjemalloc.so.2 /usr/local/lib/libjemalloc.so \\\n" +
				"    && rm -f /usr/local/lib/libjemalloc.so\n",
			want: false,
		},
		{
			// Regression: mv away from canonical path is also a removal.
			name: "later mv away from canonical path undoes earlier ln",
			content: "FROM ruby:3.3-slim\n" +
				"RUN apt-get install -y libjemalloc2 \\\n" +
				"    && ln -s /usr/lib/x86_64-linux-gnu/libjemalloc.so.2 /usr/local/lib/libjemalloc.so\n" +
				"RUN mv /usr/local/lib/libjemalloc.so /tmp/old.so\n",
			want: false,
		},
		{
			// Regression: GNU `mv -t DIR SRC...` reverses arg order.
			// SRC is /usr/local/lib/libjemalloc.so → still removed.
			name: "later mv -t away from canonical path undoes earlier ln",
			content: "FROM ruby:3.3-slim\n" +
				"RUN apt-get install -y libjemalloc2 \\\n" +
				"    && ln -s /usr/lib/x86_64-linux-gnu/libjemalloc.so.2 /usr/local/lib/libjemalloc.so\n" +
				"RUN mv -t /tmp /usr/local/lib/libjemalloc.so\n",
			want: false,
		},
		{
			// Same as above but using long form `--target-directory`.
			name: "mv --target-directory removes canonical source",
			content: "FROM ruby:3.3-slim\n" +
				"RUN apt-get install -y libjemalloc2 \\\n" +
				"    && ln -s /usr/lib/x86_64-linux-gnu/libjemalloc.so.2 /usr/local/lib/libjemalloc.so\n" +
				"RUN mv --target-directory /tmp /usr/local/lib/libjemalloc.so\n",
			want: false,
		},
		{
			// Combined `-t=DIR` form keeps the value in the same token.
			name: "mv -t=DIR removes canonical source",
			content: "FROM ruby:3.3-slim\n" +
				"RUN apt-get install -y libjemalloc2 \\\n" +
				"    && ln -s /usr/lib/x86_64-linux-gnu/libjemalloc.so.2 /usr/local/lib/libjemalloc.so\n" +
				"RUN mv -t=/tmp /usr/local/lib/libjemalloc.so\n",
			want: false,
		},
		{
			// GNU short flag with directly-attached value: `mv -tDIR SRC`.
			name: "mv -tDIR (attached value) removes canonical source",
			content: "FROM ruby:3.3-slim\n" +
				"RUN apt-get install -y libjemalloc2 \\\n" +
				"    && ln -s /usr/lib/x86_64-linux-gnu/libjemalloc.so.2 /usr/local/lib/libjemalloc.so\n" +
				"RUN mv -t/tmp /usr/local/lib/libjemalloc.so\n",
			want: false,
		},
		{
			// Regression: unlink also removes.
			name: "unlink of canonical path undoes earlier ln",
			content: "FROM ruby:3.3-slim\n" +
				"RUN apt-get install -y libjemalloc2 \\\n" +
				"    && ln -s /usr/lib/x86_64-linux-gnu/libjemalloc.so.2 /usr/local/lib/libjemalloc.so \\\n" +
				"    && unlink /usr/local/lib/libjemalloc.so\n",
			want: false,
		},
		{
			// Order matters: a `rm` followed by a re-creation leaves the
			// file present at the end of the stage.
			name: "rm followed by recreate is present",
			content: "FROM ruby:3.3-slim\n" +
				"RUN apt-get install -y libjemalloc2 \\\n" +
				"    && rm -f /usr/local/lib/libjemalloc.so \\\n" +
				"    && ln -s /usr/lib/x86_64-linux-gnu/libjemalloc.so.2 /usr/local/lib/libjemalloc.so\n",
			want: true,
		},
		{
			// Regression: a later `cp /tmp/libfoo.so` to the canonical
			// target overwrites the earlier valid symlink with an
			// unrelated .so. The runtime LD_PRELOAD would load the wrong
			// library, so present must be cleared.
			name: "later cp of unrelated .so to canonical overwrites valid symlink",
			content: "FROM ruby:3.3-slim\n" +
				"RUN apt-get install -y libjemalloc2 \\\n" +
				"    && ln -s /usr/lib/x86_64-linux-gnu/libjemalloc.so.2 /usr/local/lib/libjemalloc.so\n" +
				"RUN cp /tmp/libfoo.so /usr/local/lib/libjemalloc.so\n",
			want: false,
		},
		{
			// Same idea with mv: `mv /tmp/libfoo.so /usr/local/lib/libjemalloc.so`
			// has the canonical path as DESTINATION (not source), so it's
			// an overwrite, not a removal — but the source is non-jemalloc.
			name: "later mv of unrelated .so to canonical overwrites valid symlink",
			content: "FROM ruby:3.3-slim\n" +
				"RUN apt-get install -y libjemalloc2 \\\n" +
				"    && ln -s /usr/lib/x86_64-linux-gnu/libjemalloc.so.2 /usr/local/lib/libjemalloc.so\n" +
				"RUN mv /tmp/libfoo.so /usr/local/lib/libjemalloc.so\n",
			want: false,
		},
		{
			// Sanity: an overwrite with a jemalloc source still keeps
			// present=true (it's effectively a re-create).
			name: "later cp of jemalloc.so.2 to canonical keeps present",
			content: "FROM ruby:3.3-slim\n" +
				"RUN apt-get install -y libjemalloc2 \\\n" +
				"    && ln -s /usr/lib/x86_64-linux-gnu/libjemalloc.so.2 /usr/local/lib/libjemalloc.so\n" +
				"RUN cp /usr/lib/x86_64-linux-gnu/libjemalloc.so.2 /usr/local/lib/libjemalloc.so\n",
			want: true,
		},
		{
			// Regression: a directory NAMED libjemalloc-backups must NOT
			// fool the matcher when the basename of the source is some
			// unrelated `.so`. Substring matching the full path used to
			// accept this.
			name: "libjemalloc in dirname but unrelated .so basename does not count",
			content: "FROM ruby:3.3-slim\n" +
				"RUN apt-get install -y libjemalloc2 \\\n" +
				"    && cp /opt/libjemalloc-backups/libfoo.so /usr/local/lib/libjemalloc.so\n",
			want: false,
		},
		{
			// `libjemalloc.so.2-backup` is a lookalike, not jemalloc.
			name: "lookalike libjemalloc.so.2-backup basename does not count",
			content: "FROM ruby:3.3-slim\n" +
				"RUN apt-get install -y libjemalloc2 \\\n" +
				"    && cp /tmp/libjemalloc.so.2-backup /usr/local/lib/libjemalloc.so\n",
			want: false,
		},
		{
			// Non-canonical paths to genuine jemalloc shared objects still
			// count via basename matching.
			name: "ln from non-standard path with jemalloc basename counts",
			content: "FROM ruby:3.3-slim\n" +
				"RUN apt-get install -y libjemalloc2 \\\n" +
				"    && ln -s /opt/jemalloc/libjemalloc.so.2 /usr/local/lib/libjemalloc.so\n",
			want: true,
		},
		{
			// Regression: target is canonical but the source is some
			// unrelated .so. Counting this would have LD_PRELOAD load a
			// non-jemalloc library.
			name: "cp of unrelated .so to canonical path does not count",
			content: "FROM ruby:3.3-slim\n" +
				"RUN apt-get install -y libjemalloc2 \\\n" +
				"    && cp /tmp/libfoo.so /usr/local/lib/libjemalloc.so\n",
			want: false,
		},
		{
			// Regression: ln -s pointing at an unrelated source. Same risk.
			name: "ln -s of unrelated .so to canonical path does not count",
			content: "FROM ruby:3.3-slim\n" +
				"RUN apt-get install -y libjemalloc2 \\\n" +
				"    && ln -s /tmp/libfoo.so /usr/local/lib/libjemalloc.so\n",
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			input := testutil.MakeLintInput(t, "Dockerfile", tt.content)
			sf := input.Facts.Stage(input.FinalStageIndex())
			if got := stageReferencesJemallocSymlink(sf); got != tt.want {
				t.Errorf("stageReferencesJemallocSymlink = %v, want %v", got, tt.want)
			}
		})
	}
	t.Run("nil stage facts", func(t *testing.T) {
		t.Parallel()
		if got := stageReferencesJemallocSymlink(nil); got {
			t.Errorf("stageReferencesJemallocSymlink(nil) = true, want false")
		}
	})
}

// Regression: a later `ENV LD_PRELOAD=""` (or any non-jemalloc value) after
// the install RUN would clobber a fix inserted on the install line. The
// inserted fix must land *after* the latest ENV write to LD_PRELOAD or
// MALLOC_CONF so the runtime env actually carries our value.
func TestJemallocInstalledButNotPreloadedRule_FixInsertsAfterLaterEnvClobber(t *testing.T) {
	t.Parallel()

	content := "FROM ruby:3.3-slim\n" + // L1
		"RUN apt-get install -y libjemalloc2\n" + // L2
		"ENV SOMETHING_ELSE=ok\n" + // L3
		"ENV LD_PRELOAD=\"\"\n" + // L4 — this would clobber a fix inserted at L3
		"CMD [\"bin/rails\", \"server\"]\n" // L5
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	violations := NewJemallocInstalledButNotPreloadedRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}
	fix := violations[0].SuggestedFix
	if fix == nil {
		t.Fatal("expected SuggestedFix")
	}
	if len(fix.Edits) != 1 {
		t.Fatalf("expected 1 edit, got %d", len(fix.Edits))
	}
	// Insert anchor must be line 5 (after the clobbering ENV at L4),
	// NOT line 3 (which would land before the clobber).
	if got := fix.Edits[0].Location.Start.Line; got != 5 {
		t.Errorf("insert anchor line = %d, want 5 (after the later ENV LD_PRELOAD clobber)", got)
	}
}

// Regression: a later `RUN rm /usr/local/lib/libjemalloc.so` would delete
// the symlink we just inserted. The fix anchor must move past that
// removal so the recreated `ln -sf` survives.
func TestJemallocInstalledButNotPreloadedRule_FixInsertsAfterLaterSymlinkRemoval(t *testing.T) {
	t.Parallel()

	content := "FROM ruby:3.3-slim\n" + // L1
		"RUN apt-get install -y libjemalloc2\n" + // L2
		"RUN rm /usr/local/lib/libjemalloc.so\n" + // L3 — would delete our symlink
		"CMD [\"bin/rails\", \"server\"]\n" // L4
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	violations := NewJemallocInstalledButNotPreloadedRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}
	fix := violations[0].SuggestedFix
	if fix == nil {
		t.Fatal("expected SuggestedFix")
	}
	if got := fix.Edits[0].Location.Start.Line; got != 4 {
		t.Errorf("insert anchor line = %d, want 4 (after the rm at L3)", got)
	}
	// Sanity: this branch must emit the symlink-creation step.
	if !strings.Contains(fix.Edits[0].NewText, "ln -sf") {
		t.Errorf("expected ln -sf in fix text, got %q", fix.Edits[0].NewText)
	}
}

// Regression: a later `cp /tmp/libfoo.so /usr/local/lib/libjemalloc.so`
// overwrites the canonical path with an unrelated .so. The fix must
// land *after* that overwrite so the recreated symlink survives.
func TestJemallocInstalledButNotPreloadedRule_FixInsertsAfterLaterCpOverwrite(t *testing.T) {
	t.Parallel()

	content := "FROM ruby:3.3-slim\n" + // L1
		"RUN apt-get install -y libjemalloc2\n" + // L2
		"RUN cp /tmp/libfoo.so /usr/local/lib/libjemalloc.so\n" + // L3 — overwrites
		"CMD [\"bin/rails\", \"server\"]\n" // L4
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	violations := NewJemallocInstalledButNotPreloadedRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}
	fix := violations[0].SuggestedFix
	if fix == nil {
		t.Fatal("expected SuggestedFix")
	}
	if got := fix.Edits[0].Location.Start.Line; got != 4 {
		t.Errorf("insert anchor line = %d, want 4 (after the cp overwrite at L3)", got)
	}
	if !strings.Contains(fix.Edits[0].NewText, "ln -sf") {
		t.Errorf("expected ln -sf in fix text after invalidation, got %q", fix.Edits[0].NewText)
	}
}

// A jemalloc-sourced overwrite is fine — canonical path stays valid, so
// the fix anchor does NOT need to be pushed past it.
func TestJemallocInstalledButNotPreloadedRule_FixDoesNotPushPastJemallocOverwrite(t *testing.T) {
	t.Parallel()

	content := "FROM ruby:3.3-slim\n" + // L1
		"RUN apt-get install -y libjemalloc2\n" + // L2
		"RUN cp /usr/lib/x86_64-linux-gnu/libjemalloc.so.2 /usr/local/lib/libjemalloc.so\n" + // L3 — valid recreate
		"CMD [\"bin/rails\", \"server\"]\n" // L4
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	violations := NewJemallocInstalledButNotPreloadedRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}
	// The L3 RUN counts as a creation (jemalloc-sourced), so present is
	// true at the end → fix is the ENV-only branch and lands at L3 (right
	// after the install RUN at L2). It must NOT be pushed to L4 by the
	// invalidating-run logic.
	if got := violations[0].SuggestedFix.Edits[0].Location.Start.Line; got != 3 {
		t.Errorf("insert anchor line = %d, want 3 (jemalloc overwrite is not invalidating)", got)
	}
}

func TestJemallocInstalledButNotPreloadedRule_FixInsertsAfterLaterMvAway(t *testing.T) {
	t.Parallel()

	content := "FROM ruby:3.3-slim\n" + // L1
		"RUN apt-get install -y libjemalloc2\n" + // L2
		"RUN mv /usr/local/lib/libjemalloc.so /tmp/old.so\n" + // L3 — undoes our symlink
		"CMD [\"bin/rails\", \"server\"]\n" // L4
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	violations := NewJemallocInstalledButNotPreloadedRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}
	fix := violations[0].SuggestedFix
	if fix == nil {
		t.Fatal("expected SuggestedFix")
	}
	if got := fix.Edits[0].Location.Start.Line; got != 4 {
		t.Errorf("insert anchor line = %d, want 4 (after the mv at L3)", got)
	}
}

func TestJemallocInstalledButNotPreloadedRule_FixSkipsClobberPushWhenEnvIsBeforeInstall(t *testing.T) {
	t.Parallel()

	// `ENV LD_PRELOAD=""` BEFORE the install — no need to push past it,
	// the fix's own ENV (placed after install) will be the last writer.
	content := "FROM ruby:3.3-slim\n" + // L1
		"ENV LD_PRELOAD=\"\"\n" + // L2 — pre-install, irrelevant
		"RUN apt-get install -y libjemalloc2\n" + // L3
		"CMD [\"bin/rails\", \"server\"]\n" // L4
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	violations := NewJemallocInstalledButNotPreloadedRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}
	fix := violations[0].SuggestedFix
	if fix == nil {
		t.Fatal("expected SuggestedFix")
	}
	if got := fix.Edits[0].Location.Start.Line; got != 4 {
		t.Errorf("insert anchor line = %d, want 4 (right after install RUN; pre-install ENV is irrelevant)", got)
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

// Regression: when the stage already has a non-jemalloc LD_PRELOAD set via
// ENV, the fix must preserve that value by prepending the jemalloc path —
// not overwrite it. Otherwise --fix-unsafe silently drops a deliberate
// preload chain (instrumentation, sanitizers, etc.).
func TestJemallocInstalledButNotPreloadedRule_FixPreservesExistingLDPreload(t *testing.T) {
	t.Parallel()

	content := "FROM ruby:3.3-slim\n" + // L1
		"RUN apt-get install -y libjemalloc2\n" + // L2
		"ENV LD_PRELOAD=/opt/instrumentation/libtrace.so\n" + // L3 — non-jemalloc
		"CMD [\"bin/rails\", \"server\"]\n" // L4
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	violations := NewJemallocInstalledButNotPreloadedRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}
	fix := violations[0].SuggestedFix
	if fix == nil {
		t.Fatal("expected SuggestedFix")
	}
	got := fix.Edits[0].NewText
	want := `ENV LD_PRELOAD="/usr/local/lib/libjemalloc.so /opt/instrumentation/libtrace.so"`
	if !strings.Contains(got, want) {
		t.Errorf("fix must preserve existing LD_PRELOAD by prepending jemalloc: got %q, want substring %q", got, want)
	}
}

func TestJemallocInstalledButNotPreloadedRule_FixIgnoresUnquotableLDPreload(t *testing.T) {
	t.Parallel()

	// Awkward existing value (embedded `"`) — fall back to the canonical
	// path alone so we don't emit a malformed ENV literal.
	content := "FROM ruby:3.3-slim\n" +
		"RUN apt-get install -y libjemalloc2\n" +
		"ENV LD_PRELOAD=/opt/with\"quote.so\n"
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	violations := NewJemallocInstalledButNotPreloadedRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}
	fix := violations[0].SuggestedFix
	if fix == nil {
		t.Fatal("expected SuggestedFix")
	}
	got := fix.Edits[0].NewText
	if !strings.Contains(got, `ENV LD_PRELOAD="/usr/local/lib/libjemalloc.so"`) {
		t.Errorf("fallback should emit canonical-only ENV: %q", got)
	}
	if strings.Contains(got, "with\"quote") {
		t.Errorf("fix must not splice an awkward-to-quote value into the ENV literal: %q", got)
	}
}

// Sanity: no existing LD_PRELOAD ENV — emit just the canonical path.
func TestJemallocInstalledButNotPreloadedRule_FixNoExistingLDPreloadEmitsCanonical(t *testing.T) {
	t.Parallel()

	content := "FROM ruby:3.3-slim\nRUN apt-get install -y libjemalloc2\n"
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	violations := NewJemallocInstalledButNotPreloadedRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}
	got := violations[0].SuggestedFix.Edits[0].NewText
	want := `ENV LD_PRELOAD="/usr/local/lib/libjemalloc.so"`
	if !strings.Contains(got, want) {
		t.Errorf("expected canonical-only ENV: got %q want substring %q", got, want)
	}
	if strings.Contains(got, "/usr/local/lib/libjemalloc.so /") {
		t.Errorf("no existing LD_PRELOAD should not produce a multi-path value: %q", got)
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

// Regression: libjemalloc1 ships libjemalloc.so.1, not .so.2, so the
// canonical fix would link to a non-existent file. The rule still fires,
// but the auto-fix must be omitted for that legacy package.
func TestJemallocInstalledButNotPreloadedRule_NoFixForLibjemalloc1(t *testing.T) {
	t.Parallel()

	content := "FROM ruby:3.3-slim\nRUN apt-get install -y libjemalloc1\n"
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	violations := NewJemallocInstalledButNotPreloadedRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}
	if violations[0].SuggestedFix != nil {
		t.Errorf("expected no SuggestedFix for libjemalloc1 (.so.2 target does not exist), got fix with NewText=%q",
			violations[0].SuggestedFix.Edits[0].NewText)
	}
}

func TestJemallocInstalledButNotPreloadedRule_FixForLibjemallocDev(t *testing.T) {
	t.Parallel()

	content := "FROM ruby:3.3-slim\nRUN apt-get install -y libjemalloc-dev\n"
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	violations := NewJemallocInstalledButNotPreloadedRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}
	if violations[0].SuggestedFix == nil {
		t.Fatal("expected SuggestedFix for libjemalloc-dev (depends on libjemalloc2)")
	}
}

func TestInstallCommandProvidesLibjemalloc2(t *testing.T) {
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
		{name: "apt-get libjemalloc2", ic: makeIC("apt-get", "libjemalloc2"), want: true},
		{name: "apt-get libjemalloc-dev", ic: makeIC("apt-get", "libjemalloc-dev"), want: true},
		{name: "apt-get libjemalloc2 versioned", ic: makeIC("apt-get", "libjemalloc2=5.3.0-1"), want: true},
		{name: "apt-get mixed with libjemalloc2", ic: makeIC("apt-get", "curl", "libjemalloc2"), want: true},
		{name: "apt-get libjemalloc1 only", ic: makeIC("apt-get", "libjemalloc1"), want: false},
		{name: "apt-get other packages only", ic: makeIC("apt-get", "curl"), want: false},
		{name: "apk jemalloc not apt family", ic: makeIC("apk", "jemalloc"), want: false},
		{name: "dnf jemalloc not apt family", ic: makeIC("dnf", "jemalloc"), want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := installCommandProvidesLibjemalloc2(tt.ic); got != tt.want {
				t.Errorf("installCommandProvidesLibjemalloc2(%v) = %v, want %v", tt.ic.Packages, got, tt.want)
			}
		})
	}
}

func TestJemallocInstalledButNotPreloadedRule_ViolationLocation(t *testing.T) {
	t.Parallel()

	const runLine = "RUN apt-get install -y libjemalloc2"
	content := "FROM ruby:3.3-slim\n" + runLine + "\n"
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	violations := NewJemallocInstalledButNotPreloadedRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}
	loc := violations[0].Location
	if loc.Start.Line != 2 || loc.End.Line != 2 {
		t.Errorf("violation line range = %d-%d, want 2-2", loc.Start.Line, loc.End.Line)
	}
	// PackageArg columns on RUN line 0 are shell-relative; the rule must
	// translate them to Dockerfile coordinates so the range covers exactly
	// "libjemalloc2" in the Dockerfile source line.
	wantStart := strings.Index(runLine, "libjemalloc2")
	wantEnd := wantStart + len("libjemalloc2")
	if loc.Start.Column != wantStart || loc.End.Column != wantEnd {
		t.Errorf("violation columns = %d-%d, want %d-%d (covering %q)",
			loc.Start.Column, loc.End.Column, wantStart, wantEnd, "libjemalloc2")
	}
}

// Regression: when the package token sits on a continuation line (pkg.Line > 0),
// the columns are already Dockerfile-relative and must NOT be offset by the
// RUN-prefix translation. This guards against accidentally re-applying the
// shell→dockerfile offset in the wrong branch.
func TestJemallocInstalledButNotPreloadedRule_ViolationLocationOnContinuation(t *testing.T) {
	t.Parallel()

	content := "FROM ruby:3.3-slim\n" +
		"RUN apt-get install -y \\\n" +
		"    libjemalloc2\n"
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	violations := NewJemallocInstalledButNotPreloadedRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}
	loc := violations[0].Location
	if loc.Start.Line != 3 {
		t.Errorf("violation line = %d, want 3 (continuation line)", loc.Start.Line)
	}
	wantStart := 4 // 4 spaces before libjemalloc2
	wantEnd := wantStart + len("libjemalloc2")
	if loc.Start.Column != wantStart || loc.End.Column != wantEnd {
		t.Errorf("violation columns = %d-%d, want %d-%d",
			loc.Start.Column, loc.End.Column, wantStart, wantEnd)
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
		{name: "jemalloc-devel", want: true},
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

func TestJemallocViolationDetail_AptLibjemalloc2(t *testing.T) {
	t.Parallel()

	ic := shell.InstallCommand{
		Manager:  "apt-get",
		Packages: []shell.PackageArg{{Normalized: "libjemalloc2"}},
	}
	got := jemallocViolationDetail(ic)
	if !strings.Contains(got, "ln -sf /usr/lib/$(uname -m)-linux-gnu/libjemalloc.so.2 /usr/local/lib/libjemalloc.so") {
		t.Errorf("apt+libjemalloc2 detail missing canonical ln -sf: %q", got)
	}
}

// Regression: libjemalloc1 ships libjemalloc.so.1, not .so.2. The detail
// must NOT propose the hardcoded .so.2 symlink — that would steer users
// toward a dangling link.
func TestJemallocViolationDetail_AptLibjemalloc1AvoidsSo2Suggestion(t *testing.T) {
	t.Parallel()

	ic := shell.InstallCommand{
		Manager:  "apt-get",
		Packages: []shell.PackageArg{{Normalized: "libjemalloc1"}},
	}
	got := jemallocViolationDetail(ic)
	if strings.Contains(got, "libjemalloc.so.2") {
		t.Errorf("libjemalloc1 detail must not mention .so.2 — that would propose a dangling symlink: %q", got)
	}
	if !strings.Contains(got, "migrate to libjemalloc2") {
		t.Errorf("libjemalloc1 detail should recommend migrating to libjemalloc2: %q", got)
	}
}

func TestJemallocViolationDetail_NonAptManager(t *testing.T) {
	t.Parallel()

	ic := shell.InstallCommand{
		Manager:  "apk",
		Packages: []shell.PackageArg{{Normalized: "jemalloc"}},
	}
	got := jemallocViolationDetail(ic)
	if strings.Contains(got, "ln -sf /usr/lib/$(uname -m)") {
		t.Errorf("non-apt detail should not suggest a debian-multiarch ln -sf command: %q", got)
	}
}
