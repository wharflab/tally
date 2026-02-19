package rules

// NewlineResolverID is the unique identifier for the newline-between-instructions fix resolver.
const NewlineResolverID = "newline-between-instructions"

// NewlineResolveData carries the configuration mode for the resolver.
// This is stored in SuggestedFix.ResolverData.
type NewlineResolveData struct {
	Mode string
}
