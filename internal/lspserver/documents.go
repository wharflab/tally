package lspserver

import "sync"

// Document represents an open text document tracked by the LSP server.
type Document struct {
	// URI is the document URI (e.g., "file:///path/to/Dockerfile").
	URI string

	// LanguageID is the language identifier (e.g., "dockerfile").
	LanguageID string

	// Version is the document version as reported by the client.
	Version int32

	// Content is the current full text of the document.
	Content string
}

// DocumentStore manages open documents in the LSP server.
// It is safe for concurrent access.
type DocumentStore struct {
	mu   sync.RWMutex
	docs map[string]*Document
}

// NewDocumentStore creates a new empty document store.
func NewDocumentStore() *DocumentStore {
	return &DocumentStore{
		docs: make(map[string]*Document),
	}
}

// Open adds or replaces a document in the store.
func (s *DocumentStore) Open(uri, languageID string, version int32, content string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.docs[uri] = &Document{
		URI:        uri,
		LanguageID: languageID,
		Version:    version,
		Content:    content,
	}
}

// Update replaces the content and version of an existing document.
// Returns false if the document is not open.
func (s *DocumentStore) Update(uri string, version int32, content string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	doc, ok := s.docs[uri]
	if !ok {
		return false
	}
	doc.Version = version
	doc.Content = content
	return true
}

// Close removes a document from the store.
func (s *DocumentStore) Close(uri string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.docs, uri)
}

// Get retrieves a document by URI. Returns nil if not found.
func (s *DocumentStore) Get(uri string) *Document {
	s.mu.RLock()
	defer s.mu.RUnlock()
	doc, ok := s.docs[uri]
	if !ok || doc == nil {
		return nil
	}
	c := *doc
	return &c
}

// All returns a snapshot slice of all currently open documents.
func (s *DocumentStore) All() []*Document {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Document, 0, len(s.docs))
	for _, doc := range s.docs {
		if doc == nil {
			continue
		}
		c := *doc
		out = append(out, &c)
	}
	return out
}
