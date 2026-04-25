package semantic

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInitFromArgEval_UsesAutomaticArgsWithAndWithoutOverrides(t *testing.T) {
	t.Parallel()

	pr := parseDockerfile(t, "FROM scratch\n")
	b := NewBuilder(pr, map[string]string{
		"BUILDPLATFORM": "override-build-platform",
	}, "Dockerfile")

	eval := b.initFromArgEval(pr.MetaArgs, effectiveTargetStageName(pr.Stages, ""))

	gotEffective, ok := eval.effectiveEnv.Get("BUILDPLATFORM")
	require.True(t, ok)
	assert.Equal(t, "override-build-platform", gotEffective)

	gotDefaults, ok := eval.defaultsEnv.Get("BUILDPLATFORM")
	require.True(t, ok)
	assert.NotEqual(t, "override-build-platform", gotDefaults)
}
