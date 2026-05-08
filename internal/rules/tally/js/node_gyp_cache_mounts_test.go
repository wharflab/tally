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
			name: "runtime native dependency still triggers when npm omits dev dependencies",
			content: `FROM node:20
WORKDIR /app
COPY package.json ./package.json
RUN npm ci --omit=dev
`,
			contextFiles: map[string]string{
				"package.json": `{"dependencies":{"sharp":"0.33.5"}}`,
			},
			wantViolations: 1,
			wantDetail:     "sharp",
		},
		{
			name: "dev-only native dependency does not trigger when npm omits dev dependencies",
			content: `FROM node:20
WORKDIR /app
COPY package.json ./package.json
RUN npm ci --omit=dev
`,
			contextFiles: map[string]string{
				"package.json": `{"devDependencies":{"sharp":"0.33.5"}}`,
			},
			wantViolations: 0,
		},
		{
			name: "dev-only native dependency triggers when npm installs dev dependencies",
			content: `FROM node:20
WORKDIR /app
COPY package.json ./package.json
RUN npm ci
`,
			contextFiles: map[string]string{
				"package.json": `{"devDependencies":{"sharp":"0.33.5"}}`,
			},
			wantViolations: 1,
			wantDetail:     "sharp",
		},
		{
			name: "bare yarn immutable install triggers",
			content: `FROM node:20
RUN apt-get update && apt-get install -y python3 make g++
RUN yarn --immutable
`,
			wantViolations: 1,
			wantDetail:     "python3",
		},
		{
			name: "bare yarn version command does not trigger",
			content: `FROM node:20
RUN apt-get update && apt-get install -y python3 make g++
RUN yarn --version
`,
			wantViolations: 0,
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
			name: "inline node-gyp devdir cache mount suppresses violation",
			content: `FROM node:20
RUN apt-get update && apt-get install -y python3 make g++
RUN --mount=type=cache,target=/cache/node-gyp npm_config_devdir=/cache/node-gyp npm ci
`,
			wantViolations: 0,
		},
		{
			name: "exported node-gyp devdir cache mount suppresses violation",
			content: `FROM node:20
RUN apt-get update && apt-get install -y python3 make g++
RUN --mount=type=cache,target=/cache/node-gyp export npm_config_devdir=/cache/node-gyp && npm ci
`,
			wantViolations: 0,
		},
		{
			name: "quoted exported node-gyp devdir cache mount suppresses violation",
			content: `FROM node:20
RUN apt-get update && apt-get install -y python3 make g++
RUN --mount=type=cache,target=/cache/node-gyp export npm_config_devdir='/cache/node-gyp' && npm ci
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

func TestConfiguredNodeGypDevDirUsesDeterministicPrecedence(t *testing.T) {
	t.Parallel()

	env := facts.EnvFacts{Values: map[string]string{
		nodeGypPackageConfigDevDirKey: "pkg-cache/node-gyp",
		nodeGypDevDirEnvAssignmentKey: "/cache/from-npm-config",
		nodeGypLowerDevDirEnvKey:      "/cache/from-lower-npm-config",
	}}

	got, ok := configuredNodeGypDevDir(env, "/app")
	if !ok {
		t.Fatal("configuredNodeGypDevDir ok = false, want true")
	}
	if got != "/app/pkg-cache/node-gyp" {
		t.Fatalf("configuredNodeGypDevDir = %q, want /app/pkg-cache/node-gyp", got)
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
	want := strings.Join([]string{
		"FROM node:20",
		"RUN apt-get update && apt-get install -y python3 make g++",
		"RUN --mount=type=cache,target=/root/.npm,id=npm " +
			"--mount=type=cache,target=/root/.cache/node-gyp,id=node-gyp,sharing=locked " +
			"--mount=type=tmpfs,target=/tmp NPM_CONFIG_DEVDIR=\"/root/.cache/node-gyp\" npm ci",
	}, "\n") + "\n"
	if got != want {
		t.Fatalf("fixed content =\n%s\nwant:\n%s", got, want)
	}
}

func TestNodeGypCacheMountsRule_FixWithInlineDevDir(t *testing.T) {
	t.Parallel()

	const content = `FROM node:20
RUN apt-get update && apt-get install -y python3 make g++
RUN npm_config_devdir=/cache/node-gyp npm ci
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
	want := strings.Join([]string{
		"FROM node:20",
		"RUN apt-get update && apt-get install -y python3 make g++",
		"RUN --mount=type=cache,target=/root/.npm,id=npm " +
			"--mount=type=cache,target=/cache/node-gyp,id=node-gyp,sharing=locked " +
			"--mount=type=tmpfs,target=/tmp npm_config_devdir=/cache/node-gyp npm ci",
	}, "\n") + "\n"
	if got != want {
		t.Fatalf("fixed content =\n%s\nwant:\n%s", got, want)
	}
}

