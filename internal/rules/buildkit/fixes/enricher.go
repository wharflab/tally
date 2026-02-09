// Package fixes provides auto-fix enrichment for BuildKit linter rules.
// It adds SuggestedFix to BuildKit violations that can be automatically fixed.
package fixes

import (
	"slices"
	"strings"

	"github.com/tinovyatkin/tally/internal/rules"
	"github.com/tinovyatkin/tally/internal/semantic"
)

var fixableRuleNames = []string{
	"StageNameCasing",
	"FromAsCasing",
	"NoEmptyContinuation",
	"MaintainerDeprecated",
	"ConsistentInstructionCasing",
	"JSONArgsRecommended",
	"InvalidDefinitionDescription",
	"LegacyKeyValueFormat",
	"MultipleInstructionsDisallowed",
}

// FixableRuleNames returns the BuildKit rule names for which tally can generate auto-fixes.
func FixableRuleNames() []string {
	return slices.Clone(fixableRuleNames)
}

// EnrichBuildKitFixes adds SuggestedFix to BuildKit violations where possible.
// This modifies violations in-place when a fix can be generated.
//
// Parameters:
//   - violations: slice of violations to enrich (modified in-place)
//   - sem: semantic model for stage reference resolution
//   - source: raw source bytes for position calculations
func EnrichBuildKitFixes(violations []rules.Violation, sem *semantic.Model, source []byte) {
	for i := range violations {
		v := &violations[i]
		if !strings.HasPrefix(v.RuleCode, rules.BuildKitRulePrefix) {
			continue
		}

		// Skip if already has a fix
		if v.SuggestedFix != nil {
			continue
		}

		ruleName := strings.TrimPrefix(v.RuleCode, rules.BuildKitRulePrefix)
		switch ruleName {
		case "StageNameCasing":
			enrichStageNameCasingFix(v, sem, source)
		case "FromAsCasing":
			enrichFromAsCasingFix(v, source)
		case "NoEmptyContinuation":
			enrichNoEmptyContinuationFix(v, source)
		case "MaintainerDeprecated":
			enrichMaintainerDeprecatedFix(v, source)
		case "ConsistentInstructionCasing":
			enrichConsistentInstructionCasingFix(v, source)
		case "JSONArgsRecommended":
			enrichJSONArgsRecommendedFix(v, source)
		case "InvalidDefinitionDescription":
			enrichInvalidDefinitionDescriptionFix(v, source)
		case "LegacyKeyValueFormat":
			enrichLegacyKeyValueFormatFix(v, source)
		case "MultipleInstructionsDisallowed":
			enrichMultipleInstructionsDisallowedFix(v, source)
		}
	}
}
