package hadolint

import (
	"sort"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/command"

	"github.com/wharflab/tally/internal/ai/autofixdata"
	"github.com/wharflab/tally/internal/facts"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/rules/configutil"
	"github.com/wharflab/tally/internal/semantic"
	"github.com/wharflab/tally/internal/shell"
)

const (
	dl4001ToolCurl     = "curl"
	dl4001ToolWget     = "wget"
	dl4001ValueUnknown = "unknown"

	// DL4001FixPreferenceAuto lets tally pick the fix direction from stage install signals.
	DL4001FixPreferenceAuto = "auto"
	// DL4001FixPreferenceCurl forces rewrites to curl regardless of which tool is installed.
	DL4001FixPreferenceCurl = dl4001ToolCurl
	// DL4001FixPreferenceWget forces rewrites to wget regardless of which tool is installed.
	DL4001FixPreferenceWget = dl4001ToolWget
)

// DL4001Config is the configuration for the DL4001 rule.
type DL4001Config struct {
	// FixPreference selects which tool auto-fixes should converge on.
	// Values: "auto" (default; infer from stage install signals), "curl", "wget".
	FixPreference string `json:"fix-preference,omitempty" koanf:"fix-preference"`
}

// DefaultDL4001Config returns the default configuration.
func DefaultDL4001Config() DL4001Config {
	return DL4001Config{FixPreference: DL4001FixPreferenceAuto}
}

// DL4001Rule implements the DL4001 linting rule.
type DL4001Rule struct {
	schema map[string]any
}

// NewDL4001Rule creates a new DL4001 rule instance.
func NewDL4001Rule() *DL4001Rule {
	schema, err := configutil.RuleSchema(rules.HadolintRulePrefix + "DL4001")
	if err != nil {
		panic(err)
	}
	return &DL4001Rule{schema: schema}
}

// Schema returns the JSON Schema for this rule's configuration.
func (r *DL4001Rule) Schema() map[string]any {
	return r.schema
}

// DefaultConfig returns the default configuration for this rule.
func (r *DL4001Rule) DefaultConfig() any {
	return DefaultDL4001Config()
}

// ValidateConfig validates the configuration against the rule's JSON Schema.
func (r *DL4001Rule) ValidateConfig(config any) error {
	return configutil.ValidateRuleOptions(r.Metadata().Code, config)
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
	stageIdx         int
	loc              rules.Location
	runFacts         *facts.RunFacts
	invocationCount  int
	installedInStage bool
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
// It warns when both wget and curl are present in the Dockerfile — either
// invoked as commands or pulled in by package installs. Each offending
// invocation gets a narrow sync rewrite; a single extra violation owns an
// async post-sync cleanup that drops the non-preferred tool from installs
// and deletes any now-stale config artifacts (e.g. a .curlrc heredoc).
func (r *DL4001Rule) Check(input rules.LintInput) []rules.Violation {
	cfg := r.resolveConfig(input.Config)
	wgetUsage, curlUsage := r.collectToolUsage(input)

	if len(wgetUsage) == 0 || len(curlUsage) == 0 {
		return nil
	}

	violations, preferredTool := r.checkStageConflicts(input, wgetUsage, curlUsage, cfg)
	if len(violations) == 0 {
		violations, preferredTool = r.checkCrossStageConflicts(input, wgetUsage, curlUsage, cfg)
	}

	if preferredTool == "" {
		return violations
	}

	if cleanup := r.buildCleanupViolation(input, preferredTool, wgetUsage, curlUsage); cleanup != nil {
		violations = append(violations, *cleanup)
	}
	hintACPFixes(violations, dl4001SourceTool(preferredTool))
	return violations
}

func (r *DL4001Rule) resolveConfig(config any) DL4001Config {
	cfg := configutil.Coerce(config, DefaultDL4001Config())
	switch cfg.FixPreference {
	case DL4001FixPreferenceCurl, DL4001FixPreferenceWget, DL4001FixPreferenceAuto:
		return cfg
	default:
		return DefaultDL4001Config()
	}
}

// collectToolUsage scans all stages and collects wget/curl usage. A stage that
// installs a tool but never invokes it still counts as "using" it for DL4001's
// purposes: installing a redundant tool is itself the offense the rule targets.
// Such stages get an install-anchored occurrence so the violation has a location
// to report and the fix has a line to anchor install-removal onto.
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

		r.seedInstallOnlyOccurrences(input, stageIdx, stageFacts, tracking)
	}

	return wgetUsage, curlUsage
}

