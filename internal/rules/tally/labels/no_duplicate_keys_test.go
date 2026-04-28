package labels

import (
	"testing"

	fixpkg "github.com/wharflab/tally/internal/fix"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/testutil"
)

func TestNoDuplicateKeysRule_Metadata(t *testing.T) {
	t.Parallel()

	meta := NewNoDuplicateKeysRule().Metadata()
	if meta.Code != NoDuplicateKeysRuleCode {
		t.Fatalf("Code = %q, want %q", meta.Code, NoDuplicateKeysRuleCode)
	}
	if meta.DefaultSeverity.String() != "warning" {
		t.Fatalf("DefaultSeverity = %s, want warning", meta.DefaultSeverity)
	}
	if meta.Category != "correctness" {
		t.Fatalf("Category = %q, want correctness", meta.Category)
	}
}

func TestNoDuplicateKeysRule_Check(t *testing.T) {
	t.Parallel()

	testutil.RunRuleTests(t, NewNoDuplicateKeysRule(), []testutil.RuleTestCase{
		{
			Name: "clean grouped labels",
			Content: `FROM alpine:3.20
LABEL org.opencontainers.image.title="demo" \
      org.opencontainers.image.source="https://github.com/example/demo"
`,
			WantViolations: 0,
		},
		{
			Name: "duplicate in same instruction",
			Content: `FROM alpine:3.20
LABEL org.opencontainers.image.title="demo" \
      org.opencontainers.image.title="demo2"
`,
			WantViolations: 1,
			WantMessages: []string{
				`label key "org.opencontainers.image.title" is overwritten later`,
			},
		},
		{
			Name: "duplicate across instructions with same value",
			Content: `FROM alpine:3.20
LABEL org.opencontainers.image.source="https://github.com/example/demo"
LABEL org.opencontainers.image.source="https://github.com/example/demo"
`,
			WantViolations: 1,
			WantMessages: []string{
				`label key "org.opencontainers.image.source" is repeated later with the same value`,
			},
		},
		{
			Name: "quoted key normalizes before duplicate check",
			Content: `FROM alpine:3.20
LABEL "org.opencontainers.image.title"="demo"
LABEL org.opencontainers.image.title="demo2"
`,
			WantViolations: 1,
		},
		{
			Name: "reports every redundant duplicate",
			Content: `FROM alpine:3.20
LABEL org.opencontainers.image.title="demo"
LABEL org.opencontainers.image.title="demo2"
LABEL org.opencontainers.image.title="demo3"
`,
			WantViolations: 2,
			WantMessages: []string{
				`label key "org.opencontainers.image.title" is overwritten later`,
				`label key "org.opencontainers.image.title" is overwritten later`,
			},
		},
		{
			Name: "same key in different stages is independent",
			Content: `FROM alpine:3.20 AS build
LABEL org.opencontainers.image.title="builder"

FROM alpine:3.20
LABEL org.opencontainers.image.title="runtime"
`,
			WantViolations: 0,
		},
		{
			Name: "dynamic keys are not grouped",
			Content: `FROM alpine:3.20
LABEL "$LABEL_PREFIX.name"="demo"
LABEL "$LABEL_PREFIX.name"="demo2"
`,
			WantViolations: 0,
		},
	})
}

func TestNoDuplicateKeysRule_ReportsEarlierIgnoredLabels(t *testing.T) {
	t.Parallel()

	input := testutil.MakeLintInput(t, "Dockerfile", `FROM alpine:3.20
LABEL org.opencontainers.image.title="demo"
LABEL org.opencontainers.image.title="demo2"
LABEL org.opencontainers.image.title="demo3"
`)

	violations := NewNoDuplicateKeysRule().Check(input)
	if len(violations) != 2 {
		t.Fatalf("got %d violations, want 2", len(violations))
	}

	if got := violations[0].Location.Start.Line; got != 2 {
		t.Errorf("violation[0] line = %d, want 2", got)
	}
	if got := violations[1].Location.Start.Line; got != 3 {
		t.Errorf("violation[1] line = %d, want 3", got)
	}
}

func TestNoDuplicateKeysRule_FixOptions(t *testing.T) {
	t.Parallel()

	content := `FROM alpine:3.20
LABEL org.opencontainers.image.title="demo"
LABEL org.opencontainers.image.title="demo2"
`
	input := testutil.MakeLintInput(t, "Dockerfile", content)

	violations := NewNoDuplicateKeysRule().Check(input)
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
	if commentFix.Safety != rules.FixSafe {
		t.Errorf("comment fix safety = %s, want safe", commentFix.Safety)
	}
	if commentFix.Priority != -1 {
		t.Errorf("comment fix priority = %d, want -1", commentFix.Priority)
	}

	gotCommented := string(fixpkg.ApplyFix([]byte(content), commentFix))
	wantCommented := "FROM alpine:3.20\n" +
		"# [commented out by tally - Docker keeps the last LABEL value for " +
		"org.opencontainers.image.title]: LABEL org.opencontainers.image.title=\"demo\"\n" +
		"LABEL org.opencontainers.image.title=\"demo2\"\n"
	if gotCommented != wantCommented {
		t.Errorf("comment fix mismatch\ngot:\n%s\nwant:\n%s", gotCommented, wantCommented)
	}

	deleteFix := allFixes[1]
	if deleteFix.IsPreferred {
		t.Fatal("delete fix should not be preferred")
	}
	if deleteFix.Safety != rules.FixSafe {
		t.Errorf("delete fix safety = %s, want safe", deleteFix.Safety)
	}

	gotDeleted := string(fixpkg.ApplyFix([]byte(content), deleteFix))
	wantDeleted := `FROM alpine:3.20
LABEL org.opencontainers.image.title="demo2"
`
	if gotDeleted != wantDeleted {
		t.Errorf("delete fix mismatch\ngot:\n%s\nwant:\n%s", gotDeleted, wantDeleted)
	}
}

func TestNoDuplicateKeysRule_FixAllPreviousLabels(t *testing.T) {
	t.Parallel()

	content := `FROM alpine:3.20
LABEL org.opencontainers.image.title="demo"
LABEL org.opencontainers.image.title="demo2"
LABEL org.opencontainers.image.title="demo3"
`
	input := testutil.MakeLintInput(t, "Dockerfile", content)

	violations := NewNoDuplicateKeysRule().Check(input)
	if len(violations) != 2 {
		t.Fatalf("got %d violations, want 2", len(violations))
	}

	var edits []rules.TextEdit
	for _, violation := range violations {
		fix := violation.PreferredFix()
		if fix == nil {
			t.Fatal("expected preferred fix")
		}
		edits = append(edits, fix.Edits...)
	}

	got := string(fixpkg.ApplyEdits([]byte(content), edits))
	want := "FROM alpine:3.20\n" +
		"# [commented out by tally - Docker keeps the last LABEL value for " +
		"org.opencontainers.image.title]: LABEL org.opencontainers.image.title=\"demo\"\n" +
		"# [commented out by tally - Docker keeps the last LABEL value for " +
		"org.opencontainers.image.title]: LABEL org.opencontainers.image.title=\"demo2\"\n" +
		"LABEL org.opencontainers.image.title=\"demo3\"\n"
	if got != want {
		t.Errorf("fix-all previous labels mismatch\ngot:\n%s\nwant:\n%s", got, want)
	}
}
