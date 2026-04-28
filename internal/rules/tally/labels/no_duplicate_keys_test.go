package labels

import (
	"testing"

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
				`label key "org.opencontainers.image.title" is set more than once`,
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
				`label key "org.opencontainers.image.source" repeats the same value`,
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
