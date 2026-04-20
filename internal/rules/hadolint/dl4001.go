package hadolint

import (
	"sort"

	"github.com/moby/buildkit/frontend/dockerfile/command"

	"github.com/wharflab/tally/internal/ai/autofixdata"
	"github.com/wharflab/tally/internal/facts"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/semantic"
	"github.com/wharflab/tally/internal/shell"
)

// DL4001Rule implements the DL4001 linting rule.
type DL4001Rule struct{}

const (
	dl4001ToolCurl     = "curl"
	dl4001ToolWget     = "wget"
	dl4001ValueUnknown = "unknown"
)

// NewDL4001Rule creates a new DL4001 rule instance.
func NewDL4001Rule() *DL4001Rule {
	return &DL4001Rule{}
}

// Metadata returns the rule metadata.
func (r *DL4001Rule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            rules.HadolintRulePrefix + "DL4001",
		Name:            "Either wget or curl but not both",
		Description:     "Either use wget or curl but not both to reduce image size",
		DocURL:          rules.HadolintDocURL("DL4001"),
		DefaultSeverity: rules.SeverityWarning,
		Category:        "maintainability",
		IsExperimental:  false,
	}
}

// toolUsageDL4001 tracks where a tool is used and whether it was installed.
type toolUsageDL4001 struct {
	occurrences []toolOccurrenceDL4001
	installed   bool
}

type toolOccurrenceDL4001 struct {
	stageIdx int
	loc      rules.Location
	runFacts *facts.RunFacts
}

// usageMapDL4001 tracks tool usage across stages.
type usageMapDL4001 map[int]*toolUsageDL4001

// anyInstalled returns true if any usage in the map has installed=true.
func (m usageMapDL4001) anyInstalled() bool {
	for _, u := range m {
		if u.installed {
			return true
		}
	}
	return false
}

// allOccurrences returns all occurrences from the usage map.
// Occurrences are sorted by stage index for deterministic output.
func (m usageMapDL4001) allOccurrences() []toolOccurrenceDL4001 {
	// Sort stage indices for deterministic output
	indices := make([]int, 0, len(m))
	for idx := range m {
		indices = append(indices, idx)
	}
	sort.Ints(indices)

	occurrences := make([]toolOccurrenceDL4001, 0, len(indices))
	for _, idx := range indices {
		u := m[idx]
		occurrences = append(occurrences, u.occurrences...)
	}
	return occurrences
}

// Check runs the DL4001 rule.
// It warns when both wget and curl are used in different RUN instructions.
func (r *DL4001Rule) Check(input rules.LintInput) []rules.Violation {
	wgetUsage, curlUsage := r.collectToolUsage(input)

	if len(wgetUsage) == 0 || len(curlUsage) == 0 {
		return nil
	}

	violations := r.checkStageConflicts(input, wgetUsage, curlUsage)
	if len(violations) > 0 {
		return violations
	}

	return r.checkCrossStageConflicts(input, wgetUsage, curlUsage)
}

// collectToolUsage scans all stages and collects wget/curl usage.
func (r *DL4001Rule) collectToolUsage(input rules.LintInput) (usageMapDL4001, usageMapDL4001) {
	wgetUsage := make(usageMapDL4001)
	curlUsage := make(usageMapDL4001)

	if input.Facts == nil {
		return wgetUsage, curlUsage
	}

	sem := input.Semantic
	for stageIdx, stageFacts := range input.Facts.Stages() {
		wgetInstalled, curlInstalled := r.getStageInfo(sem, stageIdx)
		tracking := &toolTrackingDL4001{
			wgetInstalled: wgetInstalled,
			curlInstalled: curlInstalled,
			wgetUsage:     wgetUsage,
			curlUsage:     curlUsage,
		}

		for _, runFacts := range stageFacts.Runs {
			if runFacts == nil || runFacts.Run == nil {
				continue
			}
			r.recordToolUsage(stageIdx, rules.NewLocationFromRanges(input.File, runFacts.Run.Location()), runFacts, tracking)
		}
	}

	return wgetUsage, curlUsage
}

// getStageInfo extracts package installation info for a stage.
func (r *DL4001Rule) getStageInfo(sem *semantic.Model, stageIdx int) (bool, bool) {
	var wgetInstalled, curlInstalled bool

	if sem != nil {
		if info := sem.StageInfo(stageIdx); info != nil {
			wgetInstalled = info.HasPackage("wget")
			curlInstalled = info.HasPackage("curl")
		}
	}

	return wgetInstalled, curlInstalled
}

