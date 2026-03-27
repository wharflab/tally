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
// patterns. This is a stronger, more targeted version of the generic DL3002
// "last USER should not be root" check.
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
	if len(input.Stages) == 0 {
		return nil
	}

	finalIdx := len(input.Stages) - 1

	fileFacts, _ := input.Facts.(*facts.FileFacts) //nolint:errcheck // nil-safe assertion
	if fileFacts == nil {
		return nil
	}

	sf := fileFacts.Stage(finalIdx)
	if sf == nil {
		return nil
	}

	// Step 1: Determine whether the effective user is root.
	implicitRoot := false
	switch {
	case sf.EffectiveUser != "":
		// Explicit USER instruction exists — check if it's root.
		if !facts.IsRootUser(sf.EffectiveUser) {
			return nil
		}
	default:
		// No USER instruction — defaults to root unless the base image
		// is known to default to a non-root user.
		if isKnownNonRootBase(input.Semantic, fileFacts, finalIdx) {
			return nil
		}
		implicitRoot = true
	}

	// Step 2: Scan for stateful signals.
	signals := detectStatefulSignals(input, sf, finalIdx)
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
	if implicitRoot {
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
// RUN mkdir commands.
func detectStatefulSignals(input rules.LintInput, sf *facts.StageFacts, stageIdx int) []statefulSignal {
	var signals []statefulSignal

	stage := input.Stages[stageIdx]

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
			if isStatePath(c.Path) {
				signals = append(signals, statefulSignal{
					kind: command.Workdir,
					path: c.Path,
					loc:  rules.NewLocationFromRanges(input.File, c.Location()),
				})
			}

		case *instructions.CopyCommand:
			if isStatePath(c.DestPath) {
				signals = append(signals, statefulSignal{
					kind: "COPY destination",
					path: c.DestPath,
					loc:  rules.NewLocationFromRanges(input.File, c.Location()),
				})
			}

		case *instructions.AddCommand:
			if isStatePath(c.DestPath) {
				signals = append(signals, statefulSignal{
					kind: "ADD destination",
					path: c.DestPath,
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
			if isMkdir && strings.HasPrefix(f, "/") && !strings.HasPrefix(f, "-") {
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

		// Check known non-root external images at this level.
		raw := strings.ToLower(info.BaseImage.Raw)
		if strings.Contains(raw, "distroless") {
			if strings.Contains(raw, "nonroot") || strings.Contains(raw, "debug-nonroot") {
				return true
			}
		}
		if strings.Contains(raw, "chainguard") || strings.Contains(raw, "cgr.dev") {
			return true
		}

		// If this stage references a local parent stage, check the parent's
		// effective USER. If the parent has an explicit non-root USER, we're
		// done. If the parent has no USER (empty), continue walking up the
		// chain to check the parent's base image.
		if !info.BaseImage.IsStageRef || info.BaseImage.StageIndex < 0 || fileFacts == nil {
			return false
		}

		parentIdx := info.BaseImage.StageIndex
		if parentFacts := fileFacts.Stage(parentIdx); parentFacts != nil {
			if parentFacts.EffectiveUser != "" {
				return !facts.IsRootUser(parentFacts.EffectiveUser)
			}
		}

		// Parent has no USER — continue walking up the chain.
		idx = parentIdx
	}

	return false
}

func init() {
	rules.Register(NewStatefulRootRuntimeRule())
}
