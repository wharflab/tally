package labels

import (
	"testing"

	fixpkg "github.com/wharflab/tally/internal/fix"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/testutil"
)

func TestNoBuildxGitOverlapRule_Metadata(t *testing.T) {
	t.Parallel()

	meta := NewNoBuildxGitOverlapRule().Metadata()
	if meta.Code != NoBuildxGitOverlapRuleCode {
		t.Fatalf("Code = %q, want %q", meta.Code, NoBuildxGitOverlapRuleCode)
	}
	if meta.DefaultSeverity != rules.SeverityWarning {
		t.Fatalf("DefaultSeverity = %s, want warning", meta.DefaultSeverity)
	}
	if meta.Category != "correctness" {
		t.Fatalf("Category = %q, want correctness", meta.Category)
	}
}

func TestNoBuildxGitOverlapRule_DefaultConfig(t *testing.T) {
	t.Parallel()

	cfg, ok := NewNoBuildxGitOverlapRule().DefaultConfig().(NoBuildxGitOverlapConfig)
	if !ok {
		t.Fatalf("DefaultConfig() type = %T, want NoBuildxGitOverlapConfig", cfg)
	}
	if cfg.BuildxGitLabels != "auto" {
		t.Fatalf("BuildxGitLabels = %q, want auto", cfg.BuildxGitLabels)
	}
}

func TestNoBuildxGitOverlapRule_ValidateConfig(t *testing.T) {
	t.Parallel()

	rule := NewNoBuildxGitOverlapRule()
	for _, tt := range []struct {
		name    string
		config  any
		wantErr bool
	}{
		{name: "nil", config: nil},
		{name: "auto", config: map[string]any{"buildx-git-labels": "auto"}},
		{name: "true", config: map[string]any{"buildx-git-labels": "true"}},
		{name: "one", config: map[string]any{"buildx-git-labels": "1"}},
		{name: "full", config: map[string]any{"buildx-git-labels": "full"}},
		{name: "off", config: map[string]any{"buildx-git-labels": "off"}},
		{name: "severity", config: map[string]any{"severity": "warning", "buildx-git-labels": "full"}},
		{name: "invalid mode", config: map[string]any{"buildx-git-labels": "maybe"}, wantErr: true},
		{name: "boolean mode", config: map[string]any{"buildx-git-labels": true}, wantErr: true},
		{name: "unknown option", config: map[string]any{"unknown": "x"}, wantErr: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := rule.ValidateConfig(tt.config)
			if tt.wantErr && err == nil {
				t.Fatal("ValidateConfig() error = nil, want error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("ValidateConfig() error = %v, want nil", err)
			}
		})
	}
}

