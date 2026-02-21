package schemas

import (
	"fmt"
	"maps"
	"slices"
)

func RuleSchemaID(ruleCode string) (string, bool) {
	schemaID, ok := ruleSchemaIDs[ruleCode]
	return schemaID, ok
}

func RuleSchemaIDs() map[string]string {
	out := make(map[string]string, len(ruleSchemaIDs))
	maps.Copy(out, ruleSchemaIDs)
	return out
}

func AllSchemaIDs() []string {
	return slices.Sorted(maps.Keys(schemaBytesByID))
}

func ReadSchemaByID(schemaID string) ([]byte, error) {
	data, ok := schemaBytesByID[schemaID]
	if !ok {
		return nil, fmt.Errorf("unknown schema ID %q", schemaID)
	}
	out := make([]byte, len(data))
	copy(out, data)
	return out, nil
}
