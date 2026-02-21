package runtime

import (
	"errors"
	"fmt"
	"maps"
	"net/url"
	"strings"
	"sync"

	jsonv2 "encoding/json/v2"

	gjsonschema "github.com/google/jsonschema-go/jsonschema"

	schemasembed "github.com/wharflab/tally/internal/schemas"
)

var ErrUnknownRuleSchema = errors.New("unknown rule schema")

type Validator interface {
	CoerceRootConfig(raw map[string]any) error
	ValidateRootConfig(raw map[string]any) error
	ValidateRuleOptions(ruleCode string, raw any) error
	RuleSchema(ruleCode string) (map[string]any, error)
	HasRuleSchema(ruleCode string) bool
}

type validator struct {
	rootResolved    *gjsonschema.Resolved
	ruleResolved    map[string]*gjsonschema.Resolved
	ruleSchemaAsMap map[string]map[string]any
	rawSchemasByID  map[string]map[string]any
}

var (
	defaultValidatorOnce sync.Once
	defaultValidator     Validator
	errDefaultValidator  error
)

func DefaultValidator() (Validator, error) {
	defaultValidatorOnce.Do(func() {
		defaultValidator, errDefaultValidator = newValidator()
	})
	if errDefaultValidator != nil {
		return nil, errDefaultValidator
	}
	return defaultValidator, nil
}

func newValidator() (*validator, error) {
	parsedSchemas := make(map[string]*gjsonschema.Schema)
	rawSchemas := make(map[string]map[string]any)

	for _, schemaID := range schemasembed.AllSchemaIDs() {
		data, err := schemasembed.ReadSchemaByID(schemaID)
		if err != nil {
			return nil, fmt.Errorf("read schema %s: %w", schemaID, err)
		}

		schema, err := parseSchema(schemaID, data)
		if err != nil {
			return nil, err
		}
		parsedSchemas[schemaID] = schema

		rawSchema, err := parseRawSchema(schemaID, data)
		if err != nil {
			return nil, err
		}
		rawSchemas[schemaID] = rawSchema
	}

	loader := func(uri *url.URL) (*gjsonschema.Schema, error) {
		schemaID := normalizeSchemaID(uri.String())
		schema, ok := parsedSchemas[schemaID]
		if !ok {
			return nil, fmt.Errorf("schema loader: unknown URI %q", uri.String())
		}
		return schema.CloneSchemas(), nil
	}

	rootSchema, ok := parsedSchemas[schemasembed.RootConfigSchemaID]
	if !ok {
		return nil, fmt.Errorf("missing embedded root schema %q", schemasembed.RootConfigSchemaID)
	}

	rootResolved, err := rootSchema.CloneSchemas().Resolve(&gjsonschema.ResolveOptions{
		BaseURI: schemasembed.RootConfigSchemaID,
		Loader:  loader,
	})
	if err != nil {
		return nil, fmt.Errorf("resolve root schema: %w", err)
	}

	ruleResolved := make(map[string]*gjsonschema.Resolved)
	ruleSchemaAsMap := make(map[string]map[string]any)

	for ruleCode, schemaID := range schemasembed.RuleSchemaIDs() {
		schema, ok := parsedSchemas[schemaID]
		if !ok {
			return nil, fmt.Errorf("missing embedded rule schema for %s (%s)", ruleCode, schemaID)
		}

		resolved, resolveErr := schema.CloneSchemas().Resolve(&gjsonschema.ResolveOptions{
			BaseURI: schemaID,
			Loader:  loader,
		})
		if resolveErr != nil {
			return nil, fmt.Errorf("resolve rule schema %s: %w", ruleCode, resolveErr)
		}
		ruleResolved[ruleCode] = resolved

		rawSchema := rawSchemas[schemaID]
		ruleSchemaAsMap[ruleCode] = rawSchema
	}

	return &validator{
		rootResolved:    rootResolved,
		ruleResolved:    ruleResolved,
		ruleSchemaAsMap: ruleSchemaAsMap,
		rawSchemasByID:  rawSchemas,
	}, nil
}

func (v *validator) CoerceRootConfig(raw map[string]any) error {
	if raw == nil {
		return nil
	}
	rootSchema := v.rawSchemasByID[schemasembed.RootConfigSchemaID]
	if rootSchema == nil {
		return fmt.Errorf("missing embedded root schema %q", schemasembed.RootConfigSchemaID)
	}
	_, err := v.coerceValue(schemasembed.RootConfigSchemaID, rootSchema, raw)
	return err
}

func (v *validator) ValidateRootConfig(raw map[string]any) error {
	if raw == nil {
		return nil
	}
	jsonValue, err := toJSONValue(raw)
	if err != nil {
		return fmt.Errorf("convert root config to JSON value: %w", err)
	}
	if err := v.rootResolved.Validate(jsonValue); err != nil {
		return fmt.Errorf("root config schema validation failed: %w", err)
	}
	return nil
}

func (v *validator) ValidateRuleOptions(ruleCode string, raw any) error {
	if isNil(raw) {
		return nil
	}
	resolved, ok := v.ruleResolved[ruleCode]
	if !ok {
		return fmt.Errorf("%w: %s", ErrUnknownRuleSchema, ruleCode)
	}
	jsonValue, err := toJSONValue(raw)
	if err != nil {
		return fmt.Errorf("convert rule options to JSON value (%s): %w", ruleCode, err)
	}
	if err := resolved.Validate(jsonValue); err != nil {
		return fmt.Errorf("rule %s schema validation failed: %w", ruleCode, err)
	}
	return nil
}

func (v *validator) RuleSchema(ruleCode string) (map[string]any, error) {
	schemaMap, ok := v.ruleSchemaAsMap[ruleCode]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnknownRuleSchema, ruleCode)
	}
	copied := make(map[string]any, len(schemaMap))
	maps.Copy(copied, schemaMap)
	return copied, nil
}

func (v *validator) HasRuleSchema(ruleCode string) bool {
	_, ok := v.ruleResolved[ruleCode]
	return ok
}

func parseSchema(schemaID string, data []byte) (*gjsonschema.Schema, error) {
	var schema gjsonschema.Schema
	if err := jsonv2.Unmarshal(data, &schema); err != nil {
		return nil, fmt.Errorf("parse schema %s: %w", schemaID, err)
	}
	return &schema, nil
}

func parseRawSchema(schemaID string, data []byte) (map[string]any, error) {
	var schemaMap map[string]any
	if err := jsonv2.Unmarshal(data, &schemaMap); err != nil {
		return nil, fmt.Errorf("parse raw schema %s: %w", schemaID, err)
	}
	return schemaMap, nil
}

func toJSONValue(value any) (any, error) {
	data, err := jsonv2.Marshal(value)
	if err != nil {
		return nil, err
	}
	var out any
	if err := jsonv2.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func normalizeSchemaID(uri string) string {
	if before, _, ok := strings.Cut(uri, "#"); ok {
		return before
	}
	return uri
}

func isNil(value any) bool {
	if value == nil {
		return true
	}
	// JSON-like config values are never typed nil pointers, so nil interface check is sufficient.
	return false
}
