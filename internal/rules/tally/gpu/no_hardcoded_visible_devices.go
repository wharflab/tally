package gpu

import (
	"slices"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/wharflab/tally/internal/facts"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/semantic"
)

// NoHardcodedVisibleDevicesRuleCode is the full rule code.
const NoHardcodedVisibleDevicesRuleCode = rules.TallyRulePrefix + "gpu/no-hardcoded-visible-devices"

// visibleDevicesClass classifies an ENV value to determine violation and fix type.
type visibleDevicesClass int

const (
	// classNone means no violation (none, void, empty, variable reference).
	classNone visibleDevicesClass = iota
	// classExplicitAll means NVIDIA_VISIBLE_DEVICES=all on a non-CUDA base — intentional, no violation.
	classExplicitAll
	// classRedundantAll means NVIDIA_VISIBLE_DEVICES=all on an nvidia/cuda base — redundant, FixSafe.
	classRedundantAll
	// classHardcodedIndex means a device index list like "0" or "0,1" — FixSuggestion.
	classHardcodedIndex
	// classHardcodedUUID means a GPU/MIG UUID — FixSuggestion.
	classHardcodedUUID
	// classHardcodedOther means any other non-empty hardcoded value — FixSuggestion.
	classHardcodedOther
)

// NoHardcodedVisibleDevicesRule flags ENV instructions that hardcode GPU device
// visibility (NVIDIA_VISIBLE_DEVICES, CUDA_VISIBLE_DEVICES). GPU visibility is
// deployment policy that should be set at runtime, not baked into the image.
type NoHardcodedVisibleDevicesRule struct{}

// NewNoHardcodedVisibleDevicesRule creates a new rule instance.
func NewNoHardcodedVisibleDevicesRule() *NoHardcodedVisibleDevicesRule {
	return &NoHardcodedVisibleDevicesRule{}
}

// Metadata returns the rule metadata.
func (r *NoHardcodedVisibleDevicesRule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            NoHardcodedVisibleDevicesRuleCode,
		Name:            "No hardcoded GPU visible devices",
		Description:     "GPU visibility is deployment policy; hardcoding it in the image reduces portability",
		DocURL:          rules.TallyDocURL(NoHardcodedVisibleDevicesRuleCode),
		DefaultSeverity: rules.SeverityWarning,
		Category:        "correctness",
		FixPriority:     8,
	}
}

// Check runs the rule against the given input.
func (r *NoHardcodedVisibleDevicesRule) Check(input rules.LintInput) []rules.Violation {
	meta := r.Metadata()
	var sem = input.Semantic
	var fileFacts = input.Facts
	if fileFacts != nil {
		return r.checkWithFacts(input, fileFacts, sem, meta)
	}

	// Fallback: iterate stages directly when facts are unavailable.
	return r.checkFallback(input, sem, meta)
}

func (r *NoHardcodedVisibleDevicesRule) checkWithFacts(
	input rules.LintInput,
	fileFacts *facts.FileFacts,
	sem *semantic.Model,
	meta rules.RuleMetadata,
) []rules.Violation {
	var violations []rules.Violation

	for _, stageFacts := range fileFacts.Stages() {
		isCUDABase := stageIsCUDABase(sem, stageFacts.Index)

		for _, key := range visibleDevicesKeys {
			binding, ok := stageFacts.EffectiveEnv.Bindings[key]
			if !ok || binding.Command == nil {
				continue
			}

			class := classifyValue(key, binding.Value, isCUDABase)
			if class == classNone || class == classExplicitAll {
				continue
			}

			if v, ok := r.buildViolation(input.File, stageFacts.Index, binding, class, meta); ok {
				violations = append(violations, v)
			}
		}
	}
	return violations
}

func (r *NoHardcodedVisibleDevicesRule) checkFallback(
	input rules.LintInput,
	sem *semantic.Model,
	meta rules.RuleMetadata,
) []rules.Violation {
	var violations []rules.Violation

	for stageIdx, stage := range input.Stages {
		isCUDABase := stageIsCUDABase(sem, stageIdx)

		for _, cmd := range stage.Commands {
			env, ok := cmd.(*instructions.EnvCommand)
			if !ok {
				continue
			}
			for _, kv := range env.Env {
				if !isVisibleDevicesKey(kv.Key) {
					continue
				}
				value := facts.Unquote(kv.Value)
				class := classifyValue(kv.Key, value, isCUDABase)
				if class == classNone || class == classExplicitAll {
					continue
				}

				binding := facts.EnvBinding{Key: kv.Key, Value: value, Command: env}
				if v, ok := r.buildViolation(input.File, stageIdx, binding, class, meta); ok {
					violations = append(violations, v)
				}
			}
		}
	}
	return violations
}

func (r *NoHardcodedVisibleDevicesRule) buildViolation(
	file string,
	stageIdx int,
	binding facts.EnvBinding,
	class visibleDevicesClass,
	meta rules.RuleMetadata,
) (rules.Violation, bool) {
	loc := rules.NewLocationFromRanges(file, binding.Command.Location())
	if loc.IsFileLevel() {
		return rules.Violation{}, false
	}

	message, detail := violationText(binding.Key, binding.Value, class)
	v := rules.NewViolation(loc, meta.Code, message, meta.DefaultSeverity).
		WithDocURL(meta.DocURL).
		WithDetail(detail)
	v.StageIndex = stageIdx

	if fix := r.buildFix(file, binding, class, meta); fix != nil {
		v = v.WithSuggestedFix(fix)
	}

	return v, true
}