// seedInstallOnlyOccurrences makes sure an installed-but-uninvoked tool still
// appears in the usage map so the rule fires and the fix has an anchor location.
func (r *DL4001Rule) seedInstallOnlyOccurrences(
	input rules.LintInput,
	stageIdx int,
	stageFacts *facts.StageFacts,
	t *toolTrackingDL4001,
) {
	if stageFacts == nil {
		return
	}
	if t.wgetInstalled && t.wgetUsage[stageIdx] == nil {
		if loc, runFacts, ok := firstInstallOccurrence(input.File, stageFacts, dl4001ToolWget); ok {
			t.wgetUsage[stageIdx] = &toolUsageDL4001{installed: true, occurrences: []toolOccurrenceDL4001{{
				stageIdx:         stageIdx,
				loc:              loc,
				runFacts:         runFacts,
				invocationCount:  0,
				installedInStage: true,
			}}}
		}
	}
	if t.curlInstalled && t.curlUsage[stageIdx] == nil {
		if loc, runFacts, ok := firstInstallOccurrence(input.File, stageFacts, dl4001ToolCurl); ok {
			t.curlUsage[stageIdx] = &toolUsageDL4001{installed: true, occurrences: []toolOccurrenceDL4001{{
				stageIdx:         stageIdx,
				loc:              loc,
				runFacts:         runFacts,
				invocationCount:  0,
				installedInStage: true,
			}}}
		}
	}
}

// firstInstallOccurrence returns the location of the first install RUN that
// installs tool and the associated RunFacts. Returns false when no such RUN
// exists (which is expected when the tool is inherited from the base image).
func firstInstallOccurrence(file string, stageFacts *facts.StageFacts, tool string) (rules.Location, *facts.RunFacts, bool) {
	for _, runFacts := range stageFacts.Runs {
		if runFacts == nil || runFacts.Run == nil {
			continue
		}
		for _, ic := range runFacts.InstallCommands {
			for _, pkg := range ic.Packages {
				if packageMatchesTool(pkg.Normalized, tool) {
					return rules.NewLocationFromRanges(file, runFacts.Run.Location()), runFacts, true
				}
			}
		}
	}
	return rules.Location{}, nil, false
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
	if n := countCommandsNamed(runFacts.CommandInfos, "wget"); n > 0 {
		if t.wgetUsage[stageIdx] == nil {
			t.wgetUsage[stageIdx] = &toolUsageDL4001{installed: t.wgetInstalled}
		}
		t.wgetUsage[stageIdx].occurrences = append(t.wgetUsage[stageIdx].occurrences, toolOccurrenceDL4001{
			stageIdx:         stageIdx,
			loc:              loc,
			runFacts:         runFacts,
			invocationCount:  n,
			installedInStage: t.wgetInstalled,
		})
	}
	if n := countCommandsNamed(runFacts.CommandInfos, "curl"); n > 0 {
		if t.curlUsage[stageIdx] == nil {
			t.curlUsage[stageIdx] = &toolUsageDL4001{installed: t.curlInstalled}
		}
		t.curlUsage[stageIdx].occurrences = append(t.curlUsage[stageIdx].occurrences, toolOccurrenceDL4001{
			stageIdx:         stageIdx,
			loc:              loc,
			runFacts:         runFacts,
			invocationCount:  n,
			installedInStage: t.curlInstalled,
		})
	}
}

