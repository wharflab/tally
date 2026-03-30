package tally

import (
	"testing"

	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/testutil"
)

func TestPreferWgetConfigRule_Metadata(t *testing.T) {
	t.Parallel()
	r := NewPreferWgetConfigRule()
	meta := r.Metadata()
	if meta.Code != "tally/prefer-wget-config" {
		t.Errorf("Code = %q, want %q", meta.Code, "tally/prefer-wget-config")
	}
	if meta.DefaultSeverity != rules.SeverityInfo {
		t.Errorf("DefaultSeverity = %v, want Info", meta.DefaultSeverity)
	}
	if meta.Category != "reliability" {
		t.Errorf("Category = %q, want %q", meta.Category, "reliability")
	}
	if meta.FixPriority != 94 { //nolint:mnd // expected value
		t.Errorf("FixPriority = %d, want 94", meta.FixPriority)
	}
}

func TestPreferWgetConfigRule_DefaultConfig(t *testing.T) {
	t.Parallel()
	r := NewPreferWgetConfigRule()
	cfg, ok := r.DefaultConfig().(PreferWgetConfigConfig)
	if !ok {
		t.Fatalf("DefaultConfig() type = %T, want PreferWgetConfigConfig", r.DefaultConfig())
	}
	if cfg.Timeout == nil || *cfg.Timeout != defaultWgetTimeout {
		t.Errorf("Timeout = %v, want %d", cfg.Timeout, defaultWgetTimeout)
	}
	if cfg.Tries == nil || *cfg.Tries != defaultWgetTries {
		t.Errorf("Tries = %v, want %d", cfg.Tries, defaultWgetTries)
	}
}

func TestPreferWgetConfigRule_ValidateConfig(t *testing.T) {
	t.Parallel()
	r := NewPreferWgetConfigRule()
	if err := r.ValidateConfig(nil); err != nil {
		t.Errorf("ValidateConfig(nil) = %v, want nil", err)
	}
	if err := r.ValidateConfig(map[string]any{"timeout": 10, "tries": 3}); err != nil {
		t.Errorf("ValidateConfig(timeout=10, tries=3) = %v, want nil", err)
	}
}

