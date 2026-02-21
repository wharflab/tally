package schemas

import (
	"fmt"
	"maps"
	"slices"
	"strings"
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

// RuleNamespaces returns the sorted list of rule namespace names
// derived from the embedded index schemas (e.g. "tally", "hadolint", "buildkit").
func RuleNamespaces() []string {
	const suffix = "/index.schema.json"
	var namespaces []string
	for id := range schemaBytesByID {
		before, ok := strings.CutSuffix(id, suffix)
		if !ok {
			continue
		}
		if idx := strings.LastIndex(before, "/"); idx >= 0 {
			namespaces = append(namespaces, before[idx+1:])
		}
	}
	slices.Sort(namespaces)
	return namespaces
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
