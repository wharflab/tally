package tally

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"

	fixpkg "github.com/wharflab/tally/internal/fix"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/testutil"
)

type telemetryBuildContext struct {
	files map[string]string
}

func (m *telemetryBuildContext) IsIgnored(string) (bool, error) { return false, nil }

func (m *telemetryBuildContext) FileExists(path string) bool {
	_, ok := m.files[path]
	return ok
}

func (m *telemetryBuildContext) ReadFile(path string) ([]byte, error) {
	content, ok := m.files[path]
	if !ok {
		return nil, fmt.Errorf("missing file %q", path)
	}
	return []byte(content), nil
}

func (m *telemetryBuildContext) IsHeredocFile(string) bool { return false }

func (m *telemetryBuildContext) HasIgnoreFile() bool { return false }

func (m *telemetryBuildContext) HasIgnoreExclusions() bool { return false }

func TestPreferTelemetryOptOutRule_Metadata(t *testing.T) {
	t.Parallel()
	snaps.MatchStandaloneJSON(t, NewPreferTelemetryOptOutRule().Metadata())
}

func TestPreferTelemetryOptOutRule_Check(t *testing.T) {
	t.Parallel()

	rule := NewPreferTelemetryOptOutRule()
	tests := []struct {
		name              string
		content           string
		contextFiles      map[string]string
		wantViolations    int
		wantMessageSubstr string
		wantDetailSubstr  string
	}{
		{
			name: "bun and next collapse into one stage violation",
			content: `FROM node:22
RUN bun install && next build
`,
			wantViolations:    1,
			wantMessageSubstr: "stage uses tools",
			wantDetailSubstr:  "DO_NOT_TRACK=1, NEXT_TELEMETRY_DISABLED=1",
		},
		{
			name: "existing env suppresses bun",
			content: `FROM node:22
ENV DO_NOT_TRACK=1
RUN bun install
`,
			wantViolations: 0,
		},
		{
			name: "child stage inherits parent env",
			content: `FROM node:22 AS base
ENV DO_NOT_TRACK=1
RUN bun install
FROM base
RUN bun install
`,
			wantViolations: 0,
		},
		{
			name: "child stage only reports extra missing tool",
			content: `FROM node:22 AS base
ENV DO_NOT_TRACK=1
RUN bun install
FROM base
RUN next build
`,
			wantViolations:    1,
			wantMessageSubstr: "Next.js",
			wantDetailSubstr:  "NEXT_TELEMETRY_DISABLED=1",
		},
		{
			name: "windows powershell and vcpkg share one violation",
			content: "# escape=`\n" + `FROM mcr.microsoft.com/windows/servercore:ltsc2022
RUN powershell -Command Write-Host hi
RUN bootstrap-vcpkg.bat
`,
			wantViolations:    1,
			wantMessageSubstr: "stage uses tools",
			wantDetailSubstr:  "POWERSHELL_TELEMETRY_OPTOUT=1, VCPKG_DISABLE_METRICS=1",
		},
		{
			name: "hugging face manifest plus pip install from requirements",
			content: `FROM python:3.12
WORKDIR /app
COPY requirements.txt ./requirements.txt
RUN pip install -r requirements.txt
`,
			contextFiles: map[string]string{
				"requirements.txt": "transformers==4.49.0\n",
			},
			wantViolations:    1,
			wantMessageSubstr: "Hugging Face Python ecosystem",
			wantDetailSubstr:  "HF_HUB_DISABLE_TELEMETRY=1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var ctx *telemetryBuildContext
			if len(tt.contextFiles) > 0 {
				ctx = &telemetryBuildContext{files: tt.contextFiles}
			}

			input := testutil.MakeLintInputWithContext(t, "Dockerfile", tt.content, ctx)
			violations := rule.Check(input)
			if len(violations) != tt.wantViolations {
				t.Fatalf("got %d violations, want %d", len(violations), tt.wantViolations)
			}
			if len(violations) == 0 {
				return
			}

			if violations[0].RuleCode != PreferTelemetryOptOutRuleCode {
				t.Fatalf("rule code = %q, want %q", violations[0].RuleCode, PreferTelemetryOptOutRuleCode)
			}
			if tt.wantMessageSubstr != "" && !strings.Contains(violations[0].Message, tt.wantMessageSubstr) {
				t.Fatalf("message = %q, want substring %q", violations[0].Message, tt.wantMessageSubstr)
			}
			if tt.wantDetailSubstr != "" && !strings.Contains(violations[0].Detail, tt.wantDetailSubstr) {
				t.Fatalf("detail = %q, want substring %q", violations[0].Detail, tt.wantDetailSubstr)
			}
			if violations[0].SuggestedFix == nil {
				t.Fatal("expected suggested fix")
			}
		})
	}
}

