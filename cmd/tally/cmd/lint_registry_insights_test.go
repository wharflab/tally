package cmd

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tinovyatkin/tally/internal/async"
	"github.com/tinovyatkin/tally/internal/registry"
)

func TestCollectRegistryInsights_SortsAndDedupes(t *testing.T) {
	t.Parallel()

	plans := []async.CheckRequest{
		{
			RuleCode:   "rule-a",
			Key:        "alpine:3.20|linux/amd64",
			ResolverID: registry.RegistryResolverID(),
			Data:       &registry.ResolveRequest{Ref: "alpine:3.20", Platform: "linux/amd64"},
			File:       "foo/Dockerfile",
			StageIndex: 2,
		},
		{
			RuleCode:   "rule-b",
			Key:        "golang:1.22|linux/amd64",
			ResolverID: registry.RegistryResolverID(),
			Data:       &registry.ResolveRequest{Ref: "golang:1.22", Platform: "linux/amd64"},
			File:       "foo/Dockerfile",
			StageIndex: 0,
		},
		// Duplicate request for the same stage/key (multiple rules share the same resolver unit).
		{
			RuleCode:   "rule-c",
			Key:        "golang:1.22|linux/amd64",
			ResolverID: registry.RegistryResolverID(),
			Data:       &registry.ResolveRequest{Ref: "golang:1.22", Platform: "linux/amd64"},
			File:       "foo/Dockerfile",
			StageIndex: 0,
		},
	}

	result := &async.RunResult{
		Resolved: map[async.ResolutionKey]any{
			{ResolverID: registry.RegistryResolverID(), Key: "golang:1.22|linux/amd64"}: &registry.ImageConfig{
				OS:     "linux",
				Arch:   "amd64",
				Digest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			},
			{ResolverID: registry.RegistryResolverID(), Key: "alpine:3.20|linux/amd64"}: &registry.PlatformMismatchError{
				Ref:       "alpine:3.20",
				Requested: "linux/amd64",
				Available: []string{"linux/arm64", "linux/amd64"},
			},
		},
	}

	got := collectRegistryInsights(plans, result)
	require.Len(t, got, 1)

	list, ok := got["foo/Dockerfile"]
	require.True(t, ok)
	require.Len(t, list, 2)

	// Sorted by stage index.
	require.Equal(t, 0, list[0].StageIndex)
	require.Equal(t, "golang:1.22", list[0].Ref)
	require.Equal(t, "linux/amd64", list[0].RequestedPlatform)
	require.Equal(t, "linux/amd64", list[0].ResolvedPlatform)
	require.Equal(t, "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", list[0].Digest)

	require.Equal(t, 2, list[1].StageIndex)
	require.Equal(t, "alpine:3.20", list[1].Ref)
	require.Equal(t, "linux/amd64", list[1].RequestedPlatform)
	require.Empty(t, list[1].ResolvedPlatform)
	require.Equal(t, []string{"linux/arm64", "linux/amd64"}, list[1].AvailablePlatforms)
}
