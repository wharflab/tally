// Package wgetorcurl implements hadolint DL4001.
// This rule warns when both wget and curl are used in the same Dockerfile,
// as it's better to standardize on one tool to reduce image size and complexity.
package wgetorcurl

import (
	"sort"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/tinovyatkin/tally/internal/rules"
	"github.com/tinovyatkin/tally/internal/semantic"
	"github.com/tinovyatkin/tally/internal/shell"
)

// Rule implements the DL4001 linting rule.
type Rule struct{}

// Metadata returns the rule metadata.
func (r *Rule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            rules.HadolintRulePrefix + "DL4001",
		Name:            "Either wget or curl but not both",
		Description:     "Either use wget or curl but not both to reduce image size",
		DocURL:          "https://github.com/hadolint/hadolint/wiki/DL4001",
		DefaultSeverity: rules.SeverityWarning,
		Category:        "maintainability",
		IsExperimental:  false,
	}
}

// toolUsage tracks where a tool is used and whether it was installed.
type toolUsage struct {
	locations []rules.Location
	installed bool
}

// usageMap tracks tool usage across stages.
type usageMap map[int]*toolUsage

// anyInstalled returns true if any usage in the map has installed=true.
func (m usageMap) anyInstalled() bool {
	for _, u := range m {
		if u.installed {
			return true
		}
	}
	return false
}

// allLocations returns all locations from the usage map.
// Locations are sorted by stage index for deterministic output.
func (m usageMap) allLocations() []rules.Location {
	// Sort stage indices for deterministic output
	indices := make([]int, 0, len(m))
	for idx := range m {
		indices = append(indices, idx)
	}
	sort.Ints(indices)

	var locs []rules.Location
	for _, idx := range indices {
		u := m[idx]
		locs = append(locs, u.locations...)
	}
	return locs
}

// Check runs the DL4001 rule.
// It warns when both wget and curl are used in different RUN instructions.
func (r *Rule) Check(input rules.LintInput) []rules.Violation {
	wgetUsage, curlUsage := r.collectToolUsage(input)

	if len(wgetUsage) == 0 || len(curlUsage) == 0 {
		return nil
	}

	violations := r.checkStageConflicts(input, wgetUsage, curlUsage)
	if len(violations) > 0 {
		return violations
	}

	return r.checkCrossStageConflicts(wgetUsage, curlUsage)
}

// collectToolUsage scans all stages and collects wget/curl usage.
func (r *Rule) collectToolUsage(input rules.LintInput) (usageMap, usageMap) {
	wgetUsage := make(usageMap)
	curlUsage := make(usageMap)

	sem, ok := input.Semantic.(*semantic.Model)
	if !ok {
		sem = nil
	}

	for stageIdx, stage := range input.Stages {
		shellVariant, wgetInstalled, curlInstalled := r.getStageInfo(sem, stageIdx)

		for _, cmd := range stage.Commands {
			run, ok := cmd.(*instructions.RunCommand)
			if !ok {
				continue
			}

			cmdStr := r.buildCommandString(run)
			loc := rules.NewLocationFromRanges(input.File, run.Location())

			r.recordToolUsage(cmdStr, shellVariant, stageIdx, loc,
				wgetInstalled, curlInstalled, wgetUsage, curlUsage)
		}
	}

	return wgetUsage, curlUsage
}

// getStageInfo extracts shell variant and package installation info for a stage.
func (r *Rule) getStageInfo(sem *semantic.Model, stageIdx int) (shell.Variant, bool, bool) {
	shellVariant := shell.VariantBash
	var wgetInstalled, curlInstalled bool

	if sem != nil {
		if info := sem.StageInfo(stageIdx); info != nil {
			shellVariant = info.ShellSetting.Variant
			wgetInstalled = info.HasPackage("wget")
			curlInstalled = info.HasPackage("curl")
		}
	}

	return shellVariant, wgetInstalled, curlInstalled
}

// buildCommandString builds the command string from a RUN command including heredocs.
func (r *Rule) buildCommandString(run *instructions.RunCommand) string {
	var cmdBuilder strings.Builder
	cmdBuilder.WriteString(strings.Join(run.CmdLine, " "))
	for _, f := range run.Files {
		cmdBuilder.WriteByte('\n')
		cmdBuilder.WriteString(f.Data)
	}
	return cmdBuilder.String()
}

