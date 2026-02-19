package tally

import (
	"strings"
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/testutil"
)

func TestEpilogueOrderMetadata(t *testing.T) {
	t.Parallel()
	snaps.MatchStandaloneJSON(t, NewEpilogueOrderRule().Metadata())
}

func TestEpilogueOrderCheck(t *testing.T) {
	t.Parallel()
	r := NewEpilogueOrderRule()

	tests := []struct {
		name           string
		content        string
		wantViolations int
		wantMessages   []string
	}{
		{
			name: "correct order - no violation",
			content: `FROM alpine:3.20
RUN echo hello
ENTRYPOINT ["/app"]
CMD ["serve"]
`,
			wantViolations: 0,
		},
		{
			name: "full correct order - no violation",
			content: `FROM alpine:3.20
RUN echo hello
STOPSIGNAL SIGTERM
HEALTHCHECK CMD curl -f http://localhost/
ENTRYPOINT ["/app"]
CMD ["serve"]
`,
			wantViolations: 0,
		},
		{
			name: "wrong order - CMD before ENTRYPOINT",
			content: `FROM alpine:3.20
RUN echo hello
CMD ["serve"]
ENTRYPOINT ["/app"]
`,
			wantViolations: 1,
			wantMessages:   []string{"epilogue instructions should appear at the end"},
		},
		{
			name: "epilogue scattered among build instructions",
			content: `FROM alpine:3.20
CMD ["serve"]
RUN echo hello
ENTRYPOINT ["/app"]
`,
			wantViolations: 1,
			wantMessages:   []string{"epilogue instructions should appear at the end"},
		},
		{
			name: "single epilogue at wrong position",
			content: `FROM alpine:3.20
CMD ["serve"]
RUN echo hello
`,
			wantViolations: 1,
		},
		{
			name: "multi-stage - builder stage skipped",
			content: `FROM golang:1.21 AS builder
RUN go build -o /app
CMD ["build"]

FROM alpine:3.20
COPY --from=builder /app /app
ENTRYPOINT ["/app"]
CMD ["serve"]
`,
			wantViolations: 0,
		},
		{
			name: "multi-stage - final stage has wrong order",
			content: `FROM golang:1.21 AS builder
RUN go build -o /app

FROM alpine:3.20
COPY --from=builder /app /app
CMD ["serve"]
ENTRYPOINT ["/app"]
`,
			wantViolations: 1,
		},
		{
			name: "healthcheck none - still moved",
			content: `FROM alpine:3.20
HEALTHCHECK NONE
RUN echo hello
CMD ["serve"]
`,
			wantViolations: 1,
		},
		{
			name: "onbuild cmd - not moved",
			content: `FROM alpine:3.20
RUN echo hello
ONBUILD CMD ["serve"]
`,
			wantViolations: 0,
		},
		{
			name: "only epilogue instructions - correct order",
			content: `FROM alpine:3.20
ENTRYPOINT ["/app"]
CMD ["serve"]
`,
			wantViolations: 0,
		},
		{
			name: "only epilogue instructions - wrong order",
			content: `FROM alpine:3.20
CMD ["serve"]
ENTRYPOINT ["/app"]
`,
			wantViolations: 1,
		},
		{
			name: "duplicate instructions at end in order - no violation",
			content: `FROM alpine:3.20
RUN echo hello
CMD ["first"]
CMD ["second"]
`,
			wantViolations: 0,
		},
		{
			name: "duplicate instructions out of position - violation but no fix",
			content: `FROM alpine:3.20
CMD ["first"]
RUN echo hello
CMD ["second"]
`,
			wantViolations: 1,
		},
		{
			name: "missing some epilogue - only CMD + ENTRYPOINT",
			content: `FROM alpine:3.20
RUN echo hello
ENTRYPOINT ["/app"]
CMD ["serve"]
`,
			wantViolations: 0,
		},
		{
			name: "missing some epilogue - wrong order",
			content: `FROM alpine:3.20
RUN echo hello
CMD ["serve"]
ENTRYPOINT ["/app"]
`,
			wantViolations: 1,
		},
		{
			name: "no epilogue instructions - no violation",
			content: `FROM alpine:3.20
RUN echo hello
COPY . /app
`,
			wantViolations: 0,
		},
		{
			name: "single instruction stage - no violation",
			content: `FROM alpine:3.20
CMD ["serve"]
`,
			wantViolations: 0,
		},
		{
			name: "stopsignal after cmd",
			content: `FROM alpine:3.20
RUN echo hello
CMD ["serve"]
STOPSIGNAL SIGTERM
`,
			wantViolations: 1,
		},
		{
			name: "no semantic model - no violation",
			content: `FROM alpine:3.20
CMD ["serve"]
RUN echo hello
`,
			wantViolations: -1, // skip count check - tested separately
		},
		{
			name: "multi-stage - independent stages both checked",
			content: `FROM alpine:3.20 AS app1
CMD ["first"]
RUN echo hello

FROM alpine:3.20 AS app2
CMD ["second"]
RUN echo world
`,
			wantViolations: 2,
		},
		{
			name: "multi-stage - mount-from stage skipped",
			content: `FROM alpine:3.20 AS deps
RUN apk add --no-cache curl
CMD ["serve"]
RUN echo hello

FROM alpine:3.20
RUN --mount=type=bind,from=deps,source=/usr/bin/curl,target=/usr/bin/curl echo ok
ENTRYPOINT ["/app"]
CMD ["serve"]
`,
			wantViolations: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var input rules.LintInput
			if tt.name == "no semantic model - no violation" {
				input = testutil.MakeLintInput(t, "Dockerfile", tt.content)
			} else {
				input = testutil.MakeLintInputWithSemantic(t, "Dockerfile", tt.content)
			}

			violations := r.Check(input)

			if tt.wantViolations >= 0 && len(violations) != tt.wantViolations {
				t.Errorf("got %d violations, want %d", len(violations), tt.wantViolations)
				for i, v := range violations {
					t.Logf("  [%d] %s: %s (line %d)", i, v.RuleCode, v.Message, v.Line())
				}
			}

			for i, msg := range tt.wantMessages {
				if i >= len(violations) {
					t.Errorf("expected violation[%d] with message containing %q, got only %d violations",
						i, msg, len(violations))
					continue
				}
				if !strings.Contains(violations[i].Message, msg) {
					t.Errorf("violation[%d].Message = %q, want substring %q",
						i, violations[i].Message, msg)
				}
			}
		})
	}
}

