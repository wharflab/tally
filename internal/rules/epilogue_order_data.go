package rules

// EpilogueOrderResolverID is the unique identifier for the epilogue-order fix resolver.
const EpilogueOrderResolverID = "epilogue-order"

// EpilogueOrderResolveData carries resolver context.
// The resolver is self-contained (re-parses and re-analyzes the file),
// so no additional data is needed.
type EpilogueOrderResolveData struct{}
