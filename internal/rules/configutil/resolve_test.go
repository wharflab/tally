package configutil

import (
	"strings"
	"testing"
)

// Test config type with various field types
// Using simple names to avoid koanf delimiter issues in tests
type TestConfig struct {
	IntField     int      `json:"intfield"`
	BoolField    bool     `json:"boolfield"`
	StringField  string   `json:"stringfield"`
	SliceField   []string `json:"slicefield"`
	PtrIntField  *int     `json:"ptrintfield"`
	PtrBoolField *bool    `json:"ptrboolfield"`
	UintField    uint     `json:"uintfield"`
	FloatField   float64  `json:"floatfield"`
}

func TestResolve_EmptyOpts(t *testing.T) {
	t.Parallel()
	defaults := TestConfig{
		IntField:    42,
		BoolField:   true,
		StringField: "default",
		SliceField:  []string{"a", "b"},
	}

	// Empty opts should return defaults unchanged
	result := Resolve(nil, defaults)
	if result.IntField != 42 {
		t.Errorf("expected IntField=42, got %d", result.IntField)
	}

	result = Resolve(map[string]any{}, defaults)
	if result.StringField != "default" {
		t.Errorf("expected StringField=default, got %s", result.StringField)
	}
}

func TestResolve_MergesWithDefaults(t *testing.T) {
	t.Parallel()
	intVal := 50
	defaults := TestConfig{
		IntField:    50,
		BoolField:   true,
		StringField: "default",
		SliceField:  []string{"x"},
		PtrIntField: &intVal,
	}

	// User only sets IntField, others should use defaults
	opts := map[string]any{
		"intfield": 100,
	}

	result := Resolve(opts, defaults)
	if result.IntField != 100 {
		t.Errorf("expected IntField=100, got %d", result.IntField)
	}
	if result.BoolField != true {
		t.Errorf("expected BoolField=true, got %v", result.BoolField)
	}
	if result.StringField != "default" {
		t.Errorf("expected StringField=default, got %s", result.StringField)
	}
	if result.PtrIntField == nil || *result.PtrIntField != 50 {
		t.Errorf("expected PtrIntField=50, got %v", result.PtrIntField)
	}
}

func TestResolve_ZeroValuesGetDefaults(t *testing.T) {
	t.Parallel()
	defaults := TestConfig{
		IntField:    50,
		BoolField:   true,
		StringField: "default",
	}

	// Non-pointer explicit zeros still get replaced with defaults (limitation of mergeDefaults)
	optsExplicitZero := map[string]any{
		"intfield":  0,
		"boolfield": false,
	}

	result := Resolve(optsExplicitZero, defaults)
	// Zero values still get replaced with defaults (that's the limitation)
	if result.IntField != 50 {
		t.Errorf("expected IntField=50 (default), got %d", result.IntField)
	}
	if result.BoolField != true {
		t.Errorf("expected BoolField=true (default), got %v", result.BoolField)
	}
}

func TestResolve_SliceHandling(t *testing.T) {
	t.Parallel()
	defaults := TestConfig{
		SliceField: []string{"default1", "default2"},
	}

	// Non-nil slice with values should be preserved
	optsWithValues := map[string]any{
		"slicefield": []string{"a", "b"},
	}
	result := Resolve(optsWithValues, defaults)
	if len(result.SliceField) != 2 || result.SliceField[0] != "a" {
		t.Errorf("expected [a b], got %v", result.SliceField)
	}

	// Omitted slice (nil after unmarshal) should use defaults
	optsOmitted := map[string]any{}
	result2 := Resolve(optsOmitted, defaults)
	if len(result2.SliceField) != 2 {
		t.Errorf("expected default slice with 2 items, got %v", result2.SliceField)
	}

	// Note: Empty slice []string{} behavior depends on koanf unmarshaling.
	// The documented behavior (preserves empty) requires the slice to be non-nil.
}

func TestResolve_InvalidType(t *testing.T) {
	t.Parallel()
	defaults := TestConfig{IntField: 42}

	// Invalid type should fall back to defaults
	opts := map[string]any{
		"intfield": "not-an-int",
	}

	result := Resolve(opts, defaults)
	if result.IntField != 42 {
		t.Errorf("expected default IntField=42, got %d", result.IntField)
	}
}