// checkStageConflicts checks for wget/curl conflicts within individual stages.
// Returns the violations and the preferred tool (empty if no conflict was found).
// Same-stage conflicts always pick the same preferred tool across stages if they all agree;
// otherwise the first stage's decision wins for install-removal purposes.
func (r *DL4001Rule) checkStageConflicts(
	input rules.LintInput,
	wgetUsage, curlUsage usageMapDL4001,
	cfg DL4001Config,
) ([]rules.Violation, string) {
	var violations []rules.Violation
	preferredTool := ""

	for stageIdx := range input.Stages {
		wget := wgetUsage[stageIdx]
		curl := curlUsage[stageIdx]

		if wget == nil || curl == nil {
			continue
		}

		stagePreferred := preferredToolForConflict(cfg, wget.occurrences, curl.occurrences)
		if preferredTool == "" {
			preferredTool = stagePreferred
		}
		msg := r.generateMessage(wget.installed, curl.installed, stagePreferred)
		occurrences := nonPreferredOccurrences(stagePreferred, wget, curl)

		for _, occurrence := range occurrences {
			violations = append(violations, r.createViolation(input, occurrence, stagePreferred, msg))
		}
	}

	return violations, preferredTool
}

// checkCrossStageConflicts checks for wget/curl conflicts across stages.
func (r *DL4001Rule) checkCrossStageConflicts(
	input rules.LintInput,
	wgetUsage, curlUsage usageMapDL4001,
	cfg DL4001Config,
) ([]rules.Violation, string) {
	anyWgetInstalled := wgetUsage.anyInstalled()
	anyCurlInstalled := curlUsage.anyInstalled()

	allWget := wgetUsage.allOccurrences()
	allCurl := curlUsage.allOccurrences()
	preferredTool := preferredToolForConflict(cfg, allWget, allCurl)

	msg := r.generateMessage(anyWgetInstalled, anyCurlInstalled, preferredTool)

	var occurrences []toolOccurrenceDL4001
	if preferredTool == dl4001ToolCurl {
		occurrences = allWget
	} else {
		occurrences = allCurl
	}

	violations := make([]rules.Violation, 0, len(occurrences))
	for _, occurrence := range occurrences {
		if occurrence.invocationCount == 0 {
			continue // install-only handled by the cleanup violation
		}
		violations = append(violations, r.createViolation(input, occurrence, preferredTool, msg))
	}

	return violations, preferredTool
}

// preferredToolForConflict returns the tool that should win an auto-mode tie-break,
// using usage-based heuristics: tools used without an explicit install beat tools
// that require one; otherwise invocation count breaks the tie; otherwise first seen.
// An explicit fix-preference always takes precedence.
func preferredToolForConflict(cfg DL4001Config, wget, curl []toolOccurrenceDL4001) string {
	switch cfg.FixPreference {
	case DL4001FixPreferenceCurl:
		return dl4001ToolCurl
	case DL4001FixPreferenceWget:
		return dl4001ToolWget
	}

	wgetUWI := hasUsageWithoutInstall(wget)
	curlUWI := hasUsageWithoutInstall(curl)
	if wgetUWI != curlUWI {
		if curlUWI {
			return dl4001ToolCurl
		}
		return dl4001ToolWget
	}

	wgetCount := totalInvocations(wget)
	curlCount := totalInvocations(curl)
	if wgetCount != curlCount {
		if curlCount > wgetCount {
			return dl4001ToolCurl
		}
		return dl4001ToolWget
	}

	if positionBefore(firstOccurrence(curl), firstOccurrence(wget)) {
		return dl4001ToolCurl
	}
	return dl4001ToolWget
}

// nonPreferredOccurrences returns the invocation occurrences of the non-preferred
// tool. Install-only occurrences (invocationCount==0) are omitted: those stages
// have no invocation to rewrite, and the cleanup violation emitted separately
// already covers removing the install and config.
func nonPreferredOccurrences(preferredTool string, wget, curl *toolUsageDL4001) []toolOccurrenceDL4001 {
	src := curl.occurrences
	if preferredTool == dl4001ToolCurl {
		src = wget.occurrences
	}
	out := make([]toolOccurrenceDL4001, 0, len(src))
	for _, occ := range src {
		if occ.invocationCount > 0 {
			out = append(out, occ)
		}
	}
	return out
}

func hasUsageWithoutInstall(occurrences []toolOccurrenceDL4001) bool {
	for _, occ := range occurrences {
		if !occ.installedInStage {
			return true
		}
	}
	return false
}

