package autofix

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	gitleaksconfig "github.com/zricethezav/gitleaks/v8/config"
	"github.com/zricethezav/gitleaks/v8/detect"
	gitleaksregexp "github.com/zricethezav/gitleaks/v8/regexp"

	"github.com/wharflab/tally/internal/ai/acp"
	"github.com/wharflab/tally/internal/ai/autofixdata"
	"github.com/wharflab/tally/internal/config"
	"github.com/wharflab/tally/internal/dockerfile"
	"github.com/wharflab/tally/internal/fix"
	patchutil "github.com/wharflab/tally/internal/patch"
	"github.com/wharflab/tally/internal/rules"

	_ "github.com/wharflab/tally/internal/rules/hadolint"
)

const emptyEditsObjectiveKind = autofixdata.ObjectiveKind("test-empty-edits")

type emptyEditsObjective struct{}

func init() {
	autofixdata.RegisterObjective(emptyEditsObjective{})
}

func (emptyEditsObjective) Kind() autofixdata.ObjectiveKind { return emptyEditsObjectiveKind }

func (emptyEditsObjective) BuildPrompt(autofixdata.PromptContext) (string, error) {
	return "rewrite the Dockerfile", nil
}

func (emptyEditsObjective) BuildRetryPrompt(autofixdata.RetryPromptContext) (string, error) {
	return "retry the Dockerfile rewrite", nil
}

func (emptyEditsObjective) BuildSimplifiedPrompt(autofixdata.SimplifiedPromptContext) string {
	return "rewrite the Dockerfile"
}

func (emptyEditsObjective) ValidateProposal(
	_ *autofixdata.ObjectiveRequest,
	_ *dockerfile.ParseResult,
	_ *dockerfile.ParseResult,
) []autofixdata.BlockingIssue {
	return nil
}

func (emptyEditsObjective) ValidatePatch(*autofixdata.ObjectiveRequest, patchutil.Meta) []autofixdata.BlockingIssue {
	return nil
}

func (emptyEditsObjective) BuildResolvedEdits(
	_ string,
	_ []byte,
	_ []byte,
	_ *autofixdata.ObjectiveRequest,
) ([]rules.TextEdit, error) {
	return nil, nil
}

type stubAgentRunner struct {
	texts []string
	i     int
}

func (r *stubAgentRunner) Run(_ context.Context, _ acp.RunRequest) (acp.RunResponse, error) {
	if r.i >= len(r.texts) {
		return acp.RunResponse{}, errors.New("unexpected Run call")
	}
	out := acp.RunResponse{Text: r.texts[r.i]}
	r.i++
	return out, nil
}

func multiStageObj(t *testing.T) autofixdata.Objective {
	t.Helper()
	obj, ok := autofixdata.GetObjective(autofixdata.ObjectiveMultiStage)
	require.Truef(t, ok, "objective %q not registered — ensure rules/tally is imported", autofixdata.ObjectiveMultiStage)
	return obj
}

func testAgentConfig(cfg *config.Config) agentConfig {
	return agentConfig{cfg: cfg, timeout: 5 * time.Second}
}

func testRoundParams(t *testing.T) roundPromptParams {
	t.Helper()
	return roundPromptParams{
		filePath: "Dockerfile",
		obj:      multiStageObj(t),
	}
}

func commandFamilyNormalizeRequest(cfg *config.Config) *autofixdata.ObjectiveRequest {
	return &autofixdata.ObjectiveRequest{
		Kind:   autofixdata.ObjectiveCommandFamilyNormalize,
		File:   "Dockerfile",
		Config: cfg,
		Facts: map[string]any{
			"platform-os":            "linux",
			"shell-variant":          "sh",
			"preferred-tool":         "wget",
			"source-tool":            "curl",
			"target-start-line":      3,
			"target-end-line":        3,
			"target-start-col":       len("RUN "),
			"target-end-col":         len("RUN curl -sS https://example.com/install.sh"),
			"target-command-text":    "curl -sS https://example.com/install.sh",
			"target-run-script":      "curl -sS https://example.com/install.sh | sh",
			"target-command-index":   0,
			"original-command-names": []string{"curl", "sh"},
			"literal-urls":           []string{"https://example.com/install.sh"},
			"blockers":               []string{"deterministic lowering is unavailable for this command"},
		},
		FixContext: autofixdata.FixContext{
			RuleFilter: []string{rules.HadolintRulePrefix + "DL4001"},
		},
	}
}

func emptyEditsRequest(cfg *config.Config) *autofixdata.ObjectiveRequest {
	return &autofixdata.ObjectiveRequest{
		Kind:   emptyEditsObjectiveKind,
		File:   "Dockerfile",
		Config: cfg,
	}
}

func testSecretDetector() *detect.Detector {
	return detect.NewDetector(gitleaksconfig.Config{
		Rules: map[string]gitleaksconfig.Rule{
			"test-github-token": {
				RuleID:      "test-github-token",
				Description: "match the test github token",
				Regex:       gitleaksregexp.MustCompile(`ghp_[0-9]{36}`),
			},
		},
	})
}