// toolTrackingDL4001 bundles per-stage tool installation state and usage maps.
type toolTrackingDL4001 struct {
	wgetInstalled, curlInstalled bool
	wgetUsage, curlUsage         usageMapDL4001
}

// recordToolUsage checks for wget/curl usage and records it.
func (r *DL4001Rule) recordToolUsage(stageIdx int, loc rules.Location, runFacts *facts.RunFacts, t *toolTrackingDL4001) {
	if hasCommandNamed(runFacts.CommandInfos, "wget") {
		if t.wgetUsage[stageIdx] == nil {
			t.wgetUsage[stageIdx] = &toolUsageDL4001{installed: t.wgetInstalled}
		}
		t.wgetUsage[stageIdx].occurrences = append(t.wgetUsage[stageIdx].occurrences, toolOccurrenceDL4001{
			stageIdx: stageIdx,
			loc:      loc,
			runFacts: runFacts,
		})
	}
	if hasCommandNamed(runFacts.CommandInfos, "curl") {
		if t.curlUsage[stageIdx] == nil {
			t.curlUsage[stageIdx] = &toolUsageDL4001{installed: t.curlInstalled}
		}
		t.curlUsage[stageIdx].occurrences = append(t.curlUsage[stageIdx].occurrences, toolOccurrenceDL4001{
			stageIdx: stageIdx,
			loc:      loc,
			runFacts: runFacts,
		})
	}
}

// checkStageConflicts checks for wget/curl conflicts within individual stages.
func (r *DL4001Rule) checkStageConflicts(input rules.LintInput, wgetUsage, curlUsage usageMapDL4001) []rules.Violation {
	var violations []rules.Violation

	for stageIdx := range input.Stages {
		wget := wgetUsage[stageIdx]
		curl := curlUsage[stageIdx]

		if wget == nil || curl == nil {
			continue
		}

		msg := r.generateMessage(wget.installed, curl.installed)
		occurrences, preferredTool := r.selectOccurrencesToReport(wget, curl)

		for _, occurrence := range occurrences {
			violations = append(violations, r.createViolation(input, occurrence, preferredTool, msg))
		}
	}

	return violations
}

// checkCrossStageConflicts checks for wget/curl conflicts across stages.
func (r *DL4001Rule) checkCrossStageConflicts(input rules.LintInput, wgetUsage, curlUsage usageMapDL4001) []rules.Violation {
	anyWgetInstalled := wgetUsage.anyInstalled()
	anyCurlInstalled := curlUsage.anyInstalled()

	msg := r.generateMessage(anyWgetInstalled, anyCurlInstalled)

	var occurrences []toolOccurrenceDL4001
	preferredTool := dl4001ToolWget
	if anyCurlInstalled && !anyWgetInstalled {
		occurrences = wgetUsage.allOccurrences()
		preferredTool = dl4001ToolCurl
	} else {
		occurrences = curlUsage.allOccurrences()
	}

	violations := make([]rules.Violation, 0, len(occurrences))
	for _, occurrence := range occurrences {
		violations = append(violations, r.createViolation(input, occurrence, preferredTool, msg))
	}

	return violations
}

// selectOccurrencesToReport chooses which tool's occurrences to report as violations.
func (r *DL4001Rule) selectOccurrencesToReport(wget, curl *toolUsageDL4001) ([]toolOccurrenceDL4001, string) {
	if curl.installed && !wget.installed {
		return wget.occurrences, dl4001ToolCurl
	}
	return curl.occurrences, dl4001ToolWget
}

// createViolation creates a violation with the given location and message.
func (r *DL4001Rule) createViolation(
	input rules.LintInput,
	occurrence toolOccurrenceDL4001,
	preferredTool string,
	msg messageInfoDL4001,
) rules.Violation {
	v := rules.NewViolation(
		occurrence.loc,
		r.Metadata().Code,
		msg.message,
		r.Metadata().DefaultSeverity,
	).WithDocURL(r.Metadata().DocURL).WithDetail(msg.detail)

	if fix := r.buildSuggestedFix(input, occurrence, preferredTool); fix != nil {
		v = v.WithSuggestedFix(fix)
	}

	return v
}

// messageInfoDL4001 holds a violation message and detail.
type messageInfoDL4001 struct {
	message string
	detail  string
}

