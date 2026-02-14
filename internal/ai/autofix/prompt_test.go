package autofix

import (
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/stretchr/testify/require"

	"github.com/tinovyatkin/tally/internal/ai/autofixdata"
	tallyrules "github.com/tinovyatkin/tally/internal/rules/tally"
	"github.com/tinovyatkin/tally/internal/testutil"
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

	data, ok := violations[0].SuggestedFix.ResolverData.(*autofixdata.MultiStageResolveData)
	require.True(t, ok, "expected MultiStageResolveData, got %T", violations[0].SuggestedFix.ResolverData)

	origParse, err := parseDockerfile(input.Source, nil)
	require.NoError(t, err)

	prompt, err := buildRound1Prompt(input.File, input.Source, data, nil, origParse)
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

	data, ok := violations[0].SuggestedFix.ResolverData.(*autofixdata.MultiStageResolveData)
	require.True(t, ok, "expected MultiStageResolveData, got %T", violations[0].SuggestedFix.ResolverData)

	data.RegistryInsights = []autofixdata.RegistryInsight{
		{
			StageIndex:        0,
			Ref:               "golang:1.22-alpine",
			RequestedPlatform: "linux/amd64",
			ResolvedPlatform:  "linux/amd64",
			Digest:            "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		},
	}

	origParse, err := parseDockerfile(input.Source, nil)
	require.NoError(t, err)

	prompt, err := buildRound1Prompt(input.File, input.Source, data, nil, origParse)
	require.NoError(t, err)

	snaps.WithConfig(snaps.Ext(".md")).MatchStandaloneSnapshot(t, prompt)
}
