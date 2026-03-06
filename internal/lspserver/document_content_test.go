package lspserver

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadValidatedFileContent_UsesConfiguredMaxFileSize(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	filePath := filepath.Join(dir, "Dockerfile")
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".tally.toml"), []byte("[file-validation]\nmax-file-size = 6\n"), 0o644))
	require.NoError(t, os.WriteFile(filePath, []byte("FROM alpine\n"), 0o644))

	s := New()
	content, ok := s.readValidatedFileContent(filePath)

	assert.False(t, ok)
	assert.Nil(t, content)
}

func TestReadValidatedFileContent_RejectsInvalidUTF8(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	filePath := filepath.Join(dir, "Dockerfile")
	require.NoError(t, os.WriteFile(filePath, []byte("FROM a\n\xff"), 0o644))

	s := New()
	content, ok := s.readValidatedFileContent(filePath)

	assert.False(t, ok)
	assert.Nil(t, content)
}

func TestReadValidatedFileContent_ReturnsValidFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	filePath := filepath.Join(dir, "Dockerfile")
	want := []byte("FROM alpine\nRUN echo hello\n")
	require.NoError(t, os.WriteFile(filePath, want, 0o644))

	s := New()
	content, ok := s.readValidatedFileContent(filePath)

	require.True(t, ok)
	assert.Equal(t, want, content)
}
