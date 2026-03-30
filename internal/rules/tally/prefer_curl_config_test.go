package tally

import (
	"testing"

	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/testutil"
)

func TestPreferCurlConfigRule_Metadata(t *testing.T) {
	t.Parallel()
	r := NewPreferCurlConfigRule()
	meta := r.Metadata()
	if meta.Code != "tally/prefer-curl-config" {
		t.Errorf("Code = %q, want %q", meta.Code, "tally/prefer-curl-config")
	}
	if meta.DefaultSeverity != rules.SeverityInfo {
		t.Errorf("DefaultSeverity = %v, want Info", meta.DefaultSeverity)
	}
	if meta.Category != "reliability" {
		t.Errorf("Category = %q, want %q", meta.Category, "reliability")
	}
	if meta.FixPriority != 93 { //nolint:mnd // expected value
		t.Errorf("FixPriority = %d, want 93", meta.FixPriority)
	}
}

func TestPreferCurlConfigRule_DefaultConfig(t *testing.T) {
	t.Parallel()
	r := NewPreferCurlConfigRule()
	cfg, ok := r.DefaultConfig().(PreferCurlConfigConfig)
	if !ok {
		t.Fatalf("DefaultConfig() type = %T, want PreferCurlConfigConfig", r.DefaultConfig())
	}
	if cfg.Retry == nil || *cfg.Retry != defaultRetry {
		t.Errorf("Retry = %v, want %d", cfg.Retry, defaultRetry)
	}
	if cfg.ConnectTimeout == nil || *cfg.ConnectTimeout != defaultConnectTimeout {
		t.Errorf("ConnectTimeout = %v, want %d", cfg.ConnectTimeout, defaultConnectTimeout)
	}
	if cfg.MaxTime == nil || *cfg.MaxTime != defaultMaxTime {
		t.Errorf("MaxTime = %v, want %d", cfg.MaxTime, defaultMaxTime)
	}
}

func TestPreferCurlConfigRule_ValidateConfig(t *testing.T) {
	t.Parallel()
	r := NewPreferCurlConfigRule()
	if err := r.ValidateConfig(nil); err != nil {
		t.Errorf("ValidateConfig(nil) = %v, want nil", err)
	}
	if err := r.ValidateConfig(map[string]any{"retry": 3}); err != nil {
		t.Errorf("ValidateConfig(retry=3) = %v, want nil", err)
	}
}

