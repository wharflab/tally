package autofixdata

import (
	"github.com/tinovyatkin/tally/internal/config"
	"github.com/tinovyatkin/tally/internal/rules"
)

// ResolverID is the FixResolver identifier for AI AutoFix.
const ResolverID = "ai-autofix"

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

// MultiStageResolveData is the resolver request for tally/prefer-multi-stage-build.
// It is produced by the rule and enriched by the CLI fix path with config + fix context.
type MultiStageResolveData struct {
	File    string   `json:"file"`
	Score   int      `json:"score"`
	Signals []Signal `json:"signals,omitempty"`

	// RegistryInsights carries resolved registry metadata for base images (slow checks).
	// It is attached by cmd/tally/cmd/lint.go when slow checks ran successfully.
	RegistryInsights []RegistryInsight `json:"-"`

	// Config is the effective per-file configuration for this fix run.
	// It is attached by cmd/tally/cmd/lint.go:applyFixes.
	Config *config.Config `json:"-"`

	// FixContext captures CLI fix intent for the resolver loop.
	// It is attached by cmd/tally/cmd/lint.go:applyFixes.
	FixContext FixContext `json:"-"`
}

func (d *MultiStageResolveData) SetConfig(cfg *config.Config) { d.Config = cfg }

func (d *MultiStageResolveData) SetFixContext(ctx FixContext) { d.FixContext = ctx }

func (d *MultiStageResolveData) SetRegistryInsights(insights []RegistryInsight) {
	d.RegistryInsights = insights
}
