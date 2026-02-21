package runtime

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	jsonv2 "encoding/json/v2"
)

func (v *validator) coerceValue(baseSchemaID string, schema map[string]any, value any) (any, error) {
	if value == nil || schema == nil {
		return value, nil
	}

	resolvedSchemaID, resolvedSchema, err := v.resolveSchema(baseSchemaID, schema)
	if err != nil {
		return value, err
	}

	switch typed := value.(type) {
	case string:
		return v.coerceString(resolvedSchemaID, resolvedSchema, typed)
	case map[string]any:
		return v.coerceObject(resolvedSchemaID, resolvedSchema, typed)
	case []any:
		return v.coerceArray(resolvedSchemaID, resolvedSchema, typed)
	case []string:
		list := make([]any, 0, len(typed))
		for _, item := range typed {
			list = append(list, item)
		}
		return v.coerceArray(resolvedSchemaID, resolvedSchema, list)
	default:
		return value, nil
	}
}

func (v *validator) coerceObject(resolvedSchemaID string, resolvedSchema, obj map[string]any) (any, error) {
	if obj == nil || resolvedSchema == nil {
		return obj, nil
	}

	properties, ok := resolvedSchema["properties"].(map[string]any)
	if !ok {
		properties = nil
	}
	additional := resolvedSchema["additionalProperties"]

	for key, child := range obj {
		childSchema := schemaForProperty(properties, additional, key)
		if childSchema == nil {
			continue
		}

		coerced, err := v.coerceValue(resolvedSchemaID, childSchema, child)
		if err != nil {
			return obj, err
		}
		obj[key] = coerced
	}

	return obj, nil
}

func (v *validator) coerceArray(resolvedSchemaID string, resolvedSchema map[string]any, arr []any) (any, error) {
	if arr == nil || resolvedSchema == nil {
		return arr, nil
	}

	items, ok := resolvedSchema["items"].(map[string]any)
	if !ok {
		items = nil
	}
	if items == nil {
		return arr, nil
	}

	for i, child := range arr {
		coerced, err := v.coerceValue(resolvedSchemaID, items, child)
		if err != nil {
			return arr, err
		}
		arr[i] = coerced
	}

	return arr, nil
}

func (v *validator) coerceString(resolvedSchemaID string, resolvedSchema map[string]any, value string) (any, error) {
	types := schemaTypes(resolvedSchema)
	if len(types) == 0 {
		return value, nil
	}

	if types["boolean"] {
		if b, err := strconv.ParseBool(value); err == nil {
			return b, nil
		}
	}

	if types["integer"] {
		if i, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64); err == nil {
			return i, nil
		}
	}

	if types["number"] {
		if f, err := strconv.ParseFloat(strings.TrimSpace(value), 64); err == nil {
			return f, nil
		}
	}

	if types["object"] {
		trimmed := strings.TrimSpace(value)
		if strings.HasPrefix(trimmed, "{") {
			var obj map[string]any
			if err := jsonv2.Unmarshal([]byte(trimmed), &obj); err == nil {
				return v.coerceObject(resolvedSchemaID, resolvedSchema, obj)
			}
		}
	}

	if types["array"] {
		trimmed := strings.TrimSpace(value)
		if strings.HasPrefix(trimmed, "[") {
			var arr []any
			if err := jsonv2.Unmarshal([]byte(trimmed), &arr); err == nil {
				return v.coerceArray(resolvedSchemaID, resolvedSchema, arr)
			}
		}

		parts := splitEnvList(value)
		items, ok := resolvedSchema["items"].(map[string]any)
		if !ok {
			items = nil
		}
		if items == nil {
			return parts, nil
		}

		coerced := make([]any, 0, len(parts))
		for _, part := range parts {
			item, err := v.coerceValue(resolvedSchemaID, items, part)
			if err != nil {
				return value, err
			}
			coerced = append(coerced, item)
		}
		return coerced, nil
	}

	return value, nil
}

func schemaForProperty(properties map[string]any, additional any, key string) map[string]any {
	if properties != nil {
		if schemaAny, ok := properties[key]; ok {
			if schemaMap, ok := schemaAny.(map[string]any); ok {
				return schemaMap
			}
		}
	}

	if additionalSchema, ok := additional.(map[string]any); ok {
		return additionalSchema
	}

	return nil
}

func splitEnvList(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out = append(out, part)
	}
	return out
}