func TestPreferWgetConfigRule_Check(t *testing.T) {
	t.Parallel()

	testutil.RunRuleTests(t, NewPreferWgetConfigRule(), []testutil.RuleTestCase{
		// === Detection cases ===
		{
			Name: "wget command in RUN triggers violation",
			Content: "FROM ubuntu:22.04\n" +
				"RUN wget https://example.com/file.tar.gz -O /tmp/file.tar.gz\n",
			WantViolations: 1,
		},
		{
			Name: "wget installed via apt-get triggers violation",
			Content: "FROM ubuntu:22.04\n" +
				"RUN apt-get update && apt-get install -y wget\n",
			WantViolations: 1,
		},
		{
			Name: "wget installed via apk triggers violation",
			Content: "FROM alpine:3.20\n" +
				"RUN apk add --no-cache wget\n",
			WantViolations: 1,
		},
		{
			Name: "multiple wget uses yield one violation per stage",
			Content: "FROM ubuntu:22.04\n" +
				"RUN apt-get update && apt-get install -y wget\n" +
				"RUN wget https://example.com/a.sh -O /tmp/a.sh\n" +
				"RUN wget https://example.com/b.tar.gz -O /tmp/b.tar.gz\n",
			WantViolations: 1,
		},
		{
			Name: "two stages with wget yield two violations",
			Content: "FROM ubuntu:22.04 AS build\n" +
				"RUN wget https://example.com/a.tgz -O /tmp/a.tgz\n" +
				"FROM alpine:3.20\n" +
				"RUN wget https://example.com/b.tgz -O /tmp/b.tgz\n",
			WantViolations: 2,
		},

		// === Suppression cases ===
		{
			Name: "existing COPY heredoc wgetrc suppresses",
			Content: "FROM ubuntu:22.04\n" +
				"ENV WGETRC=/etc/wgetrc\n" +
				"COPY --chmod=0644 <<EOF /etc/wgetrc\n" +
				"tries=3\n" +
				"EOF\n" +
				"RUN wget https://example.com/file.tar.gz -O /tmp/file.tar.gz\n",
			WantViolations: 0,
		},
		{
			Name: "WGETRC env already set suppresses",
			Content: "FROM ubuntu:22.04\n" +
				"ENV WGETRC=/opt/wgetrc\n" +
				"RUN wget https://example.com/file.tar.gz -O /tmp/file.tar.gz\n",
			WantViolations: 0,
		},
		{
			Name: "wgetrc created via RUN suppresses",
			Content: "FROM ubuntu:22.04\n" +
				"RUN echo 'tries=3' > /etc/wgetrc\n" +
				"RUN wget https://example.com/file.tar.gz -O /tmp/file.tar.gz\n",
			WantViolations: 0,
		},
		{
			Name: "dot wgetrc created via RUN suppresses",
			Content: "FROM ubuntu:22.04\n" +
				"RUN echo 'tries=3' > /root/.wgetrc\n" +
				"RUN wget https://example.com/file.tar.gz -O /tmp/file.tar.gz\n",
			WantViolations: 0,
		},
		{
			Name: "child stage inheriting from fixed parent is suppressed",
			Content: "FROM ubuntu:22.04 AS downloader\n" +
				"RUN apt-get update && apt-get install -y wget\n" +
				"RUN wget https://example.com/file.tar.gz -O /tmp/file.tar.gz\n" +
				"FROM downloader AS fetcher\n" +
				"RUN wget https://example.com/data.json -O /tmp/data.json\n",
			WantViolations: 1,
		},
		{
			Name: "child stage inheriting from already-configured parent is suppressed",
			Content: "FROM ubuntu:22.04 AS base\n" +
				"ENV WGETRC=/etc/wgetrc\n" +
				"COPY --chmod=0644 <<EOF /etc/wgetrc\n" +
				"tries=3\n" +
				"EOF\n" +
				"FROM base AS runner\n" +
				"RUN wget https://example.com/file.tar.gz -O /tmp/file.tar.gz\n",
			WantViolations: 0,
		},

		// === No-trigger cases ===
		{
			Name: "no wget in stage - no violation",
			Content: "FROM ubuntu:22.04\n" +
				"RUN apt-get update && apt-get install -y curl\n",
			WantViolations: 0,
		},
		{
			Name:           "empty stage - no violation",
			Content:        "FROM scratch\n",
			WantViolations: 0,
		},
		{
			Name: "curl only - no violation",
			Content: "FROM ubuntu:22.04\n" +
				"RUN curl -fsSL https://example.com/install.sh | bash\n",
			WantViolations: 0,
		},

		// === Windows cases ===
		{
			Name: "Windows wget.exe triggers violation",
			Content: "FROM mcr.microsoft.com/windows/servercore:ltsc2022\n" +
				"RUN wget.exe https://example.com/file.zip -O C:\\tmp\\file.zip\n",
			WantViolations: 1,
		},
		{
			Name: "Windows choco install wget triggers violation",
			Content: "FROM mcr.microsoft.com/windows/servercore:ltsc2022\n" +
				"RUN choco install -y wget\n",
			WantViolations: 1,
		},
	})
}

func TestPreferWgetConfigRule_SkipsAddUnpackOwnedInvocationWhenRuleEnabled(t *testing.T) {
	t.Parallel()

	content := "FROM ubuntu:22.04\n" +
		"RUN wget https://example.com/app.tar.gz | tar -xz -C /opt\n"
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	input.EnabledRules = []string{PreferWgetConfigRuleCode, PreferAddUnpackRuleCode}

	r := NewPreferWgetConfigRule()
	violations := r.Check(input)

	if len(violations) != 0 {
		t.Fatalf("expected 0 violations, got %d", len(violations))
	}
}

func TestPreferWgetConfigRule_SuggestedFix(t *testing.T) {
	t.Parallel()

	content := "FROM ubuntu:22.04\nRUN wget https://example.com/file.tar.gz -O /tmp/file.tar.gz\n"
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	r := NewPreferWgetConfigRule()
	violations := r.Check(input)

	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}

	v := violations[0]
	if v.SuggestedFix == nil {
		t.Fatal("expected a SuggestedFix")
	}
	if v.SuggestedFix.Safety != rules.FixSuggestion {
		t.Errorf("Safety = %v, want FixSuggestion", v.SuggestedFix.Safety)
	}
	if len(v.SuggestedFix.Edits) != 1 {
		t.Fatalf("expected 1 edit, got %d", len(v.SuggestedFix.Edits))
	}

	edit := v.SuggestedFix.Edits[0]
	if edit.Location.Start.Line != 2 || edit.Location.Start.Column != 0 {
		t.Errorf("edit Start = (%d,%d), want (2,0)", edit.Location.Start.Line, edit.Location.Start.Column)
	}

	wantNewText := "# [tally] wget configuration for improved robustness\n" +
		"ENV WGETRC=/etc/wgetrc\n" +
		"COPY --chmod=0644 <<EOF ${WGETRC}\n" +
		"retry_connrefused = on\n" +
		"timeout=15\n" +
		"tries=5\n" +
		"retry-on-host-error=on\n" +
		"EOF\n"

	if edit.NewText != wantNewText {
		t.Errorf("NewText =\n%s\nwant:\n%s", edit.NewText, wantNewText)
	}
}

