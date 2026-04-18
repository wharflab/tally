package autofixdata

import (
	"math"

	"github.com/wharflab/tally/internal/config"
	"github.com/wharflab/tally/internal/rules"
)

// ResolverID is the FixResolver identifier for AI AutoFix.
const ResolverID = "ai-autofix"

// ObjectiveKind identifies the type of AI AutoFix objective.
type ObjectiveKind string

const (
	// ObjectiveMultiStage is the objective for tally/prefer-multi-stage-build.
	ObjectiveMultiStage ObjectiveKind = "prefer-multi-stage-build"
	// ObjectiveUVOverConda is the objective for tally/gpu/prefer-uv-over-conda.
	ObjectiveUVOverConda ObjectiveKind = "prefer-uv-over-conda"
)

// FixContext carries the outer fix-application intent into the resolver so the
// agent loop can respect CLI restrictions (safety threshold, --fix-rule, fix modes).
type FixContext struct {
	SafetyThreshold rules.FixSafety
	RuleFilter      []string
	FixModes        map[string]map[string]config.FixMode
}

type SignalKind string

const (
	SignalKindPackageInstall  SignalKind = "package_install"
	SignalKindBuildStep       SignalKind = "build_step"
	SignalKindDownloadInstall SignalKind = "download_install"
)

// Signal is a compact piece of evidence explaining why the rule triggered.
// It is embedded into the prompt as JSON to reduce agent variability.
type Signal struct {
	Kind     SignalKind `json:"kind"`
	Manager  string     `json:"manager,omitempty"`
	Packages []string   `json:"packages,omitempty"`
	Tool     string     `json:"tool,omitempty"`
	Evidence string     `json:"evidence,omitempty"`
	Line     int        `json:"line"`
}

// ObjectiveRequest is the generic resolver request for AI AutoFix objectives.
// It is produced by rules and enriched by the CLI fix path with config,
// fix context, and registry insights.
//
// The Kind field determines which Objective implementation handles prompt
// construction and validation. Signals and Facts carry evidence and
// objective-specific data respectively.
type ObjectiveRequest struct {
	Kind    ObjectiveKind  `json:"kind"`
	File    string         `json:"file"`
	Signals []Signal       `json:"signals,omitempty"`
	Facts   map[string]any `json:"facts,omitempty"`

	// RegistryInsights carries resolved registry metadata for base images (slow checks).
	// It is attached by cmd/tally/cmd/lint.go when slow checks ran successfully.
	RegistryInsights []RegistryInsight `json:"-"`

	// Config is the effective per-file configuration for this fix run.
	// It is attached by cmd/tally/cmd/lint.go:applyFixes.
	Config *config.Config `json:"-"`

	// FixContext captures CLI fix intent for the resolver loop.
	// It is attached by cmd/tally/cmd/lint.go:applyFixes.
	FixContext FixContext `json:"-"`

	// ContextDir is the explicit build context directory (--context flag).
	// Empty when not provided. It is attached by cmd/tally/cmd/lint.go:applyFixes.
	ContextDir string `json:"-"`
}

func (r *ObjectiveRequest) SetConfig(cfg *config.Config) { r.Config = cfg }

func (r *ObjectiveRequest) SetFixContext(ctx FixContext) { r.FixContext = ctx }

func (r *ObjectiveRequest) SetContextDir(dir string) { r.ContextDir = dir }

func (r *ObjectiveRequest) SetRegistryInsights(insights []RegistryInsight) {
	r.RegistryInsights = insights
}

// FactsInt reads an integer value from a facts map, tolerating the different
// numeric types that an `any` value can hold — native int (in-process), int64,
// or float64 (after an encoding/json round-trip). Non-finite floats
// (NaN, ±Inf) and non-integral floats (e.g., 12.9) are rejected so callers
// cannot silently feed malformed numeric data into downstream logic.
// Returns (0, false) when the key is absent or the value is not a valid
// whole-number.
func FactsInt(m map[string]any, key string) (int, bool) {
	v, ok := m[key]
	if !ok {
		return 0, false
	}
	switch x := v.(type) {
	case int:
		return x, true
	case int64:
		return int(x), true
	case int32:
		return int(x), true
	case float64:
		if n, ok := floatAsInt(x); ok {
			return n, true
		}
	case float32:
		if n, ok := floatAsInt(float64(x)); ok {
			return n, true
		}
	}
	return 0, false
}

// floatAsInt converts f to int only when f is a finite whole number.
func floatAsInt(f float64) (int, bool) {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, false
	}
	if math.Trunc(f) != f {
		return 0, false
	}
	return int(f), true
}
