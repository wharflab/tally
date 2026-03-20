package autofix

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/zricethezav/gitleaks/v8/detect"

	"github.com/wharflab/tally/internal/ai/acp"
	"github.com/wharflab/tally/internal/ai/autofixdata"
	"github.com/wharflab/tally/internal/config"
)

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

func multiStageObj() Objective {
	obj, _ := getObjective(autofixdata.ObjectiveMultiStage)
	return obj
}

func testAgentConfig(cfg *config.Config) agentConfig {
	return agentConfig{cfg: cfg, timeout: 5 * time.Second}
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
		context.Background(), "Dockerfile", testAgentConfig(cfg),
		"prompt", []byte("FROM alpine:3.20\n"), multiStageObj(), agentOutputPatch,
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
		context.Background(), "Dockerfile", testAgentConfig(cfg),
		"prompt", []byte("FROM alpine:3.20\n"), multiStageObj(), agentOutputPatch,
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
		gitleaksFactory: detect.NewDetectorDefaultConfig,
	}

	roundInput := []byte(
		"FROM alpine:3.20\n" +
			"ENV GITHUB_TOKEN=ghp_123456789012345678901234567890123456\n",
	)

	_, err := r.runRound(
		context.Background(), "Dockerfile", testAgentConfig(cfg),
		"prompt", roundInput, multiStageObj(), agentOutputPatch,
	)
	require.Error(t, err)

	var fallbackErr *patchFallbackError
	require.ErrorAs(t, err, &fallbackErr)
	require.ErrorContains(t, err, "ai.redact-secrets=true")
}