func TestPreferWgetConfigRule_SuggestedFix_Windows(t *testing.T) {
	t.Parallel()

	content := "FROM mcr.microsoft.com/windows/servercore:ltsc2022\n" +
		"RUN wget.exe https://example.com/file.zip -O C:\\tmp\\file.zip\n"
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	r := NewPreferWgetConfigRule()
	violations := r.Check(input)

	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}

	edit := violations[0].SuggestedFix.Edits[0]
	wantNewText := "# [tally] wget configuration for improved robustness\n" +
		"ENV WGETRC=c:\\wgetrc\n" +
		"COPY <<EOF ${WGETRC}\n" +
		"retry_connrefused = on\n" +
		"timeout=15\n" +
		"tries=5\n" +
		"retry-on-host-error=on\n" +
		"EOF\n"

	if edit.NewText != wantNewText {
		t.Errorf("NewText =\n%s\nwant:\n%s", edit.NewText, wantNewText)
	}
}

func TestPreferWgetConfigRule_SuggestedFix_InvocationInsertsBeforeFirstRUN(t *testing.T) {
	t.Parallel()

	content := "FROM ubuntu:22.04\n" +
		"RUN apt-get update\n" +
		"RUN wget https://example.com/file.tar.gz -O /tmp/file.tar.gz\n"
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	r := NewPreferWgetConfigRule()
	violations := r.Check(input)

	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}

	v := violations[0]
	if v.Location.Start.Line != 3 {
		t.Errorf("violation Start.Line = %d, want 3", v.Location.Start.Line)
	}
	edit := v.SuggestedFix.Edits[0]
	if edit.Location.Start.Line != 2 {
		t.Errorf("edit Start.Line = %d, want 2 (first RUN)", edit.Location.Start.Line)
	}
}

func TestPreferWgetConfigRule_SuggestedFix_InstallInsertsBeforeSameRUN(t *testing.T) {
	t.Parallel()

	content := "FROM ubuntu:22.04\n" +
		"RUN apt-get update\n" +
		"RUN apt-get install -y wget curl\n" +
		"RUN wget https://example.com/file.tar.gz -O /tmp/file.tar.gz\n"
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	r := NewPreferWgetConfigRule()
	violations := r.Check(input)

	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}

	v := violations[0]
	if v.Location.Start.Line != 3 {
		t.Errorf("violation Start.Line = %d, want 3", v.Location.Start.Line)
	}
	edit := v.SuggestedFix.Edits[0]
	if edit.Location.Start.Line != 3 {
		t.Errorf("edit Start.Line = %d, want 3 (install RUN)", edit.Location.Start.Line)
	}
}

func TestPreferWgetConfigRule_SuggestedFix_CustomConfig(t *testing.T) {
	t.Parallel()

	content := "FROM ubuntu:22.04\nRUN wget https://example.com/file.tar.gz -O /tmp/file.tar.gz\n"
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	input.Config = map[string]any{
		"timeout": 10,
		"tries":   3,
	}
	r := NewPreferWgetConfigRule()
	violations := r.Check(input)

	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}

	edit := violations[0].SuggestedFix.Edits[0]
	wantNewText := "# [tally] wget configuration for improved robustness\n" +
		"ENV WGETRC=/etc/wgetrc\n" +
		"COPY --chmod=0644 <<EOF ${WGETRC}\n" +
		"retry_connrefused = on\n" +
		"timeout=10\n" +
		"tries=3\n" +
		"retry-on-host-error=on\n" +
		"EOF\n"

	if edit.NewText != wantNewText {
		t.Errorf("NewText =\n%s\nwant:\n%s", edit.NewText, wantNewText)
	}
}
