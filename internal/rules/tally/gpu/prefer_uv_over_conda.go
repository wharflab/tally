package gpu

import (
	"strings"

	"github.com/wharflab/tally/internal/ai/autofixdata"
	"github.com/wharflab/tally/internal/facts"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/semantic"
	"github.com/wharflab/tally/internal/shell"
)

// PreferUVOverCondaRuleCode is the full rule code for the prefer-uv-over-conda rule.
const PreferUVOverCondaRuleCode = rules.TallyRulePrefix + "gpu/prefer-uv-over-conda"

// pytorchImageNames lists GPU-oriented base image names beyond nvidia/cuda
// that imply a Python/ML image where migrating to uv is plausible.
var pytorchImageNames = map[string]bool{
	"pytorch/pytorch":                      true,
	"docker.io/pytorch/pytorch":            true,
	"nvcr.io/nvidia/pytorch":               true,
	"huggingface/transformers-pytorch-gpu": true,
}

// condaPythonMLPackages are Python/ML package names that, when installed via
// conda/mamba/micromamba on a GPU image, indicate a realistically-migratable
// "conda as package installer" workflow.
var condaPythonMLPackages = map[string]bool{
	"pytorch":         true,
	"torch":           true,
	"torchvision":     true,
	"torchaudio":      true,
	"pytorch-cuda":    true,
	"tensorflow":      true,
	"tensorflow-gpu":  true,
	"jax":             true,
	"jaxlib":          true,
	"transformers":    true,
	"datasets":        true,
	"accelerate":      true,
	"huggingface_hub": true,
	"numpy":           true,
	"scipy":           true,
	"pandas":          true,
	"scikit-learn":    true,
	"sklearn":         true,
	"flash-attn":      true,
	"xformers":        true,
	"torch-scatter":   true,
	"torch-sparse":    true,
	"apex":            true,
	"bitsandbytes":    true,
	"einops":          true,
	"matplotlib":      true,
	"opencv":          true,
	"pillow":          true,
}

// condaManagers enumerates the conda-family package managers recognized by
// this rule. shell.FindInstallPackages uses the same identifiers.
var condaManagers = map[string]bool{
	"conda":      true,
	"mamba":      true,
	"micromamba": true,
}

// condaEnvFileBasenames lists COPY source basenames that indicate a heavy
// conda environment-management workflow (not a simple package install).
var condaEnvFileBasenames = map[string]bool{
	"environment.yml":  true,
	"environment.yaml": true,
	"conda-lock.yml":   true,
	"conda-lock.yaml":  true,
}

// PreferUVOverCondaRule suggests migrating narrow, GPU/PyTorch-oriented
// Dockerfiles from conda/mamba/micromamba to uv.
type PreferUVOverCondaRule struct{}

// NewPreferUVOverCondaRule creates a new rule instance.
func NewPreferUVOverCondaRule() *PreferUVOverCondaRule {
	return &PreferUVOverCondaRule{}
}

// Metadata returns the rule metadata.
func (r *PreferUVOverCondaRule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            PreferUVOverCondaRuleCode,
		Name:            "Prefer uv over conda",
		Description:     "Narrow GPU Python Dockerfiles can often be migrated from conda to uv for faster, lock-friendly installs",
		DocURL:          rules.TallyDocURL(PreferUVOverCondaRuleCode),
		DefaultSeverity: rules.SeverityInfo,
		Category:        "best-practices",
		IsExperimental:  true,
		FixPriority:     150,
	}
}

// Check runs the rule against the given input.
func (r *PreferUVOverCondaRule) Check(input rules.LintInput) []rules.Violation {
	return r.checkWithFacts(input, input.Facts, input.Semantic, r.Metadata())
}

