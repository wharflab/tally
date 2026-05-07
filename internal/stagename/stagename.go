// Package stagename contains small helpers for classifying Dockerfile stage
// names. Multiple rules need to know whether a stage looks like a development,
// test, debug, or CI stage so they can skip production-only checks; that
// classification has no rule-specific knowledge and lives here so each rule
// does not re-derive it.
package stagename

import (
	"slices"
	"strings"
)

// nonProductionTokens are the substring tokens that mark a stage as
// development-, test-, debug-, or CI-only when found as a delimited part of
// the stage name. Matching is case-insensitive and operates on tokens split
// by '-', '_', '.', '/' or ':' so that "device" or "developer-tooling" do not
// collide with "dev".
var nonProductionTokens = []string{"dev", "development", "test", "testing", "ci", "debug"}

// LooksLikeDev reports whether a stage name is a development, test, debug, or
// CI stage. Production-only rules use this to skip such stages.
//
// The match is token-based: the name is split by '-', '_', '.', '/' or ':'
// and each part is compared case-insensitively against the known tokens.
// Substrings inside a single token (e.g. "device", "production-deploy") do
// not count.
func LooksLikeDev(name string) bool {
	normalized := strings.ToLower(strings.TrimSpace(name))
	if normalized == "" {
		return false
	}

	parts := strings.FieldsFunc(normalized, func(r rune) bool {
		switch r {
		case '-', '_', '.', '/', ':':
			return true
		default:
			return false
		}
	})
	if len(parts) == 0 {
		parts = []string{normalized}
	}

	return slices.ContainsFunc(parts, func(part string) bool {
		return slices.Contains(nonProductionTokens, part)
	})
}