func TestNoBuildxGitOverlapRule_CheckConfiguredModes(t *testing.T) {
	t.Parallel()

	testutil.RunRuleTests(t, NewNoBuildxGitOverlapRule(), []testutil.RuleTestCase{
		{
			Name: "off mode skips matching labels",
			Config: map[string]any{
				"buildx-git-labels": "off",
			},
			Content: `FROM alpine:3.20
LABEL org.opencontainers.image.revision="abc123"
`,
			WantViolations: 0,
		},
		{
			Name: "true mode checks revision and Dockerfile entrypoint only",
			Config: map[string]any{
				"buildx-git-labels": "true",
			},
			Content: `FROM alpine:3.20
LABEL org.opencontainers.image.revision="abc123" \
      org.opencontainers.image.source="https://github.com/example/app" \
      com.docker.image.source.entrypoint="Dockerfile"
`,
			WantViolations: 1,
			WantMessages: []string{
				`BUILDX_GIT_LABELS=1 can emit labels "org.opencontainers.image.revision", "com.docker.image.source.entrypoint"`,
			},
		},
		{
			Name: "full mode checks source revision and Dockerfile entrypoint",
			Config: map[string]any{
				"buildx-git-labels": "full",
			},
			Content: `FROM alpine:3.20
LABEL org.opencontainers.image.revision="abc123" \
      org.opencontainers.image.source="https://github.com/example/app" \
      com.docker.image.source.entrypoint="Dockerfile"
`,
			WantViolations: 1,
			WantMessages: []string{
				`BUILDX_GIT_LABELS=full can emit labels "org.opencontainers.image.revision", ` +
					`"org.opencontainers.image.source", "com.docker.image.source.entrypoint"`,
			},
		},
		{
			Name: "same generated key repeated in one LABEL is listed once",
			Config: map[string]any{
				"buildx-git-labels": "full",
			},
			Content: `FROM alpine:3.20
LABEL org.opencontainers.image.revision="abc123" \
      org.opencontainers.image.revision="def456"
`,
			WantViolations: 1,
			WantMessages:   []string{`can emit label "org.opencontainers.image.revision"`},
		},
		{
			Name: "dynamic values still overlap responsibility",
			Config: map[string]any{
				"buildx-git-labels": "1",
			},
			Content: `FROM alpine:3.20
ARG VCS_REF
LABEL org.opencontainers.image.revision=$VCS_REF
`,
			WantViolations: 1,
			WantMessages:   []string{`org.opencontainers.image.revision`},
		},
		{
			Name: "dynamic keys are skipped",
			Config: map[string]any{
				"buildx-git-labels": "full",
			},
			Content: `FROM alpine:3.20
ARG LABEL_KEY=org.opencontainers.image.revision
LABEL "$LABEL_KEY"="abc123"
`,
			WantViolations: 0,
		},
		{
			Name: "legacy LABEL format is owned by BuildKit",
			Config: map[string]any{
				"buildx-git-labels": "full",
			},
			Content: `FROM alpine:3.20
LABEL org.opencontainers.image.revision abc123
`,
			WantViolations: 0,
		},
		{
			Name: "builder-only labels do not affect exported image labels",
			Config: map[string]any{
				"buildx-git-labels": "full",
			},
			Content: `FROM alpine:3.20 AS build
LABEL org.opencontainers.image.revision="abc123"

FROM alpine:3.20
RUN true
`,
			WantViolations: 0,
		},
		{
			Name: "labels inherited by final stage are checked",
			Config: map[string]any{
				"buildx-git-labels": "full",
			},
			Content: `FROM alpine:3.20 AS metadata
LABEL org.opencontainers.image.revision="abc123"

FROM metadata
RUN true
`,
			WantViolations: 1,
			WantMessages:   []string{`org.opencontainers.image.revision`},
		},
	})
}

func TestNoBuildxGitOverlapRule_AutoReadsEnvironment(t *testing.T) {
	t.Setenv("BUILDX_GIT_LABELS", "full")

	input := testutil.MakeLintInput(t, "Dockerfile", `FROM alpine:3.20
LABEL org.opencontainers.image.revision="abc123" \
      org.opencontainers.image.source="https://github.com/example/app" \
      com.docker.image.source.entrypoint="Dockerfile"
`)

	violations := NewNoBuildxGitOverlapRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("got %d violations, want 1", len(violations))
	}
}

func TestNoBuildxGitOverlapRule_AutoWithoutActiveEnvironmentSkips(t *testing.T) {
	t.Setenv("BUILDX_GIT_LABELS", "0")

	input := testutil.MakeLintInput(t, "Dockerfile", `FROM alpine:3.20
LABEL org.opencontainers.image.revision="abc123"
`)

	violations := NewNoBuildxGitOverlapRule().Check(input)
	if len(violations) != 0 {
		t.Fatalf("got %d violations, want 0", len(violations))
	}
}

