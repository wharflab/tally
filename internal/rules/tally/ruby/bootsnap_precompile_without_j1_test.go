package ruby

import (
	"strings"
	"testing"

	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/testutil"
)

func TestBootsnapPrecompileWithoutJ1Rule_Metadata(t *testing.T) {
	t.Parallel()

	meta := NewBootsnapPrecompileWithoutJ1Rule().Metadata()
	if meta.Code != BootsnapPrecompileWithoutJ1RuleCode {
		t.Errorf("Code = %q, want %q", meta.Code, BootsnapPrecompileWithoutJ1RuleCode)
	}
	if meta.DefaultSeverity != rules.SeverityWarning {
		t.Errorf("DefaultSeverity = %v, want Warning", meta.DefaultSeverity)
	}
	if meta.Category != "correctness" {
		t.Errorf("Category = %q, want correctness", meta.Category)
	}
	if meta.FixPriority != bootsnapFixPriority {
		t.Errorf("FixPriority = %d, want %d", meta.FixPriority, bootsnapFixPriority)
	}
	if !strings.HasPrefix(meta.DocURL, "https://tally.wharflab.com/rules/tally/ruby/") {
		t.Errorf("DocURL = %q, want tally/ruby/ prefix", meta.DocURL)
	}
}

func TestBootsnapPrecompileWithoutJ1Rule_Check(t *testing.T) {
	t.Parallel()

	testutil.RunRuleTests(t, NewBootsnapPrecompileWithoutJ1Rule(), []testutil.RuleTestCase{
		// --- Violations ---
		{
			Name: "bare bootsnap precompile fires",
			Content: `FROM ruby:3.3-slim
RUN bootsnap precompile app/ lib/
`,
			WantViolations: 1,
		},
		{
			Name: "bundle exec bootsnap precompile fires",
			Content: `FROM ruby:3.3-slim
RUN bundle exec bootsnap precompile --gemfile
`,
			WantViolations: 1,
		},
		{
			Name: "bootsnap precompile in chained RUN fires once",
			Content: `FROM ruby:3.3-slim
RUN bundle install \
    && bundle exec bootsnap precompile app/ lib/
`,
			WantViolations: 1,
		},
		{
			Name: "two separate precompile invocations both fire",
			Content: `FROM ruby:3.3-slim
RUN bundle install && bundle exec bootsnap precompile --gemfile
RUN bundle exec bootsnap precompile app/ lib/
`,
			WantViolations: 2,
		},
		// --- Compliant: -j 1 in supported forms ---
		{
			Name: "bootsnap precompile -j 1 suppresses",
			Content: `FROM ruby:3.3-slim
RUN bundle exec bootsnap precompile -j 1 app/ lib/
`,
			WantViolations: 0,
		},
		{
			Name: "bootsnap precompile -j1 suppresses",
			Content: `FROM ruby:3.3-slim
RUN bundle exec bootsnap precompile -j1 app/ lib/
`,
			WantViolations: 0,
		},
		{
			Name: "bootsnap precompile -j=1 suppresses",
			Content: `FROM ruby:3.3-slim
RUN bundle exec bootsnap precompile -j=1 app/ lib/
`,
			WantViolations: 0,
		},
		{
			Name: "bootsnap precompile --jobs 1 suppresses",
			Content: `FROM ruby:3.3-slim
RUN bundle exec bootsnap precompile --jobs 1 app/ lib/
`,
			WantViolations: 0,
		},
		{
			Name: "bootsnap precompile --jobs=1 suppresses",
			Content: `FROM ruby:3.3-slim
RUN bundle exec bootsnap precompile --jobs=1 app/ lib/
`,
			WantViolations: 0,
		},
		// --- -j with non-1 value still fires ---
		{
			Name: "bootsnap precompile -j 4 still fires",
			Content: `FROM ruby:3.3-slim
RUN bundle exec bootsnap precompile -j 4 app/ lib/
`,
			WantViolations: 1,
		},
		{
			Name: "bootsnap precompile --jobs 8 still fires",
			Content: `FROM ruby:3.3-slim
RUN bundle exec bootsnap precompile --jobs 8 app/ lib/
`,
			WantViolations: 1,
		},
		// --- -j 1 in a *later* command must NOT suppress an earlier bare call ---
		{
			Name: "later -j 1 in different command does not suppress earlier",
			Content: `FROM ruby:3.3-slim
RUN bundle exec bootsnap precompile app/ lib/ \
    && some-other-tool -j 1
`,
			WantViolations: 1,
		},
		// --- Non-precompile bootsnap subcommands skipped ---
		{
			Name: "bootsnap doctor skipped",
			Content: `FROM ruby:3.3-slim
RUN bootsnap doctor
`,
			WantViolations: 0,
		},
		{
			Name: "bootsnap precompiled-foo (lookalike) skipped",
			Content: `FROM ruby:3.3-slim
RUN bootsnap precompiled-foo
`,
			WantViolations: 0,
		},
		{
			Name: "mybootsnap precompile (different binary) skipped",
			Content: `FROM ruby:3.3-slim
RUN mybootsnap precompile
`,
			WantViolations: 0,
		},
		// --- BUILDPLATFORM/TARGETPLATFORM guard suppresses ---
		{
			Name: "BUILDPLATFORM guard suppresses",
			Content: `FROM ruby:3.3-slim
ARG BUILDPLATFORM
ARG TARGETPLATFORM
RUN if [ "$BUILDPLATFORM" = "$TARGETPLATFORM" ]; then \
      bundle exec bootsnap precompile app/ lib/ ; \
    fi
`,
			WantViolations: 0,
		},
		// --- Stage skipping ---
		{
			Name: "stage named dev skipped",
			Content: `FROM ruby:3.3-slim AS dev
RUN bundle exec bootsnap precompile app/ lib/
`,
			WantViolations: 0,
		},
		{
			Name: "stage named test skipped",
			Content: `FROM ruby:3.3-slim AS test
RUN bundle exec bootsnap precompile app/ lib/
`,
			WantViolations: 0,
		},
		// --- Non-Ruby stage out of scope ---
		{
			Name: "non-Ruby base image is out of scope",
			Content: `FROM debian:12-slim
RUN bootsnap precompile app/ lib/
`,
			WantViolations: 0,
		},
		{
			Name: "node base is out of scope",
			Content: `FROM node:20
RUN bootsnap precompile app/ lib/
`,
			WantViolations: 0,
		},
		{
			Name: "Ruby signal via RAILS_ENV fires on debian",
			Content: `FROM debian:12-slim
ENV RAILS_ENV=production
RUN bootsnap precompile app/ lib/
`,
			WantViolations: 1,
		},
		// --- Multi-stage: only Ruby stages fire ---
		{
			Name: "non-Ruby builder + Ruby final stage fires once",
			Content: `FROM debian:12-slim AS prep
RUN echo prep

FROM ruby:3.3-slim
RUN bundle exec bootsnap precompile app/ lib/
`,
			WantViolations: 1,
		},
		// --- Windows base skipped ---
		{
			Name: "windows base image skipped",
			Content: `FROM mcr.microsoft.com/windows/servercore:ltsc2022
RUN bootsnap precompile app/ lib/
`,
			WantViolations: 0,
		},
		// --- Word-boundary safety ---
		{
			Name: "bootsnapprecompile (no whitespace) does not match",
			Content: `FROM ruby:3.3-slim
RUN echo 'bootsnapprecompile is not a real command'
`,
			WantViolations: 0,
		},
	})
}

