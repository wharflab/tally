package gpu

import (
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/wharflab/tally/internal/facts"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/semantic"
	"github.com/wharflab/tally/internal/shell"
)

// NoRedundantCUDAInstallRuleCode is the full rule code.
const NoRedundantCUDAInstallRuleCode = rules.TallyRulePrefix + "gpu/no-redundant-cuda-install"

// cudaPackages are exact package names that indicate a CUDA-stack package.
var cudaPackages = map[string]bool{
	"nvidia-cuda-toolkit": true,
	"cuda":                true,
	"cuda-toolkit":        true,
	"cuda-runtime":        true,
	"cuda-nvcc":           true,
}

// cudaPackagePrefixes are package name prefixes for CUDA-stack packages.
var cudaPackagePrefixes = []string{
	"cuda-toolkit-",
	"cuda-runtime-",
	"cuda-libraries-",
	"cuda-compat-",
	"cuda-nvcc-",
	"libcudnn",
	"tensorrt",
}

// NoRedundantCUDAInstallRule flags installation of CUDA userspace packages
// via a package manager in stages that already inherit from nvidia/cuda:*.
// The base image already provides these packages, so reinstalling them is
// usually redundant and can introduce version drift.
type NoRedundantCUDAInstallRule struct{}

// NewNoRedundantCUDAInstallRule creates a new rule instance.
func NewNoRedundantCUDAInstallRule() *NoRedundantCUDAInstallRule {
	return &NoRedundantCUDAInstallRule{}
}

// Metadata returns the rule metadata.
func (r *NoRedundantCUDAInstallRule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            NoRedundantCUDAInstallRuleCode,
		Name:            "No redundant CUDA install",
		Description:     "CUDA packages are already provided by the nvidia/cuda base image",
		DocURL:          rules.TallyDocURL(NoRedundantCUDAInstallRuleCode),
		DefaultSeverity: rules.SeverityWarning,
		Category:        "correctness",
	}
}

// Check runs the rule against the given input.
func (r *NoRedundantCUDAInstallRule) Check(input rules.LintInput) []rules.Violation {
	meta := r.Metadata()

	sem, _ := input.Semantic.(*semantic.Model) //nolint:errcheck // nil-safe assertion

	fileFacts, hasFacts := input.Facts.(*facts.FileFacts)
	if hasFacts && fileFacts != nil {
		return r.checkWithFacts(input, fileFacts, sem, meta)
	}

	// Fallback: iterate stages directly when facts are unavailable.
	var violations []rules.Violation
	for stageIdx, stage := range input.Stages {
		if !r.stageIsGated(sem, stageIdx) {
			continue
		}
		for _, cmd := range stage.Commands {
			run, ok := cmd.(*instructions.RunCommand)
			if !ok {
				continue
			}
			if v, ok := r.checkRun(input.File, stageIdx, run, nil, meta); ok {
				violations = append(violations, v)
			}
		}
	}
	return violations
}

func (r *NoRedundantCUDAInstallRule) checkWithFacts(
	input rules.LintInput,
	fileFacts *facts.FileFacts,
	sem *semantic.Model,
	meta rules.RuleMetadata,
) []rules.Violation {
	var violations []rules.Violation

	for _, stageFacts := range fileFacts.Stages() {
		if !r.stageIsGated(sem, stageFacts.Index) {
			continue
		}
		for _, runFacts := range stageFacts.Runs {
			if v, ok := r.checkRun(input.File, stageFacts.Index, runFacts.Run, runFacts.InstallCommands, meta); ok {
				violations = append(violations, v)
			}
		}
	}
	return violations
}

// stageIsGated returns true if the stage uses an nvidia/cuda base image.
func (r *NoRedundantCUDAInstallRule) stageIsGated(sem *semantic.Model, stageIdx int) bool {
	if sem == nil {
		return false
	}
	info := sem.StageInfo(stageIdx)
	return stageUsesNVIDIACUDABase(info)
}

func (r *NoRedundantCUDAInstallRule) checkRun(
	file string,
	stageIdx int,
	run *instructions.RunCommand,
	installCmds []shell.InstallCommand,
	meta rules.RuleMetadata,
) (rules.Violation, bool) {
	if installCmds == nil {
		script := strings.Join(run.CmdLine, " ")
		installCmds = shell.FindInstallPackages(script, shell.VariantPOSIX)
	}

	matched := findRedundantCUDAPackages(installCmds)
	if len(matched) == 0 {
		return rules.Violation{}, false
	}

	loc := rules.NewLocationFromRanges(file, run.Location())
	if loc.IsFileLevel() {
		return rules.Violation{}, false
	}

	pkgList := strings.Join(matched, ", ")
	message := "redundant CUDA package install on nvidia/cuda base image: " + pkgList
	detail := "The nvidia/cuda base image already includes CUDA userspace packages. " +
		"Reinstalling them via the package manager is usually redundant and " +
		"can introduce version drift between the base image CUDA stack and the " +
		"newly installed packages."

	v := rules.NewViolation(loc, meta.Code, message, meta.DefaultSeverity).
		WithDocURL(meta.DocURL).
		WithDetail(detail)
	v.StageIndex = stageIdx
	return v, true
}

// findRedundantCUDAPackages returns the names of CUDA-stack packages found in install commands.
func findRedundantCUDAPackages(installCmds []shell.InstallCommand) []string {
	var matched []string
	seen := make(map[string]bool)

	for _, ic := range installCmds {
		for _, pkg := range ic.Packages {
			name := strings.ToLower(pkg.Normalized)
			if seen[name] {
				continue
			}
			if cudaPackages[name] {
				seen[name] = true
				matched = append(matched, pkg.Normalized)
				continue
			}
			for _, prefix := range cudaPackagePrefixes {
				if strings.HasPrefix(name, prefix) {
					seen[name] = true
					matched = append(matched, pkg.Normalized)
					break
				}
			}
		}
	}
	return matched
}

func init() {
	rules.Register(NewNoRedundantCUDAInstallRule())
}
