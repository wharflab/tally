// Package buildkit provides metadata for BuildKit's built-in linter rules.
//
// BuildKit's linter produces warnings during parsing (Phase 1) and LLB conversion (Phase 2).
// This registry wraps BuildKit's exported rules and adds tally-specific metadata
// (severity and category) so they can be individually configured.
package buildkit

import (
	"slices"

	"github.com/moby/buildkit/frontend/dockerfile/linter"

	"github.com/tinovyatkin/tally/internal/rules"
)

// CapturedRuleNames lists BuildKit rule names that can be captured by tally during parsing.
//
// BuildKit runs some checks during parsing (instructions.Parse / parser warnings) and others
// only during LLB conversion (dockerfile2llb). Since tally is a static linter and doesn't
// run LLB conversion, only parse-time checks are "captured".
//
// Note: This is intentionally a small, explicit set that reflects tally's current pipeline.
// It is enforced by tests to stay in sync with upstream BuildKit parse-time checks.
var CapturedRuleNames = []string{
	"StageNameCasing",
	"FromAsCasing",
	"MaintainerDeprecated",
	"InvalidDefinitionDescription", // experimental
	"NoEmptyContinuation",          // parser warning
}

// RuleInfo contains metadata for a BuildKit linter rule.
// Most fields are derived from BuildKit's LinterRule; we add severity and category.
type RuleInfo struct {
	// Name is the rule name as reported by BuildKit (e.g., "StageNameCasing").
	Name string

	// Description explains what the rule checks.
	Description string

	// DocURL links to Docker's official documentation.
	DocURL string

	// DefaultSeverity is the severity when not overridden by config.
	// This is tally-specific; BuildKit treats all as warnings.
	DefaultSeverity rules.Severity

	// Category groups related rules. Tally-specific.
	Category string

	// Experimental indicates this is an experimental rule.
	Experimental bool
}

// ruleEntry pairs a BuildKit rule with tally-specific metadata.
type ruleEntry struct {
	rule     linter.LinterRuleI
	severity rules.Severity
	category string
}

// allRules defines the complete list of BuildKit rules with tally-specific metadata.
// BuildKit doesn't export a registry, so we create one referencing their exported variables.
//
// MAINTENANCE: Keep this list in sync with BuildKit's exported linter.Rule* variables.
// When upgrading BuildKit, check for new/removed rules in linter/linter.go.
var allRules = []ruleEntry{
	// Style rules - Formatting and naming conventions
	{&linter.RuleStageNameCasing, rules.SeverityWarning, "style"},
	{&linter.RuleFromAsCasing, rules.SeverityWarning, "style"},
	{&linter.RuleConsistentInstructionCasing, rules.SeverityWarning, "style"},
	{&linter.RuleLegacyKeyValueFormat, rules.SeverityWarning, "style"},
	{&linter.RuleExposeProtoCasing, rules.SeverityWarning, "style"},
	{&linter.RuleInvalidDefinitionDescription, rules.SeverityInfo, "style"},

	// Correctness rules - Potential errors and bugs
	{&linter.RuleNoEmptyContinuation, rules.SeverityError, "correctness"},
	{&linter.RuleDuplicateStageName, rules.SeverityError, "correctness"},
	{&linter.RuleReservedStageName, rules.SeverityError, "correctness"},
	{&linter.RuleUndefinedArgInFrom, rules.SeverityWarning, "correctness"},
	{&linter.RuleUndefinedVar, rules.SeverityWarning, "correctness"},
	{&linter.RuleInvalidDefaultArgInFrom, rules.SeverityError, "correctness"},
	{&linter.RuleInvalidBaseImagePlatform, rules.SeverityError, "correctness"},
	{&linter.RuleExposeInvalidFormat, rules.SeverityWarning, "correctness"},
	{&linter.RuleCopyIgnoredFile, rules.SeverityWarning, "correctness"},

	// Best practices - Recommendations for better Dockerfiles
	{&linter.RuleJSONArgsRecommended, rules.SeverityInfo, "best-practice"},
	{&linter.RuleMaintainerDeprecated, rules.SeverityWarning, "best-practice"},
	{&linter.RuleWorkdirRelativePath, rules.SeverityWarning, "best-practice"},
	{&linter.RuleMultipleInstructionsDisallowed, rules.SeverityWarning, "best-practice"},
	{&linter.RuleRedundantTargetPlatform, rules.SeverityInfo, "best-practice"},
	{&linter.RuleFromPlatformFlagConstDisallowed, rules.SeverityWarning, "best-practice"},

	// Security rules - Potential security issues
	{&linter.RuleSecretsUsedInArgOrEnv, rules.SeverityWarning, "security"},
}