func (r *PreferUVOverCondaRule) checkWithFacts(
	input rules.LintInput,
	fileFacts *facts.FileFacts,
	sem *semantic.Model,
	meta rules.RuleMetadata,
) []rules.Violation {
	if fileFacts == nil || sem == nil {
		return nil
	}

	if hasHeavyCondaEnvWorkflow(fileFacts) {
		return nil
	}

	var violations []rules.Violation
	for _, stageFacts := range fileFacts.Stages() {
		if stageFacts == nil {
			continue
		}
		stageInfo := sem.StageInfo(stageFacts.Index)
		if !stageGPUOriented(stageFacts, stageInfo) {
			continue
		}
		v, ok := r.checkStage(input.File, stageFacts, meta)
		if ok {
			violations = append(violations, v)
		}
	}
	return violations
}

// checkStage emits at most one violation per stage, at the first qualifying
// conda/mamba/micromamba install whose RUN carries a source-level location.
// RUNs with file-level-only locations are promoted past: their signal is
// still recorded, but a later RUN with a real range provides the anchor.
func (r *PreferUVOverCondaRule) checkStage(
	file string,
	stageFacts *facts.StageFacts,
	meta rules.RuleMetadata,
) (rules.Violation, bool) {
	var signals []autofixdata.Signal
	anchorLoc := rules.NewFileLocation(file)

	for _, runFacts := range stageFacts.Runs {
		if runFacts == nil || runFacts.Run == nil {
			continue
		}
		for _, ic := range runFacts.InstallCommands {
			if !condaManagers[ic.Manager] {
				continue
			}
			pkgs := condaMLPackageNames(ic)
			if len(pkgs) == 0 {
				continue
			}
			line := 0
			if loc := runFacts.Run.Location(); len(loc) > 0 {
				line = loc[0].Start.Line
			}
			evidence := strings.TrimSpace(runFacts.Run.String())
			signals = append(signals, autofixdata.Signal{
				Kind:     autofixdata.SignalKindPackageInstall,
				Manager:  ic.Manager,
				Packages: pkgs,
				Evidence: evidence,
				Line:     line,
			})
			if anchorLoc.IsFileLevel() {
				anchorLoc = rules.NewLocationFromRanges(file, runFacts.Run.Location())
			}
		}
	}

	if len(signals) == 0 {
		return rules.Violation{}, false
	}

	if anchorLoc.IsFileLevel() {
		return rules.Violation{}, false
	}
	loc := anchorLoc

	message := "GPU Python Dockerfile installs packages via conda; consider migrating to uv for faster, lock-friendly installs"
	detail := "This stage uses conda/mamba/micromamba as a Python package installer on a GPU/PyTorch base image. " +
		"For narrow workflows that do not rely on `conda env create` or an `environment.yml`, uv is usually a faster, " +
		"lock-friendly alternative with explicit CUDA wheel index support. See https://docs.astral.sh/uv/guides/integration/pytorch/ ."

	v := rules.NewViolation(loc, meta.Code, message, meta.DefaultSeverity).
		WithDocURL(meta.DocURL).
		WithDetail(detail).
		WithSuggestedFix(&rules.SuggestedFix{
			Description:  "AI AutoFix: migrate from conda to uv",
			Safety:       rules.FixUnsafe,
			NeedsResolve: true,
			ResolverID:   autofixdata.ResolverID,
			ResolverData: &autofixdata.ObjectiveRequest{
				Kind:    autofixdata.ObjectiveUVOverConda,
				File:    file,
				Signals: signals,
				Facts: map[string]any{
					"stage-index": stageFacts.Index,
					"cuda-major":  stageFacts.CUDAMajor,
					"cuda-minor":  stageFacts.CUDAMinor,
				},
			},
			Priority: meta.FixPriority,
		})
	v.StageIndex = stageFacts.Index
	return v, true
}

