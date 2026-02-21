package runtime

import (
	"maps"
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

func TestSchemaTypes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		schema map[string]any
		want   map[string]bool
	}{
		{
			name:   "nil schema",
			schema: nil,
			want:   nil,
		},
		{
			name:   "single type string",
			schema: map[string]any{"type": "integer"},
			want:   map[string]bool{"integer": true},
		},
		{
			name:   "type array",
			schema: map[string]any{"type": []any{"string", "integer"}},
			want:   map[string]bool{"string": true, "integer": true},
		},
		{
			name:   "inferred object from properties",
			schema: map[string]any{"properties": map[string]any{"foo": map[string]any{}}},
			want:   map[string]bool{"object": true},
		},
		{
			name:   "inferred array from items",
			schema: map[string]any{"items": map[string]any{"type": "string"}},
			want:   map[string]bool{"array": true},
		},
		{
			name: "oneOf collects types from sub-schemas",
			schema: map[string]any{
				"oneOf": []any{
					map[string]any{"type": "string"},
					map[string]any{"type": "integer"},
				},
			},
			want: map[string]bool{"string": true, "integer": true},
		},
		{
			name: "anyOf collects types from sub-schemas",
			schema: map[string]any{
				"anyOf": []any{
					map[string]any{"type": "boolean"},
					map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				},
			},
			want: map[string]bool{"boolean": true, "array": true},
		},
		{
			name: "allOf collects types from sub-schemas",
			schema: map[string]any{
				"allOf": []any{
					map[string]any{"type": "object"},
				},
			},
			want: map[string]bool{"object": true},
		},
		{
			name: "oneOf with nested inferred types",
			schema: map[string]any{
				"oneOf": []any{
					map[string]any{"type": "string"},
					map[string]any{"properties": map[string]any{"key": map[string]any{}}},
				},
			},
			want: map[string]bool{"string": true, "object": true},
		},
		{
			name: "mixed top-level and composition types",
			schema: map[string]any{
				"items": map[string]any{"type": "string"},
				"oneOf": []any{
					map[string]any{"type": "integer"},
				},
			},
			want: map[string]bool{"array": true, "integer": true},
		},
		{
			name:   "empty schema returns nil",
			schema: map[string]any{},
			want:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := schemaTypes(tt.schema)
			if !maps.Equal(got, tt.want) {
				t.Errorf("schemaTypes() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCoerceString_OneOfWithIntegerAndString(t *testing.T) {
	t.Parallel()

	const schemaID = "https://example.test/root.schema.json"
	schema := map[string]any{
		"$id": schemaID,
		"oneOf": []any{
			map[string]any{"type": "string"},
			map[string]any{"type": "integer"},
		},
	}

	v := &validator{
		rawSchemasByID: map[string]map[string]any{
			schemaID: schema,
		},
	}

	got, err := v.coerceValue(schemaID, schema, "42")
	if err != nil {
		t.Fatalf("coerceValue() error = %v", err)
	}
	if got != int64(42) {
		t.Errorf("coerceValue() = %v (%T), want int64(42)", got, got)
	}
}

func TestCoerceString_AnyOfWithBooleanAndString(t *testing.T) {
	t.Parallel()

	const schemaID = "https://example.test/root.schema.json"
	schema := map[string]any{
		"$id": schemaID,
		"anyOf": []any{
			map[string]any{"type": "boolean"},
			map[string]any{"type": "string"},
		},
	}

	v := &validator{
		rawSchemasByID: map[string]map[string]any{
			schemaID: schema,
		},
	}

	got, err := v.coerceValue(schemaID, schema, "true")
	if err != nil {
		t.Fatalf("coerceValue() error = %v", err)
	}
	if got != true {
		t.Errorf("coerceValue() = %v (%T), want true", got, got)
	}
}

func TestCoerceString_ArrayWithoutItemsReturnsSliceAny(t *testing.T) {
	t.Parallel()

	const schemaID = "https://example.test/root.schema.json"
	schema := map[string]any{
		"$id":  schemaID,
		"type": "array",
	}

	v := &validator{
		rawSchemasByID: map[string]map[string]any{
			schemaID: schema,
		},
	}

	got, err := v.coerceValue(schemaID, schema, "a,b,c")
	if err != nil {
		t.Fatalf("coerceValue() error = %v", err)
	}
	arr, ok := got.([]any)
	if !ok {
		t.Fatalf("coerceValue() returned %T, want []any", got)
	}
	if len(arr) != 3 || arr[0] != "a" || arr[1] != "b" || arr[2] != "c" {
		t.Errorf("coerceValue() = %v, want [a b c]", arr)
	}
}
