package gpu

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/wharflab/tally/internal/facts"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/semantic"
	"github.com/wharflab/tally/internal/shell"
)

// CUDAVersionMismatchRuleCode is the full rule code.
const CUDAVersionMismatchRuleCode = rules.TallyRulePrefix + "gpu/cuda-version-mismatch"

// CUDAVersionMismatchRule flags pip/uv/conda install commands whose CUDA
// version suffix does not match the base image's CUDA toolkit version.
// Mismatched CUDA versions can cause silent CPU fallback, build failures,
// or runtime errors.
type CUDAVersionMismatchRule struct{}

// NewCUDAVersionMismatchRule creates a new rule instance.
func NewCUDAVersionMismatchRule() *CUDAVersionMismatchRule {
	return &CUDAVersionMismatchRule{}
}

// Metadata returns the rule metadata.
func (r *CUDAVersionMismatchRule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            CUDAVersionMismatchRuleCode,
		Name:            "CUDA version mismatch",
		Description:     "CUDA-specific pip/conda wheel version does not match the base image's CUDA toolkit",
		DocURL:          rules.TallyDocURL(CUDAVersionMismatchRuleCode),
		DefaultSeverity: rules.SeverityWarning,
		Category:        "correctness",
		FixPriority:     8,
	}
}

// Check runs the rule against the given input.
func (r *CUDAVersionMismatchRule) Check(input rules.LintInput) []rules.Violation {
	return r.checkWithFacts(input, input.Facts, input.Semantic, r.Metadata())
}

func (r *CUDAVersionMismatchRule) checkWithFacts(
	input rules.LintInput,
	fileFacts *facts.FileFacts,
	sem *semantic.Model,
	meta rules.RuleMetadata,
) []rules.Violation {
	var violations []rules.Violation

	for _, stageFacts := range fileFacts.Stages() {
		baseMajor, baseMinor := stageFacts.CUDAMajor, stageFacts.CUDAMinor
		if baseMajor == 0 {
			continue // not an nvidia/cuda image or version unparseable
		}

		for _, runFacts := range stageFacts.Runs {
			if v, ok := r.checkRun(
				input.File, stageFacts.Index,
				baseMajor, baseMinor,
				runFacts, sem.StageInfo(stageFacts.Index),
				meta,
			); ok {
				violations = append(violations, v)
			}
		}
	}
	return violations
}

// cudaRef captures a detected CUDA version reference found in a RUN instruction.
type cudaRef struct {
	label string // human-readable label (e.g., "cu118", "pytorch-cuda=11.8")
	major int
	minor int
	kind  cudaRefKind
}

type cudaRefKind int

const (
	// cuSuffixRef: a cuXYZ suffix in a package name or URL.
	cuSuffixRef cudaRefKind = iota
	// condaVersionRef: a major.minor version in a conda package specifier.
	condaVersionRef
)

func (r *CUDAVersionMismatchRule) checkRun(
	file string,
	stageIdx int,
	baseMajor, baseMinor int,
	runFacts *facts.RunFacts,
	stageInfo *semantic.StageInfo,
	meta rules.RuleMetadata,
) (rules.Violation, bool) {
	script := runFacts.SourceScript
	if script == "" {
		script = strings.Join(runFacts.Run.CmdLine, " ")
	}

	refs := r.findCUDARefs(script, runFacts.InstallCommands)
	if len(refs) == 0 {
		return rules.Violation{}, false
	}

	// Filter to only mismatched refs.
	var mismatched []cudaRef
	for _, ref := range refs {
		if isCUDAVersionMismatch(baseMajor, baseMinor, ref.major, ref.minor) {
			mismatched = append(mismatched, ref)
		}
	}
	if len(mismatched) == 0 {
		return rules.Violation{}, false
	}

	return r.buildViolation(violationParams{
		file:       file,
		stageIdx:   stageIdx,
		baseMajor:  baseMajor,
		baseMinor:  baseMinor,
		mismatched: mismatched,
		run:        runFacts.Run,
		stageInfo:  stageInfo,
		meta:       meta,
	})
}

