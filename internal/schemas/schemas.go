package schemas

import (
	"fmt"
	"maps"
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
	ids := make([]string, 0, len(schemaBytesByID))
	for schemaID := range schemaBytesByID {
		ids = append(ids, schemaID)
	}
	return ids
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
