package js

import (
	"context"
	"strings"
	"testing"

	"github.com/wharflab/tally/internal/facts"
	fixpkg "github.com/wharflab/tally/internal/fix"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/testutil"
)

type nodeGypBuildContext struct {
	files map[string]string
}

func (c *nodeGypBuildContext) FileExists(path string) bool {
	_, ok := c.files[path]
	return ok
}

func (c *nodeGypBuildContext) ReadFile(path string) ([]byte, error) {
	return []byte(c.files[path]), nil
}

func (c *nodeGypBuildContext) IsIgnored(string) (bool, error) { return false, nil }
func (c *nodeGypBuildContext) IsHeredocFile(string) bool      { return false }

func TestNodeGypCacheMountsRule_Metadata(t *testing.T) {
	t.Parallel()

	meta := NewNodeGypCacheMountsRule().Metadata()
	if meta.Code != NodeGypCacheMountsRuleCode {
		t.Fatalf("Code = %q, want %q", meta.Code, NodeGypCacheMountsRuleCode)
	}
	if meta.DefaultSeverity != rules.SeverityInfo {
		t.Fatalf("DefaultSeverity = %v, want %v", meta.DefaultSeverity, rules.SeverityInfo)
	}
	if meta.Category != "performance" {
		t.Fatalf("Category = %q, want performance", meta.Category)
	}
	if meta.FixPriority != 91 { //nolint:mnd // stable coordination contract with package cache mounts.
		t.Fatalf("FixPriority = %d, want 91", meta.FixPriority)
	}
}

func TestNodeGypCacheMountsRule_Check(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		content        string
		contextFiles   map[string]string
		wantViolations int
		wantDetail     string
	}{
		{
			name: "toolchain stage plus npm ci triggers",
			content: `FROM node:20
RUN apt-get update && apt-get install -y python3 make g++
RUN npm ci
`,
			wantViolations: 1,
			wantDetail:     "python3",
		},
		{
			name: "same RUN toolchain and npm ci triggers",
			content: `FROM node:20
RUN apt-get update && apt-get install -y build-essential && npm ci
`,
			wantViolations: 1,
			wantDetail:     "build-essential",
		},
		{
			name: "no native signal does not trigger",
			content: `FROM node:20
RUN npm ci
`,
			wantViolations: 0,
		},
		{
			name: "npm rebuild is an explicit native addon signal",
			content: `FROM node:20
RUN npm rebuild sharp
`,
			wantViolations: 1,
			wantDetail:     "npm",
		},
		{
			name: "context package json with native dependency triggers",
			content: `FROM node:20
WORKDIR /app
COPY package.json ./package.json
RUN npm ci
`,
			contextFiles: map[string]string{
				"package.json": `{"dependencies":{"sharp":"0.33.5"}}`,
			},
			wantViolations: 1,
			wantDetail:     "sharp",
		},
		{
			name: "existing node-gyp cache mount suppresses violation",
			content: `FROM node:20
RUN apt-get update && apt-get install -y python3 make g++
ENV npm_package_config_node_gyp_devdir=/cache/node-gyp
RUN --mount=type=cache,target=/cache/node-gyp npm ci
`,
			wantViolations: 0,
		},
		{
			name: "ccache cache mount suppresses stage",
			content: `FROM node:20
RUN --mount=type=cache,target=/root/.cache/ccache apt-get update && apt-get install -y python3 make g++
RUN npm ci
`,
			wantViolations: 0,
		},
		{
			name: "heredoc install is detected",
			content: `FROM node:20
RUN apt-get update && apt-get install -y python3 make g++
RUN <<EOF
npm ci
EOF
`,
			wantViolations: 1,
		},
		{
			name: "windows stage skipped",
			content: `FROM mcr.microsoft.com/windows/servercore:ltsc2022
RUN npm rebuild sharp
`,
			wantViolations: 0,
		},
	}

	rule := NewNodeGypCacheMountsRule()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var ctx facts.ContextFileReader
			if tt.contextFiles != nil {
				ctx = &nodeGypBuildContext{files: tt.contextFiles}
			}
			input := testutil.MakeLintInputWithContext(t, "Dockerfile", tt.content, ctx)
			violations := rule.Check(input)
			if len(violations) != tt.wantViolations {
				t.Fatalf("got %d violations, want %d: %#v", len(violations), tt.wantViolations, violations)
			}
			if tt.wantDetail != "" && (len(violations) == 0 || !strings.Contains(violations[0].Detail, tt.wantDetail)) {
				t.Fatalf("detail = %q, want substring %q", violations[0].Detail, tt.wantDetail)
			}
		})
	}
}

func TestNodeGypCacheMountsRule_Fix(t *testing.T) {
	t.Parallel()

	const content = `FROM node:20
RUN apt-get update && apt-get install -y python3 make g++
RUN npm ci
`
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	violations := NewNodeGypCacheMountsRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("got %d violations, want 1", len(violations))
	}
	if violations[0].SuggestedFix == nil {
		t.Fatal("expected suggested fix")
	}
	if violations[0].SuggestedFix.Safety != rules.FixSuggestion {
		t.Fatalf("fix safety = %v, want %v", violations[0].SuggestedFix.Safety, rules.FixSuggestion)
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
	want := `FROM node:20
RUN apt-get update && apt-get install -y python3 make g++
RUN --mount=type=cache,target=/root/.npm,id=npm --mount=type=cache,target=/root/.cache/node-gyp,id=node-gyp,sharing=locked --mount=type=tmpfs,target=/tmp NPM_CONFIG_DEVDIR=/root/.cache/node-gyp npm ci
`
	if got != want {
		t.Fatalf("fixed content =\n%s\nwant:\n%s", got, want)
	}
}

func TestNodeGypCacheMountsRule_FixWithExistingRunFlag(t *testing.T) {
	t.Parallel()

	const content = `FROM node:20
RUN apt-get update && apt-get install -y python3 make g++
RUN --network=none npm ci
`
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	violations := NewNodeGypCacheMountsRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("got %d violations, want 1", len(violations))
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
	want := `FROM node:20
RUN apt-get update && apt-get install -y python3 make g++
RUN --mount=type=cache,target=/root/.npm,id=npm --mount=type=cache,target=/root/.cache/node-gyp,id=node-gyp,sharing=locked --mount=type=tmpfs,target=/tmp --network=none NPM_CONFIG_DEVDIR=/root/.cache/node-gyp npm ci
`
	if got != want {
		t.Fatalf("fixed content =\n%s\nwant:\n%s", got, want)
	}
}

func TestNodeGypCacheMountsRule_CoordinatesWithPreferPackageCacheMounts(t *testing.T) {
	t.Parallel()

	const content = `FROM node:20
RUN apt-get update && apt-get install -y python3 make g++
RUN npm ci
`
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	input.EnabledRules = []string{preferPackageCacheMountsCode}

	violations := NewNodeGypCacheMountsRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("got %d violations, want 1", len(violations))
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
	if strings.Contains(got, "/root/.npm") {
		t.Fatalf("fixed content unexpectedly contains package-manager cache mount:\n%s", got)
	}
	want := `FROM node:20
RUN apt-get update && apt-get install -y python3 make g++
RUN --mount=type=cache,target=/root/.cache/node-gyp,id=node-gyp,sharing=locked --mount=type=tmpfs,target=/tmp NPM_CONFIG_DEVDIR=/root/.cache/node-gyp npm ci
`
	if got != want {
		t.Fatalf("fixed content =\n%s\nwant:\n%s", got, want)
	}
}
