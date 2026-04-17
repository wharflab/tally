package gpu

import (
	"regexp"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/wharflab/tally/internal/ai/autofixdata"
	"github.com/wharflab/tally/internal/facts"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/semantic"
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

// condaEnvCreateRe matches conda/mamba/micromamba `env create` invocations.
var condaEnvCreateRe = regexp.MustCompile(`\b(conda|mamba|micromamba)\s+env\s+create\b`)

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
// conda/mamba/micromamba install.
func (r *PreferUVOverCondaRule) checkStage(
	file string,
	stageFacts *facts.StageFacts,
	meta rules.RuleMetadata,
) (rules.Violation, bool) {
	var signals []autofixdata.Signal
	var firstRun *instructions.RunCommand

	for _, runFacts := range stageFacts.Runs {
		if runFacts == nil || runFacts.Run == nil {
			continue
		}
		script := runFacts.SourceScript
		if script == "" {
			script = runFacts.CommandScript
		}
		if script == "" {
			continue
		}
		// Skip RUNs that use `conda env create` so we don't double-count them
		// against the suppression that already ran at file level.
		if condaEnvCreateRe.MatchString(script) {
			continue
		}
		manager, packages := findCondaMLPackageInstall(script)
		if len(packages) == 0 {
			continue
		}
		line := 0
		if loc := runFacts.Run.Location(); len(loc) > 0 {
			line = loc[0].Start.Line
		}
		evidence := strings.TrimSpace(runFacts.Run.String())
		signals = append(signals, autofixdata.Signal{
			Kind:     autofixdata.SignalKindPackageInstall,
			Manager:  manager,
			Packages: packages,
			Evidence: evidence,
			Line:     line,
		})
		if firstRun == nil {
			firstRun = runFacts.Run
		}
	}

	if firstRun == nil {
		return rules.Violation{}, false
	}

	loc := rules.NewLocationFromRanges(file, firstRun.Location())
	if loc.IsFileLevel() {
		return rules.Violation{}, false
	}

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

// isCondaManager reports whether the command name is a conda-family package
// manager used as a Python installer.
func isCondaManager(manager string) bool {
	switch manager {
	case "conda", "mamba", "micromamba":
		return true
	}
	return false
}

// condaInstallRe matches a conda/mamba/micromamba install segment and captures
// the manager name in group 1. The regex is anchored to word boundaries so it
// does not match `conda-lock`, `mamba-forge`, etc.
var condaInstallRe = regexp.MustCompile(`(?i)(?:^|[\s;&|(` + "`" + `])(conda|mamba|micromamba)\s+install\b`)

// findCondaMLPackageInstall scans a shell script for conda/mamba/micromamba
// install invocations and returns the first manager + Python/ML packages
// present. Returns ("", nil) when no qualifying install is found.
func findCondaMLPackageInstall(script string) (string, []string) {
	lower := strings.ToLower(script)
	matches := condaInstallRe.FindAllStringIndex(lower, -1)
	if len(matches) == 0 {
		return "", nil
	}

	for _, m := range matches {
		manager := extractCondaManager(lower[m[0]:m[1]])
		segStart := m[1]
		segEnd := commandSegmentEnd(lower, segStart)
		pkgs := extractMLPackagesFromArgs(lower[segStart:segEnd])
		if len(pkgs) > 0 {
			return manager, pkgs
		}
	}
	return "", nil
}

// extractCondaManager extracts conda|mamba|micromamba from a matched prefix.
func extractCondaManager(prefix string) string {
	prefix = strings.ToLower(prefix)
	switch {
	case strings.Contains(prefix, "micromamba"):
		return "micromamba"
	case strings.Contains(prefix, "mamba"):
		return "mamba"
	default:
		return "conda"
	}
}

// commandSegmentEnd returns the end offset of the current shell command
// segment starting at idx, stopping at `&&`, `||`, `;`, `|`, or EOS.
// Backslash-newline continuations are part of the same segment and are
// treated as whitespace.
func commandSegmentEnd(s string, start int) int {
	for i := start; i < len(s); i++ {
		c := s[i]
		if c == ';' {
			return i
		}
		if c == '|' || c == '&' {
			return i
		}
	}
	return len(s)
}

// extractMLPackagesFromArgs scans the argument region after `install` and
// returns matching Python/ML package names. Arguments starting with `-` are
// flags and are skipped. A flag that takes a value consumes the next token.
func extractMLPackagesFromArgs(args string) []string {
	tokens := strings.Fields(args)
	var pkgs []string
	seen := make(map[string]bool)
	for i := 0; i < len(tokens); i++ {
		tok := tokens[i]
		if tok == "\\" {
			continue
		}
		if strings.HasPrefix(tok, "-") {
			if condaFlagTakesValue(tok) && i+1 < len(tokens) {
				i++
			}
			continue
		}
		name := normalizeCondaPackageName(tok)
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

// condaFlagTakesValue reports whether a conda-family install flag consumes
// the next argument as its value. `--flag=value` is self-contained and does
// not count.
func condaFlagTakesValue(flag string) bool {
	if strings.Contains(flag, "=") {
		return false
	}
	switch flag {
	case "-c", "--channel",
		"-n", "--name",
		"-p", "--prefix",
		"-f", "--file",
		"--override-channels",
		"--repodata-fn",
		"--subdir":
		return true
	}
	return false
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
			if condaEnvCreateRe.MatchString(runFacts.SourceScript) ||
				condaEnvCreateRe.MatchString(runFacts.CommandScript) {
				return true
			}
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
