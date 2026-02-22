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

	// Good: variable as 5th argument.
	docVar := "https://example.com/var"
	newIssue("Dockerfile", 1, "DL3003", "msg", docVar)

	// Bad: another hardcoded URL.
	newIssue("Dockerfile", 2, "DL3004", "another message", "https://docs.example.com/DL3004") // want `use rules.TallyDocURL, rules.BuildKitDocURL, or rules.HadolintDocURL instead of hardcoded DocURL string "https://docs.example.com/DL3004"`

	// Good: non-newIssue function with same signature doesn't get flagged.
	otherFunc("Dockerfile", 1, "DL3006", "msg", "https://example.com/other")
}

func otherFunc(file string, location int, code, message, docURL string) {}

// Bad: multiple newIssue calls with hardcoded URLs.
func multipleIssues() {
	newIssue("Dockerfile", 1, "DL3008", "msg", "https://example.com/DL3008") // want `use rules.TallyDocURL, rules.BuildKitDocURL, or rules.HadolintDocURL instead of hardcoded DocURL string "https://example.com/DL3008"`
	newIssue("Dockerfile", 2, "DL3009", "msg", "https://example.com/DL3009") // want `use rules.TallyDocURL, rules.BuildKitDocURL, or rules.HadolintDocURL instead of hardcoded DocURL string "https://example.com/DL3009"`
}