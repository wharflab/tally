package labels

import (
	"strings"
	"testing"

	fixpkg "github.com/wharflab/tally/internal/fix"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/testutil"
)

func TestPreferGroupedRule_Metadata(t *testing.T) {
	t.Parallel()

	meta := NewPreferGroupedRule().Metadata()
	if meta.Code != PreferGroupedRuleCode {
		t.Fatalf("Code = %q, want %q", meta.Code, PreferGroupedRuleCode)
	}
	if meta.DefaultSeverity.String() != "info" {
		t.Fatalf("DefaultSeverity = %s, want info", meta.DefaultSeverity)
	}
	if meta.Category != "style" {
		t.Fatalf("Category = %q, want style", meta.Category)
	}
	if meta.FixPriority != 96 {
		t.Fatalf("FixPriority = %d, want 96 (just before newline-per-chained-call)", meta.FixPriority)
	}
}

func TestPreferGroupedRule_DefaultConfig(t *testing.T) {
	t.Parallel()

	cfg, ok := NewPreferGroupedRule().DefaultConfig().(PreferGroupedConfig)
	if !ok {
		t.Fatalf("DefaultConfig type = %T, want PreferGroupedConfig", NewPreferGroupedRule().DefaultConfig())
	}
	if cfg.MinLabels == nil {
		t.Fatal("DefaultConfig.MinLabels is nil")
	}
	if *cfg.MinLabels != 3 {
		t.Errorf("DefaultConfig.MinLabels = %d, want 3", *cfg.MinLabels)
	}
}

func TestPreferGroupedRule_ValidateConfig(t *testing.T) {
	t.Parallel()

	rule := NewPreferGroupedRule()

	if err := rule.ValidateConfig(map[string]any{"min-labels": 5}); err != nil {
		t.Errorf("min-labels=5 should be valid: %v", err)
	}
	if err := rule.ValidateConfig(map[string]any{"min-labels": 1}); err == nil {
		t.Errorf("min-labels=1 should be rejected (minimum is 2)")
	}
	if err := rule.ValidateConfig(map[string]any{"min-labels": "three"}); err == nil {
		t.Errorf("non-integer min-labels should be rejected")
	}
}

