package tally

import (
	"testing"

	"github.com/tinovyatkin/tally/internal/rules"
	"github.com/tinovyatkin/tally/internal/testutil"
)

func TestConsistentIndentationMetadata(t *testing.T) {
	r := NewConsistentIndentationRule()
	meta := r.Metadata()

	if meta.Code != "tally/consistent-indentation" {
		t.Errorf("Code = %q, want %q", meta.Code, "tally/consistent-indentation")
	}
	if meta.Category != "style" {
		t.Errorf("Category = %q, want %q", meta.Category, "style")
	}
	if meta.DefaultSeverity != rules.SeverityOff {
		t.Errorf("DefaultSeverity = %v, want %v (off by default, opt-in via config)", meta.DefaultSeverity, rules.SeverityOff)
	}
	if !meta.IsExperimental {
		t.Error("IsExperimental = false, want true")
	}
	if meta.FixPriority != 50 {
		t.Errorf("FixPriority = %d, want 50", meta.FixPriority)
	}
}

func TestConsistentIndentationCheck(t *testing.T) {
	testutil.RunRuleTests(t, NewConsistentIndentationRule(), []testutil.RuleTestCase{
		// === Single-stage Dockerfiles: no indentation expected ===
		{
			Name:           "single stage no indent - pass",
			Content:        "FROM alpine\nRUN echo hello\nCOPY . /app\n",
			WantViolations: 0,
		},
		{
			Name:           "single stage with unwanted tab indent",
			Content:        "FROM alpine\n\tRUN echo hello\n\tCOPY . /app\n",
			WantViolations: 2,
			WantMessages:   []string{"unexpected indentation", "unexpected indentation"},
		},
		{
			Name:           "single stage with unwanted space indent",
			Content:        "FROM alpine\n  RUN echo hello\n",
			WantViolations: 1,
			WantMessages:   []string{"unexpected indentation"},
		},
		{
			Name:           "single stage indented FROM",
			Content:        "  FROM alpine\nRUN echo hello\n",
			WantViolations: 1,
			WantMessages:   []string{"unexpected indentation"},
		},

		// === Multi-stage Dockerfiles: commands should be indented with 1 tab ===
		{
			Name:           "multi-stage properly indented with tabs",
			Content:        "FROM alpine AS builder\n\tRUN echo build\nFROM scratch\n\tCOPY --from=builder /app /app\n",
			WantViolations: 0,
		},
		{
			Name:           "multi-stage no indent on commands",
			Content:        "FROM alpine AS builder\nRUN echo build\nFROM scratch\nCOPY --from=builder /app /app\n",
			WantViolations: 2,
			WantMessages:   []string{"missing indentation", "missing indentation"},
		},
		{
			Name:           "multi-stage wrong indent style (spaces instead of tabs)",
			Content:        "FROM alpine AS builder\n  RUN echo build\nFROM scratch\n  COPY --from=builder /app /app\n",
			WantViolations: 2,
			WantMessages:   []string{"wrong indentation style", "wrong indentation style"},
		},
		{
			Name:           "multi-stage indented FROM",
			Content:        "\tFROM alpine AS builder\n\tRUN echo build\nFROM scratch\n\tCOPY --from=builder /app /app\n",
			WantViolations: 1,
			WantMessages:   []string{"unexpected indentation"},
		},
		{
			Name:           "multi-stage mixed - some indented some not",
			Content:        "FROM alpine AS builder\n\tRUN echo build\nRUN echo test\nFROM scratch\nCOPY --from=builder /app /app\n",
			WantViolations: 2, // RUN test and COPY missing indent
			WantMessages:   []string{"missing indentation", "missing indentation"},
		},

		// === Edge cases ===
		{
			Name:           "empty file",
			Content:        "FROM scratch\n",
			WantViolations: 0,
		},
		{
			Name: "multi-stage three stages",
			Content: "FROM alpine AS deps\n\tRUN apk add curl\n" +
				"FROM golang AS build\n\tRUN go build\n" +
				"FROM scratch\n\tCOPY --from=build /app /app\n",
			WantViolations: 0,
		},
		{
			Name:           "global ARG before FROM should not be indented",
			Content:        "ARG BASE=alpine\nFROM ${BASE} AS builder\n\tRUN echo build\nFROM scratch\n\tCOPY --from=builder /app /app\n",
			WantViolations: 0,
		},
		{
			Name:           "indented global ARG",
			Content:        "\tARG BASE=alpine\nFROM ${BASE} AS builder\n\tRUN echo build\nFROM scratch\n\tCOPY --from=builder /app /app\n",
			WantViolations: 1,
			WantMessages:   []string{"unexpected indentation"},
		},

		// === Continuation line coverage ===
		{
			Name: "multi-stage continuation lines missing indent",
			Content: "FROM alpine AS builder\n\tRUN --mount=type=secret \\\n" +
				"--mount=type=cache \\\npip install\n" +
				"FROM scratch\n\tCOPY --from=builder /app /app\n",
			WantViolations: 1,
			WantMessages:   []string{"missing indentation"},
		},
		{
			Name:           "single stage continuation lines with indent",
			Content:        "FROM alpine\nRUN echo hello \\\n\techo world\n",
			WantViolations: 1,
			WantMessages:   []string{"unexpected indentation"},
		},

		// === Heredoc cases ===
		{
			Name: "multi-stage heredoc properly indented",
			Content: "FROM alpine AS builder\n\tRUN <<-EOF\n" +
				"\t\techo hello\n\tEOF\n" +
				"FROM scratch\n\tCOPY --from=builder /app /app\n",
			WantViolations: 0,
		},
		{
			Name: "multi-stage heredoc missing indent",
			Content: "FROM alpine AS builder\nRUN <<EOF\n" +
				"echo hello\nEOF\n" +
				"FROM scratch\n\tCOPY --from=builder /app /app\n",
			WantViolations: 1,
			WantMessages:   []string{"missing indentation"},
		},
	})
}

