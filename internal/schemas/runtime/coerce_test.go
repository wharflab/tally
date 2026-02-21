package runtime

import (
	"strings"
	"testing"
)

func TestResolveSchema_CircularRef(t *testing.T) {
	t.Parallel()

	const schemaID = "https://example.test/root.schema.json"
	raw := map[string]any{
		"$id":  schemaID,
		"$ref": "#/$defs/a",
		"$defs": map[string]any{
			"a": map[string]any{
				"$ref": "#/$defs/b",
			},
			"b": map[string]any{
				"$ref": "#/$defs/a",
			},
		},
	}

	v := &validator{
		rawSchemasByID: map[string]map[string]any{
			schemaID: raw,
		},
	}

	_, _, err := v.resolveSchema(schemaID, raw)
	if err == nil {
		t.Fatal("resolveSchema() error = nil, want circular $ref error")
	}
	if !strings.Contains(err.Error(), "circular $ref detected") {
		t.Fatalf("resolveSchema() error = %q, want circular $ref detected", err)
	}
}