func TestPreferGroupedRule_Check(t *testing.T) {
	t.Parallel()

	testutil.RunRuleTests(t, NewPreferGroupedRule(), []testutil.RuleTestCase{
		{
			Name: "single grouped multi-line LABEL is clean",
			Content: `FROM alpine:3.20
LABEL org.opencontainers.image.title="demo" \
      org.opencontainers.image.description="desc" \
      org.opencontainers.image.source="https://example.com"
`,
			WantViolations: 0,
		},
		{
			Name: "two adjacent single-pair LABELs below default min",
			Content: `FROM alpine:3.20
LABEL org.opencontainers.image.title="demo"
LABEL org.opencontainers.image.description="desc"
`,
			WantViolations: 0,
		},
		{
			Name: "three adjacent single-pair LABELs",
			Content: `FROM alpine:3.20
LABEL org.opencontainers.image.title="demo"
LABEL org.opencontainers.image.description="desc"
LABEL org.opencontainers.image.source="https://example.com"
`,
			WantViolations: 1,
			WantMessages: []string{
				"3 adjacent LABEL instructions",
			},
		},
		{
			Name: "three adjacent LABELs spanning multiple pairs",
			Content: `FROM alpine:3.20
LABEL org.opencontainers.image.title="demo" \
      org.opencontainers.image.description="desc"
LABEL org.opencontainers.image.source="https://example.com"
LABEL org.opencontainers.image.licenses="Apache-2.0" \
      org.opencontainers.image.vendor="acme"
`,
			WantViolations: 1,
		},
		{
			Name: "ENV between LABELs breaks the run",
			Content: `FROM alpine:3.20
LABEL org.opencontainers.image.title="demo"
LABEL org.opencontainers.image.description="desc"
ENV FOO=bar
LABEL org.opencontainers.image.source="https://example.com"
`,
			WantViolations: 0,
		},
		{
			Name: "comment between LABELs breaks the run",
			Content: `FROM alpine:3.20
LABEL org.opencontainers.image.title="demo"
LABEL org.opencontainers.image.description="desc"
# Provenance metadata follows
LABEL org.opencontainers.image.source="https://example.com"
`,
			WantViolations: 0,
		},
		{
			Name: "blank line between LABELs breaks the run",
			Content: `FROM alpine:3.20
LABEL org.opencontainers.image.title="demo"
LABEL org.opencontainers.image.description="desc"

LABEL org.opencontainers.image.source="https://example.com"
`,
			WantViolations: 0,
		},
		{
			Name: "LABELs in different stages are independent",
			// Each stage has 3 labels (the default min-labels threshold), so
			// the cross-stage run of 6 pairs would trigger one violation if
			// stage isolation were broken; per-stage runs of 3 do not.
			Content: `FROM alpine:3.20 AS build
LABEL stage.a="1"
LABEL stage.b="2"
LABEL stage.c="3"

FROM alpine:3.20
LABEL final.a="1"
LABEL final.b="2"
LABEL final.c="3"
`,
			WantViolations: 2,
		},
		{
			Name: "run with dynamic key still reports but no fix",
			Content: `FROM alpine:3.20
LABEL org.opencontainers.image.title="demo"
LABEL "$LABEL_PREFIX.name"="demo"
LABEL org.opencontainers.image.source="https://example.com"
`,
			WantViolations: 1,
		},
		{
			Name: "run with duplicate key still reports but no fix",
			Content: `FROM alpine:3.20
LABEL org.opencontainers.image.title="first"
LABEL org.opencontainers.image.title="second"
LABEL org.opencontainers.image.description="desc"
`,
			WantViolations: 1,
		},
		{
			Name: "configurable min-labels suppresses small runs",
			Content: `FROM alpine:3.20
LABEL a=1
LABEL b=2
LABEL c=3
`,
			Config:         map[string]any{"min-labels": 5},
			WantViolations: 0,
		},
		{
			Name: "configurable min-labels matches",
			Content: `FROM alpine:3.20
LABEL a=1
LABEL b=2
LABEL c=3
LABEL d=4
LABEL e=5
`,
			Config:         map[string]any{"min-labels": 5},
			WantViolations: 1,
		},
	})
}

func TestPreferGroupedRule_FixOptions(t *testing.T) {
	t.Parallel()

	content := `FROM alpine:3.20
LABEL org.opencontainers.image.title="demo"
LABEL org.opencontainers.image.description="desc"
LABEL org.opencontainers.image.source="https://example.com"
`
	input := testutil.MakeLintInput(t, "Dockerfile", content)

	violations := NewPreferGroupedRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("got %d violations, want 1", len(violations))
	}

	fix := violations[0].PreferredFix()
	if fix == nil {
		t.Fatal("expected preferred fix")
	}
	if !fix.IsPreferred {
		t.Error("merge fix should be preferred")
	}
	if fix.Safety != rules.FixSafe {
		t.Errorf("fix safety = %s, want safe", fix.Safety)
	}
	if fix.Priority != 96 {
		t.Errorf("fix priority = %d, want 96", fix.Priority)
	}
	if got := len(fix.Edits); got != 3 {
		t.Errorf("fix edits = %d, want 3 (replace + 2 deletes)", got)
	}

	got := string(fixpkg.ApplyFix([]byte(content), fix))
	want := "FROM alpine:3.20\n" +
		"LABEL org.opencontainers.image.title=\"demo\" \\\n" +
		"\torg.opencontainers.image.description=\"desc\" \\\n" +
		"\torg.opencontainers.image.source=\"https://example.com\"\n"
	if got != want {
		t.Errorf("merge fix mismatch\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestPreferGroupedRule_FixCombinesMultiPair(t *testing.T) {
	t.Parallel()

	content := `FROM alpine:3.20
LABEL org.opencontainers.image.title="demo" \
      org.opencontainers.image.description="desc"
LABEL org.opencontainers.image.source="https://example.com"
LABEL org.opencontainers.image.licenses="Apache-2.0"
`
	input := testutil.MakeLintInput(t, "Dockerfile", content)

	violations := NewPreferGroupedRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("got %d violations, want 1", len(violations))
	}

	got := string(fixpkg.ApplyFix([]byte(content), violations[0].PreferredFix()))
	want := "FROM alpine:3.20\n" +
		"LABEL org.opencontainers.image.title=\"demo\" \\\n" +
		"\torg.opencontainers.image.description=\"desc\" \\\n" +
		"\torg.opencontainers.image.source=\"https://example.com\" \\\n" +
		"\torg.opencontainers.image.licenses=\"Apache-2.0\"\n"
	if got != want {
		t.Errorf("merge fix mismatch\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestPreferGroupedRule_NoFixWhenDuplicateKey(t *testing.T) {
	t.Parallel()

	content := `FROM alpine:3.20
LABEL org.opencontainers.image.title="first"
LABEL org.opencontainers.image.title="second"
LABEL org.opencontainers.image.description="desc"
`
	input := testutil.MakeLintInput(t, "Dockerfile", content)

	violations := NewPreferGroupedRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("got %d violations, want 1", len(violations))
	}
	if got := violations[0].PreferredFix(); got != nil {
		t.Errorf("expected no fix for duplicate-key run, got %+v", got)
	}
}

func TestPreferGroupedRule_NoFixWhenDynamicKey(t *testing.T) {
	t.Parallel()

	content := `FROM alpine:3.20
LABEL org.opencontainers.image.title="demo"
LABEL "$LABEL_PREFIX.name"="dynamic"
LABEL org.opencontainers.image.source="https://example.com"
`
	input := testutil.MakeLintInput(t, "Dockerfile", content)

	violations := NewPreferGroupedRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("got %d violations, want 1", len(violations))
	}
	if got := violations[0].PreferredFix(); got != nil {
		t.Errorf("expected no fix for dynamic-key run, got %+v", got)
	}
}

