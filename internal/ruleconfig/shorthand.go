package ruleconfig

import (
	"math"
	"strconv"
	"strings"
)

type shorthandKind int

const (
	shorthandInteger shorthandKind = iota
	shorthandString
)

type shorthandSpec struct {
	optionKey string
	kind      shorthandKind
}

var shorthandByRule = map[string]shorthandSpec{
	"tally/max-lines":                    {optionKey: "max", kind: shorthandInteger},
	"tally/newline-between-instructions": {optionKey: "mode", kind: shorthandString},
}

// CanonicalizeRuleOptions converts supported shorthand values to canonical
// object form used by schema validation and config resolution.
func CanonicalizeRuleOptions(ruleCode string, value any) any {
	spec, ok := shorthandByRule[ruleCode]
	if !ok {
		return value
	}

	if _, isMap := value.(map[string]any); isMap {
		return value
	}

	switch spec.kind {
	case shorthandInteger:
		if !isIntegerLike(value) {
			return value
		}
	case shorthandString:
		if _, ok := value.(string); !ok {
			return value
		}
	}

	return map[string]any{spec.optionKey: value}
}

// CanonicalizeRulesMap normalizes shorthand values in rules.<namespace>.<rule>
// entries in-place.
func CanonicalizeRulesMap(rules map[string]any) {
	for namespace, namespaceRaw := range rules {
		ruleEntries, ok := namespaceRaw.(map[string]any)
		if !ok {
			continue
		}

		for ruleName, value := range ruleEntries {
			ruleCode := namespace + "/" + ruleName
			ruleEntries[ruleName] = CanonicalizeRuleOptions(ruleCode, value)
		}
	}
}

func isIntegerLike(value any) bool {
	switch typed := value.(type) {
	case int, int8, int16, int32, int64:
		return true
	case uint:
		return uint64(typed) <= math.MaxInt64
	case uint64:
		return typed <= math.MaxInt64
	case uint8, uint16, uint32:
		return true
	case float32:
		return typed == float32(int64(typed)) && !math.IsInf(float64(typed), 0)
	case float64:
		return typed == math.Trunc(typed) && !math.IsInf(typed, 0) && !math.IsNaN(typed) &&
			typed >= math.MinInt64 && typed <= math.MaxInt64
	case string:
		_, err := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
		return err == nil
	default:
		return false
	}
}