// recordToolUsage checks for wget/curl usage and records it.
// Skips analysis for non-POSIX shells (e.g., PowerShell) since shell
// command parsing doesn't apply to them.
func (r *Rule) recordToolUsage(
	cmdStr string,
	shellVariant shell.Variant,
	stageIdx int,
	loc rules.Location,
	wgetInstalled, curlInstalled bool,
	wgetUsage, curlUsage usageMap,
) {
	// Skip shell command analysis for non-POSIX shells
	if shellVariant.IsNonPOSIX() {
		return
	}

	if shell.ContainsCommandWithVariant(cmdStr, "wget", shellVariant) {
		if wgetUsage[stageIdx] == nil {
			wgetUsage[stageIdx] = &toolUsage{installed: wgetInstalled}
		}
		wgetUsage[stageIdx].locations = append(wgetUsage[stageIdx].locations, loc)
	}
	if shell.ContainsCommandWithVariant(cmdStr, "curl", shellVariant) {
		if curlUsage[stageIdx] == nil {
			curlUsage[stageIdx] = &toolUsage{installed: curlInstalled}
		}
		curlUsage[stageIdx].locations = append(curlUsage[stageIdx].locations, loc)
	}
}

// checkStageConflicts checks for wget/curl conflicts within individual stages.
func (r *Rule) checkStageConflicts(input rules.LintInput, wgetUsage, curlUsage usageMap) []rules.Violation {
	var violations []rules.Violation

	for stageIdx := range input.Stages {
		wget := wgetUsage[stageIdx]
		curl := curlUsage[stageIdx]

		if wget == nil || curl == nil {
			continue
		}

		msg := r.generateMessage(wget.installed, curl.installed)
		locsToReport := r.selectLocationsToReport(wget, curl)

		for _, loc := range locsToReport {
			violations = append(violations, r.createViolation(loc, msg))
		}
	}

	return violations
}

// checkCrossStageConflicts checks for wget/curl conflicts across stages.
func (r *Rule) checkCrossStageConflicts(wgetUsage, curlUsage usageMap) []rules.Violation {
	anyWgetInstalled := wgetUsage.anyInstalled()
	anyCurlInstalled := curlUsage.anyInstalled()

	msg := r.generateMessage(anyWgetInstalled, anyCurlInstalled)

	var locsToReport []rules.Location
	if anyCurlInstalled && !anyWgetInstalled {
		locsToReport = wgetUsage.allLocations()
	} else {
		locsToReport = curlUsage.allLocations()
	}

	violations := make([]rules.Violation, 0, len(locsToReport))
	for _, loc := range locsToReport {
		violations = append(violations, r.createViolation(loc, msg))
	}

	return violations
}

// selectLocationsToReport chooses which tool's locations to report as violations.
func (r *Rule) selectLocationsToReport(wget, curl *toolUsage) []rules.Location {
	if curl.installed && !wget.installed {
		return wget.locations
	}
	return curl.locations
}

// createViolation creates a violation with the given location and message.
func (r *Rule) createViolation(loc rules.Location, msg messageInfo) rules.Violation {
	return rules.NewViolation(
		loc,
		r.Metadata().Code,
		msg.message,
		r.Metadata().DefaultSeverity,
	).WithDocURL(r.Metadata().DocURL).WithDetail(msg.detail)
}

// messageInfo holds a violation message and detail.
type messageInfo struct {
	message string
	detail  string
}

// generateMessage creates a context-aware message based on which tools are installed.
func (r *Rule) generateMessage(wgetInstalled, curlInstalled bool) messageInfo {
	switch {
	case curlInstalled && !wgetInstalled:
		return messageInfo{
			message: "wget is used but curl is installed; use curl instead to avoid installing wget",
			detail: "You're already installing curl in this Dockerfile. " +
				"Using wget requires installing an additional package, increasing image size. " +
				"Replace wget commands with curl equivalents.",
		}

	case wgetInstalled && !curlInstalled:
		return messageInfo{
			message: "curl is used but wget is installed; use wget instead to avoid installing curl",
			detail: "You're already installing wget in this Dockerfile. " +
				"Using curl requires installing an additional package, increasing image size. " +
				"Replace curl commands with wget equivalents.",
		}

	case wgetInstalled && curlInstalled:
		return messageInfo{
			message: "both wget and curl are installed; pick one to reduce image size",
			detail: "Both wget and curl are being installed, which increases image size unnecessarily. " +
				"Choose one tool and use it consistently. curl is generally preferred in containers " +
				"due to better scripting support and broader protocol support.",
		}

	default:
		return messageInfo{
			message: "both wget and curl are used; pick one to reduce image size and complexity",
			detail: "Using both wget and curl increases image size and maintenance burden. " +
				"Standardize on one tool. curl is generally preferred in containers " +
				"due to better scripting support and broader protocol support.",
		}
	}
}

// New creates a new DL4001 rule instance.
func New() *Rule {
	return &Rule{}
}

// init registers the rule with the default registry.
func init() {
	rules.Register(New())
}