func TestConsistentIndentationCheckWithFixes(t *testing.T) {
	r := NewConsistentIndentationRule()

	tests := []struct {
		name      string
		content   string
		wantEdits int // total text edits across all violations
	}{
		{
			name:      "single stage remove tab indent",
			content:   "FROM alpine\n\tRUN echo hello\n",
			wantEdits: 1,
		},
		{
			name:      "single stage remove space indent",
			content:   "FROM alpine\n    RUN echo hello\n    COPY . /app\n",
			wantEdits: 2,
		},
		{
			name:      "multi-stage add indent",
			content:   "FROM alpine AS builder\nRUN echo build\nFROM scratch\nCOPY --from=builder /app /app\n",
			wantEdits: 2,
		},
		{
			name:      "multi-stage fix indent style (spaces to tab)",
			content:   "FROM alpine AS builder\n  RUN echo build\nFROM scratch\n",
			wantEdits: 1,
		},
		{
			name: "multi-stage heredoc adds indent and converts to <<-",
			content: "FROM alpine AS builder\nRUN <<EOF\n" +
				"echo hello\nEOF\nFROM scratch\n",
			wantEdits: 2, // 1 indent edit + 1 <<- conversion edit
		},
		{
			name: "heredoc on continuation line converts to <<-",
			content: "FROM alpine AS builder\nRUN cmd && \\\n" +
				"cat <<EOF\nhello\nEOF\nFROM scratch\n",
			wantEdits: 3, // 2 indent edits (lines 2-3) + 1 <<- conversion
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := testutil.MakeLintInputWithConfig(t, "Dockerfile", tt.content, nil)
			violations := r.Check(input)

			totalEdits := 0
			for _, v := range violations {
				if v.SuggestedFix == nil {
					t.Error("violation has no SuggestedFix")
					continue
				}
				if v.SuggestedFix.Safety != rules.FixSafe {
					t.Errorf("fix safety = %v, want FixSafe", v.SuggestedFix.Safety)
				}
				totalEdits += len(v.SuggestedFix.Edits)
			}

			if totalEdits != tt.wantEdits {
				t.Errorf("total edits = %d, want %d", totalEdits, tt.wantEdits)
			}
		})
	}
}

func TestDescribeIndent(t *testing.T) {
	tests := []struct {
		indent string
		want   string
	}{
		{"", "no indentation"},
		{"\t", "1 tab"},
		{"\t\t", "2 tabs"},
		{" ", "1 space"},
		{"  ", "2 spaces"},
		{"    ", "4 spaces"},
		{"\t ", "2 mixed characters"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := describeIndent(tt.indent)
			if got != tt.want {
				t.Errorf("describeIndent(%q) = %q, want %q", tt.indent, got, tt.want)
			}
		})
	}
}

func TestLeadingWhitespace(t *testing.T) {
	tests := []struct {
		line string
		want string
	}{
		{"RUN echo", ""},
		{"\tRUN echo", "\t"},
		{"  RUN echo", "  "},
		{"\t\tRUN echo", "\t\t"},
		{"    RUN echo", "    "},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.line, func(t *testing.T) {
			got := leadingWhitespace(tt.line)
			if got != tt.want {
				t.Errorf("leadingWhitespace(%q) = %q, want %q", tt.line, got, tt.want)
			}
		})
	}
}