func schemaTypes(schema map[string]any) map[string]bool {
	if schema == nil {
		return nil
	}

	if t, ok := schema["type"]; ok {
		switch v := t.(type) {
		case string:
			return map[string]bool{v: true}
		case []any:
			out := make(map[string]bool, len(v))
			for _, item := range v {
				if s, ok := item.(string); ok {
					out[s] = true
				}
			}
			if len(out) > 0 {
				return out
			}
		}
	}

	out := make(map[string]bool)
	if _, ok := schema["properties"].(map[string]any); ok {
		out["object"] = true
	}
	if _, ok := schema["additionalProperties"].(map[string]any); ok {
		out["object"] = true
	}
	if _, ok := schema["items"].(map[string]any); ok {
		out["array"] = true
	}

	// Collect types from oneOf, anyOf, and allOf sub-schemas.
	for _, keyword := range []string{"oneOf", "anyOf", "allOf"} {
		if subSchemas, ok := schema[keyword].([]any); ok {
			for _, sub := range subSchemas {
				if subSchema, ok := sub.(map[string]any); ok {
					for t := range schemaTypes(subSchema) {
						out[t] = true
					}
				}
			}
		}
	}

	if len(out) > 0 {
		return out
	}

	return nil
}

func (v *validator) resolveSchema(baseSchemaID string, schema map[string]any) (string, map[string]any, error) {
	const maxRefDepth = 128

	schemaID := baseSchemaID
	current := schema
	seenRefs := make(map[string]struct{})
	depth := 0

	for {
		ref, ok := current["$ref"].(string)
		if !ok || ref == "" {
			return schemaID, current, nil
		}

		depth++
		if depth > maxRefDepth {
			return "", nil, fmt.Errorf("max $ref depth exceeded while resolving schema %q", baseSchemaID)
		}

		refKey := schemaID + "|" + ref
		if _, seen := seenRefs[refKey]; seen {
			return "", nil, fmt.Errorf("circular $ref detected while resolving schema %q at %q", baseSchemaID, refKey)
		}
		seenRefs[refKey] = struct{}{}

		nextSchemaID, next, err := v.resolveRef(schemaID, ref)
		if err != nil {
			return "", nil, err
		}
		schemaID = nextSchemaID
		current = next
	}
}

func (v *validator) resolveRef(baseSchemaID, ref string) (string, map[string]any, error) {
	if trimmed, ok := strings.CutPrefix(ref, "#"); ok {
		node, err := resolvePointer(v.rawSchemasByID[baseSchemaID], trimmed)
		if err != nil {
			return "", nil, fmt.Errorf("resolve ref %q from %q: %w", ref, baseSchemaID, err)
		}
		return baseSchemaID, node, nil
	}

	baseURL, err := url.Parse(baseSchemaID)
	if err != nil {
		return "", nil, fmt.Errorf("parse base schema ID %q: %w", baseSchemaID, err)
	}
	refURL, err := url.Parse(ref)
	if err != nil {
		return "", nil, fmt.Errorf("parse ref %q: %w", ref, err)
	}

	resolved := baseURL.ResolveReference(refURL)
	targetSchemaID := normalizeSchemaID(resolved.String())
	target := v.rawSchemasByID[targetSchemaID]
	if target == nil {
		return "", nil, fmt.Errorf("unknown schema ID %q (from %q ref %q)", targetSchemaID, baseSchemaID, ref)
	}

	node := target
	if resolved.Fragment != "" {
		fragment := resolved.Fragment
		if fragment != "" {
			node, err = resolvePointer(target, fragment)
			if err != nil {
				return "", nil, fmt.Errorf("resolve ref %q from %q: %w", ref, baseSchemaID, err)
			}
		}
	}

	return targetSchemaID, node, nil
}

func resolvePointer(doc map[string]any, pointer string) (map[string]any, error) {
	if doc == nil {
		return nil, errors.New("nil schema document")
	}
	if pointer == "" || pointer == "/" {
		return doc, nil
	}
	if !strings.HasPrefix(pointer, "/") {
		return nil, fmt.Errorf("unsupported JSON pointer %q", pointer)
	}

	current := any(doc)
	for _, rawPart := range strings.Split(pointer, "/")[1:] {
		part := strings.ReplaceAll(rawPart, "~1", "/")
		part = strings.ReplaceAll(part, "~0", "~")

		m, ok := current.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("pointer %q traversed into non-object", pointer)
		}
		next, ok := m[part]
		if !ok {
			return nil, fmt.Errorf("pointer %q missing key %q", pointer, part)
		}
		current = next
	}

	node, ok := current.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("pointer %q did not resolve to schema object", pointer)
	}
	return node, nil
}
