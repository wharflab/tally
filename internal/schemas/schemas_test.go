package schemas_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/wharflab/tally/internal/rules"
	_ "github.com/wharflab/tally/internal/rules/all"
	"github.com/wharflab/tally/internal/schemas"
)

func TestAllSchemaIDsAreReadable(t *testing.T) {
	t.Parallel()

	ids := schemas.AllSchemaIDs()
	if len(ids) == 0 {
		t.Fatal("AllSchemaIDs() returned no schema IDs")
	}

	for _, schemaID := range ids {
		data, err := schemas.ReadSchemaByID(schemaID)
		if err != nil {
			t.Fatalf("ReadSchemaByID(%q) error = %v", schemaID, err)
		}
		if len(data) == 0 {
			t.Fatalf("ReadSchemaByID(%q) returned empty data", schemaID)
		}
	}
}

func TestRuleSchemaMappingCoversConfigurableRules(t *testing.T) {
	t.Parallel()

	configurableRuleCodes := make(map[string]struct{})
	for _, rule := range rules.All() {
		if _, ok := rule.(rules.ConfigurableRule); !ok {
			continue
		}
		ruleCode := rule.Metadata().Code
		configurableRuleCodes[ruleCode] = struct{}{}

		if _, ok := schemas.RuleSchemaID(ruleCode); !ok {
			t.Errorf("missing schema mapping for configurable rule %q", ruleCode)
		}
	}

	for ruleCode := range schemas.RuleSchemaIDs() {
		if _, ok := configurableRuleCodes[ruleCode]; !ok {
			t.Errorf("schema mapping exists for non-configurable or unknown rule %q", ruleCode)
		}
	}
}

func TestRuleNamespacesMatchesRegisteredRules(t *testing.T) {
	t.Parallel()

	namespaces := schemas.RuleNamespaces()
	if len(namespaces) == 0 {
		t.Fatal("RuleNamespaces() returned no namespaces")
	}

	// Every namespace from RuleSchemaIDs must appear in RuleNamespaces.
	for ruleCode := range schemas.RuleSchemaIDs() {
		ns, _, _ := strings.Cut(ruleCode, "/")
		if !slices.Contains(namespaces, ns) {
			t.Errorf("namespace %q (from rule %q) not in RuleNamespaces()", ns, ruleCode)
		}
	}

	// Verify the result is sorted (contract of the function).
	if !slices.IsSorted(namespaces) {
		t.Errorf("RuleNamespaces() not sorted: %v", namespaces)
	}
}
