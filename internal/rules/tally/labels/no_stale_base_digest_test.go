package labels

import (
	"testing"

	fixpkg "github.com/wharflab/tally/internal/fix"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/testutil"
)

const (
	baseDigestA = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	baseDigestB = "sha256:2222222222222222222222222222222222222222222222222222222222222222"
)

func TestNoStaleBaseDigestRule_Metadata(t *testing.T) {
	t.Parallel()

	meta := NewNoStaleBaseDigestRule().Metadata()
	if meta.Code != NoStaleBaseDigestRuleCode {
		t.Fatalf("Code = %q, want %q", meta.Code, NoStaleBaseDigestRuleCode)
	}
	if meta.DefaultSeverity != rules.SeverityWarning {
		t.Fatalf("DefaultSeverity = %s, want warning", meta.DefaultSeverity)
	}
	if meta.Category != "correctness" {
		t.Fatalf("Category = %q, want correctness", meta.Category)
	}
}

func TestNoStaleBaseDigestRule_Check(t *testing.T) {
	t.Parallel()

	testutil.RunRuleTests(t, NewNoStaleBaseDigestRule(), []testutil.RuleTestCase{
		{
			Name: "label without digest pinned FROM is stale",
			Content: `FROM alpine:3.20
LABEL org.opencontainers.image.base.digest="sha256:deadbeef"
`,
			WantViolations: 1,
			WantMessages:   []string{`requires a digest-pinned FROM`},
		},
		{
			Name: "label matching digest pinned FROM is valid",
			Content: `FROM alpine:3.20@` + baseDigestA + `
LABEL org.opencontainers.image.base.digest="` + baseDigestA + `"
`,
			WantViolations: 0,
		},
		{
			Name: "label mismatching digest pinned FROM is stale",
			Content: `FROM alpine:3.20@` + baseDigestA + `
LABEL org.opencontainers.image.base.digest="` + baseDigestB + `"
`,
			WantViolations: 1,
			WantMessages:   []string{`but the exported base FROM is pinned to "` + baseDigestA + `"`},
		},
		{
			Name: "dynamic value with digest pinned FROM is left to build time",
			Content: `FROM alpine:3.20@` + baseDigestA + `
ARG BASE_DIGEST
LABEL org.opencontainers.image.base.digest="${BASE_DIGEST}"
`,
			WantViolations: 0,
		},
		{
			Name: "dynamic value without digest pinned FROM is still stale",
			Content: `FROM alpine:3.20
ARG BASE_DIGEST
LABEL org.opencontainers.image.base.digest="${BASE_DIGEST}"
`,
			WantViolations: 1,
			WantMessages:   []string{`requires a digest-pinned FROM`},
		},
		{
			Name: "builder-only base digest labels do not affect exported image",
			Content: `FROM alpine:3.20 AS build
LABEL org.opencontainers.image.base.digest="sha256:deadbeef"

FROM alpine:3.20
RUN true
`,
			WantViolations: 0,
		},
		{
			Name: "labels inherited by final stage are checked",
			Content: `FROM alpine:3.20 AS metadata
LABEL org.opencontainers.image.base.digest="sha256:deadbeef"

FROM metadata
RUN true
`,
			WantViolations: 1,
			WantMessages:   []string{`requires a digest-pinned FROM`},
		},
		{
			Name: "ancestor base digest label shadowed by final stage is ignored",
			Content: `FROM alpine:3.20@` + baseDigestA + ` AS metadata
LABEL org.opencontainers.image.base.digest="` + baseDigestB + `"

FROM metadata
LABEL org.opencontainers.image.base.digest="` + baseDigestA + `"
`,
			WantViolations: 0,
		},
		{
			Name: "stage reference chain can carry a digest pinned external base",
			Content: `FROM alpine:3.20@` + baseDigestA + ` AS base

FROM base
LABEL org.opencontainers.image.base.digest="` + baseDigestA + `"
`,
			WantViolations: 0,
		},
		{
			Name: "scratch final stage has no base digest",
			Content: `FROM scratch
LABEL org.opencontainers.image.base.digest="sha256:deadbeef"
`,
			WantViolations: 1,
			WantMessages:   []string{`requires a digest-pinned FROM`},
		},
		{
			Name: "dynamic keys are skipped",
			Content: `FROM alpine:3.20
ARG LABEL_KEY=org.opencontainers.image.base.digest
LABEL "$LABEL_KEY"="sha256:deadbeef"
`,
			WantViolations: 0,
		},
	})
}

