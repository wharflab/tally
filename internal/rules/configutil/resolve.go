// Package configutil provides utilities for rule configuration resolution.
package configutil

import (
	"encoding/json"
	"reflect"

	"github.com/knadh/koanf/providers/confmap"
	"github.com/knadh/koanf/v2"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

// Resolve merges user options over defaults and unmarshals to typed config.
// If opts is nil or empty, returns defaults unchanged.
// This eliminates duplicated map-to-struct conversion in each rule.
//
// Note: For slice/map fields, only nil values are replaced with defaults.
// An explicitly empty slice ([]string{}) preserves the empty value,
// allowing users to explicitly clear defaults.
func Resolve[T any](opts map[string]any, defaults T) T {
	if len(opts) == 0 {
		return defaults
	}

	k := koanf.New(".")
	if err := k.Load(confmap.Provider(opts, "."), nil); err != nil {
		return defaults
	}

	var result T
	if err := k.Unmarshal("", &result); err != nil {
		return defaults
	}

	// Merge defaults for zero-valued fields
	return mergeDefaults(result, defaults)
}

// mergeDefaults fills zero-valued fields in result with values from defaults.
func mergeDefaults[T any](result, defaults T) T {
	resultVal := reflect.ValueOf(&result).Elem()
	defaultsVal := reflect.ValueOf(defaults)

	if resultVal.Kind() != reflect.Struct {
		return result
	}

	for i := range resultVal.NumField() {
		field := resultVal.Field(i)
		if !field.CanSet() {
			continue
		}
		if isZero(field) {
			field.Set(defaultsVal.Field(i))
		}
	}

	return result
}

// isZero checks if a reflect.Value is the zero value for its type.
func isZero(v reflect.Value) bool {
	//exhaustive:ignore
	switch v.Kind() {
	case reflect.Bool:
		return !v.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return v.Uint() == 0
	case reflect.Float32, reflect.Float64:
		return v.Float() == 0
	case reflect.String:
		return v.String() == ""
	case reflect.Slice, reflect.Map:
		return v.IsNil()
	case reflect.Ptr, reflect.Interface:
		return v.IsNil()
	default:
		return false
	}
}

// ValidateWithSchema validates config against a JSON Schema.
// The schema parameter is the map[string]any returned by ConfigurableRule.Schema().
// Returns nil if valid, or an error describing validation failures.
func ValidateWithSchema(config any, schema map[string]any) error {
	if schema == nil {
		return nil
	}

	// Handle nil config (including typed nil pointers like (*Config)(nil))
	if config == nil {
		return nil
	}
	rv := reflect.ValueOf(config)
	if rv.Kind() == reflect.Ptr && rv.IsNil() {
		return nil
	}

	// AddResource expects an unmarshaled JSON value (map[string]any), not bytes
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource("schema.json", schema); err != nil {
		return err
	}

	sch, err := compiler.Compile("schema.json")
	if err != nil {
		return err
	}

	// Convert config to JSON-compatible format for validation.
	// The jsonschema library validates against unmarshaled JSON values.
	configJSON, err := json.Marshal(config)
	if err != nil {
		return err
	}
	var configValue any
	if err := json.Unmarshal(configJSON, &configValue); err != nil {
		return err
	}

	return sch.Validate(configValue)
}