func TestValidateWithSchema_NilInputs(t *testing.T) {
	t.Parallel()
	// Nil schema should return nil
	if err := ValidateWithSchema(map[string]any{}, nil); err != nil {
		t.Errorf("expected nil, got %v", err)
	}

	// Nil config should return nil
	schema := map[string]any{"type": "object"}
	if err := ValidateWithSchema(nil, schema); err != nil {
		t.Errorf("expected nil, got %v", err)
	}

	// Typed nil pointer should return nil
	var cfg *TestConfig
	if err := ValidateWithSchema(cfg, schema); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestValidateWithSchema_ValidConfig(t *testing.T) {
	t.Parallel()
	schema := map[string]any{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"type":    "object",
		"properties": map[string]any{
			"max": map[string]any{
				"type":    "integer",
				"minimum": 0,
			},
		},
	}

	// Valid config
	config := map[string]any{"max": 10}
	if err := ValidateWithSchema(config, schema); err != nil {
		t.Errorf("expected valid, got error: %v", err)
	}

	// Invalid config (negative)
	badConfig := map[string]any{"max": -1}
	if err := ValidateWithSchema(badConfig, schema); err == nil {
		t.Error("expected validation error for negative max, got nil")
	}
}

func TestValidateWithSchema_InvalidSchema(t *testing.T) {
	t.Parallel()
	// Invalid schema should return error
	badSchema := map[string]any{
		"type": "invalid-type",
	}
	config := map[string]any{"foo": "bar"}

	err := ValidateWithSchema(config, badSchema)
	if err == nil {
		t.Error("expected error for invalid schema, got nil")
	}
}

func TestValidateWithSchema_ComplexTypes(t *testing.T) {
	t.Parallel()
	schema := map[string]any{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"type":    "object",
		"properties": map[string]any{
			"items": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"minItems":    1,
				"uniqueItems": true,
			},
		},
	}

	// Valid array
	config := map[string]any{"items": []string{"a", "b"}}
	if err := ValidateWithSchema(config, schema); err != nil {
		t.Errorf("expected valid, got error: %v", err)
	}

	// Empty array violates minItems
	badConfig := map[string]any{"items": []string{}}
	if err := ValidateWithSchema(badConfig, schema); err == nil {
		t.Error("expected validation error for empty array, got nil")
	}

	// Duplicate items violates uniqueItems
	dupConfig := map[string]any{"items": []string{"a", "a"}}
	if err := ValidateWithSchema(dupConfig, schema); err == nil {
		t.Error("expected validation error for duplicate items, got nil")
	}
}

func TestResolve_PointerFields(t *testing.T) {
	t.Parallel()
	intVal := 100
	boolVal := true
	defaults := TestConfig{
		PtrIntField:  &intVal,
		PtrBoolField: &boolVal,
	}

	// Nil pointer fields should get defaults
	opts := map[string]any{}
	result := Resolve(opts, defaults)

	if result.PtrIntField == nil {
		t.Error("expected PtrIntField to have default value")
	} else if *result.PtrIntField != 100 {
		t.Errorf("expected *PtrIntField=100, got %d", *result.PtrIntField)
	}
}

func TestResolve_TrustedRegistries(t *testing.T) {
	t.Parallel()
	type Config struct {
		TrustedRegistries []string `koanf:"trusted-registries"`
	}

	defaults := Config{
		TrustedRegistries: nil,
	}

	opts := map[string]any{
		"trusted-registries": []string{"docker.io"},
	}

	result := Resolve(opts, defaults)
	if len(result.TrustedRegistries) != 1 {
		t.Errorf("expected 1 registry, got %d: %v", len(result.TrustedRegistries), result.TrustedRegistries)
	}
	if result.TrustedRegistries[0] != "docker.io" {
		t.Errorf("expected docker.io, got %s", result.TrustedRegistries[0])
	}
}