func TestPreferGroupedRule_FixHonorsBacktickEscape(t *testing.T) {
	t.Parallel()

	// Windows-style Dockerfile that opts into the backtick escape token. The
	// merged LABEL must use backticks (not backslashes) for line continuations,
	// otherwise BuildKit would parse the literal `\` as part of the value.
	content := "# escape=`\n" +
		"FROM mcr.microsoft.com/windows/servercore:ltsc2022\n" +
		"LABEL org.opencontainers.image.title=\"demo\"\n" +
		"LABEL org.opencontainers.image.description=\"desc\"\n" +
		"LABEL org.opencontainers.image.source=\"https://example.com\"\n"
	input := testutil.MakeLintInput(t, "Dockerfile", content)

	violations := NewPreferGroupedRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("got %d violations, want 1", len(violations))
	}

	got := string(fixpkg.ApplyFix([]byte(content), violations[0].PreferredFix()))
	want := "# escape=`\n" +
		"FROM mcr.microsoft.com/windows/servercore:ltsc2022\n" +
		"LABEL org.opencontainers.image.title=\"demo\" `\n" +
		"\torg.opencontainers.image.description=\"desc\" `\n" +
		"\torg.opencontainers.image.source=\"https://example.com\"\n"
	if got != want {
		t.Errorf("merge fix mismatch\ngot:\n%s\nwant:\n%s", got, want)
	}
	if strings.Contains(got, " \\\n") {
		t.Errorf("merged output must not contain backslash continuations under # escape=`")
	}
}

func TestPreferGroupedRule_PreservesIndentation(t *testing.T) {
	t.Parallel()

	content := "FROM alpine:3.20\n" +
		"\tLABEL a=1\n" +
		"\tLABEL b=2\n" +
		"\tLABEL c=3\n"

	input := testutil.MakeLintInput(t, "Dockerfile", content)

	violations := NewPreferGroupedRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("got %d violations, want 1", len(violations))
	}

	got := string(fixpkg.ApplyFix([]byte(content), violations[0].PreferredFix()))
	if !strings.HasPrefix(got, "FROM alpine:3.20\n\tLABEL a=1 \\\n") {
		t.Errorf("merged output should preserve leading tab indent; got:\n%s", got)
	}
	if !strings.Contains(got, "\n\t\tb=2") {
		t.Errorf("continuation lines should reuse indent + tab; got:\n%s", got)
	}
}
