package tally

import (
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/command"
	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/wharflab/tally/internal/facts"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/semantic"
)

// StatefulRootRuntimeRuleCode is the full rule code.
const StatefulRootRuntimeRuleCode = rules.TallyRulePrefix + "stateful-root-runtime"

// statefulSignal describes a single stateful/persistent-state indicator found
// in the final stage.
type statefulSignal struct {
	kind string // "VOLUME", "WORKDIR", "COPY destination", "ADD destination", "RUN mkdir"
	path string // the actual path detected
	loc  rules.Location
}

// statePathPrefixes lists directory prefixes that indicate mutable/persistent
// state in a container (data dirs, log dirs, cache dirs, runtime dirs).
var statePathPrefixes = []string{
	"/var/lib/",
	"/var/log/",
	"/var/cache/",
	"/var/run/",
	"/var/spool/",
}

// statePathExact lists exact paths that indicate mutable/persistent state.
var statePathExact = []string{
	"/data",
	"/srv",
}

// StatefulRootRuntimeRule detects final stages that run as root and signal
// mutable/persistent state through VOLUME instructions or data/state directory
// patterns.
//
// Cross-rule interaction with hadolint/DL3002:
//
//   - DL3002 fires when the last USER is explicitly root, regardless of state.
//   - This rule fires when root (explicit or implicit) intersects with stateful
//     signals. It also suppresses for privilege-drop entrypoints and known
//     non-root base images, which DL3002 does not.
//   - Both rules can fire on the same Dockerfile (e.g., USER root + VOLUME /data).
//     This is intentional: DL3002 gives a broad "consider non-root" nudge, while
//     this rule highlights the specific elevated-risk combination.
//   - No EnabledRules suppression is applied — the rules are complementary, not
//     conflicting, and neither has fixes that could overlap.
type StatefulRootRuntimeRule struct{}

// NewStatefulRootRuntimeRule creates a new rule instance.
func NewStatefulRootRuntimeRule() *StatefulRootRuntimeRule {
	return &StatefulRootRuntimeRule{}
}

// Metadata returns the rule metadata.
func (r *StatefulRootRuntimeRule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            StatefulRootRuntimeRuleCode,
		Name:            "Stateful Root Runtime",
		Description:     "Final stage runs as root and signals mutable/persistent state",
		DocURL:          rules.TallyDocURL(StatefulRootRuntimeRuleCode),
		DefaultSeverity: rules.SeverityWarning,
		Category:        "security",
	}
}

// Check runs the stateful-root-runtime rule.
//
// It fires when ALL of these are true in the final stage:
//  1. The effective runtime user is root (explicit or implicit).
//  2. The stage positively signals mutable/persistent state (VOLUME, data dirs).
//  3. No privilege-drop entrypoint pattern is detected (gosu, su-exec, etc.).
func (r *StatefulRootRuntimeRule) Check(input rules.LintInput) []rules.Violation {
	rc := checkFinalStageRoot(input)
	if rc == nil {
		return nil
	}

	finalIdx := rc.FinalIdx
	sf := rc.StageFacts

	// Step 2: Scan for stateful signals.
	signals := detectStatefulSignals(input, sf, rc.FileFacts, finalIdx)
	if len(signals) == 0 {
		return nil
	}

	// Step 3: Check for privilege-drop suppression.
	if sf.DropsPrivilegesAtRuntime() {
		return nil
	}

	// Build violation.
	meta := r.Metadata()
	primary := signals[0]

	var rootDesc string
	if rc.ImplicitRoot {
		rootDesc = "no USER instruction (defaults to root)"
	} else {
		rootDesc = "USER is " + sf.EffectiveUser
	}

	msg := fmt.Sprintf(
		"final stage runs as root (%s) and signals persistent state via %s %s",
		rootDesc, primary.kind, primary.path,
	)

	v := rules.NewViolation(primary.loc, meta.Code, msg, meta.DefaultSeverity).
		WithDocURL(meta.DocURL)

	if len(signals) > 1 {
		var detail strings.Builder
		detail.WriteString("Additional stateful signals detected:\n")
		for _, s := range signals[1:] {
			fmt.Fprintf(&detail, "  - %s %s\n", s.kind, s.path)
		}
		v = v.WithDetail(detail.String())
	}

	v.StageIndex = finalIdx

	return []rules.Violation{v}
}

