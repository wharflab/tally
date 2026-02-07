// Package configutil provides utilities for rule configuration resolution.
package configutil

import (
	"encoding/json/v2"
	"errors"
	"reflect"
	"strings"

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

	// Use an absolute URI so the library doesn't resolve against cwd
	// (which would leak the build machine's path into error messages).
	const schemaURI = "urn:tally:rule-config"
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource(schemaURI, schema); err != nil {
		return err
	}

	sch, err := compiler.Compile(schemaURI)
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

	if err := sch.Validate(configValue); err != nil {
		// Use BasicOutput to get a flat list of validation errors,
		// then extract clean messages without schema URIs.
		var verr *jsonschema.ValidationError
		if errors.As(err, &verr) {
			if msg := formatBasicOutput(verr); msg != "" {
				return errors.New(msg)
			}
		}
		return err
	}
	return nil
}

// formatBasicOutput extracts clean error messages from a ValidationError
// using the JSON Schema "Basic" output format (flat list of leaf errors).
func formatBasicOutput(verr *jsonschema.ValidationError) string {
	basic := verr.BasicOutput()
	if basic == nil || len(basic.Errors) == 0 {
		return ""
	}

	var msgs []string
	for _, unit := range basic.Errors {
		if unit.Error == nil {
			continue
		}
		// Marshal the error to get the localized message string
		b, err := unit.Error.MarshalJSON()
		if err != nil {
			continue
		}
		var detail string
		if json.Unmarshal(b, &detail) != nil {
			continue
		}
		if unit.InstanceLocation == "" || unit.InstanceLocation == "/" {
			msgs = append(msgs, detail)
		} else {
			msgs = append(msgs, "at '"+unit.InstanceLocation+"': "+detail)
		}
	}
	if len(msgs) == 0 {
		return ""
	}
	return strings.Join(msgs, "; ")
}
