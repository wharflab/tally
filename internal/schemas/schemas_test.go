package schemas_test

import (
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