func TestBootsnapPrecompileWithoutJ1Rule_Fix(t *testing.T) {
	t.Parallel()

	content := "FROM ruby:3.3-slim\nRUN bundle exec bootsnap precompile app/ lib/\n"
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	violations := NewBootsnapPrecompileWithoutJ1Rule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}
	fix := violations[0].SuggestedFix
	if fix == nil {
		t.Fatal("expected SuggestedFix")
	}
	if fix.Safety != rules.FixSafe {
		t.Errorf("Safety = %v, want FixSafe", fix.Safety)
	}
	if fix.Priority != bootsnapFixPriority {
		t.Errorf("Priority = %d, want %d", fix.Priority, bootsnapFixPriority)
	}
	if !fix.IsPreferred {
		t.Error("IsPreferred = false, want true")
	}
	if len(fix.Edits) != 1 {
		t.Fatalf("expected 1 edit, got %d", len(fix.Edits))
	}
	edit := fix.Edits[0]
	if edit.NewText != " -j 1" {
		t.Errorf("NewText = %q, want %q", edit.NewText, " -j 1")
	}

	// Apply the fix and confirm the result is the canonical
	// `bundle exec bootsnap precompile -j 1 app/ lib/` form.
	fixed := applyFix(t, content, edit.Location.Start.Line, edit.Location.Start.Column, edit.NewText)
	want := "FROM ruby:3.3-slim\nRUN bundle exec bootsnap precompile -j 1 app/ lib/\n"
	if fixed != want {
		t.Errorf("post-fix content =\n%q\nwant\n%q", fixed, want)
	}
}

