package fixes

import (
	"slices"
	"testing"
)

func TestFixableRuleNames_IncludesJSONArgsRecommended(t *testing.T) {
	names := FixableRuleNames()
	if !slices.Contains(names, "JSONArgsRecommended") {
		t.Fatalf("FixableRuleNames() missing JSONArgsRecommended: %v", names)
	}

	// Ensure caller can't mutate the underlying list.
	if len(names) > 0 {
		names[0] = "MUTATED"
	}
	names2 := FixableRuleNames()
	if slices.Contains(names2, "MUTATED") {
		t.Fatalf("FixableRuleNames() returned a shared slice (mutation leaked)")
	}
}