func TestNoBuildxGitOverlapRule_RevisionFixOptions(t *testing.T) {
	t.Parallel()

	content := `FROM alpine:3.20
LABEL org.opencontainers.image.revision="abc123"
`
	input := testutil.MakeLintInputWithConfig(t, "Dockerfile", content, map[string]any{"buildx-git-labels": "full"})

	violations := NewNoBuildxGitOverlapRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("got %d violations, want 1", len(violations))
	}
	if violations[0].Severity != rules.SeverityWarning {
		t.Fatalf("severity = %s, want warning", violations[0].Severity)
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
		"# [commented out by tally - Buildx can generate org.opencontainers.image.revision]: " +
		"LABEL org.opencontainers.image.revision=\"abc123\"\n"
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

func TestNoBuildxGitOverlapRule_NoFixForGroupedGeneratedLabels(t *testing.T) {
	t.Parallel()

	input := testutil.MakeLintInputWithConfig(t, "Dockerfile", `FROM alpine:3.20
LABEL org.opencontainers.image.revision="abc123" \
      org.opencontainers.image.source="https://github.com/example/app"
`, map[string]any{"buildx-git-labels": "full"})

	violations := NewNoBuildxGitOverlapRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("got %d violations, want 1", len(violations))
	}
	if violations[0].SuggestedFix != nil {
		t.Fatal("SuggestedFix = non-nil, want nil")
	}
}

func TestNoBuildxGitOverlapRule_GroupedRevisionFixOptions(t *testing.T) {
	t.Parallel()

	content := `FROM alpine:3.20
LABEL org.opencontainers.image.revision="abc123" \
      org.opencontainers.image.title="app"
`
	input := testutil.MakeLintInputWithConfig(t, "Dockerfile", content, map[string]any{"buildx-git-labels": "full"})

	violations := NewNoBuildxGitOverlapRule().Check(input)
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
		"# [commented out by tally - Buildx can generate org.opencontainers.image.revision]: " +
		"LABEL org.opencontainers.image.revision=\"abc123\"\n" +
		"LABEL org.opencontainers.image.title=\"app\"\n"
	if gotCommented != wantCommented {
		t.Errorf("comment fix mismatch\ngot:\n%s\nwant:\n%s", gotCommented, wantCommented)
	}

	deleteFix := allFixes[1]
	gotDeleted := string(fixpkg.ApplyFix([]byte(content), deleteFix))
	wantDeleted := "FROM alpine:3.20\n" +
		"LABEL org.opencontainers.image.title=\"app\"\n"
	if gotDeleted != wantDeleted {
		t.Errorf("delete fix mismatch\ngot:\n%s\nwant:\n%s", gotDeleted, wantDeleted)
	}
}

func TestActiveBuildxGitLabelsMode(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name string
		cfg  NoBuildxGitOverlapConfig
		env  string
		ok   bool
		want buildxGitLabelsMode
	}{
		{name: "auto without env", cfg: NoBuildxGitOverlapConfig{BuildxGitLabels: "auto"}, want: buildxGitLabelsOff},
		{name: "auto env true", cfg: NoBuildxGitOverlapConfig{BuildxGitLabels: "auto"}, env: "true", ok: true, want: buildxGitLabelsTrue},
		{name: "auto env one", cfg: NoBuildxGitOverlapConfig{BuildxGitLabels: "auto"}, env: "1", ok: true, want: buildxGitLabelsTrue},
		{name: "auto env full", cfg: NoBuildxGitOverlapConfig{BuildxGitLabels: "auto"}, env: "full", ok: true, want: buildxGitLabelsFull},
		{name: "auto env false", cfg: NoBuildxGitOverlapConfig{BuildxGitLabels: "auto"}, env: "false", ok: true, want: buildxGitLabelsOff},
		{name: "config wins over env", cfg: NoBuildxGitOverlapConfig{BuildxGitLabels: "off"}, env: "full", ok: true, want: buildxGitLabelsOff},
		{name: "config full", cfg: NoBuildxGitOverlapConfig{BuildxGitLabels: "full"}, want: buildxGitLabelsFull},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := activeBuildxGitLabelsMode(tt.cfg, func(string) (string, bool) {
				return tt.env, tt.ok
			})
			if got != tt.want {
				t.Fatalf("activeBuildxGitLabelsMode() = %q, want %q", got, tt.want)
			}
		})
	}
}