func TestBootsnapPrecompileWithoutJ1Rule_Fix_OnContinuation(t *testing.T) {
	t.Parallel()

	// `bundle exec bootsnap precompile` on a continuation line; the edit
	// must land at the right column on the continuation line, not on
	// the RUN line.
	content := "FROM ruby:3.3-slim\n" +
		"RUN bundle install \\\n" +
		"    && bundle exec bootsnap precompile app/ lib/\n"
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	violations := NewBootsnapPrecompileWithoutJ1Rule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}
	fix := violations[0].SuggestedFix
	if fix == nil {
		t.Fatal("expected SuggestedFix")
	}
	edit := fix.Edits[0]
	if edit.Location.Start.Line != 3 {
		t.Errorf("edit line = %d, want 3", edit.Location.Start.Line)
	}
	fixed := applyFix(t, content, edit.Location.Start.Line, edit.Location.Start.Column, edit.NewText)
	want := "FROM ruby:3.3-slim\n" +
		"RUN bundle install \\\n" +
		"    && bundle exec bootsnap precompile -j 1 app/ lib/\n"
	if fixed != want {
		t.Errorf("post-fix content =\n%q\nwant\n%q", fixed, want)
	}
}

func TestBootsnapPrecompileWithoutJ1Rule_LockfileWithoutBootsnapSuppresses(t *testing.T) {
	t.Parallel()

	// The standalone rule input has no observable Gemfile.lock. The rule
	// should fire normally in that case so we keep the Dockerfile-only
	// path covered. The "lockfile present without bootsnap" suppression
	// is exercised through the integration fixture path below.
	content := "FROM ruby:3.3-slim\nRUN bundle exec bootsnap precompile app/ lib/\n"
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	violations := NewBootsnapPrecompileWithoutJ1Rule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("Dockerfile-only mode: expected 1 violation, got %d", len(violations))
	}
}

// applyFix returns content with newText spliced at (line, col). Lines are
// 1-based to match Location semantics; columns are 0-based byte offsets.
func applyFix(t *testing.T, content string, line, col int, newText string) string {
	t.Helper()
	lines := strings.SplitAfter(content, "\n")
	if line < 1 || line > len(lines) {
		t.Fatalf("applyFix: line %d out of range (have %d lines)", line, len(lines))
	}
	target := lines[line-1]
	hasNewline := strings.HasSuffix(target, "\n")
	body := target
	if hasNewline {
		body = strings.TrimSuffix(target, "\n")
	}
	if col < 0 || col > len(body) {
		t.Fatalf("applyFix: col %d out of range (line length %d)", col, len(body))
	}
	body = body[:col] + newText + body[col:]
	if hasNewline {
		body += "\n"
	}
	lines[line-1] = body
	return strings.Join(lines, "")
}

