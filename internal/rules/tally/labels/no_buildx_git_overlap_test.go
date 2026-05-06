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
	if cfg.BuildxGitLabels != "full" {
		t.Fatalf("BuildxGitLabels = %q, want full", cfg.BuildxGitLabels)
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
		{name: "true", config: map[string]any{"buildx-git-labels": "true"}},
		{name: "one", config: map[string]any{"buildx-git-labels": "1"}},
		{name: "short true", config: map[string]any{"buildx-git-labels": "t"}},
		{name: "uppercase short true", config: map[string]any{"buildx-git-labels": "T"}},
		{name: "full", config: map[string]any{"buildx-git-labels": "full"}},
		{name: "off", config: map[string]any{"buildx-git-labels": "off"}},
		{name: "false", config: map[string]any{"buildx-git-labels": "false"}},
		{name: "zero", config: map[string]any{"buildx-git-labels": "0"}},
		{name: "short false", config: map[string]any{"buildx-git-labels": "f"}},
		{name: "none", config: map[string]any{"buildx-git-labels": "none"}},
		{name: "severity", config: map[string]any{"severity": "warning", "buildx-git-labels": "full"}},
		{name: "auto mode", config: map[string]any{"buildx-git-labels": "auto"}, wantErr: true},
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
			Name: "default mode checks source revision and Dockerfile entrypoint",
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
		{
			Name: "ancestor generated labels shadowed by final stage are not checked twice",
			Config: map[string]any{
				"buildx-git-labels": "full",
			},
			Content: `FROM alpine:3.20 AS metadata
LABEL org.opencontainers.image.revision="ancestor"

FROM metadata
LABEL org.opencontainers.image.revision="final"
`,
			WantViolations: 1,
			WantMessages:   []string{`org.opencontainers.image.revision`},
		},
	})
}

func TestNoBuildxGitOverlapRule_DefaultDoesNotReadEnvironment(t *testing.T) {
	t.Setenv("BUILDX_GIT_LABELS", "0")

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
	if fixes := violations[0].AllFixes(); len(fixes) != 0 {
		t.Fatalf("got %d fix options, want 0", len(fixes))
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

func TestNoBuildxGitOverlapRule_GroupedRepeatedRevisionFixOptions(t *testing.T) {
	t.Parallel()

	content := `FROM alpine:3.20
LABEL org.opencontainers.image.revision="abc123" \
      org.opencontainers.image.revision="def456" \
      org.opencontainers.image.title="app"
`
	config := map[string]any{"buildx-git-labels": "full"}
	input := testutil.MakeLintInputWithConfig(t, "Dockerfile", content, config)

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
		"# [commented out by tally - Buildx can generate org.opencontainers.image.revision]: " +
		"LABEL org.opencontainers.image.revision=\"def456\"\n" +
		"LABEL org.opencontainers.image.title=\"app\"\n"
	if gotCommented != wantCommented {
		t.Errorf("comment fix mismatch\ngot:\n%s\nwant:\n%s", gotCommented, wantCommented)
	}
	commentedInput := testutil.MakeLintInputWithConfig(t, "Dockerfile", gotCommented, config)
	if got := NewNoBuildxGitOverlapRule().Check(commentedInput); len(got) != 0 {
		t.Fatalf("comment fix left %d violations, want 0", len(got))
	}

	deleteFix := allFixes[1]
	gotDeleted := string(fixpkg.ApplyFix([]byte(content), deleteFix))
	wantDeleted := "FROM alpine:3.20\n" +
		"LABEL org.opencontainers.image.title=\"app\"\n"
	if gotDeleted != wantDeleted {
		t.Errorf("delete fix mismatch\ngot:\n%s\nwant:\n%s", gotDeleted, wantDeleted)
	}
	deletedInput := testutil.MakeLintInputWithConfig(t, "Dockerfile", gotDeleted, config)
	if got := NewNoBuildxGitOverlapRule().Check(deletedInput); len(got) != 0 {
		t.Fatalf("delete fix left %d violations, want 0", len(got))
	}
}

func TestConfiguredBuildxGitLabelsMode(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name string
		cfg  NoBuildxGitOverlapConfig
		want buildxGitLabelsMode
	}{
		{name: "empty defaults full", cfg: NoBuildxGitOverlapConfig{}, want: buildxGitLabelsFull},
		{name: "off", cfg: NoBuildxGitOverlapConfig{BuildxGitLabels: "off"}, want: buildxGitLabelsOff},
		{name: "false", cfg: NoBuildxGitOverlapConfig{BuildxGitLabels: "false"}, want: buildxGitLabelsOff},
		{name: "short false", cfg: NoBuildxGitOverlapConfig{BuildxGitLabels: "f"}, want: buildxGitLabelsOff},
		{name: "zero", cfg: NoBuildxGitOverlapConfig{BuildxGitLabels: "0"}, want: buildxGitLabelsOff},
		{name: "none", cfg: NoBuildxGitOverlapConfig{BuildxGitLabels: "none"}, want: buildxGitLabelsOff},
		{name: "true", cfg: NoBuildxGitOverlapConfig{BuildxGitLabels: "true"}, want: buildxGitLabelsTrue},
		{name: "one", cfg: NoBuildxGitOverlapConfig{BuildxGitLabels: "1"}, want: buildxGitLabelsTrue},
		{name: "short true", cfg: NoBuildxGitOverlapConfig{BuildxGitLabels: "t"}, want: buildxGitLabelsTrue},
		{name: "uppercase short true", cfg: NoBuildxGitOverlapConfig{BuildxGitLabels: "T"}, want: buildxGitLabelsTrue},
		{name: "config full", cfg: NoBuildxGitOverlapConfig{BuildxGitLabels: "full"}, want: buildxGitLabelsFull},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := configuredBuildxGitLabelsMode(tt.cfg)
			if got != tt.want {
				t.Fatalf("configuredBuildxGitLabelsMode() = %q, want %q", got, tt.want)
			}
		})
	}
}
