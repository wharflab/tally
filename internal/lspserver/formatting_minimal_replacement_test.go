package lspserver

import (
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMinimalReplacement_DoesNotSplitRunes_PrefixSharedBytes(t *testing.T) {
	t.Parallel()

	original := []byte("a🙂b")
	modified := []byte("a🙃b")

	start, end, replacement, ok := minimalReplacement(original, modified)
	require.True(t, ok)

	assert.Equal(t, len([]byte("a")), start)
	assert.Equal(t, len([]byte("a🙂")), end)
	assert.Equal(t, "🙃", string(replacement))
	assert.True(t, utf8.Valid(replacement))

	replEnd := start + len(replacement)
	assert.True(t, utf8.Valid(original[:start]))
	assert.True(t, utf8.Valid(original[start:end]))
	assert.True(t, utf8.Valid(original[end:]))
	assert.True(t, utf8.Valid(modified[:start]))
	assert.True(t, utf8.Valid(modified[start:replEnd]))
	assert.True(t, utf8.Valid(modified[replEnd:]))
}

func TestMinimalReplacement_DoesNotSplitRunes_SuffixSharedBytes(t *testing.T) {
	t.Parallel()

	// These runes share the trailing UTF-8 byte, so byte-wise suffix scanning would
	// incorrectly match a partial rune.
	original := []byte("xé") // C3 A9
	modified := []byte("xĩ") // C4 A9

	start, end, replacement, ok := minimalReplacement(original, modified)
	require.True(t, ok)

	assert.Equal(t, len([]byte("x")), start)
	assert.Equal(t, len(original), end)
	assert.Equal(t, "ĩ", string(replacement))
	assert.True(t, utf8.Valid(replacement))

	replEnd := start + len(replacement)
	assert.True(t, utf8.Valid(original[:start]))
	assert.True(t, utf8.Valid(original[start:end]))
	assert.True(t, utf8.Valid(original[end:]))
	assert.True(t, utf8.Valid(modified[:start]))
	assert.True(t, utf8.Valid(modified[start:replEnd]))
	assert.True(t, utf8.Valid(modified[replEnd:]))
}
