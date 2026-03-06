package lspserver

import (
	"context"
	"sync"

	"github.com/wharflab/tally/internal/highlight"
)

var analyzeSemanticDocument = highlight.Analyze

type semanticDocCache struct {
	mu      sync.RWMutex
	entries map[string]semanticDocCacheEntry
}

type semanticDocCacheEntry struct {
	resultID string
	doc      *highlight.Document
}

func newSemanticDocCache() *semanticDocCache {
	return &semanticDocCache{entries: make(map[string]semanticDocCacheEntry)}
}

func (c *semanticDocCache) get(uri, resultID string) (*highlight.Document, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	entry, ok := c.entries[uri]
	if !ok || entry.resultID != resultID || entry.doc == nil {
		return nil, false
	}
	return entry.doc, true
}

func (c *semanticDocCache) set(uri, resultID string, doc *highlight.Document) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[uri] = semanticDocCacheEntry{resultID: resultID, doc: doc}
}

func (c *semanticDocCache) delete(uri string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, uri)
}

func (s *Server) getOrAnalyzeSemanticDocument(
	ctx context.Context,
	uri string,
) (*highlight.Document, string, bool) {
	if err := ctx.Err(); err != nil {
		return nil, "", false
	}

	content, ok := s.semanticTokenContent(uri)
	if !ok {
		return nil, "", false
	}
	resultID := contentHash(content)
	if doc, ok := s.semCache.get(uri, resultID); ok {
		return doc, resultID, true
	}

	if err := ctx.Err(); err != nil {
		return nil, "", false
	}

	doc := analyzeSemanticDocument(uriToPath(uri), content)

	if err := ctx.Err(); err != nil {
		return nil, "", false
	}

	s.semCache.set(uri, resultID, doc)
	return doc, resultID, true
}
