package labels

import (
	"testing"

	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/testutil"
)

func TestValidKeyRule_Metadata(t *testing.T) {
	t.Parallel()

	meta := NewValidKeyRule().Metadata()
	if meta.Code != ValidKeyRuleCode {
		t.Fatalf("Code = %q, want %q", meta.Code, ValidKeyRuleCode)
	}
	if meta.DefaultSeverity != rules.SeverityWarning {
		t.Fatalf("DefaultSeverity = %s, want warning", meta.DefaultSeverity)
	}
	if meta.Category != "correctness" {
		t.Fatalf("Category = %q, want correctness", meta.Category)
	}
}

func TestValidKeyRule_Check(t *testing.T) {
	t.Parallel()

	testutil.RunRuleTests(t, NewValidKeyRule(), []testutil.RuleTestCase{
		{
			Name: "valid OCI and unqualified legacy keys",
			Content: `FROM alpine:3.20
LABEL org.opencontainers.image.title="demo" \
      version="1.0" \
      maintainer="ops@example.com"
`,
			WantViolations: 0,
		},
		{
			Name: "quoted valid key",
			Content: `FROM alpine:3.20
LABEL "com.example.vendor"="ACME Incorporated"
`,
			WantViolations: 0,
		},
		{
			Name: "key contains whitespace",
			Content: `FROM alpine:3.20
LABEL "bad key"=value
`,
			WantViolations: 1,
			WantMessages:   []string{"contains whitespace"},
		},
		{
			Name: "key contains unsupported punctuation",
			Content: `FROM alpine:3.20
LABEL bad/key=value
`,
			WantViolations: 1,
			WantMessages:   []string{`contains '/'`},
		},
		{
			Name: "uppercase key",
			Content: `FROM alpine:3.20
LABEL Bad.Key=value
`,
			WantViolations: 1,
			WantMessages:   []string{"uses uppercase characters"},
		},
		{
			Name: "key starts with punctuation",
			Content: `FROM alpine:3.20
LABEL .bad=value
`,
			WantViolations: 1,
			WantMessages:   []string{"must start and end"},
		},
		{
			Name: "repeated separators",
			Content: `FROM alpine:3.20
LABEL com..example.name=value
`,
			WantViolations: 1,
			WantMessages:   []string{"contains repeated separators"},
		},
		{
			Name: "reserved docker namespace",
			Content: `FROM alpine:3.20
LABEL com.docker.compose.project=demo
`,
			WantViolations: 1,
			WantMessages:   []string{"uses a Docker-reserved namespace"},
		},
		{
			Name: "known docker namespace keys are allowed",
			Content: `FROM alpine:3.20
LABEL com.docker.image.source.entrypoint=Dockerfile \
      com.docker.extension.publisher-url="https://example.com"
`,
			WantViolations: 0,
		},
		{
			Name: "dynamic key reports informational diagnostic",
			Content: `FROM alpine:3.20
LABEL "$LABEL_PREFIX.name"=demo
`,
			WantViolations: 1,
			WantMessages:   []string{"uses variable expansion"},
		},
		{
			Name: "legacy key value format is owned by BuildKit rule",
			Content: `FROM alpine:3.20
LABEL key value with spaces
`,
			WantViolations: 0,
		},
	})
}

func TestValidKeyRule_DynamicKeySeverity(t *testing.T) {
	t.Parallel()

	input := testutil.MakeLintInput(t, "Dockerfile", `FROM alpine:3.20
LABEL "$LABEL_PREFIX.name"=demo
`)
	violations := NewValidKeyRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("violations count = %d, want 1", len(violations))
	}
	if violations[0].Severity != rules.SeverityInfo {
		t.Fatalf("dynamic key severity = %s, want info", violations[0].Severity)
	}
}
