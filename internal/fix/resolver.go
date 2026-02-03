package fix

import (
	"context"
	"sync"

	"github.com/tinovyatkin/tally/internal/rules"
)

// ResolveContext provides context for fix resolution.
// This includes the current file content after sync fixes have been applied,
// allowing resolvers to compute edits based on the modified state.
type ResolveContext struct {
	// FilePath is the path to the file being fixed.
	FilePath string

	// Content is the current file content after sync fixes have been applied.
	// Resolvers should parse this to compute correct positions.
	Content []byte
}

// FixResolver computes fix edits that require external data or operate
// on content after other fixes have been applied.
//
// Resolvers are called AFTER sync fixes have been applied to the file.
// This allows structural transforms to operate on already-modified content,
// avoiding position drift issues.
//
// Examples:
//   - Image digest resolver: fetches digests from container registries
//   - Heredoc resolver: transforms RUN instructions after content fixes
type FixResolver interface {
	// ID returns the unique identifier for this resolver.
	// This matches the ResolverID field in SuggestedFix.
	ID() string

	// Resolve computes the actual edits for a fix.
	// The fix.ResolverData contains resolver-specific data needed for resolution.
	// The resolveCtx provides the current file content after sync fixes.
	// Returns the edits to apply, or an error if resolution fails.
	//
	// Implementations should respect context cancellation and timeouts.
	Resolve(ctx context.Context, resolveCtx ResolveContext, fix *rules.SuggestedFix) ([]rules.TextEdit, error)
}

// resolverRegistry holds all registered fix resolvers.
var (
	resolversMu sync.RWMutex
	resolvers   = make(map[string]FixResolver)
)

// RegisterResolver adds a resolver to the global registry.
// Panics if a resolver with the same ID is already registered.
func RegisterResolver(r FixResolver) {
	resolversMu.Lock()
	defer resolversMu.Unlock()

	id := r.ID()
	if _, exists := resolvers[id]; exists {
		panic("fix: duplicate resolver registration: " + id)
	}
	resolvers[id] = r
}

// GetResolver returns the resolver with the given ID, or nil if not found.
func GetResolver(id string) FixResolver {
	resolversMu.RLock()
	defer resolversMu.RUnlock()
	return resolvers[id]
}

// ListResolvers returns the IDs of all registered resolvers.
func ListResolvers() []string {
	resolversMu.RLock()
	defer resolversMu.RUnlock()

	ids := make([]string, 0, len(resolvers))
	for id := range resolvers {
		ids = append(ids, id)
	}
	return ids
}

// ClearResolvers removes all registered resolvers.
// This is primarily useful for testing.
func ClearResolvers() {
	resolversMu.Lock()
	defer resolversMu.Unlock()
	resolvers = make(map[string]FixResolver)
}
