// Package configutil provides utilities for rule configuration resolution.
package configutil

import (
	"fmt"
	"reflect"
	"sync"

	jsonv2 "encoding/json/v2"

	gjsonschema "github.com/google/jsonschema-go/jsonschema"
	"github.com/knadh/koanf/providers/confmap"
	"github.com/knadh/koanf/v2"

	"github.com/wharflab/tally/internal/ruleconfig"
	schemavalidator "github.com/wharflab/tally/internal/schemas/runtime"
)

var resolvedSchemaCache sync.Map

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

// Coerce converts a dynamic rule config value to a typed config with defaults.
// Supported inputs:
//   - T
//   - *T
//   - map[string]any (decoded via Resolve)
//
// Any unsupported value falls back to defaults.
func Coerce[T any](config any, defaults T) T {
	switch v := config.(type) {
	case *T:
		if v != nil {
			return *v
		}
	case map[string]any:
		return Resolve(v, defaults)
	case T:
		return v
	}
	return defaults
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
	case reflect.Pointer, reflect.Interface:
		return v.IsNil()
	default:
		return false
	}
}

// RuleSchema returns the externalized JSON schema map for a rule.
func RuleSchema(ruleCode string) (map[string]any, error) {
	v, err := schemavalidator.DefaultValidator()
	if err != nil {
		return nil, fmt.Errorf("failed to initialize schema validator: %w", err)
	}
	schema, err := v.RuleSchema(ruleCode)
	if err != nil {
		return nil, fmt.Errorf("failed to load rule schema for %s: %w", ruleCode, err)
	}
	return schema, nil
}

// ValidateRuleOptions validates rule-specific options using the schema registry.
func ValidateRuleOptions(ruleCode string, config any) error {
	if isNilConfig(config) {
		return nil
	}
	config = ruleconfig.CanonicalizeRuleOptions(ruleCode, config)

	v, err := schemavalidator.DefaultValidator()
	if err != nil {
		return err
	}
	return v.ValidateRuleOptions(ruleCode, config)
}

// ValidateWithSchema validates config against a JSON Schema map.
// Returns nil if valid, or an error describing validation failures.
func ValidateWithSchema(config any, schema map[string]any) error {
	if schema == nil || isNilConfig(config) {
		return nil
	}

	schemaData, err := jsonv2.Marshal(schema, jsonv2.Deterministic(true))
	if err != nil {
		return fmt.Errorf("marshal schema: %w", err)
	}

	resolved, err := resolveSchema(schemaData)
	if err != nil {
		return err
	}

	return validateResolved(config, resolved)
}

func resolveSchema(schemaData []byte) (*gjsonschema.Resolved, error) {
	if cached, ok := resolvedSchemaCache.Load(string(schemaData)); ok {
		if resolved, ok := cached.(*gjsonschema.Resolved); ok {
			return resolved, nil
		}
	}

	var parsedSchema gjsonschema.Schema
	if err := jsonv2.Unmarshal(schemaData, &parsedSchema); err != nil {
		return nil, fmt.Errorf("parse schema: %w", err)
	}

	resolved, err := parsedSchema.Resolve(nil)
	if err != nil {
		return nil, err
	}
	resolvedSchemaCache.Store(string(schemaData), resolved)
	return resolved, nil
}

func validateResolved(config any, resolved *gjsonschema.Resolved) error {
	configData, err := jsonv2.Marshal(config)
	if err != nil {
		return err
	}
	var configJSON any
	if err := jsonv2.Unmarshal(configData, &configJSON); err != nil {
		return err
	}
	if err := resolved.Validate(configJSON); err != nil {
		return err
	}
	return nil
}

func isNilConfig(config any) bool {
	if config == nil {
		return true
	}
	rv := reflect.ValueOf(config)
	if rv.Kind() == reflect.Pointer && rv.IsNil() {
		return true
	}
	return false
}