func TestNodeGypCacheMountsRule_FixWithExportedDevDir(t *testing.T) {
	t.Parallel()

	const content = `FROM node:20
RUN apt-get update && apt-get install -y python3 make g++
RUN export npm_config_devdir=/cache/node-gyp && npm ci
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
	want := strings.Join([]string{
		"FROM node:20",
		"RUN apt-get update && apt-get install -y python3 make g++",
		"RUN --mount=type=cache,target=/root/.npm,id=npm " +
			"--mount=type=cache,target=/cache/node-gyp,id=node-gyp,sharing=locked " +
			"--mount=type=tmpfs,target=/tmp export npm_config_devdir=/cache/node-gyp && npm ci",
	}, "\n") + "\n"
	if got != want {
		t.Fatalf("fixed content =\n%s\nwant:\n%s", got, want)
	}
}

func TestNodeGypCacheMountsRule_FixWithQuotedExportedDevDir(t *testing.T) {
	t.Parallel()

	const content = `FROM node:20
RUN apt-get update && apt-get install -y python3 make g++
RUN export npm_config_devdir='/cache/node-gyp' && npm ci
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
	want := strings.Join([]string{
		"FROM node:20",
		"RUN apt-get update && apt-get install -y python3 make g++",
		"RUN --mount=type=cache,target=/root/.npm,id=npm " +
			"--mount=type=cache,target=/cache/node-gyp,id=node-gyp,sharing=locked " +
			"--mount=type=tmpfs,target=/tmp export npm_config_devdir='/cache/node-gyp' && npm ci",
	}, "\n") + "\n"
	if got != want {
		t.Fatalf("fixed content =\n%s\nwant:\n%s", got, want)
	}
}

func TestNodeGypCacheMountsRule_FixAddsDevDirEnvToEachInstallCommand(t *testing.T) {
	t.Parallel()

	const content = `FROM node:20
RUN apt-get update && apt-get install -y python3 make g++
RUN npm install && npm rebuild sharp
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
	want := strings.Join([]string{
		"FROM node:20",
		"RUN apt-get update && apt-get install -y python3 make g++",
		"RUN --mount=type=cache,target=/root/.npm,id=npm " +
			"--mount=type=cache,target=/root/.cache/node-gyp,id=node-gyp,sharing=locked " +
			"--mount=type=tmpfs,target=/tmp NPM_CONFIG_DEVDIR=\"/root/.cache/node-gyp\" npm install && " +
			"NPM_CONFIG_DEVDIR=\"/root/.cache/node-gyp\" npm rebuild sharp",
	}, "\n") + "\n"
	if got != want {
		t.Fatalf("fixed content =\n%s\nwant:\n%s", got, want)
	}
}

// Regression: a cache mount whose target is unrelated to the actual
// devdir but happens to carry id=node-gyp must NOT suppress the rule.
// node-gyp still writes to its real devdir, so the build is still
// uncached and the violation must fire.
func TestNodeGypCacheMountsRule_FiresWhenCacheTargetMismatchesDevDir(t *testing.T) {
	t.Parallel()

	const content = `FROM node:20
RUN apt-get update && apt-get install -y python3 make g++
RUN --mount=type=cache,target=/tmp,id=node-gyp npm install
`
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	violations := NewNodeGypCacheMountsRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("got %d violations, want 1 (id=node-gyp on /tmp does not cache the real devdir)", len(violations))
	}
}

// Regression: when a stage uses a non-POSIX shell (e.g. PowerShell via
// `SHELL ["pwsh", "-Command"]`), the inline `KEY="…" cmd` env-prefix is
// not valid POSIX syntax and would break the RUN. The rule must skip the
// inline env edits in that case (the cache-mount edits, which live on
// the RUN flag prefix, are still safe and remain).
func TestNodeGypCacheMountsRule_FixSkipsDevDirEnvUnderPowerShell(t *testing.T) {
	t.Parallel()

	const content = `FROM node:20
RUN apt-get update && apt-get install -y python3 make g++
SHELL ["pwsh", "-Command"]
RUN npm install
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
	if strings.Contains(got, `NPM_CONFIG_DEVDIR="`) {
		t.Errorf("PowerShell stage must not get POSIX-style env prefix; got:\n%s", got)
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
	want := strings.Join([]string{
		"FROM node:20",
		"RUN apt-get update && apt-get install -y python3 make g++",
		"RUN --mount=type=cache,target=/root/.npm,id=npm " +
			"--mount=type=cache,target=/root/.cache/node-gyp,id=node-gyp,sharing=locked " +
			"--mount=type=tmpfs,target=/tmp --network=none " +
			"NPM_CONFIG_DEVDIR=\"/root/.cache/node-gyp\" npm ci",
	}, "\n") + "\n"
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
	want := strings.Join([]string{
		"FROM node:20",
		"RUN apt-get update && apt-get install -y python3 make g++",
		"RUN --mount=type=cache,target=/root/.cache/node-gyp,id=node-gyp,sharing=locked " +
			"--mount=type=tmpfs,target=/tmp NPM_CONFIG_DEVDIR=\"/root/.cache/node-gyp\" npm ci",
	}, "\n") + "\n"
	if got != want {
		t.Fatalf("fixed content =\n%s\nwant:\n%s", got, want)
	}
}
