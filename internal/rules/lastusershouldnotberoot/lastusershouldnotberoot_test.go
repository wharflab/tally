package lastusershouldnotberoot

import (
	"testing"

	"github.com/tinovyatkin/tally/internal/rules"
	"github.com/tinovyatkin/tally/internal/testutil"
)

func TestRule_Metadata(t *testing.T) {
	r := New()
	meta := r.Metadata()

	if meta.Code != rules.HadolintRulePrefix+"DL3002" {
		t.Errorf("Code = %q, want %q", meta.Code, rules.HadolintRulePrefix+"DL3002")
	}
	if meta.DefaultSeverity != rules.SeverityWarning {
		t.Errorf("DefaultSeverity = %v, want %v", meta.DefaultSeverity, rules.SeverityWarning)
	}
	if meta.Category != "security" {
		t.Errorf("Category = %q, want %q", meta.Category, "security")
	}
}

func TestRule_Check(t *testing.T) {
	tests := []struct {
		name       string
		dockerfile string
		wantCount  int
		wantCode   string
	}{
		{
			name: "last USER is root by name",
			dockerfile: `FROM ubuntu:22.04
USER root
`,
			wantCount: 1,
			wantCode:  rules.HadolintRulePrefix + "DL3002",
		},
		{
			name: "last USER is root by UID",
			dockerfile: `FROM ubuntu:22.04
USER 0
`,
			wantCount: 1,
		},
		{
			name: "last USER is root with group",
			dockerfile: `FROM ubuntu:22.04
USER root:root
`,
			wantCount: 1,
		},
		{
			name: "last USER is root UID with GID",
			dockerfile: `FROM ubuntu:22.04
USER 0:0
`,
			wantCount: 1,
		},
		{
			name: "last USER is non-root",
			dockerfile: `FROM ubuntu:22.04
RUN useradd -m appuser
USER appuser
`,
			wantCount: 0,
		},
		{
			name: "switches to root then back to non-root",
			dockerfile: `FROM ubuntu:22.04
RUN useradd -m appuser
USER root
RUN apt-get update
USER appuser
`,
			wantCount: 0, // Final user is appuser, not root
		},
		{
			name: "switches to non-root then back to root",
			dockerfile: `FROM ubuntu:22.04
RUN useradd -m appuser
USER appuser
USER root
`,
			wantCount: 1, // Final user is root
		},
		{
			name: "no USER instruction",
			dockerfile: `FROM ubuntu:22.04
RUN echo hello
`,
			wantCount: 0, // No USER instruction, no violation (default behavior)
		},
		{
			name: "numeric non-root UID",
			dockerfile: `FROM ubuntu:22.04
USER 1000
`,
			wantCount: 0,
		},
		{
			name: "non-root with group",
			dockerfile: `FROM ubuntu:22.04
USER appuser:appgroup
`,
			wantCount: 0,
		},
		{
			name: "case insensitive root check",
			dockerfile: `FROM ubuntu:22.04
USER ROOT
`,
			wantCount: 1,
		},
		{
			name: "multi-stage only checks final stage",
			dockerfile: `FROM ubuntu:22.04 AS builder
USER root
RUN make build

FROM alpine:3.18
RUN adduser -D appuser
USER appuser
COPY --from=builder /app/bin /app/bin
`,
			wantCount: 0, // Final stage ends with appuser
		},
		{
			name: "multi-stage final stage has root",
			dockerfile: `FROM ubuntu:22.04 AS builder
USER appuser
RUN make build

FROM alpine:3.18
USER root
`,
			wantCount: 1, // Final stage ends with root
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := testutil.MakeLintInput(t, "Dockerfile", tt.dockerfile)

			r := New()
			violations := r.Check(input)

			if len(violations) != tt.wantCount {
				t.Errorf("got %d violations, want %d", len(violations), tt.wantCount)
				for _, v := range violations {
					t.Logf("  - %s: %s", v.RuleCode, v.Message)
				}
			}

			if tt.wantCode != "" && len(violations) > 0 {
				if violations[0].RuleCode != tt.wantCode {
					t.Errorf("RuleCode = %q, want %q", violations[0].RuleCode, tt.wantCode)
				}
			}
		})
	}
}

func TestIsRootUser(t *testing.T) {
	tests := []struct {
		user string
		want bool
	}{
		{"root", true},
		{"ROOT", true},
		{"Root", true},
		{"0", true},
		{"root:root", true},
		{"0:0", true},
		{"root:wheel", true},
		{"0:wheel", true},
		{"appuser", false},
		{"1000", false},
		{"appuser:appgroup", false},
		{"1000:1000", false},
		{"  root  ", true},   // whitespace trimmed
		{"nobody", false},
		{"www-data", false},
	}

	for _, tt := range tests {
		t.Run(tt.user, func(t *testing.T) {
			got := isRootUser(tt.user)
			if got != tt.want {
				t.Errorf("isRootUser(%q) = %v, want %v", tt.user, got, tt.want)
			}
		})
	}
}