func TestPreferCurlConfigRule_Check(t *testing.T) {
	t.Parallel()

	testutil.RunRuleTests(t, NewPreferCurlConfigRule(), []testutil.RuleTestCase{
		// === Detection cases ===
		{
			Name: "curl command in RUN triggers violation",
			Content: "FROM ubuntu:22.04\n" +
				"RUN curl -fsSL https://example.com/install.sh | bash\n",
			WantViolations: 1,
		},
		{
			Name: "curl installed via apt-get triggers violation",
			Content: "FROM ubuntu:22.04\n" +
				"RUN apt-get update && apt-get install -y curl\n",
			WantViolations: 1,
		},
		{
			Name: "curl installed via apk triggers violation",
			Content: "FROM alpine:3.20\n" +
				"RUN apk add --no-cache curl\n",
			WantViolations: 1,
		},
		{
			Name: "multiple curl uses yield one violation per stage",
			Content: "FROM ubuntu:22.04\n" +
				"RUN apt-get update && apt-get install -y curl\n" +
				"RUN curl -fsSL https://example.com/a.sh | bash\n" +
				"RUN curl -o /tmp/b.tar.gz https://example.com/b.tar.gz\n",
			WantViolations: 1,
		},
		{
			Name: "two stages with curl yield two violations",
			Content: "FROM ubuntu:22.04 AS build\n" +
				"RUN curl -fsSL https://example.com/install.sh | bash\n" +
				"FROM alpine:3.20\n" +
				"RUN curl -o /tmp/file https://example.com/file\n",
			WantViolations: 2,
		},

		// === Suppression cases ===
		{
			Name: "existing COPY heredoc curlrc suppresses",
			Content: "FROM ubuntu:22.04\n" +
				"ENV CURL_HOME=/etc/curl\n" +
				"COPY --chmod=0644 <<EOF /etc/curl/.curlrc\n" +
				"--retry 3\n" +
				"EOF\n" +
				"RUN curl -fsSL https://example.com/install.sh | bash\n",
			WantViolations: 0,
		},
		{
			Name: "CURL_HOME env already set suppresses",
			Content: "FROM ubuntu:22.04\n" +
				"ENV CURL_HOME=/opt/curl\n" +
				"RUN curl -fsSL https://example.com/install.sh | bash\n",
			WantViolations: 0,
		},
		{
			Name: "curlrc created via RUN suppresses",
			Content: "FROM ubuntu:22.04\n" +
				"RUN mkdir -p /etc/curl && echo '--retry 3' > /etc/curl/.curlrc\n" +
				"RUN curl -fsSL https://example.com/install.sh | bash\n",
			WantViolations: 0,
		},
		{
			Name: "child stage inheriting from fixed parent is suppressed",
			Content: "FROM ubuntu:22.04 AS downloader\n" +
				"RUN apt-get update && apt-get install -y curl\n" +
				"RUN curl -fsSL https://example.com/install.sh | bash\n" +
				"FROM downloader AS fetcher\n" +
				"RUN curl -o /tmp/data.json https://example.com/data.json\n",
			WantViolations: 1, // only stage 1, not stage 2
		},
		{
			Name: "child stage inheriting from already-configured parent is suppressed",
			Content: "FROM ubuntu:22.04 AS base\n" +
				"ENV CURL_HOME=/etc/curl\n" +
				"COPY --chmod=0644 <<EOF /etc/curl/.curlrc\n" +
				"--retry 3\n" +
				"EOF\n" +
				"FROM base AS runner\n" +
				"RUN curl -fsSL https://example.com/install.sh | bash\n",
			WantViolations: 0, // parent already configured, child inherits
		},
		{
			Name: "independent stages both fire",
			Content: "FROM ubuntu:22.04 AS a\n" +
				"RUN curl https://example.com/a\n" +
				"FROM alpine:3.20 AS b\n" +
				"RUN curl https://example.com/b\n",
			WantViolations: 2, // different base images, no inheritance
		},
		{
			Name: "Windows existing uppercase _CURLRC suppresses case-insensitively",
			Content: "FROM mcr.microsoft.com/windows/servercore:ltsc2022\n" +
				"COPY <<EOF C:\\CURL\\_CURLRC\n" +
				"--retry 3\n" +
				"EOF\n" +
				"RUN curl.exe -fsSL https://example.com/install.ps1 -o install.ps1\n",
			WantViolations: 0,
		},
		{
			Name: "curlrc at non-default path suppresses",
			Content: "FROM ubuntu:22.04\n" +
				"COPY --chmod=0644 <<EOF /root/.curlrc\n" +
				"--retry 3\n" +
				"EOF\n" +
				"RUN curl -fsSL https://example.com/install.sh | bash\n",
			WantViolations: 0,
		},

		// === No-trigger cases ===
		{
			Name: "no curl in stage - no violation",
			Content: "FROM ubuntu:22.04\n" +
				"RUN apt-get update && apt-get install -y wget\n",
			WantViolations: 0,
		},
		{
			Name:           "empty stage - no violation",
			Content:        "FROM scratch\n",
			WantViolations: 0,
		},
		{
			Name: "wget only - no violation",
			Content: "FROM ubuntu:22.04\n" +
				"RUN wget https://example.com/file\n",
			WantViolations: 0,
		},

		// === Windows cases ===
		{
			Name: "Windows curl.exe triggers violation",
			Content: "FROM mcr.microsoft.com/windows/servercore:ltsc2022\n" +
				"RUN curl.exe -fsSL https://example.com/install.ps1 -o install.ps1\n",
			WantViolations: 1,
		},
		{
			Name: "Windows choco install curl triggers violation",
			Content: "FROM mcr.microsoft.com/windows/servercore:ltsc2022\n" +
				"RUN choco install -y curl\n",
			WantViolations: 1,
		},
	})
}

func TestPreferCurlConfigRule_SkipsAddUnpackOwnedInvocationWhenRuleEnabled(t *testing.T) {
	t.Parallel()

	content := "FROM ubuntu:22.04\n" +
		"RUN curl -fsSL https://example.com/app.tar.gz | tar -xz -C /opt\n"
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	input.EnabledRules = []string{PreferCurlConfigRuleCode, PreferAddUnpackRuleCode}

	r := NewPreferCurlConfigRule()
	violations := r.Check(input)

	if len(violations) != 0 {
		t.Fatalf("expected 0 violations, got %d", len(violations))
	}
}