func TestResolve_UintAndFloatDefaults(t *testing.T) {
	t.Parallel()
	defaults := TestConfig{
		UintField:  10,
		FloatField: 3.14,
	}

	// Omitted fields should get defaults via isZero → mergeDefaults
	opts := map[string]any{"intfield": 1}
	result := Resolve(opts, defaults)

	if result.UintField != 10 {
		t.Errorf("expected UintField=10, got %d", result.UintField)
	}
	if result.FloatField != 3.14 {
		t.Errorf("expected FloatField=3.14, got %f", result.FloatField)
	}

	// Non-zero uint/float should be preserved
	opts2 := map[string]any{"uintfield": 42, "floatfield": 2.71}
	result2 := Resolve(opts2, defaults)

	if result2.UintField != 42 {
		t.Errorf("expected UintField=42, got %d", result2.UintField)
	}
	if result2.FloatField != 2.71 {
		t.Errorf("expected FloatField=2.71, got %f", result2.FloatField)
	}
}

func TestMergeDefaults_NonStruct(t *testing.T) {
	t.Parallel()
	// mergeDefaults with a non-struct type should return result unchanged
	got := mergeDefaults(42, 100)
	if got != 42 {
		t.Errorf("expected 42, got %d", got)
	}

	got2 := mergeDefaults("user", "default")
	if got2 != "user" {
		t.Errorf("expected %q, got %q", "user", got2)
	}
}

func TestMergeDefaults_UnexportedFields(t *testing.T) {
	t.Parallel()
	type config struct {
		Public  int
		private int
	}
	result := mergeDefaults(config{Public: 0}, config{Public: 5, private: 9})
	if result.Public != 5 {
		t.Errorf("expected Public=5, got %d", result.Public)
	}
	// private field should remain zero (CanSet returns false)
	if result.private != 0 {
		t.Errorf("expected private=0, got %d", result.private)
	}
}

func TestValidateWithSchema_ErrorMessages(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		schema      map[string]any
		config      map[string]any
		wantSubstrs []string // all must appear in error message
	}{
		{
			name: "additional properties",
			schema: map[string]any{
				"type":                 "object",
				"additionalProperties": false,
			},
			config:      map[string]any{"indent": "tab", "indent-width": 1},
			wantSubstrs: []string{"additional properties"}, // property order is non-deterministic
		},
		{
			name: "wrong type",
			schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"max": map[string]any{"type": "integer"},
				},
				"additionalProperties": false,
			},
			config:      map[string]any{"max": "not-a-number"},
			wantSubstrs: []string{"has type \"string\", want \"integer\""},
		},
		{
			name: "multiple errors",
			schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"max": map[string]any{"type": "integer"},
				},
				"additionalProperties": false,
			},
			config:      map[string]any{"max": "bad", "extra": true},
			wantSubstrs: []string{"has type \"string\", want \"integer\""},
		},
		{
			name: "minimum violation",
			schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"max": map[string]any{"type": "integer", "minimum": 0},
				},
			},
			config:      map[string]any{"max": -1},
			wantSubstrs: []string{"minimum"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateWithSchema(tt.config, tt.schema)
			if err == nil {
				t.Fatal("expected validation error, got nil")
			}
			msg := err.Error()
			// Must not contain schema URI
			if strings.Contains(msg, "urn:tally") {
				t.Errorf("error message should not contain schema URI, got: %s", msg)
			}
			if strings.Contains(msg, "file://") {
				t.Errorf("error message should not contain file:// path, got: %s", msg)
			}
			// Must contain all expected human-readable details
			for _, want := range tt.wantSubstrs {
				if !strings.Contains(msg, want) {
					t.Errorf("error message should contain %q, got: %s", want, msg)
				}
			}
		})
	}
}

func TestRuleSchema(t *testing.T) {
	t.Parallel()

	t.Run("known rule returns schema", func(t *testing.T) {
		t.Parallel()

		schema, err := RuleSchema("tally/max-lines")
		if err != nil {
			t.Fatalf("RuleSchema(tally/max-lines) error = %v", err)
		}
		if schema == nil {
			t.Fatal("RuleSchema(tally/max-lines) = nil, want schema")
		}
	})

	t.Run("unknown rule returns contextual error", func(t *testing.T) {
		t.Parallel()

		_, err := RuleSchema("tally/does-not-exist")
		if err == nil {
			t.Fatal("RuleSchema(tally/does-not-exist) error = nil, want error")
		}
		msg := err.Error()
		if !strings.Contains(msg, "failed to load rule schema for tally/does-not-exist") {
			t.Fatalf("RuleSchema error missing context, got: %q", msg)
		}
	})
}
