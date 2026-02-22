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

// Good: other field names don't trigger the check.
type OtherStruct struct {
	Name string
	URL  string
}

var goodOtherField = OtherStruct{
	Name: "example",
	URL:  "https://example.com/other", // Not flagged - not DocURL field
}

// Bad: multiple DocURL fields with hardcoded strings.
var badMultiple = Metadata{
	Code:   "TY0004",
	DocURL: "https://docs.example.com/TY0004", // want `use rules.TallyDocURL, rules.BuildKitDocURL, or rules.HadolintDocURL instead of hardcoded DocURL string "https://docs.example.com/TY0004"`
}

// Good: empty struct literal.
var goodEmpty = Metadata{}

// Good: only Code field set.
var goodPartial = Metadata{
	Code: "TY0005",
}

// Bad: nested struct with DocURL.
type RuleWithMetadata struct {
	Name string
	Meta Metadata
}

var badNested = RuleWithMetadata{
	Name: "TestRule",
	Meta: Metadata{
		Code:   "TY0006",
		DocURL: "https://example.com/nested", // want `use rules.TallyDocURL, rules.BuildKitDocURL, or rules.HadolintDocURL instead of hardcoded DocURL string "https://example.com/nested"`
	},
}

// Good: inline struct initialization with function call.
func createMetadata() Metadata {
	return Metadata{
		Code:   "TY0007",
		DocURL: someDocURL("TY0007"),
	}
}

// Good: short form composite literal (analyzer only checks KeyValueExpr).
var goodShort = Metadata{"TY0008", someDocURL("TY0008")}

// Good: non-string value (won't be flagged).
const docURLConst = "https://example.com/const"

var goodConst = Metadata{
	Code:   "TY0009",
	DocURL: docURLConst,
}

// Bad: another hardcoded DocURL in named fields.
var badAnother = Metadata{
	Code:   "TY0010",
	DocURL: "https://example.com/another", // want `use rules.TallyDocURL, rules.BuildKitDocURL, or rules.HadolintDocURL instead of hardcoded DocURL string "https://example.com/another"`
}