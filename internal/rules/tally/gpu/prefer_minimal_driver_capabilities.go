package gpu

import (
	"strings"

	"github.com/wharflab/tally/internal/facts"
	"github.com/wharflab/tally/internal/rules"
)

// PreferMinimalDriverCapabilitiesRuleCode is the full rule code.
const PreferMinimalDriverCapabilitiesRuleCode = rules.TallyRulePrefix + "gpu/prefer-minimal-driver-capabilities"

const (
	driverCapabilitiesKey          = "NVIDIA_DRIVER_CAPABILITIES"
	driverCapabilitiesDefaultValue = "compute,utility"
)

// PreferMinimalDriverCapabilitiesRule flags ENV instructions that set
// NVIDIA_DRIVER_CAPABILITIES=all. The "all" capability set mounts every NVIDIA
// driver library and binary, but most ML/CUDA workloads only need
// compute,utility. A smaller set follows the principle of least privilege.
type PreferMinimalDriverCapabilitiesRule struct{}

// NewPreferMinimalDriverCapabilitiesRule creates a new rule instance.
func NewPreferMinimalDriverCapabilitiesRule() *PreferMinimalDriverCapabilitiesRule {
	return &PreferMinimalDriverCapabilitiesRule{}
}

// Metadata returns the rule metadata.
func (r *PreferMinimalDriverCapabilitiesRule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            PreferMinimalDriverCapabilitiesRuleCode,
		Name:            "Prefer minimal NVIDIA driver capabilities",
		Description:     "NVIDIA_DRIVER_CAPABILITIES=all exposes more driver surface than most workloads need; prefer a minimal capability set",
		DocURL:          rules.TallyDocURL(PreferMinimalDriverCapabilitiesRuleCode),
		DefaultSeverity: rules.SeverityInfo,
		Category:        "correctness",
		FixPriority:     8,
	}
}

// Check runs the rule against the given input.
func (r *PreferMinimalDriverCapabilitiesRule) Check(input rules.LintInput) []rules.Violation {
	return r.checkWithFacts(input, input.Facts, r.Metadata())
}

func (r *PreferMinimalDriverCapabilitiesRule) checkWithFacts(
	input rules.LintInput,
	fileFacts *facts.FileFacts,
	meta rules.RuleMetadata,
) []rules.Violation {
	var violations []rules.Violation

	for _, stageFacts := range fileFacts.Stages() {
		binding, ok := stageFacts.EffectiveEnv.Bindings[driverCapabilitiesKey]
		if !ok || binding.Command == nil {
			continue
		}

		if !isDriverCapabilitiesAll(binding.Value) {
			continue
		}

		if v, ok := r.buildViolation(input.File, stageFacts.Index, binding, meta); ok {
			violations = append(violations, v)
		}
	}
	return violations
}

func (r *PreferMinimalDriverCapabilitiesRule) buildViolation(
	file string,
	stageIdx int,
	binding facts.EnvBinding,
	meta rules.RuleMetadata,
) (rules.Violation, bool) {
	loc := rules.NewLocationFromRanges(file, binding.Command.Location())
	if loc.IsFileLevel() {
		return rules.Violation{}, false
	}

	v := rules.NewViolation(
		loc,
		meta.Code,
		"NVIDIA_DRIVER_CAPABILITIES=all exposes more driver libraries than most workloads need",
		meta.DefaultSeverity,
	).
		WithDocURL(meta.DocURL).
		WithDetail(
			"The 'all' capability set mounts every NVIDIA driver library and binary. " +
				"Most ML and CUDA workloads only need 'compute,utility'. A smaller set follows " +
				"the principle of least privilege and avoids potential compatibility issues. " +
				"Set NVIDIA_DRIVER_CAPABILITIES=compute,utility unless your workload needs " +
				"graphics, video, or display capabilities.",
		)
	v.StageIndex = stageIdx

	if fix := r.buildFix(file, binding, meta); fix != nil {
		v = v.WithSuggestedFix(fix)
	}

	return v, true
}

func (r *PreferMinimalDriverCapabilitiesRule) buildFix(
	file string,
	binding facts.EnvBinding,
	meta rules.RuleMetadata,
) *rules.SuggestedFix {
	edit := rules.BuildEnvValueReplacementEdit(file, binding.Command, driverCapabilitiesKey, driverCapabilitiesDefaultValue)
	if edit == nil {
		return nil
	}

	return &rules.SuggestedFix{
		Description: "Replace NVIDIA_DRIVER_CAPABILITIES=all with compute,utility",
		Edits:       []rules.TextEdit{*edit},
		Safety:      rules.FixSuggestion,
		Priority:    meta.FixPriority,
		IsPreferred: true,
	}
}

// isDriverCapabilitiesAll returns true when the value is literally "all"
// (case-insensitive, trimmed). Variable references ($...) are skipped.
func isDriverCapabilitiesAll(value string) bool {
	trimmed := strings.TrimSpace(value)
	if strings.Contains(trimmed, "$") {
		return false
	}
	return strings.EqualFold(trimmed, "all")
}

func init() {
	rules.Register(NewPreferMinimalDriverCapabilitiesRule())
}