// Registry maps BuildKit rule names to their metadata.
// Built dynamically from BuildKit's exported rule variables.
var Registry = buildRegistry()

func buildRegistry() map[string]RuleInfo {
	reg := make(map[string]RuleInfo, len(allRules))
	for _, entry := range allRules {
		name := entry.rule.RuleName()
		reg[name] = RuleInfo{
			Name:            name,
			Description:     getDescription(entry.rule),
			DocURL:          getURL(entry.rule),
			DefaultSeverity: entry.severity,
			Category:        entry.category,
			Experimental:    entry.rule.IsExperimental(),
		}
	}
	return reg
}

// getDescription extracts description from a LinterRule.
// We use type assertion since LinterRuleI doesn't expose Description directly.
func getDescription(rule linter.LinterRuleI) string {
	// Each rule type has different Format signature, but they all embed the same fields.
	// We need to access the Description field via reflection or type switches.
	// For now, we'll use the known rule types.
	switch r := rule.(type) {
	case *linter.LinterRule[func(string) string]:
		return r.Description
	case *linter.LinterRule[func(string, string) string]:
		return r.Description
	case *linter.LinterRule[func(string, string, string) string]:
		return r.Description
	case *linter.LinterRule[func() string]:
		return r.Description
	default:
		// Unknown Format signature - return empty. Add new cases if BuildKit adds signatures.
		return ""
	}
}

// getURL extracts URL from a LinterRule.
func getURL(rule linter.LinterRuleI) string {
	switch r := rule.(type) {
	case *linter.LinterRule[func(string) string]:
		return r.URL
	case *linter.LinterRule[func(string, string) string]:
		return r.URL
	case *linter.LinterRule[func(string, string, string) string]:
		return r.URL
	case *linter.LinterRule[func() string]:
		return r.URL
	default:
		// Unknown Format signature - return empty. Add new cases if BuildKit adds signatures.
		return ""
	}
}

// Get returns the RuleInfo for a BuildKit rule name.
// Returns nil if the rule is not found.
func Get(ruleName string) *RuleInfo {
	if info, ok := Registry[ruleName]; ok {
		return &info
	}
	return nil
}

// All returns all BuildKit rules metadata.
func All() []RuleInfo {
	result := make([]RuleInfo, 0, len(Registry))
	for _, info := range Registry {
		result = append(result, info)
	}
	return result
}

// Captured returns BuildKit rules that can be produced during parsing and therefore can be
// captured by tally without running LLB conversion.
func Captured() []RuleInfo {
	out := make([]RuleInfo, 0, len(CapturedRuleNames))
	for _, name := range CapturedRuleNames {
		if info := Get(name); info != nil {
			out = append(out, *info)
		}
	}
	return out
}

// GetMetadata converts a BuildKit rule name to a rules.RuleMetadata.
// The returned Code is prefixed with "buildkit/" namespace.
func GetMetadata(ruleName string) *rules.RuleMetadata {
	info := Get(ruleName)
	if info == nil {
		return nil
	}
	return &rules.RuleMetadata{
		Code:            rules.BuildKitRulePrefix + ruleName,
		Name:            ruleName,
		Description:     info.Description,
		DocURL:          info.DocURL,
		DefaultSeverity: info.DefaultSeverity,
		Category:        info.Category,
		IsExperimental:  info.Experimental,
	}
}

// ByCategory returns all BuildKit rules in a given category.
func ByCategory(category string) []string {
	var names []string
	for name, info := range Registry {
		if info.Category == category {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return names
}

// Categories returns all unique categories.
func Categories() []string {
	seen := make(map[string]bool)
	for _, info := range Registry {
		seen[info.Category] = true
	}
	categories := make([]string, 0, len(seen))
	for cat := range seen {
		categories = append(categories, cat)
	}
	slices.Sort(categories)
	return categories
}