// detectStatefulSignals scans the final stage for VOLUME instructions and
// data/state directory patterns in WORKDIR, COPY/ADD destinations, and
// RUN mkdir commands. For local stage refs (FROM <parent-stage>), inherited
// VOLUME paths and effective WORKDIR from the parent are also included.
func detectStatefulSignals(input rules.LintInput, sf *facts.StageFacts, fileFacts *facts.FileFacts, stageIdx int) []statefulSignal {
	var signals []statefulSignal

	stage := input.Stages[stageIdx]
	workdir := "/" // track effective workdir to resolve relative paths

	// Inherit state from parent stage when the base is a local stage ref.
	fromLoc := rules.NewLocationFromRanges(input.File, stage.Location)
	inheritedSignals := inheritParentStatefulSignals(input.Semantic, fileFacts, stageIdx, fromLoc)
	signals = append(signals, inheritedSignals...)

	// Initialize workdir from parent stage if this is a local stage ref.
	if parentWorkdir := inheritedParentWorkdir(input.Semantic, fileFacts, stageIdx); parentWorkdir != "" {
		workdir = parentWorkdir
	}

	for _, cmd := range stage.Commands {
		switch c := cmd.(type) {
		case *instructions.VolumeCommand:
			loc := rules.NewLocationFromRanges(input.File, c.Location())
			for _, vol := range c.Volumes {
				signals = append(signals, statefulSignal{
					kind: command.Volume,
					path: vol,
					loc:  loc,
				})
			}

		case *instructions.WorkdirCommand:
			workdir = facts.ResolveWorkdir(workdir, c.Path)
			if isStatePath(workdir) {
				signals = append(signals, statefulSignal{
					kind: command.Workdir,
					path: workdir,
					loc:  rules.NewLocationFromRanges(input.File, c.Location()),
				})
			}

		case *instructions.CopyCommand:
			dest := resolveDestPath(c.DestPath, workdir)
			if isStatePath(dest) {
				signals = append(signals, statefulSignal{
					kind: "COPY destination",
					path: dest,
					loc:  rules.NewLocationFromRanges(input.File, c.Location()),
				})
			}

		case *instructions.AddCommand:
			dest := resolveDestPath(c.DestPath, workdir)
			if isStatePath(dest) {
				signals = append(signals, statefulSignal{
					kind: "ADD destination",
					path: dest,
					loc:  rules.NewLocationFromRanges(input.File, c.Location()),
				})
			}
		}
	}

	// Scan RUN commands for mkdir of state paths.
	for _, run := range sf.Runs {
		if mkdirPaths := findMkdirStatePaths(run.CommandScript); len(mkdirPaths) > 0 {
			loc := rules.NewLocationFromRanges(input.File, run.Run.Location())
			for _, p := range mkdirPaths {
				signals = append(signals, statefulSignal{
					kind: "RUN mkdir",
					path: p,
					loc:  loc,
				})
			}
		}
	}

	return signals
}

// inheritParentStatefulSignals collects stateful signals (VOLUME paths, state-path
// WORKDIR) from parent stages when the base is a local stage ref. Walks the
// stage-ref chain to collect inherited state. The loc parameter points at the
// FROM instruction for attribution.
func inheritParentStatefulSignals(sem any, fileFacts *facts.FileFacts, stageIdx int, loc rules.Location) []statefulSignal {
	if sem == nil || fileFacts == nil {
		return nil
	}

	model, ok := sem.(*semantic.Model)
	if !ok || model == nil {
		return nil
	}

	var signals []statefulSignal
	visited := make(map[int]bool)

	for idx := stageIdx; !visited[idx]; {
		visited[idx] = true

		info := model.StageInfo(idx)
		if info == nil || info.BaseImage == nil || !info.BaseImage.IsStageRef || info.BaseImage.StageIndex < 0 {
			break
		}

		parentIdx := info.BaseImage.StageIndex
		parentFacts := fileFacts.Stage(parentIdx)
		if parentFacts == nil {
			break
		}

		for _, vol := range parentFacts.Volumes {
			signals = append(signals, statefulSignal{
				kind: "inherited " + command.Volume,
				path: vol,
				loc:  loc,
			})
		}

		if isStatePath(parentFacts.FinalWorkdir) {
			signals = append(signals, statefulSignal{
				kind: "inherited " + command.Workdir,
				path: parentFacts.FinalWorkdir,
				loc:  loc,
			})
		}

		idx = parentIdx
	}

	return signals
}

