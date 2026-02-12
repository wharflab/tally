package async

import (
	"context"
	"sync"
)

// Resolver fulfills async check requests.
type Resolver interface {
	// ID returns the unique identifier for this resolver.
	ID() string

	// Resolve executes the async operation. data is the resolver-specific input
	// from CheckRequest.Data. The returned value is passed to ResultHandler.OnSuccess.
	Resolve(ctx context.Context, data any) (any, error)
}

var (
	resolverMu sync.RWMutex
	resolvers  = make(map[string]Resolver)
)

// RegisterResolver adds a resolver to the global registry.
func RegisterResolver(r Resolver) {
	resolverMu.Lock()
	defer resolverMu.Unlock()
	resolvers[r.ID()] = r
}

// GetResolver returns the resolver with the given ID, or nil.
func GetResolver(id string) Resolver {
	resolverMu.RLock()
	defer resolverMu.RUnlock()
	return resolvers[id]
}
