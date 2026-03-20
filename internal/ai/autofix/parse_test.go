package autofix

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/wharflab/tally/internal/ai/autofixdata"
)

func TestParseRoundOutput_PatchModeAppliesUnifiedDiff(t *testing.T) {
	t.Parallel()

	diff := "diff --git a/Dockerfile b/Dockerfile\n" +
		"--- a/Dockerfile\n" +
		"+++ b/Dockerfile\n" +
		"@@ -1,5 +1,9 @@\n" +
		"-FROM golang:1.22-alpine\n" +
		"+FROM golang:1.22-alpine AS builder\n" +
		" WORKDIR /src\n" +
		" COPY . .\n" +
		" RUN go build -o /out/app ./cmd/app\n" +
		"+\n" +
		"+FROM alpine:3.20\n" +
		"+WORKDIR /src\n" +
		"+COPY --from=builder /out/app /usr/local/bin/app\n" +
		" CMD [\"app\"]\n"
	text := "```diff\n" +
		diff +
		"```"
	base := []byte("FROM golang:1.22-alpine\nWORKDIR /src\nCOPY . .\nRUN go build -o /out/app ./cmd/app\nCMD [\"app\"]\n")

	parsed, noChange, err := parseAgentPatchResponse(text)
	require.NoError(t, err)
	require.False(t, noChange)
	require.Equal(t, diff, parsed)

	result, err := parseRoundOutput(text, base, autofixdata.OutputPatch)
	require.NoError(t, err)
	require.False(t, result.noChange)
	require.Contains(t, string(result.proposed), "FROM alpine:3.20")
}

func TestParseAgentPatchResponse_PreservesTrailingNewline(t *testing.T) {
	t.Parallel()

	text := "```diff\n" +
		"diff --git a/Dockerfile b/Dockerfile\n" +
		"--- a/Dockerfile\n" +
		"+++ b/Dockerfile\n" +
		"@@ -1,5 +1,9 @@\n" +
		"-FROM golang:1.22-alpine\n" +
		"+FROM golang:1.22-alpine AS builder\n" +
		" WORKDIR /src\n" +
		" COPY . .\n" +
		" RUN go build -o /out/app ./cmd/app\n" +
		"+\n" +
		"+FROM alpine:3.20\n" +
		"+WORKDIR /src\n" +
		"+COPY --from=builder /out/app /usr/local/bin/app\n" +
		" CMD [\"app\"]\n" +
		"```"
	parsed, _, err := parseAgentPatchResponse(text)
	require.NoError(t, err)
	require.True(t, strings.HasSuffix(parsed, "\n"))
}

func TestParseAgentPatchResponse_RejectsEmptyFencedBlock(t *testing.T) {
	t.Parallel()

	_, _, err := parseAgentPatchResponse("```diff\n```")
	require.EqualError(t, err, "empty diff patch code block")
}