func TestEpilogueOrderCheckNoSemanticModel(t *testing.T) {
	t.Parallel()
	r := NewEpilogueOrderRule()
	input := testutil.MakeLintInput(t, "Dockerfile", `FROM alpine:3.20
CMD ["serve"]
RUN echo hello
`)
	violations := r.Check(input)
	if len(violations) != 0 {
		t.Errorf("expected 0 violations when semantic model is nil, got %d", len(violations))
	}
}

func TestEpilogueOrderCheckWithFixes(t *testing.T) {
	t.Parallel()
	r := NewEpilogueOrderRule()

	tests := []struct {
		name           string
		content        string
		wantViolations int
		wantFix        bool
	}{
		{
			name: "wrong order - has fix",
			content: `FROM alpine:3.20
RUN echo hello
CMD ["serve"]
ENTRYPOINT ["/app"]
`,
			wantViolations: 1,
			wantFix:        true,
		},
		{
			name: "scattered - has fix",
			content: `FROM alpine:3.20
CMD ["serve"]
RUN echo hello
ENTRYPOINT ["/app"]
`,
			wantViolations: 1,
			wantFix:        true,
		},
		{
			name: "duplicates out of position - no fix",
			content: `FROM alpine:3.20
CMD ["first"]
RUN echo hello
CMD ["second"]
`,
			wantViolations: 1,
			wantFix:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			input := testutil.MakeLintInputWithSemantic(t, "Dockerfile", tt.content)
			violations := r.Check(input)

			if len(violations) != tt.wantViolations {
				t.Fatalf("violations = %d, want %d", len(violations), tt.wantViolations)
			}

			for _, v := range violations {
				if tt.wantFix {
					if v.SuggestedFix == nil {
						t.Error("expected SuggestedFix, got nil")
						continue
					}
					if v.SuggestedFix.Safety != rules.FixSafe {
						t.Errorf("fix safety = %v, want FixSafe", v.SuggestedFix.Safety)
					}
					if !v.SuggestedFix.NeedsResolve {
						t.Error("expected NeedsResolve=true")
					}
					if v.SuggestedFix.ResolverID != rules.EpilogueOrderResolverID {
						t.Errorf("ResolverID = %q, want %q",
							v.SuggestedFix.ResolverID, rules.EpilogueOrderResolverID)
					}
					if v.SuggestedFix.Priority != 175 {
						t.Errorf("Priority = %d, want 175", v.SuggestedFix.Priority)
					}
				} else if v.SuggestedFix != nil {
					t.Error("expected no SuggestedFix for duplicate case")
				}
			}
		})
	}
}
