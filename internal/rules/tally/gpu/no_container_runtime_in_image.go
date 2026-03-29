package gpu

import (
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/wharflab/tally/internal/facts"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/shell"
)

// NoContainerRuntimeInImageRuleCode is the full rule code.
const NoContainerRuntimeInImageRuleCode = rules.TallyRulePrefix + "gpu/no-container-runtime-in-image"

// runtimePackages are exact package names that indicate host-side NVIDIA runtime.
var runtimePackages = map[string]bool{
	"nvidia-container-toolkit": true,
	"nvidia-docker2":           true,
}

// runtimePackagePrefixes are package name prefixes for the NVIDIA container library.
var runtimePackagePrefixes = []string{
	"libnvidia-container",
}

// NoContainerRuntimeInImageRule flags installation of NVIDIA Container Toolkit
// packages inside the image. These packages belong to the host-side runtime
// setup and do not make the image GPU-enabled by themselves.
type NoContainerRuntimeInImageRule struct{}

// NewNoContainerRuntimeInImageRule creates a new rule instance.
func NewNoContainerRuntimeInImageRule() *NoContainerRuntimeInImageRule {
	return &NoContainerRuntimeInImageRule{}
}

// Metadata returns the rule metadata.
func (r *NoContainerRuntimeInImageRule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            NoContainerRuntimeInImageRuleCode,
		Name:            "No NVIDIA container runtime in image",
		Description:     "NVIDIA container runtime packages belong on the host, not inside the image",
		DocURL:          rules.TallyDocURL(NoContainerRuntimeInImageRuleCode),
		DefaultSeverity: rules.SeverityWarning,
		Category:        "correctness",
	}
}

// Check runs the rule against the given input.
func (r *NoContainerRuntimeInImageRule) Check(input rules.LintInput) []rules.Violation {
	return r.checkWithFacts(input, input.Facts, r.Metadata())
}

func (r *NoContainerRuntimeInImageRule) checkWithFacts(
	input rules.LintInput,
	fileFacts *facts.FileFacts,
	meta rules.RuleMetadata,
) []rules.Violation {
	var violations []rules.Violation

	for _, stageFacts := range fileFacts.Stages() {
		for _, runFacts := range stageFacts.Runs {
			if v, ok := r.checkRun(input.File, stageFacts.Index, runFacts.Run, runFacts.InstallCommands, meta); ok {
				violations = append(violations, v)
			}
		}
	}
	return violations
}

func (r *NoContainerRuntimeInImageRule) checkRun(
	file string,
	stageIdx int,
	run *instructions.RunCommand,
	installCmds []shell.InstallCommand,
	meta rules.RuleMetadata,
) (rules.Violation, bool) {
	// Derive install commands if not provided via facts.
	if installCmds == nil {
		script := strings.Join(run.CmdLine, " ")
		installCmds = shell.FindInstallPackages(script, shell.VariantPOSIX)
	}

	matched := findRuntimePackages(installCmds)
	if len(matched) == 0 {
		return rules.Violation{}, false
	}

	loc := rules.NewLocationFromRanges(file, run.Location())
	if loc.IsFileLevel() {
		return rules.Violation{}, false
	}

	pkgList := strings.Join(matched, ", ")
	message := "installing NVIDIA container runtime packages inside the image: " + pkgList
	detail := "nvidia-container-toolkit, nvidia-docker2, and libnvidia-container* are host-side " +
		"NVIDIA Container Toolkit packages. They do not make the image GPU-enabled; " +
		"configure the host or cluster runtime instead."

	v := rules.NewViolation(loc, meta.Code, message, meta.DefaultSeverity).
		WithDocURL(meta.DocURL).
		WithDetail(detail)
	v.StageIndex = stageIdx
	return v, true
}

// findRuntimePackages returns the names of NVIDIA runtime packages found in install commands.
func findRuntimePackages(installCmds []shell.InstallCommand) []string {
	var matched []string
	seen := make(map[string]bool)

	for _, ic := range installCmds {
		for _, pkg := range ic.Packages {
			name := strings.ToLower(pkg.Normalized)
			if seen[name] {
				continue
			}
			if runtimePackages[name] {
				seen[name] = true
				matched = append(matched, name)
				continue
			}
			for _, prefix := range runtimePackagePrefixes {
				if strings.HasPrefix(name, prefix) {
					seen[name] = true
					matched = append(matched, name)
					break
				}
			}
		}
	}
	return matched
}

func init() {
	rules.Register(NewNoContainerRuntimeInImageRule())
}
