package semantic

import (
	"testing"

	"github.com/moby/buildkit/frontend/dockerfile/shell"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFromEnv_KeysSorted(t *testing.T) {
	t.Parallel()

	env := newFromEnv(map[string]string{
		"b": "2",
		"a": "1",
		"c": "3",
	})

	assert.Equal(t, []string{"a", "b", "c"}, env.Keys())
}

func TestDefaultFromArgs_DefaultTargetStageAndOverrides(t *testing.T) {
	t.Parallel()

	args := defaultFromArgs("", nil)
	assert.Equal(t, defaultTargetStageName, args["TARGETSTAGE"])
	assert.NotEmpty(t, args["BUILDPLATFORM"])
	assert.NotEmpty(t, args["TARGETPLATFORM"])

	overridden := defaultFromArgs("stage", map[string]string{
		"BUILDPLATFORM": "custom-build",
		"TARGETSTAGE":   "custom-stage",
	})
	assert.Equal(t, "custom-build", overridden["BUILDPLATFORM"])
	assert.Equal(t, "custom-stage", overridden["TARGETSTAGE"])
}

func TestScopeArgKeys_NilScope(t *testing.T) {
	t.Parallel()

	keys, set := scopeArgKeys(nil)
	assert.Nil(t, keys)
	assert.Nil(t, set)
}

func TestUndefinedFromArgs_EarlyReturnsAndFiltering(t *testing.T) {
	t.Parallel()

	env := newFromEnv(nil)
	assert.Nil(t, undefinedFromArgs("scratch", nil, env, nil, nil))

	shlex := shell.NewLex('\\')
	assert.Nil(t, undefinedFromArgs("scratch", shlex, nil, nil, nil))

	// All variables matched: no Unmatched keys from shlex.
	env = newFromEnv(map[string]string{"tag": "latest"})
	assert.Nil(t, undefinedFromArgs("busybox:${tag}", shlex, env, nil, nil))

	// Variable is unmatched, but known in the global scope: should not be reported.
	env = newFromEnv(nil)
	knownSet := map[string]struct{}{"tag": {}}
	assert.Nil(t, undefinedFromArgs("busybox:${tag}", shlex, env, knownSet, nil))
}

func TestUndefinedFromArgs_UndefinedSortedAndSuggested(t *testing.T) {
	t.Parallel()

	shlex := shell.NewLex('\\')
	env := newFromEnv(nil)

	refs := undefinedFromArgs("busybox:${FOO}${BULIDPLATFORM}", shlex, env, nil, []string{"BUILDPLATFORM"})
	if assert.Len(t, refs, 2) {
		assert.Equal(t, "BULIDPLATFORM", refs[0].Name)
		assert.Equal(t, "BUILDPLATFORM", refs[0].Suggest)
		assert.Equal(t, "FOO", refs[1].Name)
		assert.Empty(t, refs[1].Suggest)
	}
}

func TestUndefinedFromArgs_ProcessWordError(t *testing.T) {
	t.Parallel()

	shlex := shell.NewLex('\\')
	env := newFromEnv(nil)
	assert.Nil(t, undefinedFromArgs("${foo", shlex, env, nil, nil))
}

func TestInvalidDefaultBaseName(t *testing.T) {
	t.Parallel()

	invalid, err := invalidDefaultBaseName("scratch", nil, nil)
	require.NoError(t, err)
	assert.False(t, invalid)

	shlex := shell.NewLex('\\')

	// Valid image after expansion.
	env := newFromEnv(map[string]string{"tag": "latest"})
	invalid, err = invalidDefaultBaseName("busybox:${tag}", shlex, env)
	require.NoError(t, err)
	assert.False(t, invalid)

	// Unmatched variable expands to empty, resulting in invalid image name.
	env = newFromEnv(nil)
	invalid, err = invalidDefaultBaseName("busybox:${tag}", shlex, env)
	require.NoError(t, err)
	assert.True(t, invalid)

	// Invalid word expansion (parse error).
	invalid, err = invalidDefaultBaseName("${foo", shlex, env)
	require.Error(t, err)
	assert.False(t, invalid)
}
