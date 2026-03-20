package autofix

import (
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/stretchr/testify/require"

	"github.com/wharflab/tally/internal/ai/autofixdata"
	tallyrules "github.com/wharflab/tally/internal/rules/tally"
	"github.com/wharflab/tally/internal/testutil"
)

func TestBuildRound1Prompt_PreferMultiStageBuild_Snapshot(t *testing.T) {
	t.Parallel()

	content := `FROM golang:1.22-alpine
WORKDIR /app
COPY . .
RUN go build -o /out/app ./cmd/app
CMD ["app"]
`

	input := testutil.MakeLintInput(t, "Dockerfile", content)
	rule := tallyrules.NewPreferMultiStageBuildRule()
	violations := rule.Check(input)
	require.Len(t, violations, 1)
	require.NotNil(t, violations[0].SuggestedFix)

	data, ok := violations[0].SuggestedFix.ResolverData.(*autofixdata.ObjectiveRequest)
	require.True(t, ok, "expected ObjectiveRequest, got %T", violations[0].SuggestedFix.ResolverData)

	obj, ok := getObjective(data.Kind)
	require.True(t, ok, "objective %q not registered", data.Kind)

	origParse, err := parseDockerfile(input.Source, nil)
	require.NoError(t, err)

	prompt, err := obj.BuildPrompt(PromptContext{
		FilePath:  input.File,
		Source:    input.Source,
		Request:   data,
		Config:    nil,
		OrigParse: origParse,
		Mode:      agentOutputPatch,
	})
	require.NoError(t, err)

	snaps.WithConfig(snaps.Ext(".md")).MatchStandaloneSnapshot(t, prompt)
}

func TestBuildRound1Prompt_PreferMultiStageBuild_RegistryContext_Snapshot(t *testing.T) {
	t.Parallel()

	content := `FROM golang:1.22-alpine
WORKDIR /app
COPY . .
RUN go build -o /out/app ./cmd/app
CMD ["app"]
`

	input := testutil.MakeLintInput(t, "Dockerfile", content)
	rule := tallyrules.NewPreferMultiStageBuildRule()
	violations := rule.Check(input)
	require.Len(t, violations, 1)
	require.NotNil(t, violations[0].SuggestedFix)

	data, ok := violations[0].SuggestedFix.ResolverData.(*autofixdata.ObjectiveRequest)
	require.True(t, ok, "expected ObjectiveRequest, got %T", violations[0].SuggestedFix.ResolverData)

	data.RegistryInsights = []autofixdata.RegistryInsight{
		{
			StageIndex:        0,
			Ref:               "golang:1.22-alpine",
			RequestedPlatform: "linux/amd64",
			ResolvedPlatform:  "linux/amd64",
			Digest:            "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		},
	}

	obj, ok := getObjective(data.Kind)
	require.True(t, ok, "objective %q not registered", data.Kind)

	origParse, err := parseDockerfile(input.Source, nil)
	require.NoError(t, err)

	prompt, err := obj.BuildPrompt(PromptContext{
		FilePath:  input.File,
		Source:    input.Source,
		Request:   data,
		Config:    nil,
		OrigParse: origParse,
		Mode:      agentOutputPatch,
	})
	require.NoError(t, err)

	snaps.WithConfig(snaps.Ext(".md")).MatchStandaloneSnapshot(t, prompt)
}

func TestBuildRound1Prompt_FileContext_Snapshot(t *testing.T) {
	t.Parallel()

	content := `FROM golang:1.22-alpine
WORKDIR /app
COPY . .
RUN go build -o /out/app ./cmd/app
CMD ["app"]
`

	input := testutil.MakeLintInput(t, "Dockerfile", content)
	rule := tallyrules.NewPreferMultiStageBuildRule()
	violations := rule.Check(input)
	require.Len(t, violations, 1)

	data, ok := violations[0].SuggestedFix.ResolverData.(*autofixdata.ObjectiveRequest)
	require.True(t, ok)

	obj, ok := getObjective(data.Kind)
	require.True(t, ok)

	origParse, err := parseDockerfile(input.Source, nil)
	require.NoError(t, err)

	prompt, err := obj.BuildPrompt(PromptContext{
		FilePath:   input.File,
		Source:     input.Source,
		Request:    data,
		AbsPath:    "/home/user/project/Dockerfile",
		ContextDir: "/home/user/project",
		OrigParse:  origParse,
		Mode:       agentOutputPatch,
	})
	require.NoError(t, err)

	snaps.WithConfig(snaps.Ext(".md")).MatchStandaloneSnapshot(t, prompt)
}

func TestBuildRound1Prompt_FileContext_Variants(t *testing.T) {
	t.Parallel()

	content := `FROM golang:1.22-alpine
WORKDIR /app
COPY . .
RUN go build -o /out/app ./cmd/app
CMD ["app"]
`

	input := testutil.MakeLintInput(t, "Dockerfile", content)
	rule := tallyrules.NewPreferMultiStageBuildRule()
	violations := rule.Check(input)
	require.Len(t, violations, 1)

	data, ok := violations[0].SuggestedFix.ResolverData.(*autofixdata.ObjectiveRequest)
	require.True(t, ok)

	obj, ok := getObjective(data.Kind)
	require.True(t, ok)

	origParse, err := parseDockerfile(input.Source, nil)
	require.NoError(t, err)

	withCtx, err := obj.BuildPrompt(PromptContext{
		FilePath:   input.File,
		Source:     input.Source,
		Request:    data,
		AbsPath:    "/home/user/project/Dockerfile",
		ContextDir: "/home/user/project",
		OrigParse:  origParse,
		Mode:       agentOutputPatch,
	})
	require.NoError(t, err)
	require.Contains(t, withCtx, "- Path: /home/user/project/Dockerfile")
	require.Contains(t, withCtx, "- Build context: /home/user/project")

	withoutCtx, err := obj.BuildPrompt(PromptContext{
		FilePath:  input.File,
		Source:    input.Source,
		Request:   data,
		AbsPath:   "/home/user/project/Dockerfile",
		OrigParse: origParse,
		Mode:      agentOutputPatch,
	})
	require.NoError(t, err)
	require.Contains(t, withoutCtx, "- Path: /home/user/project/Dockerfile")
	require.NotContains(t, withoutCtx, "Build context:")

	noPath, err := obj.BuildPrompt(PromptContext{
		FilePath:  input.File,
		Source:    input.Source,
		Request:   data,
		OrigParse: origParse,
		Mode:      agentOutputPatch,
	})
	require.NoError(t, err)
	require.NotContains(t, noPath, "File context:")
}
