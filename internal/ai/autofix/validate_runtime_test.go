package autofix

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

	patchutil "github.com/wharflab/tally/internal/patch"
)

func TestCollectRuntimeValidationErrors_AggregatesMultipleViolations(t *testing.T) {
	t.Parallel()

	orig, err := parseDockerfile([]byte("FROM golang:1.22-alpine\nWORKDIR /src\nCMD [\"app\"]\n"), nil)
	require.NoError(t, err)

	proposed, err := parseDockerfile([]byte("FROM golang:1.22-alpine\nWORKDIR /app\nCMD [\"/app/app\"]\n"), nil)
	require.NoError(t, err)

	errs := collectRuntimeValidationErrors(orig, proposed)
	require.Len(t, errs, 2)

	messages := make([]string, 0, len(errs))
	for _, err := range errs {
		messages = append(messages, err.Error())
	}

	wantCMDChange := `proposed Dockerfile changed CMD in the final stage (want "CMD [\"app\"]", got "CMD [\"/app/app\"]")`
	require.Contains(
		t,
		messages,
		wantCMDChange,
	)
	require.Contains(t, messages, "proposed Dockerfile changed WORKDIR in the final stage (want \"/src\", got \"/app\")")
	require.NoError(t, validateRuntimeSettings(orig, orig))
	require.False(t, bytes.Equal([]byte(messages[0]), []byte{}))

	firstErr := validateRuntimeSettings(orig, proposed)
	require.Error(t, firstErr)
	require.Equal(t, errs[0].Error(), firstErr.Error())
}

func TestValidateMultiStagePatch_AcceptsFromWithTabSeparatedArgs(t *testing.T) {
	t.Parallel()

	meta := patchutil.Meta{
		AddedLines: []string{"FROM\tgolang:1.22-alpine AS builder"},
	}

	require.Nil(t, validateMultiStagePatch(meta))
}