// generateMessage creates a context-aware message based on which tools are installed.
func (r *DL4001Rule) generateMessage(wgetInstalled, curlInstalled bool) messageInfoDL4001 {
	switch {
	case curlInstalled && !wgetInstalled:
		return messageInfoDL4001{
			message: "wget is used but curl is installed; use curl instead to avoid installing wget",
			detail: "You're already installing curl in this Dockerfile. " +
				"Using wget requires installing an additional package, increasing image size. " +
				"Replace wget commands with curl equivalents.",
		}

	case wgetInstalled && !curlInstalled:
		return messageInfoDL4001{
			message: "curl is used but wget is installed; use wget instead to avoid installing curl",
			detail: "You're already installing wget in this Dockerfile. " +
				"Using curl requires installing an additional package, increasing image size. " +
				"Replace curl commands with wget equivalents.",
		}

	case wgetInstalled && curlInstalled:
		return messageInfoDL4001{
			message: "both wget and curl are installed; pick one to reduce image size",
			detail: "Both wget and curl are being installed, which increases image size unnecessarily. " +
				"Choose one tool and use it consistently across the image.",
		}

	default:
		return messageInfoDL4001{
			message: "both wget and curl are used; pick one to reduce image size and complexity",
			detail: "Using both wget and curl increases image size and maintenance burden. " +
				"Standardize on one tool for consistency across the image.",
		}
	}
}

func hasCommandNamed(commands []shell.CommandInfo, name string) bool {
	for i := range commands {
		if commands[i].Name == name {
			return true
		}
	}
	return false
}

func countCommandsNamed(commands []shell.CommandInfo, name string) int {
	count := 0
	for i := range commands {
		if commands[i].Name == name {
			count++
		}
	}
	return count
}

func (r *DL4001Rule) buildSuggestedFix(
	input rules.LintInput,
	occurrence toolOccurrenceDL4001,
	preferredTool string,
) *rules.SuggestedFix {
	if fix := r.buildDeterministicSuggestedFix(input.File, occurrence, preferredTool); fix != nil {
		return fix
	}
	return r.buildAISuggestedFix(input, occurrence, preferredTool)
}

func (r *DL4001Rule) buildDeterministicSuggestedFix(
	file string,
	occurrence toolOccurrenceDL4001,
	preferredTool string,
) *rules.SuggestedFix {
	if file == "" || occurrence.runFacts == nil {
		return nil
	}

	sourceTool := dl4001ToolCurl
	if preferredTool == dl4001ToolCurl {
		sourceTool = dl4001ToolWget
	}

	if countCommandsNamed(occurrence.runFacts.CommandInfos, sourceTool) == 0 {
		return nil
	}
	if countCommandsNamed(occurrence.runFacts.CommandInfos, preferredTool) > 0 {
		return nil
	}

	edits := make([]rules.TextEdit, 0, len(occurrence.runFacts.CommandOperationFacts))
	covered := 0

	for i := range occurrence.runFacts.CommandOperationFacts {
		fact := occurrence.runFacts.CommandOperationFacts[i]
		if fact.Tool != sourceTool {
			continue
		}
		covered++
		if fact.Status != facts.CommandOperationLifted || fact.SourceRange == nil || fact.HTTPTransfer == nil {
			return nil
		}

		replacement, ok := fact.HTTPTransfer.LowerToTool(preferredTool)
		if !ok {
			return nil
		}

		edits = append(edits, rules.TextEdit{
			Location: rules.NewRangeLocation(
				file,
				fact.SourceRange.StartLine,
				fact.SourceRange.StartCol,
				fact.SourceRange.EndLine,
				fact.SourceRange.EndCol,
			),
			NewText: replacement,
		})
	}

	if covered == 0 || covered != countCommandsNamed(occurrence.runFacts.CommandInfos, sourceTool) {
		return nil
	}

	return &rules.SuggestedFix{
		Description: "Replace " + sourceTool + " with " + preferredTool,
		Safety:      rules.FixUnsafe,
		Edits:       edits,
	}
}