// findCUDARefs collects all CUDA version references from a RUN script.
func (r *CUDAVersionMismatchRule) findCUDARefs(script string, installCmds []shell.InstallCommand) []cudaRef {
	seen := make(map[string]bool)
	var refs []cudaRef

	add := func(ref cudaRef) {
		if !seen[ref.label] {
			seen[ref.label] = true
			refs = append(refs, ref)
		}
	}

	// Path A: pip/pip3/uv package suffixes like torch==2.0.0+cu118.
	for _, ic := range installCmds {
		if !isPipFamily(ic.Manager) {
			continue
		}
		for _, pkg := range ic.Packages {
			if m := cuPackageSuffixRe.FindStringSubmatch(pkg.Normalized); m != nil {
				if major, minor, ok := parseCUSuffix(m[1]); ok {
					add(cudaRef{
						label: "cu" + m[1],
						major: major,
						minor: minor,
						kind:  cuSuffixRef,
					})
				}
			}
		}
	}

	// Path B: --index-url / --extra-index-url with cuXYZ.
	for _, m := range cuIndexURLRe.FindAllStringSubmatch(script, -1) {
		if major, minor, ok := parseCUSuffix(m[1]); ok {
			add(cudaRef{
				label: "cu" + m[1],
				major: major,
				minor: minor,
				kind:  cuSuffixRef,
			})
		}
	}

	// Path C: uv --torch-backend cuXYZ.
	for _, m := range torchBackendRe.FindAllStringSubmatch(script, -1) {
		if major, minor, ok := parseCUSuffix(m[1]); ok {
			add(cudaRef{
				label: "cu" + m[1],
				major: major,
				minor: minor,
				kind:  cuSuffixRef,
			})
		}
	}

	// Path D: conda/mamba/micromamba pytorch-cuda=X.Y or cudatoolkit=X.Y.
	for _, m := range condaCUDAVersionRe.FindAllStringSubmatch(script, -1) {
		major, minor, ok := parseCondaVersion(m[1])
		if ok {
			add(cudaRef{
				label: m[0],
				major: major,
				minor: minor,
				kind:  condaVersionRef,
			})
		}
	}

	return refs
}

// isCUDAVersionMismatch returns true when a wheel/conda CUDA version
// does not align with the base image version.
//
// NVIDIA forward compatibility: within the same major version, a wheel
// built for an older minor runs on a newer runtime. So wheelMinor <=
// baseMinor is fine; wheelMinor > baseMinor or different major is not.
func isCUDAVersionMismatch(baseMajor, baseMinor, wheelMajor, wheelMinor int) bool {
	if baseMajor != wheelMajor {
		return true // cross-major is always a mismatch
	}
	return wheelMinor > baseMinor // wheel needs newer CUDA than base provides
}

// violationParams holds the inputs needed to build a CUDA version mismatch violation.
type violationParams struct {
	file       string
	stageIdx   int
	baseMajor  int
	baseMinor  int
	mismatched []cudaRef
	run        *instructions.RunCommand
	stageInfo  *semantic.StageInfo
	meta       rules.RuleMetadata
}