func TestResolver_RunAndParseRound_NoChange_ShortCircuits(t *testing.T) {
	t.Parallel()

	cfg := config.Default()
	cfg.AI.Command = []string{"stub"}
	cfg.AI.RedactSecrets = false

	r := &resolver{
		runner: &stubAgentRunner{texts: []string{"NO_CHANGE"}},
	}

	out, err := r.runRound(
		context.Background(), testAgentConfig(cfg),
		"prompt", []byte("FROM alpine:3.20\n"), testRoundParams(t), autofixdata.OutputPatch,
	)
	require.NoError(t, err)
	require.True(t, out.noChange)
	require.Nil(t, out.proposed)
}

func TestResolver_RunAndParseRound_NoChange_ShortCircuitsAfterRetry(t *testing.T) {
	t.Parallel()

	cfg := config.Default()
	cfg.AI.Command = []string{"stub"}
	cfg.AI.RedactSecrets = false

	r := &resolver{
		runner: &stubAgentRunner{texts: []string{"not a diff block", "NO_CHANGE"}},
	}

	out, err := r.runRound(
		context.Background(), testAgentConfig(cfg),
		"prompt", []byte("FROM alpine:3.20\n"), testRoundParams(t), autofixdata.OutputPatch,
	)
	require.NoError(t, err)
	require.True(t, out.noChange)
	require.Nil(t, out.proposed)
}

func TestResolver_RunRound_RedactSecretsInPatchModeFallsBack(t *testing.T) {
	t.Parallel()

	cfg := config.Default()
	cfg.AI.Command = []string{"stub"}
	cfg.AI.RedactSecrets = true

	r := &resolver{
		runner:          &stubAgentRunner{},
		gitleaksFactory: func() (*detect.Detector, error) { return testSecretDetector(), nil },
	}

	roundInput := []byte(
		"FROM alpine:3.20\n" +
			"ENV GITHUB_TOKEN=ghp_123456789012345678901234567890123456\n",
	)

	_, err := r.runRound(
		context.Background(), testAgentConfig(cfg),
		"prompt", roundInput, testRoundParams(t), autofixdata.OutputPatch,
	)
	require.Error(t, err)

	var fallbackErr *patchFallbackError
	require.ErrorAs(t, err, &fallbackErr)
	require.ErrorContains(t, err, "ai.redact-secrets=true")
}

func TestResolver_Resolve_CommandFamilyNormalizeReturnsFocusedEdit(t *testing.T) {
	t.Parallel()

	cfg := config.Default()
	cfg.AI.Enabled = true
	cfg.AI.Timeout = "5s"
	cfg.AI.Command = []string{"stub"}
	cfg.AI.RedactSecrets = false

	r := &resolver{
		runner: &stubAgentRunner{texts: []string{
			"```diff\n" +
				"diff --git a/Dockerfile b/Dockerfile\n" +
				"--- a/Dockerfile\n" +
				"+++ b/Dockerfile\n" +
				"@@ -1,3 +1,3 @@\n" +
				" FROM ubuntu:22.04\n" +
				" RUN wget -qO- https://example.com/bootstrap.sh >/dev/null\n" +
				"-RUN curl -sS https://example.com/install.sh | sh\n" +
				"+RUN wget -nv -O- https://example.com/install.sh | sh\n" +
				"```\n",
		}},
	}

	fixed, err := r.Resolve(context.Background(), fix.ResolveContext{
		FilePath: "Dockerfile",
		Content: []byte(
			"FROM ubuntu:22.04\n" +
				"RUN wget -qO- https://example.com/bootstrap.sh >/dev/null\n" +
				"RUN curl -sS https://example.com/install.sh | sh\n",
		),
	}, &rules.SuggestedFix{
		NeedsResolve: true,
		ResolverID:   autofixdata.ResolverID,
		ResolverData: commandFamilyNormalizeRequest(cfg),
	})
	require.NoError(t, err)
	require.Len(t, fixed, 1)
	require.Equal(
		t,
		rules.NewRangeLocation("Dockerfile", 3, len("RUN "), 3, len("RUN curl -sS https://example.com/install.sh")),
		fixed[0].Location,
	)
	require.Equal(t, "wget -nv -O- https://example.com/install.sh", fixed[0].NewText)
}

func TestResolver_Resolve_ResolvedEditsBuilderRejectsEmptyEdits(t *testing.T) {
	t.Parallel()

	cfg := config.Default()
	cfg.AI.Enabled = true
	cfg.AI.Timeout = "5s"
	cfg.AI.Command = []string{"stub"}
	cfg.AI.RedactSecrets = false

	r := &resolver{
		runner: &stubAgentRunner{texts: []string{
			"```diff\n" +
				"diff --git a/Dockerfile b/Dockerfile\n" +
				"--- a/Dockerfile\n" +
				"+++ b/Dockerfile\n" +
				"@@ -1 +1 @@\n" +
				"-FROM alpine:3.20\n" +
				"+FROM alpine:3.21\n" +
				"```\n",
		}},
	}

	_, err := r.Resolve(context.Background(), fix.ResolveContext{
		FilePath: "Dockerfile",
		Content:  []byte("FROM alpine:3.20\n"),
	}, &rules.SuggestedFix{
		NeedsResolve: true,
		ResolverID:   autofixdata.ResolverID,
		ResolverData: emptyEditsRequest(cfg),
	})
	require.Error(t, err)
	require.ErrorContains(t, err, "resolved edit builder produced no edits")
}