// stageGPUOriented reports whether a stage is GPU/PyTorch oriented enough for
// this rule to consider. Returns true when the stage inherits a CUDA version
// (including from a parent stage) or uses a known GPU-oriented image family.
func stageGPUOriented(stageFacts *facts.StageFacts, stageInfo *semantic.StageInfo) bool {
	if stageFacts == nil {
		return false
	}
	if stageFacts.CUDAMajor > 0 {
		return true
	}
	if stageInfo == nil {
		return false
	}
	if stageUsesNVIDIABase(stageInfo) {
		return true
	}
	name := stageBaseImageName(stageInfo)
	return pytorchImageNames[name]
}

// condaMLPackageNames returns the subset of packages from a conda-family
// install command that match the known Python/ML package list.
func condaMLPackageNames(ic shell.InstallCommand) []string {
	var pkgs []string
	seen := make(map[string]bool)
	for _, pkg := range ic.Packages {
		name := normalizeCondaPackageName(pkg.Normalized)
		if name == "" || seen[name] {
			continue
		}
		if condaPythonMLPackages[name] {
			seen[name] = true
			pkgs = append(pkgs, name)
		}
	}
	return pkgs
}

// normalizeCondaPackageName lowercases a conda package argument and strips a
// version specifier (`=`, `==`, `<`, `>`, `!`, space) so it can be compared to
// the known Python/ML package list.
func normalizeCondaPackageName(raw string) string {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if raw == "" {
		return ""
	}
	if idx := strings.IndexAny(raw, "=<>! "); idx > 0 {
		raw = raw[:idx]
	}
	return raw
}

// hasHeavyCondaEnvWorkflow reports whether the Dockerfile shows signs of a
// full conda environment-management workflow (env create, environment.yml,
// conda-lock) in any stage. Such workflows are out of scope for a narrow
// uv migration.
func hasHeavyCondaEnvWorkflow(fileFacts *facts.FileFacts) bool {
	for _, stageFacts := range fileFacts.Stages() {
		if stageFacts == nil {
			continue
		}
		for _, src := range stageFacts.BuildContextSources {
			if src == nil {
				continue
			}
			base := basenameLower(src.NormalizedSourcePath)
			if base == "" {
				base = basenameLower(src.SourcePath)
			}
			if condaEnvFileBasenames[base] {
				return true
			}
		}
		for _, runFacts := range stageFacts.Runs {
			if runFacts == nil {
				continue
			}
			if runHasCondaEnvCreate(runFacts) {
				return true
			}
		}
	}
	return false
}

// condaEnvSubcommand and condaEnvCreateAction name the tokens used by the
// conda/mamba/micromamba `env create` workflow. These are shell subcommands,
// not Dockerfile instruction keywords.
const (
	condaEnvSubcommand   = "env" //nolint:customlint // shell subcommand, not Dockerfile ENV
	condaEnvCreateAction = "create"
)

// runHasCondaEnvCreate returns true when a RUN invokes
// `conda|mamba|micromamba env create` as parsed by the shell AST.
func runHasCondaEnvCreate(runFacts *facts.RunFacts) bool {
	script := runFacts.SourceScript
	if script == "" {
		script = runFacts.CommandScript
	}
	if script == "" {
		return false
	}
	cmds := shell.FindCommands(script, runFacts.Shell.Variant, "conda", "mamba", "micromamba")
	for _, cmd := range cmds {
		if cmd.Subcommand != condaEnvSubcommand {
			continue
		}
		// After the subcommand, look for `create` as the next non-flag arg.
		sawEnv := false
		for _, a := range cmd.Args {
			if !sawEnv {
				if a == condaEnvSubcommand {
					sawEnv = true
				}
				continue
			}
			if strings.HasPrefix(a, "-") {
				continue
			}
			if a == condaEnvCreateAction {
				return true
			}
			break
		}
	}
	return false
}

// basenameLower returns the lowercased basename of a POSIX-style path.
func basenameLower(p string) string {
	if p == "" {
		return ""
	}
	if idx := strings.LastIndex(p, "/"); idx >= 0 {
		p = p[idx+1:]
	}
	return strings.ToLower(p)
}

func init() {
	rules.Register(NewPreferUVOverCondaRule())
}
