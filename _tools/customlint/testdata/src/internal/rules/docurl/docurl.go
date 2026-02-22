package docurl

// Metadata represents a rule's metadata (simplified for testing).
type Metadata struct {
	Code   string
	DocURL string
}

func someDocURL(code string) string { return "https://example.com/" + code }

var someVar = "https://example.com/rule"

// Bad: hardcoded string literal in DocURL field.
var badMeta = Metadata{
	Code:   "TY0001",
	DocURL: "https://example.com/rule", // want `use rules.TallyDocURL, rules.BuildKitDocURL, or rules.HadolintDocURL instead of hardcoded DocURL string "https://example.com/rule"`
}

// Good: function call in DocURL field.
var goodMetaFunc = Metadata{
	Code:   "TY0002",
	DocURL: someDocURL("TY0002"),
}

// Good: variable in DocURL field.
var goodMetaVar = Metadata{
	Code:   "TY0003",
	DocURL: someVar,
}
