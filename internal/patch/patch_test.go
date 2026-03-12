package patch

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseAndApply_SucceedsForSingleFileDiff(t *testing.T) {
	t.Parallel()

	base := []byte("FROM golang:1.22-alpine\nWORKDIR /src\nCOPY . .\nRUN go build -o /out/app ./cmd/app\nCMD [\"app\"]\n")
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
	want := []byte(
		"FROM golang:1.22-alpine AS builder\n" +
			"WORKDIR /src\n" +
			"COPY . .\n" +
			"RUN go build -o /out/app ./cmd/app\n" +
			"\n" +
			"FROM alpine:3.20\n" +
			"WORKDIR /src\n" +
			"COPY --from=builder /out/app /usr/local/bin/app\n" +
			"CMD [\"app\"]\n",
	)

	got, meta, err := ParseAndApply(base, diff)
	require.NoError(t, err)
	require.Equal(t, want, got)
	require.Equal(t, 5, meta.AddedLineCount)
	require.Contains(t, meta.AddedLines, "FROM alpine:3.20")
}

func TestParseAndApply_AcceptsMockFilenameWhenPatchApplies(t *testing.T) {
	t.Parallel()

	base := []byte("FROM golang:1.22-alpine\nWORKDIR /src\nCOPY . .\nRUN go build -o /out/app ./cmd/app\nCMD [\"app\"]\n")
	diff := "diff --git a/mock-input.txt b/mock-output.txt\n" +
		"--- a/mock-input.txt\n" +
		"+++ b/mock-output.txt\n" +
		"@@ -1,5 +1,9 @@\n" +
		"-FROM golang:1.22-alpine\n" +
		"+FROM golang:1.22-alpine AS builder\n" +
		" WORKDIR /src\n" +
		" COPY . .\n" +
		" RUN go build -o /out/app ./cmd/app\n" +
		"+\n" +
		"+FROM alpine:3.19\n" +
		"+WORKDIR /src\n" +
		"+COPY --from=builder /out/app /usr/local/bin/app\n" +
		" CMD [\"app\"]\n"
	want := []byte(
		"FROM golang:1.22-alpine AS builder\n" +
			"WORKDIR /src\n" +
			"COPY . .\n" +
			"RUN go build -o /out/app ./cmd/app\n" +
			"\n" +
			"FROM alpine:3.19\n" +
			"WORKDIR /src\n" +
			"COPY --from=builder /out/app /usr/local/bin/app\n" +
			"CMD [\"app\"]\n",
	)

	got, meta, err := ParseAndApply(base, diff)
	require.NoError(t, err)
	require.Equal(t, want, got)
	require.Equal(t, "mock-output.txt", meta.NewName)
}

func TestParseAndApply_PreservesMissingTrailingNewline(t *testing.T) {
	t.Parallel()

	base := []byte("FROM alpine:3.20")
	diff := "--- a/Dockerfile\n" +
		"+++ b/Dockerfile\n" +
		"@@ -1 +1 @@\n" +
		"-FROM alpine:3.20\n" +
		"\\ No newline at end of file\n" +
		"+FROM alpine:3.21\n" +
		"\\ No newline at end of file\n"

	got, _, err := ParseAndApply(base, diff)
	require.NoError(t, err)
	require.Equal(t, []byte("FROM alpine:3.21"), got)
}