func (r *CUDAVersionMismatchRule) buildViolation(p violationParams) (rules.Violation, bool) {
	loc := rules.NewLocationFromRanges(p.file, p.run.Location())
	if loc.IsFileLevel() {
		return rules.Violation{}, false
	}

	labels := make([]string, len(p.mismatched))
	for i, ref := range p.mismatched {
		labels[i] = ref.label
	}
	labelList := strings.Join(labels, ", ")
	baseVer := cudaVersionString(p.baseMajor, p.baseMinor)

	// Use the first mismatch for the primary message.
	ref := p.mismatched[0]
	var refVerStr string
	if ref.kind == condaVersionRef {
		refVerStr = fmt.Sprintf("%d.%d", ref.major, ref.minor)
	} else {
		refVerStr = fmt.Sprintf("CUDA %d.%d", ref.major, ref.minor)
	}

	var message string
	if len(p.mismatched) == 1 {
		message = fmt.Sprintf("CUDA version mismatch: install targets %s (%s) but base image provides CUDA %s",
			ref.label, refVerStr, baseVer)
	} else {
		message = fmt.Sprintf("CUDA version mismatch: install targets %s but base image provides CUDA %s",
			labelList, baseVer)
	}

	detail := fmt.Sprintf("The base image provides CUDA %s, but the install references %s. "+
		"Wheels built for a mismatched CUDA version may fail at runtime or silently fall back to CPU execution.",
		baseVer, labelList)

	v := rules.NewViolation(loc, p.meta.Code, message, p.meta.DefaultSeverity).
		WithDocURL(p.meta.DocURL).
		WithDetail(detail)
	v.StageIndex = p.stageIdx

	// Only emit fix suggestions when all mismatched refs agree on the same
	// CUDA version. If a RUN has both +cu118 and +cu124, the correct fix
	// target is ambiguous — skip fixes entirely.
	if allMismatchesAgree(p.mismatched) {
		fixes := r.buildFixes(p.baseMajor, p.baseMinor, ref, p.stageInfo, p.meta)
		if len(fixes) > 0 {
			v = v.WithSuggestedFixes(fixes)
		}
	}

	return v, true
}

// allMismatchesAgree returns true when every entry in refs targets the same
// CUDA major.minor version. Returns true for single-element slices.
func allMismatchesAgree(refs []cudaRef) bool {
	if len(refs) <= 1 {
		return true
	}
	first := refs[0]
	for _, r := range refs[1:] {
		if r.major != first.major || r.minor != first.minor {
			return false
		}
	}
	return true
}

func (r *CUDAVersionMismatchRule) buildFixes(
	baseMajor, baseMinor int,
	ref cudaRef,
	stageInfo *semantic.StageInfo,
	meta rules.RuleMetadata,
) []*rules.SuggestedFix {
	baseHigher := baseMajor > ref.major || (baseMajor == ref.major && baseMinor >= ref.minor)

	var fixes []*rules.SuggestedFix

	// Fix A: rewrite wheel/index to match base image.
	if fixA := r.buildFixWheelToBase(baseMajor, baseMinor, ref, baseHigher, meta); fixA != nil {
		fixes = append(fixes, fixA)
	}

	// Fix B: rewrite base image FROM tag to match wheel.
	if fixB := r.buildFixBaseToWheel(ref, stageInfo, !baseHigher, meta); fixB != nil {
		fixes = append(fixes, fixB)
	}

	return fixes
}

// buildFixWheelToBase creates a fix that rewrites the wheel/conda CUDA
// version to match the base image.
func (r *CUDAVersionMismatchRule) buildFixWheelToBase(
	baseMajor, baseMinor int,
	ref cudaRef,
	isPreferred bool,
	meta rules.RuleMetadata,
) *rules.SuggestedFix {
	if ref.kind == condaVersionRef {
		// Conda uses raw version; always computable.
		targetVer := cudaVersionString(baseMajor, baseMinor)
		return &rules.SuggestedFix{
			Description: fmt.Sprintf("Update conda CUDA version to %s to match base image", targetVer),
			Safety:      rules.FixSuggestion,
			Priority:    meta.FixPriority,
			IsPreferred: isPreferred,
		}
	}

	// For cuXYZ suffixes, look up the best known suffix.
	suffix, ok := bestCUDASuffix(baseMajor, baseMinor)
	if !ok {
		return nil // no known suffix for this CUDA major — cannot suggest
	}
	return &rules.SuggestedFix{
		Description: fmt.Sprintf("Update CUDA wheel index/suffix to %s to match base image (CUDA %s)",
			suffix, cudaVersionString(baseMajor, baseMinor)),
		Safety:      rules.FixSuggestion,
		Priority:    meta.FixPriority,
		IsPreferred: isPreferred,
	}
}