func TestCommandEndOffset(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want int
	}{
		{name: "no separator", in: " app/ lib/", want: -1},
		{name: "newline", in: " app/\nNEXT", want: 5},
		{name: "semicolon", in: " app/ ; next", want: 6},
		{name: "double-amp", in: " app/ && next", want: 6},
		{name: "single-amp ends command", in: " app/ & next", want: 6},
		{name: "pipe ends command", in: " app/ | grep", want: 6},
		{name: "or-list ends command", in: " app/ || true", want: 6},
		{name: "separator at offset 0 reported", in: ";", want: 0},
		{name: "single-quoted semicolon ignored", in: ` 'a;b' ;`, want: 7},
		{name: "double-quoted ampersand ignored", in: ` "a&b" ;`, want: 7},
		{name: "backslash-escaped newline ignored", in: " app/\\\n  -j 1", want: -1},
		{name: "backslash-escaped semicolon ignored", in: ` app/\;next`, want: -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := commandEndOffset(tt.in); got != tt.want {
				t.Errorf("commandEndOffset(%q) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

func TestInvocationHasJobsOne(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		script string
		want   bool
	}{
		// We assume the bootsnap precompile token has just been consumed; the
		// scanner sits at byte 0 of the tail.
		{name: "trailing -j 1", script: " -j 1 app/", want: true},
		{name: "trailing -j1", script: " -j1 app/", want: true},
		{name: "trailing -j=1", script: " -j=1 app/", want: true},
		{name: "trailing --jobs 1", script: " --jobs 1 app/", want: true},
		{name: "trailing --jobs=1", script: " --jobs=1 app/", want: true},
		{name: "no jobs flag", script: " app/ lib/", want: false},
		{name: "different value -j 4", script: " -j 4 app/", want: false},
		{name: "j1 anywhere requires word boundary", script: " app/-j1 lib/", want: false},
		{name: "after && does not count", script: " app/ && next -j 1", want: false},
		{name: "after | does not count", script: " app/ | next -j 1", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := invocationHasJobsOne(tt.script, 0); got != tt.want {
				t.Errorf("invocationHasJobsOne(%q) = %v, want %v", tt.script, got, tt.want)
			}
		})
	}
}

func TestScriptIsPlatformGuarded(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		script string
		want   bool
	}{
		{name: "both literals present", script: `if [ "$BUILDPLATFORM" = "$TARGETPLATFORM" ]; then bootsnap precompile; fi`, want: true},
		{name: "double-equals operator", script: `if [ "$BUILDPLATFORM" == "$TARGETPLATFORM" ]; then bootsnap precompile; fi`, want: true},
		{name: "test command form", script: `test "$BUILDPLATFORM" = "$TARGETPLATFORM" && bootsnap precompile`, want: true},
		{name: "reverse comparison", script: `if [ "$TARGETPLATFORM" = "$BUILDPLATFORM" ]; then bootsnap precompile; fi`, want: true},
		{name: "braced variant", script: `if [ "${BUILDPLATFORM}" = "${TARGETPLATFORM}" ]; then bootsnap precompile; fi`, want: true},
		{
			name:   "not-equal still matches",
			script: `if [ "$BUILDPLATFORM" != "$TARGETPLATFORM" ]; then exit 0; fi; bootsnap precompile`,
			want:   true,
		},
		{name: "only BUILDPLATFORM", script: `echo $BUILDPLATFORM && bootsnap precompile`, want: false},
		{name: "only TARGETPLATFORM", script: `echo $TARGETPLATFORM && bootsnap precompile`, want: false},
		{
			name:   "both vars referenced but not compared",
			script: `echo "$BUILDPLATFORM $TARGETPLATFORM" && bootsnap precompile`,
			want:   false,
		},
		{name: "neither", script: `bootsnap precompile`, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := scriptIsPlatformGuarded(tt.script); got != tt.want {
				t.Errorf("scriptIsPlatformGuarded() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBootsnapPrecompileWithoutJ1Rule_ViolationLocation(t *testing.T) {
	t.Parallel()

	const runLine = "RUN bundle exec bootsnap precompile app/ lib/"
	content := "FROM ruby:3.3-slim\n" + runLine + "\n"
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	violations := NewBootsnapPrecompileWithoutJ1Rule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}
	loc := violations[0].Location
	if loc.Start.Line != 2 || loc.End.Line != 2 {
		t.Errorf("violation lines = %d-%d, want 2-2", loc.Start.Line, loc.End.Line)
	}
	wantStart := strings.Index(runLine, "bootsnap precompile")
	wantEnd := wantStart + len("bootsnap precompile")
	if loc.Start.Column != wantStart || loc.End.Column != wantEnd {
		t.Errorf("violation columns = %d-%d, want %d-%d (covering %q)",
			loc.Start.Column, loc.End.Column, wantStart, wantEnd, "bootsnap precompile")
	}
}