func totalInvocations(occurrences []toolOccurrenceDL4001) int {
	total := 0
	for _, occ := range occurrences {
		total += occ.invocationCount
	}
	return total
}

func firstOccurrence(occurrences []toolOccurrenceDL4001) rules.Position {
	if len(occurrences) == 0 {
		return rules.Position{Line: -1, Column: -1}
	}
	best := occurrences[0].loc.Start
	for _, occ := range occurrences[1:] {
		if positionBefore(occ.loc.Start, best) {
			best = occ.loc.Start
		}
	}
	return best
}

// positionBefore reports whether a precedes b in source order.
func positionBefore(a, b rules.Position) bool {
	if a.Line != b.Line {
		return a.Line < b.Line
	}
	return a.Column < b.Column
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

// generateMessage creates a context-aware message aligned with the tool that
// was actually chosen to win (preferredTool). Without this, the wording could
// contradict the attached fix — e.g. "curl is installed; use curl instead"
// while the fix rewrites curl → wget because the usage-based heuristics
// picked wget.
func (r *DL4001Rule) generateMessage(
	wgetInstalled, curlInstalled bool,
	preferredTool string,
) messageInfoDL4001 {
	sourceTool := dl4001SourceTool(preferredTool)
	preferredInstalled, sourceInstalled := wgetInstalled, curlInstalled
	if preferredTool == dl4001ToolCurl {
		preferredInstalled, sourceInstalled = curlInstalled, wgetInstalled
	}

	switch {
	case preferredInstalled && sourceInstalled:
		return messageInfoDL4001{
			message: "both wget and curl are installed; keep " + preferredTool +
				" and remove " + sourceTool,
			detail: "Both wget and curl are being installed, which increases image size unnecessarily. " +
				"Keep " + preferredTool + " and replace " + sourceTool + " usages with " + preferredTool + ".",
		}
	case sourceInstalled && !preferredInstalled:
		// The non-preferred tool is the one being installed; preferring the
		// other means the install is redundant once invocations are rewritten.
		return messageInfoDL4001{
			message: sourceTool + " is installed but " + preferredTool +
				" is available; switch to " + preferredTool + " and drop the " +
				sourceTool + " install",
			detail: "You're installing " + sourceTool + " only for a couple of downloads, " +
				"but " + preferredTool + " is already available. " +
				"Replace " + sourceTool + " commands with " + preferredTool +
				" equivalents and drop the " + sourceTool + " install.",
		}
	case preferredInstalled && !sourceInstalled:
		// The preferred tool is installed; the non-preferred tool is coming
		// from the base image and just needs its invocations rewritten.
		return messageInfoDL4001{
			message: preferredTool + " is installed; replace " + sourceTool +
				" commands with " + preferredTool + " to avoid mixing two tools",
			detail: "You're already installing " + preferredTool + " in this Dockerfile. " +
				"Using " + sourceTool + " alongside it adds maintenance burden. " +
				"Replace " + sourceTool + " commands with " + preferredTool + " equivalents.",
		}
	default:
		// Neither installed — both tools come from the base image. Still
		// worth standardizing on one to reduce cognitive overhead.
		return messageInfoDL4001{
			message: "both wget and curl are used; standardize on " + preferredTool,
			detail: "Using both wget and curl increases maintenance burden. " +
				"Replace " + sourceTool + " commands with " + preferredTool + " to keep the Dockerfile consistent.",
		}
	}
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
	if occurrence.invocationCount == 0 {
		// Install-only occurrence: no invocation to rewrite here. The async
		// cleanup violation (emitted separately) handles install/config
		// removal after sync fixes run.
		return nil
	}
	lowerOpts := facts.HTTPTransferLowerOptions{
		WindowsTarget: dl4001StageIsWindows(input.Semantic, occurrence.stageIdx),
	}
	if fix := r.buildDeterministicSuggestedFix(input.File, occurrence, preferredTool, lowerOpts); fix != nil {
		return fix
	}
	return r.buildAISuggestedFix(input, occurrence, preferredTool, lowerOpts)
}

func dl4001SourceTool(preferredTool string) string {
	if preferredTool == dl4001ToolCurl {
		return dl4001ToolWget
	}
	return dl4001ToolCurl
}

// buildCleanupViolation emits a single violation whose fix is an async
// post-sync cleanup for the non-preferred tool. The violation is anchored at
// the first install RUN that installs the source tool; if no such install
// exists (tool inherited from base image), no cleanup violation is emitted.
func (r *DL4001Rule) buildCleanupViolation(
	input rules.LintInput,
	preferredTool string,
	wgetUsage, curlUsage usageMapDL4001,
) *rules.Violation {
	sourceTool := dl4001SourceTool(preferredTool)
	installed := false
	if sourceTool == dl4001ToolCurl {
		installed = curlUsage.anyInstalled()
	} else {
		installed = wgetUsage.anyInstalled()
	}
	if !installed || input.Facts == nil || input.File == "" {
		return nil
	}

	loc, ok := r.findFirstInstallLocation(input, sourceTool)
	if !ok {
		return nil
	}

	message := "remove stale " + sourceTool + " install and config after switching to " + preferredTool
	detail := "DL4001 cleanup: after rewriting " + sourceTool + " invocations to " + preferredTool +
		", the " + sourceTool + " install and any " + sourceTool + "-specific config files are dead weight."

	v := rules.NewViolation(loc, r.Metadata().Code, message, r.Metadata().DefaultSeverity).
		WithDocURL(r.Metadata().DocURL).
		WithDetail(detail).
		WithSuggestedFix(&rules.SuggestedFix{
			Description:  "Drop " + sourceTool + " install and config",
			Safety:       rules.FixUnsafe,
			NeedsResolve: true,
			ResolverID:   rules.DL4001CleanupResolverID,
			ResolverData: &rules.DL4001CleanupResolveData{SourceTool: sourceTool},
		})
	return &v
}

func (r *DL4001Rule) findFirstInstallLocation(input rules.LintInput, sourceTool string) (rules.Location, bool) {
	for _, stageFacts := range input.Facts.Stages() {
		loc, _, ok := firstInstallOccurrence(input.File, stageFacts, sourceTool)
		if ok {
			return loc, true
		}
	}
	return rules.Location{}, false
}

func (r *DL4001Rule) buildDeterministicSuggestedFix(
	file string,
	occurrence toolOccurrenceDL4001,
	preferredTool string,
	lowerOpts facts.HTTPTransferLowerOptions,
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

		replacement, ok := fact.HTTPTransfer.LowerToTool(preferredTool, lowerOpts)
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

func dl4001StageIsWindows(sem *semantic.Model, stageIdx int) bool {
	if sem == nil {
		return false
	}
	info := sem.StageInfo(stageIdx)
	if info == nil {
		return false
	}
	return info.IsWindows()
}

func (r *DL4001Rule) buildAISuggestedFix(
	input rules.LintInput,
	occurrence toolOccurrenceDL4001,
	preferredTool string,
	lowerOpts facts.HTTPTransferLowerOptions,
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
		if _, ok := targetFact.HTTPTransfer.LowerToTool(preferredTool, lowerOpts); !ok {
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

func hintACPFixes(violations []rules.Violation, sourceTool string) {
	for i := range violations {
		fix := violations[i].SuggestedFix
		if fix == nil || !fix.NeedsResolve {
			continue
		}
		req, ok := fix.ResolverData.(*autofixdata.ObjectiveRequest)
		if !ok || req == nil {
			continue
		}
		if req.Facts == nil {
			req.Facts = map[string]any{}
		}
		req.Facts["remove-source-tool-install"] = sourceTool
	}
}

// packageMatchesTool reports whether a normalized package token matches sourceTool,
// ignoring common version suffixes used by apt/apk/dnf/zypper/yum/choco. Used by
// firstInstallOccurrence to find the anchor for the cleanup violation.
func packageMatchesTool(normalized, tool string) bool {
	return strings.EqualFold(shell.StripPackageVersion(normalized), tool)
}

// init registers the rule with the default registry.
func init() {
	rules.Register(NewDL4001Rule())
}
