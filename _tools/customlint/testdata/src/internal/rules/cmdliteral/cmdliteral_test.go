package cmdliteral

import "strings"

// Test files should not trigger the analyzer.
func testExample() {
	var nodeValue string

	// These would be flagged in non-test files, but are allowed here.
	_ = strings.EqualFold(nodeValue, "from")
	_ = strings.EqualFold(nodeValue, "FROM")
	_ = strings.EqualFold(nodeValue, "run")
	_ = strings.EqualFold(nodeValue, "COPY")

	m := map[string]bool{
		"entrypoint": true,
		"onbuild":    true,
	}
	_ = m
}