func (r *NoHardcodedVisibleDevicesRule) buildFix(
	file string,
	binding facts.EnvBinding,
	class visibleDevicesClass,
	meta rules.RuleMetadata,
) *rules.SuggestedFix {
	edit := buildEnvKeyRemovalEdit(file, binding.Command, []string{binding.Key})
	if edit == nil {
		return nil
	}

	if class == classRedundantAll {
		return &rules.SuggestedFix{
			Description: "Remove redundant NVIDIA_VISIBLE_DEVICES=all (already set by nvidia/cuda base image)",
			Edits:       []rules.TextEdit{*edit},
			Safety:      rules.FixSafe,
			Priority:    meta.FixPriority,
			IsPreferred: true,
		}
	}

	desc := "Remove hardcoded " + binding.Key + " (set GPU visibility at runtime instead)"
	return &rules.SuggestedFix{
		Description: desc,
		Edits:       []rules.TextEdit{*edit},
		Safety:      rules.FixSuggestion,
		Priority:    meta.FixPriority,
		IsPreferred: true,
	}
}

// visibleDevicesKeys is the ordered list of ENV keys this rule inspects.
var visibleDevicesKeys = []string{
	"NVIDIA_VISIBLE_DEVICES",
	"CUDA_VISIBLE_DEVICES",
}

func isVisibleDevicesKey(key string) bool {
	return slices.Contains(visibleDevicesKeys, key)
}

// classifyValue determines the violation class for a visible-devices ENV value.
func classifyValue(key, value string, isCUDABase bool) visibleDevicesClass {
	trimmed := strings.TrimSpace(value)
	lower := strings.ToLower(trimmed)

	// Skip empty, none, void, and variable references.
	switch {
	case trimmed == "":
		return classNone
	case lower == "none" || lower == "void":
		return classNone
	case strings.Contains(trimmed, "$"):
		return classNone // ARG/variable reference, not hardcoded
	}

	if key == "NVIDIA_VISIBLE_DEVICES" {
		return classifyNvidiaVisibleDevices(trimmed, lower, isCUDABase)
	}
	return classifyCudaVisibleDevices(trimmed, lower)
}

func classifyNvidiaVisibleDevices(trimmed, lower string, isCUDABase bool) visibleDevicesClass {
	if lower == "all" {
		if isCUDABase {
			return classRedundantAll
		}
		return classExplicitAll
	}

	if isGPUOrMIGUUID(trimmed) {
		return classHardcodedUUID
	}
	if isDeviceIndexList(trimmed) {
		return classHardcodedIndex
	}
	return classHardcodedOther
}

func classifyCudaVisibleDevices(trimmed, lower string) visibleDevicesClass {
	// CUDA_VISIBLE_DEVICES uses "NoDevFiles" as a disable value in some CUDA versions.
	if lower == "nodevfiles" {
		return classNone
	}

	if isGPUOrMIGUUID(trimmed) {
		return classHardcodedUUID
	}
	if isDeviceIndexList(trimmed) {
		return classHardcodedIndex
	}
	return classHardcodedOther
}

// isDeviceIndexList matches patterns like "0", "1", "0,1", "0, 1, 2".
func isDeviceIndexList(value string) bool {
	hasDigit := false
	for _, ch := range value {
		switch {
		case ch >= '0' && ch <= '9':
			hasDigit = true
		case ch == ',' || ch == ' ':
			// allowed separators
		default:
			return false
		}
	}
	return hasDigit
}

// isGPUOrMIGUUID matches GPU-<uuid> or MIG-<guid> prefixes.
func isGPUOrMIGUUID(value string) bool {
	upper := strings.ToUpper(value)
	return strings.HasPrefix(upper, "GPU-") || strings.HasPrefix(upper, "MIG-")
}

func stageIsCUDABase(sem *semantic.Model, stageIdx int) bool {
	if sem == nil {
		return false
	}
	return stageUsesNVIDIACUDABase(sem.StageInfo(stageIdx))
}

func violationText(key, value string, class visibleDevicesClass) (message, detail string) {
	switch class { //nolint:exhaustive // classNone and classExplicitAll never reach here
	case classRedundantAll:
		message = "redundant " + key + "=all on nvidia/cuda base image"
		detail = "The nvidia/cuda base image already sets " + key + "=all. " +
			"This ENV instruction is redundant and can be safely removed."

	case classHardcodedIndex:
		message = "hardcoded GPU device index in " + key + "=" + value
		detail = "GPU device visibility is deployment policy. Hardcoding device indices " +
			"makes the image non-portable across hosts with different GPU topologies. " +
			"Set GPU visibility at runtime via docker run --gpus or orchestrator configuration."

	case classHardcodedUUID:
		message = "hardcoded GPU UUID in " + key + "=" + truncateValue(value, 40)
		detail = "GPU UUIDs are host-specific hardware identifiers. Hardcoding them " +
			"makes the image non-portable. Set GPU visibility at runtime."

	default: // classHardcodedOther
		message = "hardcoded " + key + "=" + value + " bakes deployment policy into the image"
		detail = "GPU device visibility should be set at runtime, not inside the image. " +
			"This allows the same image to run on different GPU configurations without rebuilding."
	}
	return message, detail
}

func truncateValue(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// buildEnvKeyRemovalEdit delegates to the shared rules.BuildEnvKeyRemovalEdit helper.
func buildEnvKeyRemovalEdit(file string, env *instructions.EnvCommand, keysToRemove []string) *rules.TextEdit {
	return rules.BuildEnvKeyRemovalEdit(file, env, keysToRemove)
}

func init() {
	rules.Register(NewNoHardcodedVisibleDevicesRule())
}