// buildFixBaseToWheel creates a fix that rewrites the FROM tag CUDA version
// to match the wheel. Skipped when the base is a stage reference (FROM builder)
// since the FROM line has no image version to rewrite.
func (r *CUDAVersionMismatchRule) buildFixBaseToWheel(
	ref cudaRef,
	stageInfo *semantic.StageInfo,
	isPreferred bool,
	meta rules.RuleMetadata,
) *rules.SuggestedFix {
	if stageInfo == nil || stageInfo.BaseImage == nil || stageInfo.BaseImage.IsStageRef {
		return nil
	}
	targetVer := cudaVersionString(ref.major, ref.minor)
	return &rules.SuggestedFix{
		Description: fmt.Sprintf("Update base image to CUDA %s to match installed wheels", targetVer),
		Safety:      rules.FixSuggestion,
		Priority:    meta.FixPriority,
		IsPreferred: isPreferred,
	}
}

// --- Regex patterns ---

// cuPackageSuffixRe matches +cuXYZ in a pip package name
// (e.g., "torch==2.0.0+cu118" captures "118").
// Supports 2-digit legacy suffixes like cu92 (CUDA 9.2).
var cuPackageSuffixRe = regexp.MustCompile(`\+cu(\d{2,4})`)

// cuIndexURLRe matches --index-url or --extra-index-url with a cuXYZ path
// component (e.g., ".../whl/cu121" captures "121").
// Supports both --flag VALUE and --flag=VALUE forms.
var cuIndexURLRe = regexp.MustCompile(`--(?:index-url|extra-index-url)(?:=|\s+)\S*/cu(\d{2,4})\b`)

// torchBackendRe matches uv's --torch-backend cuXYZ flag
// (e.g., "--torch-backend cu118" captures "118").
// Supports both --flag VALUE and --flag=VALUE forms.
var torchBackendRe = regexp.MustCompile(`--torch-backend(?:=|\s+)cu(\d{2,4})\b`)

// condaCUDAVersionRe matches conda pytorch-cuda=X.Y or cudatoolkit=X.Y
// specifiers (e.g., "pytorch-cuda=11.8" captures "11.8").
var condaCUDAVersionRe = regexp.MustCompile(`(?:pytorch-cuda|cudatoolkit)[= ]+(\d+\.\d+)`)

// --- Helpers ---

// parseCUSuffix extracts CUDA major and minor versions from a cuXYZ numeric
// suffix (e.g., "118" → 11, 8; "121" → 12, 1). Only 2- and 3-digit suffixes
// are supported; 4-digit inputs are rejected since no published PyTorch wheel
// uses that format and the n/10 formula would misparse them.
func parseCUSuffix(digits string) (major, minor int, ok bool) {
	n, err := strconv.Atoi(digits)
	if err != nil || n < 10 || n >= 1000 {
		return 0, 0, false
	}
	return n / 10, n % 10, true
}

// parseCondaVersion parses a dotted version string like "11.8" or "12.1"
// into major and minor components.
func parseCondaVersion(ver string) (major, minor int, ok bool) {
	before, after, found := strings.Cut(ver, ".")
	if !found {
		return 0, 0, false
	}
	maj, err := strconv.Atoi(before)
	if err != nil {
		return 0, 0, false
	}
	mnr, err := strconv.Atoi(after)
	if err != nil {
		return 0, 0, false
	}
	return maj, mnr, true
}

// isPipFamily returns true if the manager is pip, pip3, or uv.
func isPipFamily(manager string) bool {
	return manager == "pip" || manager == "pip3" || manager == "uv"
}

func init() {
	rules.Register(NewCUDAVersionMismatchRule())
}