func TestPreferCurlConfigRule_SuggestedFix(t *testing.T) {
	t.Parallel()

	content := "FROM ubuntu:22.04\nRUN curl -fsSL https://example.com/install.sh | bash\n"
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	r := NewPreferCurlConfigRule()
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

	// Zero-width insertion before the RUN line.
	if edit.Location.Start.Line != 2 || edit.Location.Start.Column != 0 {
		t.Errorf("edit Start = (%d,%d), want (2,0)", edit.Location.Start.Line, edit.Location.Start.Column)
	}

	wantNewText := "# [tally] curl configuration for improved robustness\n" +
		"ENV CURL_HOME=/etc/curl\n" +
		"COPY --chmod=0644 <<EOF ${CURL_HOME}/.curlrc\n" +
		"--retry-connrefused\n" +
		"--connect-timeout 15\n" +
		"--retry 5\n" +
		"--max-time 300\n" +
		"EOF\n"

	if edit.NewText != wantNewText {
		t.Errorf("NewText =\n%s\nwant:\n%s", edit.NewText, wantNewText)
	}
}

func TestPreferCurlConfigRule_SuggestedFix_Windows(t *testing.T) {
	t.Parallel()

	content := "FROM mcr.microsoft.com/windows/servercore:ltsc2022\n" +
		"RUN curl.exe -fsSL https://example.com/file -o C:\\tmp\\file\n"
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	r := NewPreferCurlConfigRule()
	violations := r.Check(input)

	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}

	v := violations[0]
	if v.SuggestedFix == nil {
		t.Fatal("expected a SuggestedFix")
	}

	edit := v.SuggestedFix.Edits[0]

	// Windows: no --chmod, CURL_HOME=c:\curl
	wantNewText := "# [tally] curl configuration for improved robustness\n" +
		"ENV CURL_HOME=c:\\curl\n" +
		"COPY <<EOF ${CURL_HOME}/.curlrc\n" +
		"--retry-connrefused\n" +
		"--connect-timeout 15\n" +
		"--retry 5\n" +
		"--max-time 300\n" +
		"EOF\n"

	if edit.NewText != wantNewText {
		t.Errorf("NewText =\n%s\nwant:\n%s", edit.NewText, wantNewText)
	}
}

func TestPreferCurlConfigRule_SuggestedFix_InvocationInsertsBeforeFirstRUN(t *testing.T) {
	t.Parallel()

	content := "FROM ubuntu:22.04\n" +
		"RUN apt-get update\n" +
		"RUN curl -fsSL https://example.com/install.sh | bash\n"
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	r := NewPreferCurlConfigRule()
	violations := r.Check(input)

	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}

	v := violations[0]
	// Violation is on the curl invocation line (line 3).
	if v.Location.Start.Line != 3 {
		t.Errorf("violation Start.Line = %d, want 3", v.Location.Start.Line)
	}
	// Fix inserts before the first RUN (line 2), not the curl line.
	edit := v.SuggestedFix.Edits[0]
	if edit.Location.Start.Line != 2 {
		t.Errorf("edit Start.Line = %d, want 2 (first RUN)", edit.Location.Start.Line)
	}
}

func TestPreferCurlConfigRule_SuggestedFix_InstallInsertsBeforeSameRUN(t *testing.T) {
	t.Parallel()

	content := "FROM ubuntu:22.04\n" +
		"RUN apt-get update\n" +
		"RUN apt-get install -y curl wget\n" +
		"RUN curl -fsSL https://example.com/install.sh | bash\n"
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	r := NewPreferCurlConfigRule()
	violations := r.Check(input)

	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}

	// Both violation and fix anchored at the install RUN (line 3).
	v := violations[0]
	if v.Location.Start.Line != 3 {
		t.Errorf("violation Start.Line = %d, want 3", v.Location.Start.Line)
	}
	edit := v.SuggestedFix.Edits[0]
	if edit.Location.Start.Line != 3 {
		t.Errorf("edit Start.Line = %d, want 3 (install RUN)", edit.Location.Start.Line)
	}
}

func TestPreferCurlConfigRule_SuggestedFix_CustomConfig(t *testing.T) {
	t.Parallel()

	content := "FROM ubuntu:22.04\nRUN curl https://example.com/file\n"
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	input.Config = map[string]any{
		"retry":           3,
		"connect-timeout": 10,
		"max-time":        120,
	}
	r := NewPreferCurlConfigRule()
	violations := r.Check(input)

	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}

	edit := violations[0].SuggestedFix.Edits[0]
	wantNewText := "# [tally] curl configuration for improved robustness\n" +
		"ENV CURL_HOME=/etc/curl\n" +
		"COPY --chmod=0644 <<EOF ${CURL_HOME}/.curlrc\n" +
		"--retry-connrefused\n" +
		"--connect-timeout 10\n" +
		"--retry 3\n" +
		"--max-time 120\n" +
		"EOF\n"

	if edit.NewText != wantNewText {
		t.Errorf("NewText =\n%s\nwant:\n%s", edit.NewText, wantNewText)
	}
}