func TestNoStaleBaseDigestRule_FixOptions(t *testing.T) {
	t.Parallel()

	content := `FROM alpine:3.20
LABEL org.opencontainers.image.base.digest="sha256:deadbeef"
`
	input := testutil.MakeLintInput(t, "Dockerfile", content)

	violations := NewNoStaleBaseDigestRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("got %d violations, want 1", len(violations))
	}

	allFixes := violations[0].AllFixes()
	if len(allFixes) != 2 {
		t.Fatalf("got %d fix options, want 2", len(allFixes))
	}

	commentFix := allFixes[0]
	if commentFix != violations[0].SuggestedFix {
		t.Fatal("preferred fix is not mirrored as SuggestedFix")
	}
	if !commentFix.IsPreferred {
		t.Fatal("comment-out fix should be preferred")
	}
	if commentFix.Safety != rules.FixSuggestion {
		t.Errorf("comment fix safety = %s, want suggestion", commentFix.Safety)
	}

	gotCommented := string(fixpkg.ApplyFix([]byte(content), commentFix))
	wantCommented := "FROM alpine:3.20\n" +
		"# [commented out by tally - org.opencontainers.image.base.digest needs a digest-pinned FROM]: " +
		"LABEL org.opencontainers.image.base.digest=\"sha256:deadbeef\"\n"
	if gotCommented != wantCommented {
		t.Errorf("comment fix mismatch\ngot:\n%s\nwant:\n%s", gotCommented, wantCommented)
	}

	deleteFix := allFixes[1]
	if deleteFix.IsPreferred {
		t.Fatal("delete fix should not be preferred")
	}
	if deleteFix.Safety != rules.FixSuggestion {
		t.Errorf("delete fix safety = %s, want suggestion", deleteFix.Safety)
	}

	gotDeleted := string(fixpkg.ApplyFix([]byte(content), deleteFix))
	wantDeleted := "FROM alpine:3.20\n"
	if gotDeleted != wantDeleted {
		t.Errorf("delete fix mismatch\ngot:\n%s\nwant:\n%s", gotDeleted, wantDeleted)
	}
}

func TestNoStaleBaseDigestRule_SemanticlessInheritedLabelIsChecked(t *testing.T) {
	t.Parallel()

	input := testutil.MakeLintInput(t, "Dockerfile", `FROM alpine:3.20 AS metadata
LABEL org.opencontainers.image.base.digest="sha256:deadbeef"

FROM metadata
RUN true
`)
	input.Semantic = nil

	violations := NewNoStaleBaseDigestRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("got %d violations, want 1", len(violations))
	}
	if violations[0].SuggestedFix == nil {
		t.Fatal("SuggestedFix = nil, want fix for inherited stale label")
	}
}

func TestNoStaleBaseDigestRule_GroupedFixOptions(t *testing.T) {
	t.Parallel()

	content := `FROM alpine:3.20
LABEL org.opencontainers.image.title="demo" \
      org.opencontainers.image.base.digest="sha256:deadbeef" \
      org.opencontainers.image.description="Demo image"
`
	input := testutil.MakeLintInput(t, "Dockerfile", content)

	violations := NewNoStaleBaseDigestRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("got %d violations, want 1", len(violations))
	}

	allFixes := violations[0].AllFixes()
	if len(allFixes) != 2 {
		t.Fatalf("got %d fix options, want 2", len(allFixes))
	}

	commentFix := allFixes[0]
	gotCommented := string(fixpkg.ApplyFix([]byte(content), commentFix))
	wantCommented := "FROM alpine:3.20\n" +
		"# [commented out by tally - org.opencontainers.image.base.digest needs a digest-pinned FROM]: " +
		"LABEL org.opencontainers.image.base.digest=\"sha256:deadbeef\"\n" +
		"LABEL org.opencontainers.image.title=\"demo\" \\\n" +
		"      org.opencontainers.image.description=\"Demo image\"\n"
	if gotCommented != wantCommented {
		t.Errorf("comment fix mismatch\ngot:\n%s\nwant:\n%s", gotCommented, wantCommented)
	}

	deleteFix := allFixes[1]
	gotDeleted := string(fixpkg.ApplyFix([]byte(content), deleteFix))
	wantDeleted := "FROM alpine:3.20\n" +
		"LABEL org.opencontainers.image.title=\"demo\" \\\n" +
		"      org.opencontainers.image.description=\"Demo image\"\n"
	if gotDeleted != wantDeleted {
		t.Errorf("delete fix mismatch\ngot:\n%s\nwant:\n%s", gotDeleted, wantDeleted)
	}
}

func TestNoStaleBaseDigestRule_GroupedLastPairFix(t *testing.T) {
	t.Parallel()

	content := `FROM alpine:3.20
LABEL org.opencontainers.image.title="demo" \
      org.opencontainers.image.base.digest="sha256:deadbeef"
`
	input := testutil.MakeLintInput(t, "Dockerfile", content)

	violations := NewNoStaleBaseDigestRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("got %d violations, want 1", len(violations))
	}

	gotDeleted := string(fixpkg.ApplyFix([]byte(content), violations[0].AllFixes()[1]))
	wantDeleted := "FROM alpine:3.20\n" +
		"LABEL org.opencontainers.image.title=\"demo\"\n"
	if gotDeleted != wantDeleted {
		t.Errorf("delete fix mismatch\ngot:\n%s\nwant:\n%s", gotDeleted, wantDeleted)
	}
}