// inheritedParentWorkdir walks the stage-ref chain to find the effective
// WORKDIR inherited from ancestor stages. If the immediate parent has no
// WORKDIR (FinalWorkdir = "/"), it continues up the chain to find the
// nearest ancestor that set one. Returns empty string for external bases.
func inheritedParentWorkdir(sem any, fileFacts *facts.FileFacts, stageIdx int) string {
	if sem == nil || fileFacts == nil {
		return ""
	}

	model, ok := sem.(*semantic.Model)
	if !ok || model == nil {
		return ""
	}

	visited := make(map[int]bool)

	for idx := stageIdx; !visited[idx]; {
		visited[idx] = true

		info := model.StageInfo(idx)
		if info == nil || info.BaseImage == nil || !info.BaseImage.IsStageRef || info.BaseImage.StageIndex < 0 {
			return ""
		}

		parentIdx := info.BaseImage.StageIndex
		if parentFacts := fileFacts.Stage(parentIdx); parentFacts != nil {
			if parentFacts.FinalWorkdir != "/" {
				return parentFacts.FinalWorkdir
			}
		}

		// Parent has default "/" workdir — continue up the chain.
		idx = parentIdx
	}

	return ""
}

// resolveDestPath resolves a COPY/ADD destination path against the effective
// workdir, following Docker's semantics: absolute paths are used as-is,
// relative paths are joined to the current WORKDIR.
func resolveDestPath(dest, workdir string) string {
	if strings.Contains(dest, "$") {
		return dest // leave variable references unresolved
	}
	return facts.ResolveWorkdir(workdir, dest)
}

// isStatePath checks whether a path matches a known data/state directory pattern.
func isStatePath(p string) bool {
	if strings.Contains(p, "$") {
		return false // skip variable references
	}

	cleaned := path.Clean(p)

	if slices.Contains(statePathExact, cleaned) {
		return true
	}

	// Append trailing slash for prefix matching so "/var/lib" matches "/var/lib/"
	// and "/var/lib/mysql" matches "/var/lib/", but "/var/library" does not.
	withSlash := cleaned + "/"
	for _, prefix := range statePathPrefixes {
		if strings.HasPrefix(withSlash, prefix) {
			return true
		}
	}

	return false
}

// findMkdirStatePaths scans a shell command script for mkdir invocations
// that create state-related directories. Returns matching paths.
func findMkdirStatePaths(script string) []string {
	if !strings.Contains(script, "mkdir") {
		return nil
	}

	var result []string

	// Simple heuristic: split by common delimiters and look for "mkdir"
	// followed by absolute paths that match state patterns.
	for _, segment := range strings.FieldsFunc(script, func(r rune) bool {
		return r == '&' || r == '|' || r == ';' || r == '\n'
	}) {
		fields := strings.Fields(strings.TrimSpace(segment))
		isMkdir := false
		for _, f := range fields {
			if strings.HasPrefix(f, "-") {
				continue // skip options (short -p, long --mode=755, etc.)
			}
			if isMkdir && strings.HasPrefix(f, "/") {
				if isStatePath(f) {
					result = append(result, f)
				}
			}
			if f == "mkdir" || strings.HasSuffix(f, "/mkdir") {
				isMkdir = true
			}
		}
	}

	return result
}

// isKnownNonRootBase checks whether a stage's base image is known to default
// to a non-root user. It walks the stage-ref chain so that
// FROM distroless:nonroot AS base → FROM base → FROM base2 is correctly
// recognized as non-root at every level.
func isKnownNonRootBase(sem any, fileFacts *facts.FileFacts, stageIdx int) bool {
	if sem == nil {
		return false
	}

	model, ok := sem.(*semantic.Model)
	if !ok || model == nil {
		return false
	}

	// Walk the stage-ref chain. Guard against cycles with a visited set.
	visited := make(map[int]bool)

	for idx := stageIdx; !visited[idx]; {
		visited[idx] = true

		info := model.StageInfo(idx)
		if info == nil || info.BaseImage == nil {
			return false
		}

		// If this is a local stage ref, skip image-name heuristics (a stage
		// named "chainguard" is not the Chainguard image) and check the
		// parent's effective USER instead.
		if info.BaseImage.IsStageRef && info.BaseImage.StageIndex >= 0 && fileFacts != nil {
			if parentFacts := fileFacts.Stage(info.BaseImage.StageIndex); parentFacts != nil {
				if parentFacts.EffectiveUser != "" {
					return !facts.IsRootUser(parentFacts.EffectiveUser)
				}
			}
			// Parent has no USER — continue walking up the chain.
			idx = info.BaseImage.StageIndex
			continue
		}

		// Check known non-root external images.
		raw := strings.ToLower(info.BaseImage.Raw)
		if strings.Contains(raw, "distroless") {
			if strings.Contains(raw, "nonroot") || strings.Contains(raw, "debug-nonroot") {
				return true
			}
		}
		if strings.Contains(raw, "chainguard") || strings.Contains(raw, "cgr.dev") {
			return true
		}

		return false
	}

	return false
}

func init() {
	rules.Register(NewStatefulRootRuntimeRule())
}