func TestPreferTelemetryOptOutRule_FixPreservesCommentBlock(t *testing.T) {
	t.Parallel()

	rule := NewPreferTelemetryOptOutRule()
	const content = `FROM node:22
# install bun deps
RUN bun install
`

	input := testutil.MakeLintInput(t, "Dockerfile", content)
	violations := rule.Check(input)
	if len(violations) != 1 {
		t.Fatalf("got %d violations, want 1", len(violations))
	}

	fix := violations[0].SuggestedFix
	if fix == nil {
		t.Fatal("expected suggested fix")
	}
	if fix.Safety != rules.FixSuggestion {
		t.Fatalf("fix safety = %v, want %v", fix.Safety, rules.FixSuggestion)
	}

	result, err := (&fixpkg.Fixer{SafetyThreshold: rules.FixSuggestion}).Apply(
		context.Background(),
		violations,
		map[string][]byte{"Dockerfile": []byte(content)},
	)
	if err != nil {
		t.Fatalf("apply fixes: %v", err)
	}

	got := string(result.Changes["Dockerfile"].ModifiedContent)
	want := `FROM node:22
# [tally] settings to opt out from telemetry
ENV DO_NOT_TRACK=1
# install bun deps
RUN bun install
`
	if got != want {
		t.Fatalf("fixed content =\n%s\nwant:\n%s", got, want)
	}
}

func TestPreferTelemetryOptOutRule_CheckWithoutFacts(t *testing.T) {
	t.Parallel()

	rule := NewPreferTelemetryOptOutRule()
	const content = `FROM oven/bun:1
RUN bun install
`

	input := testutil.MakeLintInput(t, "Dockerfile", content)
	input.Facts = nil

	violations := rule.Check(input)
	if len(violations) != 1 {
		t.Fatalf("got %d violations, want 1", len(violations))
	}
	if !strings.Contains(violations[0].Detail, "DO_NOT_TRACK=1") {
		t.Fatalf("detail = %q, want DO_NOT_TRACK=1", violations[0].Detail)
	}
}

func TestPreferTelemetryOptOutRule_FixInsertsTelemetryBlockAtStageTop(t *testing.T) {
	t.Parallel()

	rule := NewPreferTelemetryOptOutRule()
	const content = `FROM node:22
COPY <<EOF /app/package.json
{"dependencies":{"next":"15.0.0"}}
EOF
RUN npm run build
`

	input := testutil.MakeLintInput(t, "Dockerfile", content)
	violations := rule.Check(input)
	if len(violations) != 1 {
		t.Fatalf("got %d violations, want 1", len(violations))
	}
	if violations[0].SuggestedFix == nil {
		t.Fatal("expected suggested fix")
	}

	result, err := (&fixpkg.Fixer{SafetyThreshold: rules.FixSuggestion}).Apply(
		context.Background(),
		violations,
		map[string][]byte{"Dockerfile": []byte(content)},
	)
	if err != nil {
		t.Fatalf("apply fixes: %v", err)
	}

	got := string(result.Changes["Dockerfile"].ModifiedContent)
	want := `FROM node:22
# [tally] settings to opt out from telemetry
ENV NEXT_TELEMETRY_DISABLED=1
COPY <<EOF /app/package.json
{"dependencies":{"next":"15.0.0"}}
EOF
RUN npm run build
`
	if got != want {
		t.Fatalf("fixed content =\n%s\nwant:\n%s", got, want)
	}
}

func TestPreferTelemetryOptOutRule_FixKeepsLeadingStageArgsAheadOfTelemetryBlock(t *testing.T) {
	t.Parallel()

	rule := NewPreferTelemetryOptOutRule()
	const content = `FROM node:22
ARG TARGETARCH
RUN bun install
`

	input := testutil.MakeLintInput(t, "Dockerfile", content)
	violations := rule.Check(input)
	if len(violations) != 1 {
		t.Fatalf("got %d violations, want 1", len(violations))
	}
	if violations[0].SuggestedFix == nil {
		t.Fatal("expected suggested fix")
	}

	result, err := (&fixpkg.Fixer{SafetyThreshold: rules.FixSuggestion}).Apply(
		context.Background(),
		violations,
		map[string][]byte{"Dockerfile": []byte(content)},
	)
	if err != nil {
		t.Fatalf("apply fixes: %v", err)
	}

	got := string(result.Changes["Dockerfile"].ModifiedContent)
	want := `FROM node:22
ARG TARGETARCH
# [tally] settings to opt out from telemetry
ENV DO_NOT_TRACK=1
RUN bun install
`
	if got != want {
		t.Fatalf("fixed content =\n%s\nwant:\n%s", got, want)
	}
}

func TestPreferTelemetryOptOutRule_FixPreservesStageIndentation(t *testing.T) {
	t.Parallel()

	rule := NewPreferTelemetryOptOutRule()
	const content = `FROM alpine:3.20 AS base
	RUN echo base
FROM node:22 AS build
	RUN bun install
`

	input := testutil.MakeLintInput(t, "Dockerfile", content)
	violations := rule.Check(input)
	if len(violations) != 1 {
		t.Fatalf("got %d violations, want 1", len(violations))
	}
	if violations[0].SuggestedFix == nil {
		t.Fatal("expected suggested fix")
	}

	result, err := (&fixpkg.Fixer{SafetyThreshold: rules.FixSuggestion}).Apply(
		context.Background(),
		violations,
		map[string][]byte{"Dockerfile": []byte(content)},
	)
	if err != nil {
		t.Fatalf("apply fixes: %v", err)
	}

	got := string(result.Changes["Dockerfile"].ModifiedContent)
	want := `FROM alpine:3.20 AS base
	RUN echo base
FROM node:22 AS build
	# [tally] settings to opt out from telemetry
	ENV DO_NOT_TRACK=1
	RUN bun install
`
	if got != want {
		t.Fatalf("fixed content =\n%s\nwant:\n%s", got, want)
	}
}
