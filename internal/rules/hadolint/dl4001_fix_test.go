package hadolint

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/wharflab/tally/internal/ai/autofixdata"
	"github.com/wharflab/tally/internal/dockerfile"
	"github.com/wharflab/tally/internal/rules"
)

func parseDL4001Dockerfile(t *testing.T, content string) *dockerfile.ParseResult {
	t.Helper()
	parsed, err := dockerfile.Parse(bytes.NewReader([]byte(content)), nil)
	require.NoError(t, err)
	return parsed
}

func testDL4001ObjectiveRequest() *autofixdata.ObjectiveRequest {
	return &autofixdata.ObjectiveRequest{
		Kind: autofixdata.ObjectiveCommandFamilyNormalize,
		File: "Dockerfile",
		Facts: map[string]any{
			"platform-os":            "linux",
			"shell-variant":          "bash",
			"preferred-tool":         "wget",
			"source-tool":            "curl",
			"target-start-line":      2,
			"target-end-line":        2,
			"target-start-col":       len("RUN "),
			"target-end-col":         len("RUN curl -fsSL https://example.com/app.tgz"),
			"target-command-text":    "curl -fsSL https://example.com/app.tgz",
			"target-run-script":      "curl -fsSL https://example.com/app.tgz | tar -xz -C /opt",
			"target-command-index":   0,
			"original-command-names": []string{"curl", "tar"},
			"literal-urls":           []string{"https://example.com/app.tgz"},
			"blockers":               []string{"deterministic lowering is unavailable for this command"},
		},
	}
}

func TestCommandFamilyNormalizeObjective_BuildPrompt_FocusedPatchContext(t *testing.T) {
	t.Parallel()

	source := []byte("FROM ubuntu:22.04\nRUN curl -fsSL https://example.com/app.tgz | tar -xz -C /opt\n")
	obj := &commandFamilyNormalizeObjective{}

	prompt, err := obj.BuildPrompt(autofixdata.PromptContext{
		FilePath: "Dockerfile",
		Source:   source,
		Request:  testDL4001ObjectiveRequest(),
		Mode:     autofixdata.OutputPatch,
	})
	require.NoError(t, err)
	require.Contains(t, prompt, "Rewrite the target curl command so it uses wget.")
	require.Contains(t, prompt, "Target command text: `curl -fsSL https://example.com/app.tgz`")
	require.Contains(t, prompt, "2 | RUN curl -fsSL https://example.com/app.tgz | tar -xz -C /opt")
	require.Contains(t, prompt, "```diff")
}

func TestCommandFamilyNormalizeObjective_ValidateProposal_LocalityAndRewrite(t *testing.T) {
	t.Parallel()

	req := testDL4001ObjectiveRequest()
	obj := &commandFamilyNormalizeObjective{}
	orig := parseDL4001Dockerfile(t, "FROM ubuntu:22.04\nRUN curl -fsSL https://example.com/app.tgz | tar -xz -C /opt\n")

	proposed := parseDL4001Dockerfile(t, "FROM ubuntu:22.04\nRUN wget -nv -O- https://example.com/app.tgz | tar -xz -C /opt\n")
	require.Empty(t, obj.ValidateProposal(req, orig, proposed))

	proposedOtherLine := parseDL4001Dockerfile(t, "FROM ubuntu:24.04\nRUN wget -nv -O- https://example.com/app.tgz | tar -xz -C /opt\n")
	blocking := obj.ValidateProposal(req, orig, proposedOtherLine)
	require.NotEmpty(t, blocking)
	require.Equal(t, "shape", blocking[0].Rule)
}

func TestCommandFamilyNormalizeObjective_BuildResolvedEdits(t *testing.T) {
	t.Parallel()

	req := testDL4001ObjectiveRequest()
	obj := &commandFamilyNormalizeObjective{}
	original := []byte("FROM ubuntu:22.04\nRUN curl -fsSL https://example.com/app.tgz | tar -xz -C /opt\n")
	proposed := []byte("FROM ubuntu:22.04\nRUN wget -nv -O- https://example.com/app.tgz | tar -xz -C /opt\n")

	edits, err := obj.BuildResolvedEdits("Dockerfile", original, proposed, req)
	require.NoError(t, err)
	require.Len(t, edits, 1)
	wantLoc := rules.NewRangeLocation(
		"Dockerfile",
		2,
		len("RUN "),
		2,
		len("RUN curl -fsSL https://example.com/app.tgz"),
	)
	require.Equal(
		t,
		wantLoc,
		edits[0].Location,
	)
	require.Equal(t, "wget -nv -O- https://example.com/app.tgz", edits[0].NewText)
}

func TestCommandFamilyNormalizeObjective_RunSourceScript_UsesOriginalSource(t *testing.T) {
	t.Parallel()

	parsed := parseDL4001Dockerfile(t, "FROM ubuntu:22.04\nRUN curl \"$URL\" \\\n    | sh -eux\n")
	run := findRunByStartLine(parsed, 2)
	require.NotNil(t, run)

	script := runSourceScript(parsed, run)
	require.True(t, strings.HasPrefix(script, "    curl "))
	require.Contains(t, script, "\"$URL\"")
	require.Contains(t, script, "\n    | sh -eux")
}
