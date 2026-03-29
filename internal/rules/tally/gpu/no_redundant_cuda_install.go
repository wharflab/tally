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

// cudaBasePackages are packages present in all nvidia/cuda flavors (base, runtime, devel).
// The base image includes at minimum the CUDA runtime library (cudart).
var cudaBasePackages = map[string]bool{
	"cuda":         true,
	"cuda-runtime": true,
}

// cudaBasePackagePrefixes are prefixes present in all nvidia/cuda flavors.
var cudaBasePackagePrefixes = []string{
	"cuda-runtime-",
	"cuda-compat-",
}

// cudaRuntimePackages are packages additionally present in runtime (and devel) flavors.
// Runtime adds math libraries (cublas, cufft, etc.) and NCCL.
var cudaRuntimePackages = map[string]bool{
	"cuda-libraries": true,
}

// cudaRuntimePackagePrefixes are prefixes additionally present in runtime (and devel).
var cudaRuntimePackagePrefixes = []string{
	"cuda-libraries-",
}

// cudaDevelPackages are packages additionally present only in the devel flavor.
// Devel adds nvcc, headers, toolkit, and development libraries.
var cudaDevelPackages = map[string]bool{
	"nvidia-cuda-toolkit": true,
	"cuda-toolkit":        true,
	"cuda-nvcc":           true,
}

// cudaDevelPackagePrefixes are prefixes additionally present only in the devel flavor.
var cudaDevelPackagePrefixes = []string{
	"cuda-toolkit-",
	"cuda-nvcc-",
}

// cudaCuDNNPackagePrefixes are packages present only in cudnn-flavored tags
// (e.g. nvidia/cuda:12.2.0-cudnn-runtime-ubuntu22.04).
var cudaCuDNNPackagePrefixes = []string{
	"libcudnn",
}

// NoRedundantCUDAInstallRule flags installation of CUDA userspace packages
// via a package manager in stages that already inherit from nvidia/cuda:*.
// The rule is flavor-aware: it only flags packages that are already provided
// by the specific image variant (base, runtime, or devel) and cuDNN tag.
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

	var sem = input.Semantic
	var fileFacts = input.Facts
	if fileFacts != nil {
		return r.checkWithFacts(input, fileFacts, sem, meta)
	}

	// Fallback: iterate stages directly when facts are unavailable.
	var violations []rules.Violation
	for stageIdx, stage := range input.Stages {
		imgInfo := r.stageImageInfo(sem, stageIdx)
		if !imgInfo.IsCUDAImage {
			continue
		}
		for _, cmd := range stage.Commands {
			run, ok := cmd.(*instructions.RunCommand)
			if !ok {
				continue
			}
			if v, ok := r.checkRun(input.File, stageIdx, run, nil, imgInfo, meta); ok {
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
		imgInfo := r.stageImageInfo(sem, stageFacts.Index)
		if !imgInfo.IsCUDAImage {
			continue
		}
		for _, runFacts := range stageFacts.Runs {
			if v, ok := r.checkRun(input.File, stageFacts.Index, runFacts.Run, runFacts.InstallCommands, imgInfo, meta); ok {
				violations = append(violations, v)
			}
		}
	}
	return violations
}

// stageImageInfo returns parsed CUDA image info for the stage.
func (r *NoRedundantCUDAInstallRule) stageImageInfo(sem *semantic.Model, stageIdx int) cudaImageInfo {
	if sem == nil {
		return cudaImageInfo{}
	}
	return parseCUDAImageInfo(sem.StageInfo(stageIdx))
}

func (r *NoRedundantCUDAInstallRule) checkRun(
	file string,
	stageIdx int,
	run *instructions.RunCommand,
	installCmds []shell.InstallCommand,
	imgInfo cudaImageInfo,
	meta rules.RuleMetadata,
) (rules.Violation, bool) {
	if installCmds == nil {
		script := strings.Join(run.CmdLine, " ")
		installCmds = shell.FindInstallPackages(script, shell.VariantPOSIX)
	}

	matched := findRedundantCUDAPackages(installCmds, imgInfo)
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

// findRedundantCUDAPackages returns CUDA packages that are already provided by the
// nvidia/cuda image variant. The check is flavor-aware:
//   - base: only cudart-level packages are redundant
//   - runtime: base + math libraries/NCCL
//   - devel: runtime + nvcc/toolkit/headers
//   - cuDNN tags: additionally flag libcudnn*
func findRedundantCUDAPackages(installCmds []shell.InstallCommand, imgInfo cudaImageInfo) []string {
	var matched []string
	seen := make(map[string]bool)

	for _, ic := range installCmds {
		for _, pkg := range ic.Packages {
			name := strings.ToLower(pkg.Normalized)
			if seen[name] {
				continue
			}
			if isRedundantForFlavor(name, imgInfo) {
				seen[name] = true
				matched = append(matched, pkg.Normalized)
			}
		}
	}
	return matched
}

// isRedundantForFlavor checks if a package name is already provided by the given image flavor.
func isRedundantForFlavor(name string, imgInfo cudaImageInfo) bool {
	// Base-level packages: present in all flavors (base, runtime, devel).
	if cudaBasePackages[name] || matchesPrefix(name, cudaBasePackagePrefixes) {
		return true
	}

	// Runtime-level packages: present in runtime and devel.
	if imgInfo.Flavor >= cudaFlavorRuntime {
		if cudaRuntimePackages[name] || matchesPrefix(name, cudaRuntimePackagePrefixes) {
			return true
		}
	}

	// Devel-level packages: present only in devel.
	if imgInfo.Flavor >= cudaFlavorDevel {
		if cudaDevelPackages[name] || matchesPrefix(name, cudaDevelPackagePrefixes) {
			return true
		}
	}

	// cuDNN packages: only present when the tag includes "cudnn".
	if imgInfo.HasCuDNN && matchesPrefix(name, cudaCuDNNPackagePrefixes) {
		return true
	}

	// TensorRT: standard nvidia/cuda tags do not include TensorRT,
	// so tensorrt* is never considered redundant currently.

	return false
}

func matchesPrefix(name string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func init() {
	rules.Register(NewNoRedundantCUDAInstallRule())
}
