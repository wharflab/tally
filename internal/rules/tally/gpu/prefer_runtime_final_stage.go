package gpu

import (
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/wharflab/tally/internal/facts"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/semantic"
	"github.com/wharflab/tally/internal/shell"
)

// PreferRuntimeFinalStageRuleCode is the full rule code.
const PreferRuntimeFinalStageRuleCode = rules.TallyRulePrefix + "gpu/prefer-runtime-final-stage"

// compileSignalCommands are executables that indicate legitimate build-time
// needs in a devel stage, suppressing the rule.
var compileSignalCommands = map[string]bool{
	"nvcc":  true,
	"gcc":   true,
	"g++":   true,
	"make":  true,
	"cmake": true,
	"ninja": true,
}

// compileSignalPackages are OS packages that indicate build-time needs.
var compileSignalPackages = map[string]bool{
	"build-essential": true,
	"gcc":             true,
	"g++":             true,
	"make":            true,
	"cmake":           true,
	"ninja-build":     true,
}

// PreferRuntimeFinalStageRule flags final stages that use an nvidia/cuda devel
// image without obvious build-time needs, suggesting a runtime or base variant.
type PreferRuntimeFinalStageRule struct{}

// NewPreferRuntimeFinalStageRule creates a new rule instance.
func NewPreferRuntimeFinalStageRule() *PreferRuntimeFinalStageRule {
	return &PreferRuntimeFinalStageRule{}
}

// Metadata returns the rule metadata.
func (r *PreferRuntimeFinalStageRule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            PreferRuntimeFinalStageRuleCode,
		Name:            "Prefer runtime image for final stage",
		Description:     "Final stage uses an NVIDIA devel image without clear build-time needs; prefer a runtime or base variant",
		DocURL:          rules.TallyDocURL(PreferRuntimeFinalStageRuleCode),
		DefaultSeverity: rules.SeverityWarning,
		Category:        "best-practices",
	}
}

// Check runs the rule against the given input.
func (r *PreferRuntimeFinalStageRule) Check(input rules.LintInput) []rules.Violation {
	var sem = input.Semantic
	if sem == nil {
		return nil
	}

	lastIdx := len(input.Stages) - 1
	if lastIdx < 0 {
		return nil
	}

	lastStage := input.Stages[lastIdx]
	if strings.TrimSpace(lastStage.SourceCode) == "" {
		return nil
	}

	stageInfo := sem.StageInfo(lastIdx)
	if stageInfo == nil {
		return nil
	}

	imgInfo := parseCUDAImageInfo(stageInfo)
	if !imgInfo.IsCUDAImage || imgInfo.Flavor != cudaFlavorDevel {
		return nil
	}

	// Check for compile signals — suppress when build tools are present.
	var fileFacts = input.Facts
	if fileFacts != nil {
		if stageFacts := fileFacts.Stage(lastIdx); stageFacts != nil {
			if hasCompileSignalFacts(stageFacts) {
				return nil
			}
		} else if hasCompileSignalFallback(lastStage) {
			return nil
		}
	} else if hasCompileSignalFallback(lastStage) {
		return nil
	}

	// Build violation on the FROM instruction.
	if stageInfo.BaseImage == nil {
		return nil
	}
	loc := rules.NewLocationFromRanges(input.File, stageInfo.BaseImage.Location)
	if loc.IsFileLevel() {
		return nil
	}

	meta := r.Metadata()
	message := "Final stage uses an NVIDIA devel image without clear build-time needs; " +
		"prefer a runtime image for the shipped stage and keep devel in builder stages"

	detail := "The nvidia/cuda devel variant includes nvcc, development headers, " +
		"and static libraries that add gigabytes to the final image. " +
		"If this stage only runs pre-built binaries or Python packages, " +
		"switch to a runtime or base variant (e.g. nvidia/cuda:12.x.y-runtime-ubuntu22.04)."
	if len(stageInfo.CopyFromRefs) > 0 {
		detail += " This stage copies artifacts from a builder stage, " +
			"which is a strong signal that it serves as a runtime image."
	}

	v := rules.NewViolation(loc, meta.Code, message, meta.DefaultSeverity).
		WithDocURL(meta.DocURL).
		WithDetail(detail)
	v.StageIndex = lastIdx
	return []rules.Violation{v}
}

// hasCompileSignalFacts checks for compile signals using precomputed facts.
func hasCompileSignalFacts(stageFacts *facts.StageFacts) bool {
	for _, runFacts := range stageFacts.Runs {
		// Check command names from parsed shell AST.
		for _, cmd := range runFacts.CommandInfos {
			if compileSignalCommands[cmd.Name] {
				return true
			}
		}
		// Check installed packages.
		for _, ic := range runFacts.InstallCommands {
			for _, pkg := range ic.Packages {
				if compileSignalPackages[strings.ToLower(pkg.Normalized)] {
					return true
				}
			}
		}
	}
	return false
}

// hasCompileSignalFallback checks for compile signals by parsing RUN commands directly.
func hasCompileSignalFallback(stage instructions.Stage) bool {
	for _, cmd := range stage.Commands {
		run, ok := cmd.(*instructions.RunCommand)
		if !ok {
			continue
		}
		script := strings.Join(run.CmdLine, " ")

		// Check for compile commands.
		for cmdName := range compileSignalCommands {
			if shell.ContainsCommand(script, cmdName) {
				return true
			}
		}

		// Check for compile-related package installs.
		installCmds := shell.FindInstallPackages(script, shell.VariantPOSIX)
		for _, ic := range installCmds {
			for _, pkg := range ic.Packages {
				if compileSignalPackages[strings.ToLower(pkg.Normalized)] {
					return true
				}
			}
		}
	}
	return false
}

func init() {
	rules.Register(NewPreferRuntimeFinalStageRule())
}
