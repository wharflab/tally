package lspserver

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDocumentStoreAll_SkipsNilAndReturnsCopies(t *testing.T) {
	t.Parallel()

	store := NewDocumentStore()
	store.Open("file:///tmp/Dockerfile", "dockerfile", 1, "FROM alpine:3.20\n")

	store.mu.Lock()
	store.docs["file:///tmp/nil.Dockerfile"] = nil
	store.mu.Unlock()

	docs := store.All()
	require.Len(t, docs, 1)
	require.Equal(t, "file:///tmp/Dockerfile", docs[0].URI)

	docs[0].Content = "MUTATED"
	got := store.Get("file:///tmp/Dockerfile")
	require.NotNil(t, got)
	require.Equal(t, "FROM alpine:3.20\n", got.Content)
}
