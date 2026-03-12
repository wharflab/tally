package autofix

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/wharflab/tally/internal/ai/acp"
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

func TestResolver_RunAndParseRound_NoChange_ShortCircuits(t *testing.T) {
	t.Parallel()

	cfg := config.Default()
	cfg.AI.Command = []string{"stub"}
	cfg.AI.RedactSecrets = false

	r := &resolver{
		runner: &stubAgentRunner{texts: []string{"NO_CHANGE"}},
	}

	out, err := r.runRound(context.Background(), "Dockerfile", cfg, 5*time.Second, "prompt", []byte("FROM alpine:3.20\n"), agentOutputPatch)
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

	out, err := r.runRound(context.Background(), "Dockerfile", cfg, 5*time.Second, "prompt", []byte("FROM alpine:3.20\n"), agentOutputPatch)
	require.NoError(t, err)
	require.True(t, out.noChange)
	require.Nil(t, out.proposed)
}
