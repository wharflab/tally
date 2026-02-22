package docurl

func someDocURL(code string) string { return "https://example.com/" + code }

func newIssue(file string, location int, code, message, docURL string) struct{} {
	return struct{}{}
}

func example() {
	// Bad: hardcoded string literal as 5th argument to newIssue.
	newIssue("Dockerfile", 1, "DL3000", "some message", "https://example.com/DL3000") // want `use rules.TallyDocURL, rules.BuildKitDocURL, or rules.HadolintDocURL instead of hardcoded DocURL string "https://example.com/DL3000"`

	// Good: function call as 5th argument to newIssue.
	newIssue("Dockerfile", 1, "DL3001", "some message", someDocURL("DL3001"))

	// Good: fewer than 5 arguments (different function signature).
	newIssue("Dockerfile", 1, "DL3002", "msg", someDocURL("DL3002"))
}
