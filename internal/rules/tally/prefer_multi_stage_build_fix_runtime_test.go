package tally

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/wharflab/tally/internal/ai/autofixdata"
	"github.com/wharflab/tally/internal/dockerfile"
	patchutil "github.com/wharflab/tally/internal/patch"
)

func parseDockerfileForTest(t *testing.T, content string) *dockerfile.ParseResult {
	t.Helper()
	parsed, err := dockerfile.Parse(bytes.NewReader([]byte(content)), nil)
	require.NoError(t, err)
	return parsed
}

func TestRuntimeValidationErrors_AggregatesMultipleViolations(t *testing.T) {
	t.Parallel()

	orig := parseDockerfileForTest(t, "FROM golang:1.22-alpine\nWORKDIR /src\nCMD [\"app\"]\n")
	proposed := parseDockerfileForTest(t, "FROM golang:1.22-alpine\nWORKDIR /app\nCMD [\"/app/app\"]\n")

	errs := runtimeValidationErrors(orig, proposed)
	require.Len(t, errs, 2)

	messages := make([]string, 0, len(errs))
	for _, err := range errs {
		messages = append(messages, err.Error())
	}

	wantCMDChange := `proposed Dockerfile changed CMD in the final stage (want "CMD [\"app\"]", got "CMD [\"/app/app\"]")`
	require.Contains(t, messages, wantCMDChange)
	require.Contains(t, messages, "proposed Dockerfile changed WORKDIR in the final stage (want \"/src\", got \"/app\")")

	// Identical input produces no errors.
	require.Empty(t, runtimeValidationErrors(orig, orig))
}

func TestMultiStageObjective_ValidatePatch_AlwaysNil(t *testing.T) {
	t.Parallel()

	obj := &multiStageObjective{}

	// Patch with FROM — should pass.
	require.Empty(t, obj.ValidatePatch(nil, patchutil.Meta{
		AddedLines: []string{"FROM golang:1.22 AS builder"},
	}))

	// Patch without FROM (e.g. a round-2 fix-up) — should also pass.
	// Stage-count is enforced by ValidateProposal, not ValidatePatch.
	require.Empty(t, obj.ValidatePatch(nil, patchutil.Meta{
		AddedLines: []string{"CMD [\"app\"]"},
	}))
}

func TestMultiStageObjective_ValidateProposal(t *testing.T) {
	t.Parallel()

	obj := &multiStageObjective{}

	orig := parseDockerfileForTest(t, "FROM alpine:3.20\nCMD [\"app\"]\n")

	// Single-stage proposal should be blocked.
	single := parseDockerfileForTest(t, "FROM alpine:3.20\nCMD [\"app\"]\n")
	blocking := obj.ValidateProposal(nil, orig, single)
	require.NotEmpty(t, blocking)
	require.Equal(t, "semantics", blocking[0].Rule)

	// Multi-stage proposal with preserved runtime should pass.
	multi := parseDockerfileForTest(t, "FROM alpine:3.20 AS builder\nRUN echo\nFROM alpine:3.20\nCMD [\"app\"]\n")
	require.Empty(t, obj.ValidateProposal(nil, orig, multi))
}

func TestMultiStageObjective_Kind(t *testing.T) {
	t.Parallel()
	obj := &multiStageObjective{}
	require.Equal(t, autofixdata.ObjectiveMultiStage, obj.Kind())
}