func TestNoStaleBaseDigestRule_SeparateRepeatedStaleDigestFixOptions(t *testing.T) {
	t.Parallel()

	content := `FROM alpine:3.20
LABEL org.opencontainers.image.base.digest="sha256:old"
LABEL org.opencontainers.image.base.digest="sha256:new"
`
	input := testutil.MakeLintInput(t, "Dockerfile", content)

	violations := NewNoStaleBaseDigestRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("got %d violations, want 1", len(violations))
	}

	allFixes := violations[0].AllFixes()
	if len(allFixes) != 2 {
		t.Fatalf("got %d fix options, want 2", len(allFixes))
	}

	gotCommented := string(fixpkg.ApplyFix([]byte(content), allFixes[0]))
	wantCommented := "FROM alpine:3.20\n" +
		"# [commented out by tally - org.opencontainers.image.base.digest needs a digest-pinned FROM]: " +
		"LABEL org.opencontainers.image.base.digest=\"sha256:old\"\n" +
		"# [commented out by tally - org.opencontainers.image.base.digest needs a digest-pinned FROM]: " +
		"LABEL org.opencontainers.image.base.digest=\"sha256:new\"\n"
	if gotCommented != wantCommented {
		t.Errorf("comment fix mismatch\ngot:\n%s\nwant:\n%s", gotCommented, wantCommented)
	}
	commentedInput := testutil.MakeLintInput(t, "Dockerfile", gotCommented)
	if got := NewNoStaleBaseDigestRule().Check(commentedInput); len(got) != 0 {
		t.Fatalf("comment fix left %d violations, want 0", len(got))
	}

	gotDeleted := string(fixpkg.ApplyFix([]byte(content), allFixes[1]))
	wantDeleted := "FROM alpine:3.20\n"
	if gotDeleted != wantDeleted {
		t.Errorf("delete fix mismatch\ngot:\n%s\nwant:\n%s", gotDeleted, wantDeleted)
	}
	deletedInput := testutil.MakeLintInput(t, "Dockerfile", gotDeleted)
	if got := NewNoStaleBaseDigestRule().Check(deletedInput); len(got) != 0 {
		t.Fatalf("delete fix left %d violations, want 0", len(got))
	}
}

func TestNoStaleBaseDigestRule_FixEditsExportedAncestorStage(t *testing.T) {
	t.Parallel()

	content := `FROM alpine:3.20 AS metadata
LABEL org.opencontainers.image.base.digest="sha256:ancestor"

FROM metadata
LABEL org.opencontainers.image.base.digest="sha256:final"
`
	input := testutil.MakeLintInput(t, "Dockerfile", content)

	violations := NewNoStaleBaseDigestRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("got %d violations, want 1", len(violations))
	}

	gotDeleted := string(fixpkg.ApplyFix([]byte(content), violations[0].AllFixes()[1]))
	wantDeleted := `FROM alpine:3.20 AS metadata

FROM metadata
`
	if gotDeleted != wantDeleted {
		t.Errorf("delete fix mismatch\ngot:\n%s\nwant:\n%s", gotDeleted, wantDeleted)
	}
	deletedInput := testutil.MakeLintInput(t, "Dockerfile", gotDeleted)
	if got := NewNoStaleBaseDigestRule().Check(deletedInput); len(got) != 0 {
		t.Fatalf("delete fix left %d violations, want 0", len(got))
	}
}

func TestExportedBaseImageDigestFallbackWithoutSemantic(t *testing.T) {
	t.Parallel()

	input := testutil.MakeLintInput(t, "Dockerfile", `FROM alpine:3.20@`+baseDigestA+`
RUN true
`)
	input.Semantic = nil

	got := exportedBaseImageDigest(input)
	if !got.HasDigest || got.Digest != baseDigestA {
		t.Fatalf("exportedBaseImageDigest() = %#v, want digest %q", got, baseDigestA)
	}

	stageRefInput := testutil.MakeLintInput(t, "Dockerfile", `FROM alpine:3.20@`+baseDigestA+` AS base

FROM base
LABEL org.opencontainers.image.base.digest="`+baseDigestA+`"
`)
	stageRefInput.Semantic = nil

	stageRefDigest := exportedBaseImageDigest(stageRefInput)
	if !stageRefDigest.HasDigest || stageRefDigest.Digest != baseDigestA {
		t.Fatalf("exportedBaseImageDigest(stage ref) = %#v, want digest %q", stageRefDigest, baseDigestA)
	}
	if got := NewNoStaleBaseDigestRule().Check(stageRefInput); len(got) != 0 {
		t.Fatalf("semantic-less stage ref produced %d violations, want 0", len(got))
	}

	empty := exportedBaseImageDigest(rules.LintInput{})
	if empty.HasDigest || empty.Digest != "" {
		t.Fatalf("exportedBaseImageDigest(empty) = %#v, want no digest", empty)
	}

	if digest, ok := imageRefDigest("alpine@sha256:not-a-real-digest"); ok || digest != "" {
		t.Fatalf("imageRefDigest(invalid) = %q, %v, want no digest", digest, ok)
	}
}