func (r *DL4001Rule) buildAISuggestedFix(
	input rules.LintInput,
	occurrence toolOccurrenceDL4001,
	preferredTool string,
) *rules.SuggestedFix {
	if input.File == "" || occurrence.runFacts == nil {
		return nil
	}

	sourceTool := dl4001ToolCurl
	if preferredTool == dl4001ToolCurl {
		sourceTool = dl4001ToolWget
	}
	if countCommandsNamed(occurrence.runFacts.CommandInfos, sourceTool) != 1 {
		return nil
	}
	if countCommandsNamed(occurrence.runFacts.CommandInfos, preferredTool) > 0 {
		return nil
	}

	var targetFact *facts.CommandOperationFact
	for i := range occurrence.runFacts.CommandOperationFacts {
		fact := &occurrence.runFacts.CommandOperationFacts[i]
		if fact.Tool != sourceTool {
			continue
		}
		if fact.SourceRange == nil || fact.SourceRange.StartLine != fact.SourceRange.EndLine {
			return nil
		}
		targetFact = fact
		break
	}
	if targetFact == nil {
		return nil
	}

	targetIndex := firstCommandIndexNamed(occurrence.runFacts.CommandInfos, sourceTool)
	if targetIndex < 0 {
		return nil
	}

	sm := input.SourceMap()
	lineText := sm.Line(targetFact.SourceRange.StartLine - 1)
	if lineText == "" || targetFact.SourceRange.EndCol > len(lineText) {
		return nil
	}
	targetCommandText := lineText[targetFact.SourceRange.StartCol:targetFact.SourceRange.EndCol]

	blockers := make([]string, 0, len(targetFact.Blockers)+1)
	for _, blocker := range targetFact.Blockers {
		blockers = append(blockers, blocker.Reason)
	}
	if targetFact.HTTPTransfer != nil {
		if _, ok := targetFact.HTTPTransfer.LowerToTool(preferredTool); !ok {
			blockers = append(blockers, "deterministic lowering is unavailable for this command")
		}
	}
	if len(blockers) == 0 {
		blockers = append(blockers, "deterministic lowering is unavailable for this command")
	}

	commandNames := make([]string, 0, len(occurrence.runFacts.CommandInfos))
	for _, cmd := range occurrence.runFacts.CommandInfos {
		commandNames = append(commandNames, cmd.Name)
	}

	return &rules.SuggestedFix{
		Description:  "AI AutoFix: replace " + sourceTool + " with " + preferredTool,
		Safety:       rules.FixUnsafe,
		NeedsResolve: true,
		ResolverID:   autofixdata.ResolverID,
		ResolverData: &autofixdata.ObjectiveRequest{
			Kind: autofixdata.ObjectiveCommandFamilyNormalize,
			File: input.File,
			Facts: map[string]any{
				"platform-os":            dl4001PlatformOS(input.Semantic, occurrence.stageIdx),
				"shell-variant":          dl4001ShellVariant(occurrence.runFacts.Shell.Variant),
				"preferred-tool":         preferredTool,
				"source-tool":            sourceTool,
				"target-start-line":      targetFact.SourceRange.StartLine,
				"target-end-line":        targetFact.SourceRange.EndLine,
				"target-start-col":       targetFact.SourceRange.StartCol,
				"target-end-col":         targetFact.SourceRange.EndCol,
				"target-command-text":    targetCommandText,
				"target-run-script":      occurrence.runFacts.SourceScript,
				"target-command-index":   targetIndex,
				"original-command-names": commandNames,
				"literal-urls":           literalURLsFromCommand(targetFact.Command),
				"blockers":               blockers,
			},
		},
	}
}

func firstCommandIndexNamed(commands []shell.CommandInfo, name string) int {
	for i, cmd := range commands {
		if cmd.Name == name {
			return i
		}
	}
	return -1
}

func dl4001PlatformOS(sem *semantic.Model, stageIdx int) string {
	if sem != nil {
		if info := sem.StageInfo(stageIdx); info != nil {
			switch {
			case info.IsWindows():
				return "windows"
			case info.IsLinux():
				return "linux"
			}
		}
	}
	return dl4001ValueUnknown
}

func dl4001ShellVariant(variant shell.Variant) string {
	switch variant {
	case shell.VariantBash:
		return "bash"
	case shell.VariantPOSIX:
		return "sh"
	case shell.VariantMksh:
		return "mksh"
	case shell.VariantZsh:
		return "zsh"
	case shell.VariantPowerShell:
		return "powershell"
	case shell.VariantCmd:
		return command.Cmd
	case shell.VariantUnknown:
		return dl4001ValueUnknown
	default:
		return dl4001ValueUnknown
	}
}

// init registers the rule with the default registry.
func init() {
	rules.Register(NewDL4001Rule())
}
