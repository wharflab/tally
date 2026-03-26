package tally

import (
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"
	"github.com/moby/buildkit/frontend/dockerfile/parser"

	"github.com/wharflab/tally/internal/dockerfile"
	"github.com/wharflab/tally/internal/facts"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/runmount"
	"github.com/wharflab/tally/internal/semantic"
	"github.com/wharflab/tally/internal/shell"
	"github.com/wharflab/tally/internal/sourcemap"
)

// PreferPackageCacheMountsRuleCode is the full rule code for prefer-package-cache-mounts.
const PreferPackageCacheMountsRuleCode = rules.TallyRulePrefix + "prefer-package-cache-mounts"

// PreferPackageCacheMountsRule suggests cache mounts for package-manager installs/builds.
type PreferPackageCacheMountsRule struct{}

// NewPreferPackageCacheMountsRule creates a new rule instance.
func NewPreferPackageCacheMountsRule() *PreferPackageCacheMountsRule {
	return &PreferPackageCacheMountsRule{}
}

// Metadata returns the rule metadata.
func (r *PreferPackageCacheMountsRule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            PreferPackageCacheMountsRuleCode,
		Name:            "Prefer package manager cache mounts",
		Description:     "Use BuildKit cache mounts for package manager install/build commands",
		DocURL:          rules.TallyDocURL(PreferPackageCacheMountsRuleCode),
		DefaultSeverity: rules.SeverityOff,
		Category:        "performance",
		IsExperimental:  true,
		FixPriority:     90, // Content rewrite before heredoc structural transforms (99/100+)
	}
}

// Check runs the rule.
func (r *PreferPackageCacheMountsRule) Check(input rules.LintInput) []rules.Violation {
	meta := r.Metadata()
	sm := input.SourceMap()
	fileFacts, _ := input.Facts.(*facts.FileFacts) //nolint:errcheck // nil-safe assertion

	var violations []rules.Violation

	for stageIdx, stage := range input.Stages {
		if fileFacts != nil {
			if stageFacts := fileFacts.Stage(stageIdx); stageFacts != nil {
				violations = append(violations, r.checkStageWithFacts(stageFacts, input.File, meta, sm)...)
				continue
			}
		}

		violations = append(violations, r.checkStageLegacy(input, stageIdx, stage, meta, sm)...)
	}

	return violations
}

func (r *PreferPackageCacheMountsRule) checkStageLegacy(
	input rules.LintInput,
	stageIdx int,
	stage instructions.Stage,
	meta rules.RuleMetadata,
	sm *sourcemap.SourceMap,
) []rules.Violation {
	shellVariant, ok := legacyStageShellVariant(input.Semantic, stageIdx)
	if !ok {
		return nil
	}

	workdir := "/"
	cachePathOverrides := map[string]string{}
	var cacheEnvEntries []cacheEnvEntry
	var violations []rules.Violation

	for _, cmd := range stage.Commands {
		if wd, ok := cmd.(*instructions.WorkdirCommand); ok {
			workdir = facts.ResolveWorkdir(workdir, wd.Path)
			continue
		}

		if env, ok := cmd.(*instructions.EnvCommand); ok {
			resolveCachePathOverrides(env, workdir, cachePathOverrides)
			cacheEnvEntries = collectCacheDisablingEnvVars(env, cacheEnvEntries)
			continue
		}

		run, ok := cmd.(*instructions.RunCommand)
		if !ok || !run.PrependShell {
			continue
		}

		script := getRunScriptFromCmd(run)
		if script == "" {
			continue
		}

		required, cleaners := detectRequiredCacheMounts(script, shellVariant, workdir, cachePathOverrides)
		if len(required) == 0 {
			continue
		}

		existing := runmount.GetMounts(run)
		mergedMounts, mountChanged := mergeCacheMounts(existing, required)
		if !mountChanged {
			continue
		}

		updatedScript, scriptCleaned := removeCacheCleanup(run, script, shellVariant, cleaners)
		runLoc := run.Location()
		if len(runLoc) == 0 {
			continue
		}

		edits, remaining, envCleaned := buildCacheMountEdits(cacheMountEditParams{
			file:            input.File,
			run:             run,
			runLoc:          runLoc,
			sm:              sm,
			shellVariant:    shellVariant,
			existing:        existing,
			merged:          mergedMounts,
			cleanedScript:   updatedScript,
			scriptCleaned:   scriptCleaned,
			cleaners:        cleaners,
			cacheEnvEntries: cacheEnvEntries,
		})
		cacheEnvEntries = remaining

		violations = append(violations, buildViolation(meta, input.File, runLoc, required, scriptCleaned, envCleaned, edits))
	}

	return violations
}

func legacyStageShellVariant(semanticValue any, stageIdx int) (shell.Variant, bool) {
	shellVariant := shell.VariantBash

	sem, _ := semanticValue.(*semantic.Model) //nolint:errcheck // Safe assertion with nil fallback
	if sem == nil {
		return shellVariant, true
	}

	info := sem.StageInfo(stageIdx)
	if info == nil {
		return shellVariant, true
	}
	if info.IsWindows() {
		return 0, false
	}

	shellVariant = info.ShellSetting.Variant
	if !shellVariant.HasParser() {
		return 0, false
	}

	return shellVariant, true
}

func (r *PreferPackageCacheMountsRule) checkStageWithFacts(
	stageFacts *facts.StageFacts,
	file string,
	meta rules.RuleMetadata,
	sm *sourcemap.SourceMap,
) []rules.Violation {
	if stageFacts == nil || stageFacts.BaseImageOS == semantic.BaseImageOSWindows {
		return nil
	}

	consumedCacheEnvEntries := make(cacheEnvEntrySet)
	var cacheEnvEntries []cacheEnvEntry
	var violations []rules.Violation
	for _, runFacts := range stageFacts.Runs {
		if runFacts == nil || !runFacts.UsesShell || runFacts.SourceScript == "" {
			continue
		}
		if !runFacts.Shell.HasParser {
			continue
		}

		required, cleaners := detectRequiredCacheMountsFromCommands(
			runFacts.CommandInfos,
			runFacts.Workdir,
			runFacts.CachePathOverrides,
		)
		if len(required) == 0 {
			continue
		}

		existing := runmount.GetMounts(runFacts.Run)
		mergedMounts, mountChanged := mergeCacheMounts(existing, required)
		if !mountChanged {
			continue
		}

		updatedScript, scriptCleaned := removeCacheCleanup(
			runFacts.Run,
			runFacts.CommandScript,
			runFacts.Shell.Variant,
			cleaners,
		)

		runLoc := runFacts.Run.Location()
		if len(runLoc) == 0 {
			continue
		}

		cacheEnvEntries = mergeCacheEnvEntries(
			cacheEnvEntries,
			cacheEnvEntriesFromFacts(runFacts.CacheDisablingEnv),
			consumedCacheEnvEntries,
		)
		edits, remaining, envCleaned := buildCacheMountEdits(cacheMountEditParams{
			file:            file,
			run:             runFacts.Run,
			runLoc:          runLoc,
			sm:              sm,
			shellVariant:    runFacts.Shell.Variant,
			existing:        existing,
			merged:          mergedMounts,
			cleanedScript:   updatedScript,
			scriptCleaned:   scriptCleaned,
			cleaners:        cleaners,
			cacheEnvEntries: cacheEnvEntries,
		})
		consumedCacheEnvEntries.markConsumed(cacheEnvEntries, remaining)
		cacheEnvEntries = remaining

		v := buildViolation(meta, file, runLoc, required, scriptCleaned, envCleaned, edits)
		violations = append(violations, v)
	}

	return violations
}

// buildCacheMountEdits produces targeted, non-overlapping edits:
//  1. Zero-length insertion right after "RUN " for new --mount=type=cache flags.
//  2. When script cleanup is needed: a replacement from right after "RUN " to end-of-RUN
//     containing existing flags + cleaned script (new mounts come from the insertion).
//  3. ENV removal edits (already at separate line ranges).
type cacheMountEditParams struct {
	file            string
	run             *instructions.RunCommand
	runLoc          []parser.Range
	sm              *sourcemap.SourceMap
	shellVariant    shell.Variant
	existing        []*instructions.Mount
	merged          []*instructions.Mount
	cleanedScript   string
	scriptCleaned   bool
	cleaners        map[cleanupKind]bool
	cacheEnvEntries []cacheEnvEntry
}

// The insertion-based approach lets cache mount additions compose with other rules'
// mount insertions at the same point (e.g., require-secret-mounts) without conflicting.
func buildCacheMountEdits(p cacheMountEditParams) ([]rules.TextEdit, []cacheEnvEntry, bool) {
	envEdits, remaining := consumeEnvRemovalEdits(p.file, p.cleaners, p.cacheEnvEntries)

	existing := p.existing
	merged := p.merged
	run := p.run
	runLoc := p.runLoc

	// Collect only the NEW mounts (not already in existing).
	newMounts := collectNewMounts(existing, merged)

	var edits []rules.TextEdit

	mutated := mountsMutated(existing, merged)

	// Pre-compute cleanup edits for the non-mutated path.
	var cleanupEdits []rules.TextEdit
	if !mutated && p.scriptCleaned {
		cleanupEdits = computeCleanupEdits(p.file, run, runLoc, p.sm, p.shellVariant, p.cleaners)
	}

	// Will a tail rewrite be emitted? If so, the mount insertion must be skipped
	// (the tail rewrite includes mounts via formatRunFlags). Tail rewrites happen
	// when mounts are mutated OR when cleanup falls back to a full rewrite
	// (e.g., heredoc RUNs where targeted cleanup edits are not available).
	needsTailRewrite := mutated || (p.scriptCleaned && len(cleanupEdits) == 0)

	// Edit 1: zero-length insertion for new mount flags right after "RUN ".
	// Skipped when a tail rewrite will handle mounts to avoid overlapping edits.
	if !needsTailRewrite && len(newMounts) > 0 {
		insertLine := runLoc[0].Start.Line
		insertCol := runLoc[0].Start.Character + 4 //nolint:mnd // len("RUN ")

		mountText := runmount.FormatMounts(newMounts) + " "
		edits = append(edits, rules.TextEdit{
			Location: rules.NewRangeLocation(p.file, insertLine, insertCol, insertLine, insertCol),
			NewText:  mountText,
		})
	}

	// Edit 2+: cleanup and/or mount rewrite.
	switch {
	case mutated:
		// Mount flags modified: full tail rewrite with merged mounts + cleaned script.
		edits = append(edits, buildTailRewrite(p, merged)...)

	case len(cleanupEdits) > 0:
		// Targeted cleanup deletions (non-heredoc): compose with other rules' edits.
		edits = append(edits, cleanupEdits...)

	case p.scriptCleaned:
		// Fallback (e.g., heredoc): targeted cleanup unavailable, tail rewrite
		// with merged mounts (includes new) + cleaned script.
		edits = append(edits, buildTailRewrite(p, merged)...)
	}

	edits = append(edits, envEdits...)
	return edits, remaining, len(envEdits) > 0
}

// buildTailRewrite produces a single edit replacing everything after "RUN "
// with formatted mounts + script. Used when targeted edits aren't possible
// (mount mutation, heredoc cleanup fallback).
func buildTailRewrite(p cacheMountEditParams, mounts []*instructions.Mount) []rules.TextEdit {
	startLine := p.runLoc[0].Start.Line
	startCol := p.runLoc[0].Start.Character + 4 //nolint:mnd // len("RUN ")
	endLine, endCol := resolveRunEndPosition(p.runLoc, p.sm, p.run)

	script := getRunScriptFromCmd(p.run)
	if p.scriptCleaned {
		script = p.cleanedScript
	}
	var tailText string
	if flags := formatRunFlags(p.run.FlagsUsed, mounts); flags != "" {
		tailText = flags + " " + script
	} else {
		tailText = script
	}
	return []rules.TextEdit{{
		Location: rules.NewRangeLocation(p.file, startLine, startCol, endLine, endCol),
		NewText:  tailText,
	}}
}

// computeCleanupEdits produces targeted deletion edits for cache cleanup
// commands within a RUN instruction. Instead of replacing the entire script,
// each cleanup command (and its separator) is deleted individually, so
// edits from other rules (e.g., DL3030 -y insertion) don't conflict.
func computeCleanupEdits(
	file string,
	run *instructions.RunCommand,
	runLoc []parser.Range,
	sm *sourcemap.SourceMap,
	variant shell.Variant,
	cleaners map[cleanupKind]bool,
) []rules.TextEdit {
	if len(cleaners) == 0 || len(runLoc) == 0 || len(run.Files) > 0 {
		return nil
	}

	startLine := runLoc[0].Start.Line
	sourceFull, script, scriptIdx := resolveCleanupSource(run, sm)
	if scriptIdx < 0 {
		return nil
	}

	commands := shell.ExtractChainedCommands(script, variant)
	if len(commands) == 0 {
		return nil
	}
	separators := shell.ExtractChainSeparators(script, variant, len(commands))
	spans := computeCommandSpans(script, commands)
	if spans == nil {
		return nil
	}

	ctx := cleanupEditContext{
		file:       file,
		sourceFull: sourceFull,
		startLine:  startLine,
		scriptIdx:  scriptIdx,
		spans:      spans,
		separators: separators,
		variant:    variant,
		cleaners:   cleaners,
	}

	var edits []rules.TextEdit
	for i, cmd := range commands {
		if e := buildCleanupEdit(ctx, i, cmd); e != nil {
			edits = append(edits, *e)
		}
	}
	return edits
}

// resolveCleanupSource extracts the source text and script for a RUN instruction.
// Returns the joined source, the script text, and the byte index of the script
// within the source (-1 if resolution fails).
func resolveCleanupSource(
	run *instructions.RunCommand,
	sm *sourcemap.SourceMap,
) (string, string, int) {
	resolved, ok := dockerfile.ResolveRunSource(run, sm)
	if !ok {
		return "", "", -1
	}
	return resolved.Source, resolved.Script, resolved.ScriptIndex
}

type cmdSpan struct {
	start int // byte offset in script
	end   int // byte offset in script (exclusive)
}

func computeCommandSpans(script string, commands []string) []cmdSpan {
	spans := make([]cmdSpan, len(commands))
	offset := 0
	for i, cmd := range commands {
		idx := strings.Index(script[offset:], cmd)
		if idx < 0 {
			return nil
		}
		spans[i].start = offset + idx
		spans[i].end = spans[i].start + len(cmd)
		offset = spans[i].end
	}
	return spans
}

type cleanupEditContext struct {
	file       string
	sourceFull string
	startLine  int
	scriptIdx  int
	spans      []cmdSpan
	separators []string
	variant    shell.Variant
	cleaners   map[cleanupKind]bool
}

func buildCleanupEdit(ctx cleanupEditContext, i int, cmd string) *rules.TextEdit {
	isCleanup := isCacheCleanupCommand(cmd, ctx.cleaners)
	_, stripped := stripNoCacheFlags(cmd, ctx.variant, ctx.cleaners)

	if !isCleanup && !stripped {
		return nil
	}

	if isCleanup {
		var delStart, delEnd int
		switch {
		case i > 0 && i <= len(ctx.separators):
			delStart = ctx.scriptIdx + ctx.spans[i-1].end
			delEnd = ctx.scriptIdx + ctx.spans[i].end
		case i < len(ctx.separators):
			delStart = ctx.scriptIdx + ctx.spans[i].start
			delEnd = ctx.scriptIdx + ctx.spans[i+1].start
		default:
			return nil
		}
		return sourceRangeEdit(ctx.file, ctx.sourceFull, ctx.startLine, delStart, delEnd, "")
	}

	cleaned, _ := stripNoCacheFlags(cmd, ctx.variant, ctx.cleaners)
	return sourceRangeEdit(
		ctx.file, ctx.sourceFull, ctx.startLine,
		ctx.scriptIdx+ctx.spans[i].start, ctx.scriptIdx+ctx.spans[i].end, cleaned,
	)
}

// sourceRangeEdit creates a TextEdit from byte offsets within a multi-line source string.
// startLine is the 1-based line number of the first line in sourceFull.
func sourceRangeEdit(file, sourceFull string, startLine, byteStart, byteEnd int, newText string) *rules.TextEdit {
	if byteStart < 0 || byteEnd > len(sourceFull) || byteStart >= byteEnd {
		return nil
	}

	// Convert byte offsets to line:col positions.
	sLine, sCol := byteToLineCol(sourceFull, byteStart)
	eLine, eCol := byteToLineCol(sourceFull, byteEnd)

	return &rules.TextEdit{
		Location: rules.NewRangeLocation(file, startLine+sLine, sCol, startLine+eLine, eCol),
		NewText:  newText,
	}
}

// byteToLineCol converts a byte offset in a string to (line, col) where line is 0-based.
func byteToLineCol(s string, offset int) (int, int) {
	line := 0
	lineStart := 0
	for i := range offset {
		if s[i] == '\n' {
			line++
			lineStart = i + 1
		}
	}
	return line, offset - lineStart
}

// collectNewMounts returns mounts in merged that are entirely new (no existing
// mount shares the same type+target). Mounts whose attributes were updated
// in-place (e.g., sharing mode changed) are NOT returned here — they are
// handled by the rewrite path which uses the merged slice.
func collectNewMounts(existing, merged []*instructions.Mount) []*instructions.Mount {
	hasSameTarget := func(m *instructions.Mount) bool {
		for _, e := range existing {
			if e.Type == m.Type && e.Target == m.Target {
				return true
			}
		}
		return false
	}

	var newMounts []*instructions.Mount
	for _, m := range merged {
		if !hasSameTarget(m) {
			newMounts = append(newMounts, m)
		}
	}
	return newMounts
}

// mountsMutated returns true if any existing mount was modified in-place by
// mergeCacheMounts (e.g., sharing mode or id changed for the same target).
func mountsMutated(existing, merged []*instructions.Mount) bool {
	for _, e := range existing {
		for _, m := range merged {
			if e.Type == m.Type && e.Target == m.Target {
				if e.CacheSharing != m.CacheSharing || e.CacheID != m.CacheID {
					return true
				}
			}
		}
	}
	return false
}

func buildViolation(
	meta rules.RuleMetadata,
	file string,
	runLoc []parser.Range,
	required []cacheMountSpec,
	scriptCleaned, envCleaned bool,
	edits []rules.TextEdit,
) rules.Violation {
	fixDescription := "Add package cache mount(s)"
	switch {
	case scriptCleaned && envCleaned:
		fixDescription = "Add package cache mount(s), remove cache cleanup commands, and remove cache-disabling ENV vars"
	case scriptCleaned:
		fixDescription = "Add package cache mount(s) and remove cache cleanup commands"
	case envCleaned:
		fixDescription = "Add package cache mount(s) and remove cache-disabling ENV vars"
	}

	mountDescriptions := describeMounts(required)
	return rules.NewViolation(
		rules.NewLocationFromRanges(file, runLoc),
		meta.Code,
		"use cache mounts for package manager cache directories",
		meta.DefaultSeverity,
	).WithDocURL(meta.DocURL).WithDetail(
		"Detected package install/build command; add cache mount(s): " + strings.Join(mountDescriptions, ", "),
	).WithSuggestedFix(&rules.SuggestedFix{
		Description: fixDescription,
		Safety:      rules.FixSuggestion,
		Priority:    meta.FixPriority,
		Edits:       edits,
	})
}

type cacheMountSpec struct {
	Target  string
	ID      string
	Sharing instructions.ShareMode
}

type cleanupKind string

const (
	cargoOrderPlaceholder = "__cargo_target_order__"
	composerCacheTarget   = "/root/.cache/composer"
	defaultNpmCachePath   = "/root/.npm"
	defaultPnpmStorePath  = "/root/.pnpm-store"
	defaultBunCachePath   = "/root/.bun/install/cache"
	noCacheFlag           = "--no-cache"
	noCacheDirFlag        = "--no-cache-dir"

	cleanupApt      cleanupKind = "apt"
	cleanupApk      cleanupKind = "apk"
	cleanupDnf      cleanupKind = "dnf"
	cleanupYum      cleanupKind = "yum"
	cleanupZypper   cleanupKind = "zypper"
	cleanupYarn     cleanupKind = "yarn"
	cleanupNpm      cleanupKind = "npm"
	cleanupPnpm     cleanupKind = "pnpm"
	cleanupPip      cleanupKind = "pip"
	cleanupBundle   cleanupKind = "bundle"
	cleanupDotnet   cleanupKind = "dotnet"
	cleanupComposer cleanupKind = "composer"
	cleanupUV       cleanupKind = "uv"
	cleanupBun      cleanupKind = "bun"
)

var orderedCacheMounts = []cacheMountSpec{
	{Target: defaultNpmCachePath, ID: "npm"},
	{Target: "/go/pkg/mod", ID: "gomod"},
	{Target: "/root/.cache/go-build", ID: "gobuild"},
	{Target: "/var/cache/apt", ID: "apt", Sharing: instructions.MountSharingLocked},
	{Target: "/var/lib/apt", ID: "aptlib", Sharing: instructions.MountSharingLocked},
	{Target: "/var/cache/apk", ID: "apk", Sharing: instructions.MountSharingLocked},
	{Target: "/var/cache/dnf", ID: "dnf", Sharing: instructions.MountSharingLocked},
	{Target: "/var/cache/yum", ID: "yum", Sharing: instructions.MountSharingLocked},
	{Target: "/var/cache/zypp", ID: "zypper", Sharing: instructions.MountSharingLocked},
	{Target: "/usr/local/share/.cache/yarn", ID: "yarn"},
	{Target: defaultPnpmStorePath, ID: "pnpm"},
	{Target: "/root/.cache/pip", ID: "pip"},
	{Target: "/root/.gem", ID: "gem"},
	{Target: cargoOrderPlaceholder, ID: "cargo-target"},
	{Target: "/usr/local/cargo/git/db", ID: "cargo-git"},
	{Target: "/usr/local/cargo/registry", ID: "cargo-registry"},
	{Target: "/root/.nuget/packages", ID: "nuget"},
	{Target: composerCacheTarget, ID: "composer"},
	{Target: "/root/.cache/uv", ID: "uv"},
	{Target: defaultBunCachePath, ID: "bun"},
}

func detectRequiredCacheMounts(
	script string, variant shell.Variant, workdir string, cachePathOverrides map[string]string,
) ([]cacheMountSpec, map[cleanupKind]bool) {
	return detectRequiredCacheMountsFromCommands(
		shell.FindCommands(
			script,
			variant,
			string(cleanupNpm),
			"go",
			"apt",
			"apt-get",
			string(cleanupApk),
			string(cleanupDnf),
			string(cleanupYum),
			string(cleanupZypper),
			string(cleanupYarn),
			string(cleanupPnpm),
			string(cleanupPip),
			"bundle",
			"cargo",
			"dotnet",
			"composer",
			string(cleanupUV),
			string(cleanupBun),
		),
		workdir,
		cachePathOverrides,
	)
}

func detectRequiredCacheMountsFromCommands(
	cmds []shell.CommandInfo,
	workdir string,
	cachePathOverrides map[string]string,
) ([]cacheMountSpec, map[cleanupKind]bool) {
	requiredByTarget := make(map[string]cacheMountSpec)
	cleaners := make(map[cleanupKind]bool)
	cargoTarget := ""

	for _, cmd := range cmds {
		if addOSPackageManagerCacheMounts(cmd, requiredByTarget, cleaners) {
			continue
		}
		cargoTarget = addLanguagePackageManagerCacheMounts(cmd, workdir, cargoTarget, cachePathOverrides, requiredByTarget, cleaners)
	}

	return orderedRequiredMounts(requiredByTarget, cargoTarget, cachePathOverrides), cleaners
}

func cacheEnvEntriesFromFacts(bindings []facts.EnvBinding) []cacheEnvEntry {
	if len(bindings) == 0 {
		return nil
	}

	entries := make([]cacheEnvEntry, 0, len(bindings))
	for _, binding := range bindings {
		if !facts.CacheDisablingEnvVars[binding.Key] || binding.Command == nil {
			continue
		}
		kind, ok := cleanupKindForCacheDisablingEnvVar(binding.Key)
		if !ok {
			continue
		}
		entries = append(entries, cacheEnvEntry{
			env:  binding.Command,
			key:  binding.Key,
			kind: kind,
		})
	}

	sortCacheEnvEntries(entries)
	return entries
}

type cacheEnvEntrySet map[*instructions.EnvCommand]map[string]bool

func (s cacheEnvEntrySet) add(entry cacheEnvEntry) {
	if s[entry.env] == nil {
		s[entry.env] = make(map[string]bool)
	}
	s[entry.env][entry.key] = true
}

func (s cacheEnvEntrySet) has(entry cacheEnvEntry) bool {
	return s[entry.env] != nil && s[entry.env][entry.key]
}

func (s cacheEnvEntrySet) markConsumed(previous, remaining []cacheEnvEntry) {
	remainingSet := make(cacheEnvEntrySet, len(remaining))
	for _, entry := range remaining {
		remainingSet.add(entry)
	}
	for _, entry := range previous {
		if remainingSet.has(entry) {
			continue
		}
		s.add(entry)
	}
}

func mergeCacheEnvEntries(existing, current []cacheEnvEntry, consumed cacheEnvEntrySet) []cacheEnvEntry {
	if len(current) == 0 {
		return existing
	}
	if len(existing) == 0 {
		merged := make([]cacheEnvEntry, 0, len(current))
		for _, entry := range current {
			if consumed.has(entry) {
				continue
			}
			merged = append(merged, entry)
		}
		sortCacheEnvEntries(merged)
		return merged
	}

	merged := append([]cacheEnvEntry(nil), existing...)
	seen := make(cacheEnvEntrySet, len(existing))
	for _, entry := range existing {
		seen.add(entry)
	}

	for _, entry := range current {
		if consumed.has(entry) || seen.has(entry) {
			continue
		}
		merged = append(merged, entry)
		seen.add(entry)
	}

	sortCacheEnvEntries(merged)
	return merged
}

func sortCacheEnvEntries(entries []cacheEnvEntry) {
	slices.SortFunc(entries, func(a, b cacheEnvEntry) int {
		aLine := 0
		if loc := a.env.Location(); len(loc) > 0 {
			aLine = loc[0].Start.Line
		}
		bLine := 0
		if loc := b.env.Location(); len(loc) > 0 {
			bLine = loc[0].Start.Line
		}
		if aLine != bLine {
			return aLine - bLine
		}
		return strings.Compare(a.key, b.key)
	})
}

func orderedRequiredMounts(
	requiredByTarget map[string]cacheMountSpec,
	cargoTarget string,
	cachePathOverrides map[string]string,
) []cacheMountSpec {
	required := make([]cacheMountSpec, 0, len(requiredByTarget))
	seen := make(map[string]bool, len(requiredByTarget))
	for _, mount := range orderedCacheMounts {
		target := mount.Target
		if target == cargoOrderPlaceholder && cargoTarget != "" {
			target = cargoTarget
		}
		if override, ok := cachePathOverrides[mount.ID]; ok {
			target = override
		}
		if req, ok := requiredByTarget[target]; ok {
			required = append(required, req)
			seen[target] = true
		}
	}

	remainingTargets := make([]string, 0, len(requiredByTarget))
	for target := range requiredByTarget {
		if !seen[target] {
			remainingTargets = append(remainingTargets, target)
		}
	}
	slices.Sort(remainingTargets)
	for _, target := range remainingTargets {
		required = append(required, requiredByTarget[target])
	}

	return required
}

func addRequiredMount(requiredByTarget map[string]cacheMountSpec, mount cacheMountSpec) {
	existing, found := requiredByTarget[mount.Target]
	if !found || (existing.Sharing == "" && mount.Sharing != "") {
		requiredByTarget[mount.Target] = mount
	}
}

func addLanguagePackageManagerCacheMounts(
	cmd shell.CommandInfo,
	workdir, cargoTarget string,
	cachePathOverrides map[string]string,
	requiredByTarget map[string]cacheMountSpec,
	cleaners map[cleanupKind]bool,
) string {
	switch cmd.Name {
	case string(cleanupNpm):
		if cmd.HasAnyArg("install", "ci", "i") {
			addRequiredMount(requiredByTarget, cacheMountSpec{
				Target: resolveCacheTarget(cachePathOverrides, "npm", defaultNpmCachePath), ID: "npm",
			})
			cleaners[cleanupNpm] = true
		}
	case "go":
		if goUsesDependencyCache(cmd) {
			addRequiredMount(requiredByTarget, cacheMountSpec{Target: "/go/pkg/mod", ID: "gomod"})
			addRequiredMount(requiredByTarget, cacheMountSpec{Target: "/root/.cache/go-build", ID: "gobuild"})
		}
	case string(cleanupPip):
		if cmd.HasAnyArg("install") {
			addRequiredMount(requiredByTarget, cacheMountSpec{Target: "/root/.cache/pip", ID: "pip"})
			cleaners[cleanupPip] = true
		}
	case "bundle":
		if cmd.HasAnyArg("install") {
			addRequiredMount(requiredByTarget, cacheMountSpec{Target: "/root/.gem", ID: "gem"})
			cleaners[cleanupBundle] = true
		}
	case string(cleanupYarn):
		if cmd.HasAnyArg("install", "add") { //nolint:customlint // "add" is a package manager subcommand, not Dockerfile ADD
			addRequiredMount(requiredByTarget, cacheMountSpec{Target: "/usr/local/share/.cache/yarn", ID: "yarn"})
			cleaners[cleanupYarn] = true
		}
	case string(cleanupPnpm):
		if cmd.HasAnyArg("install", "add", "i") { //nolint:customlint // "add" is a package manager subcommand, not Dockerfile ADD
			addRequiredMount(requiredByTarget, cacheMountSpec{
				Target: resolveCacheTarget(cachePathOverrides, "pnpm", defaultPnpmStorePath), ID: "pnpm",
			})
			cleaners[cleanupPnpm] = true
		}
	case "cargo":
		if cmd.Subcommand == "build" {
			// Skip deriving target path when WORKDIR still has unresolved shell references.
			if !hasUnresolvedWorkdirReference(workdir) {
				cargoTarget = path.Clean(path.Join(workdir, "target"))
				addRequiredMount(requiredByTarget, cacheMountSpec{Target: cargoTarget, ID: "cargo-target"})
			}
			addRequiredMount(requiredByTarget, cacheMountSpec{Target: "/usr/local/cargo/git/db", ID: "cargo-git"})
			addRequiredMount(requiredByTarget, cacheMountSpec{Target: "/usr/local/cargo/registry", ID: "cargo-registry"})
		}
	case "dotnet":
		if cmd.HasAnyArg("restore") {
			addRequiredMount(requiredByTarget, cacheMountSpec{Target: "/root/.nuget/packages", ID: "nuget"})
			cleaners[cleanupDotnet] = true
		}
	case "composer":
		if cmd.HasAnyArg("install") {
			addRequiredMount(requiredByTarget, cacheMountSpec{Target: composerCacheTarget, ID: "composer"})
			cleaners[cleanupComposer] = true
		}
	case string(cleanupUV):
		if uvUsesCache(cmd) {
			addRequiredMount(requiredByTarget, cacheMountSpec{Target: "/root/.cache/uv", ID: "uv"})
			cleaners[cleanupUV] = true
		}
	case string(cleanupBun):
		if cmd.HasAnyArg("install") {
			addRequiredMount(requiredByTarget, cacheMountSpec{
				Target: resolveCacheTarget(cachePathOverrides, "bun", defaultBunCachePath), ID: "bun",
			})
			cleaners[cleanupBun] = true
		}
	}

	return cargoTarget
}

func addOSPackageManagerCacheMounts(
	cmd shell.CommandInfo,
	requiredByTarget map[string]cacheMountSpec,
	cleaners map[cleanupKind]bool,
) bool {
	switch cmd.Name {
	case "apt", "apt-get":
		if !cmd.HasAnyArg("install", "update", "upgrade", "dist-upgrade", "full-upgrade", "distro-sync") {
			return true
		}
		addRequiredMount(requiredByTarget, cacheMountSpec{Target: "/var/cache/apt", ID: "apt", Sharing: instructions.MountSharingLocked})
		addRequiredMount(requiredByTarget, cacheMountSpec{Target: "/var/lib/apt", ID: "aptlib", Sharing: instructions.MountSharingLocked})
		cleaners[cleanupApt] = true
	case "apk":
		if !cmd.HasAnyArg("add", "update", "upgrade") { //nolint:customlint // "add" is a package manager subcommand, not Dockerfile ADD
			return true
		}
		addRequiredMount(requiredByTarget, cacheMountSpec{Target: "/var/cache/apk", ID: "apk", Sharing: instructions.MountSharingLocked})
		cleaners[cleanupApk] = true
	case "dnf":
		if !cmd.HasAnyArg("install", "update", "upgrade", "distro-sync") {
			return true
		}
		addRequiredMount(requiredByTarget, cacheMountSpec{Target: "/var/cache/dnf", ID: "dnf", Sharing: instructions.MountSharingLocked})
		cleaners[cleanupDnf] = true
	case "yum":
		if !cmd.HasAnyArg("install", "update", "upgrade", "distro-sync") {
			return true
		}
		addRequiredMount(requiredByTarget, cacheMountSpec{Target: "/var/cache/yum", ID: "yum", Sharing: instructions.MountSharingLocked})
		cleaners[cleanupYum] = true
	case "zypper":
		if !cmd.HasAnyArg("install", "in", "update", "up", "patch", "dup", "dist-upgrade") {
			return true
		}
		addRequiredMount(
			requiredByTarget,
			cacheMountSpec{Target: "/var/cache/zypp", ID: "zypper", Sharing: instructions.MountSharingLocked},
		)
		cleaners[cleanupZypper] = true
	default:
		return false
	}

	return true
}

func hasUnresolvedWorkdirReference(workdir string) bool {
	return strings.Contains(workdir, "$")
}

// cleanupKindForCacheDisablingEnvVar maps shared cache-disabling ENV variables
// to the cleanup kind they disable for this rule.
//
// Cross-rule interaction: when a Dockerfile uses the legacy format (e.g., "ENV UV_NO_CACHE 1"),
// buildkit/LegacyKeyValueFormat (priority 91) yields to this rule's fix (priority 90), so the
// ENV deletion runs first and the reformatting fix is harmlessly skipped.
func cleanupKindForCacheDisablingEnvVar(key string) (cleanupKind, bool) {
	switch key {
	case "UV_NO_CACHE":
		return cleanupUV, true
	case "PIP_NO_CACHE_DIR":
		return cleanupPip, true
	default:
		return "", false
	}
}

// cacheEnvEntry tracks a single cache-disabling ENV variable for later removal.
type cacheEnvEntry struct {
	env  *instructions.EnvCommand
	key  string
	kind cleanupKind
}

// collectCacheDisablingEnvVars appends any cache-disabling variables from env to entries.
func collectCacheDisablingEnvVars(env *instructions.EnvCommand, entries []cacheEnvEntry) []cacheEnvEntry {
	for _, kv := range env.Env {
		if !facts.CacheDisablingEnvVars[kv.Key] {
			continue
		}
		if kind, ok := cleanupKindForCacheDisablingEnvVar(kv.Key); ok {
			entries = append(entries, cacheEnvEntry{env: env, key: kv.Key, kind: kind})
		}
	}
	return entries
}

// consumeEnvRemovalEdits builds TextEdits for matching entries and returns the remaining (unconsumed) entries.
func consumeEnvRemovalEdits(file string, cleaners map[cleanupKind]bool, entries []cacheEnvEntry) ([]rules.TextEdit, []cacheEnvEntry) {
	// Partition entries into matched vs remaining.
	var matched []cacheEnvEntry
	var remaining []cacheEnvEntry
	for _, e := range entries {
		if cleaners[e.kind] {
			matched = append(matched, e)
		} else {
			remaining = append(remaining, e)
		}
	}
	if len(matched) == 0 {
		return nil, entries
	}

	// Group matched entries by ENV instruction pointer.
	type envGroup struct {
		env  *instructions.EnvCommand
		keys []string
	}
	var groups []envGroup
	seen := make(map[*instructions.EnvCommand]int)
	for _, e := range matched {
		idx, ok := seen[e.env]
		if !ok {
			idx = len(groups)
			seen[e.env] = idx
			groups = append(groups, envGroup{env: e.env})
		}
		groups[idx].keys = append(groups[idx].keys, e.key)
	}

	var edits []rules.TextEdit
	for _, g := range groups {
		if edit := buildEnvKeyRemovalEdit(file, g.env, g.keys); edit != nil {
			edits = append(edits, *edit)
		}
	}

	return edits, remaining
}

// buildEnvKeyRemovalEdit delegates to the shared rules.BuildEnvKeyRemovalEdit helper.
func buildEnvKeyRemovalEdit(file string, env *instructions.EnvCommand, keysToRemove []string) *rules.TextEdit {
	return rules.BuildEnvKeyRemovalEdit(file, env, keysToRemove)
}

// resolveCachePathOverrides updates overrides if the ENV instruction sets any cache-location variables.
// Relative paths are resolved against the current workdir (same logic as cargo target resolution).
func resolveCachePathOverrides(env *instructions.EnvCommand, workdir string, overrides map[string]string) {
	for _, kv := range env.Env {
		for _, loc := range facts.CacheLocationEnvVars {
			match := kv.Key == loc.EnvKey
			if loc.CaseInsensitive {
				match = strings.EqualFold(kv.Key, loc.EnvKey)
			}
			if !match {
				continue
			}
			val := facts.Unquote(kv.Value)
			if val == "" || strings.Contains(val, "$") {
				continue
			}
			target := path.Clean(val)
			if !path.IsAbs(target) {
				target = path.Clean(path.Join(workdir, target))
			}
			if loc.Suffix != "" {
				target = path.Join(target, loc.Suffix)
			}
			overrides[loc.MountID] = target
		}
	}
}

// resolveCacheTarget returns the overridden cache path for a mount ID, or the default.
func resolveCacheTarget(overrides map[string]string, mountID, defaultTarget string) string {
	if t, ok := overrides[mountID]; ok {
		return t
	}
	return defaultTarget
}

func goUsesDependencyCache(cmd shell.CommandInfo) bool {
	if cmd.Subcommand == "build" {
		return true
	}
	if cmd.Subcommand != "mod" {
		return false
	}
	if len(cmd.Args) < 2 {
		return false
	}
	return cmd.Args[1] == "download"
}

func uvUsesCache(cmd shell.CommandInfo) bool {
	switch cmd.Subcommand {
	case "sync":
		return true
	case "pip", "tool", "python":
		if len(cmd.Args) < 2 {
			return false
		}
		return slices.Contains(cmd.Args[1:], "install")
	default:
		return false
	}
}

func mergeCacheMounts(existing []*instructions.Mount, required []cacheMountSpec) ([]*instructions.Mount, bool) {
	merged := cloneMounts(existing)
	changed := false

	for _, req := range required {
		idx := slices.IndexFunc(merged, func(m *instructions.Mount) bool {
			return m != nil && m.Type == instructions.MountTypeCache && m.Target == req.Target
		})
		if idx >= 0 {
			if req.Sharing != "" && merged[idx].CacheSharing != req.Sharing {
				merged[idx].CacheSharing = req.Sharing
				// Also set ID when we're already modifying the mount.
				if req.ID != "" && merged[idx].CacheID == "" {
					merged[idx].CacheID = req.ID
				}
				changed = true
			}
			continue
		}

		mount := &instructions.Mount{
			Type:    instructions.MountTypeCache,
			Target:  req.Target,
			CacheID: req.ID,
		}
		if req.Sharing != "" {
			mount.CacheSharing = req.Sharing
		}

		merged = append(merged, mount)
		changed = true
	}

	return merged, changed
}

func cloneMounts(mounts []*instructions.Mount) []*instructions.Mount {
	cloned := make([]*instructions.Mount, 0, len(mounts))
	for _, mount := range mounts {
		if mount == nil {
			continue
		}
		copyMount := *mount
		copyMount.UID = cloneUint64Ptr(mount.UID)
		copyMount.GID = cloneUint64Ptr(mount.GID)
		copyMount.Mode = cloneUint64Ptr(mount.Mode)
		cloned = append(cloned, &copyMount)
	}
	return cloned
}

func cloneUint64Ptr(value *uint64) *uint64 {
	if value == nil {
		return nil
	}
	copyValue := *value
	return &copyValue
}

func removeCacheCleanup(
	run *instructions.RunCommand,
	script string,
	variant shell.Variant,
	cleaners map[cleanupKind]bool,
) (string, bool) {
	if len(cleaners) == 0 {
		return script, false
	}

	if len(run.Files) > 0 {
		return removeCacheCleanupFromHeredoc(script, variant, cleaners)
	}

	return removeCacheCleanupFromChain(script, variant, cleaners)
}

func removeCacheCleanupFromChain(script string, variant shell.Variant, cleaners map[cleanupKind]bool) (string, bool) {
	commands := shell.ExtractChainedCommands(script, variant)
	if len(commands) == 0 {
		return script, false
	}
	separators := shell.ExtractChainSeparators(script, variant, len(commands))

	filtered := make([]string, 0, len(commands))
	keptIndexes := make([]int, 0, len(commands))
	changed := false
	for idx, command := range commands {
		if isCacheCleanupCommand(command, cleaners) {
			changed = true
			continue
		}

		updated, stripped := stripNoCacheFlags(command, variant, cleaners)
		if stripped {
			changed = true
		}
		filtered = append(filtered, updated)
		keptIndexes = append(keptIndexes, idx)
	}

	if !changed || len(filtered) == 0 {
		return script, false
	}

	if joined, ok := joinWithOriginalSeparators(filtered, keptIndexes, separators); ok {
		return joined, true
	}

	return strings.Join(filtered, " && "), true
}

func joinWithOriginalSeparators(filtered []string, keptIndexes []int, separators []string) (string, bool) {
	if len(filtered) == 0 {
		return "", false
	}
	if len(filtered) == 1 {
		return filtered[0], true
	}
	if len(separators) == 0 {
		return "", false
	}

	var sb strings.Builder
	sb.WriteString(filtered[0])
	for i := 1; i < len(filtered); i++ {
		prev := keptIndexes[i-1]
		curr := keptIndexes[i]
		if curr == prev+1 && prev >= 0 && prev < len(separators) {
			sb.WriteString(separators[prev])
		} else {
			sb.WriteString(" && ")
		}
		sb.WriteString(filtered[i])
	}

	return sb.String(), true
}

func removeCacheCleanupFromHeredoc(script string, variant shell.Variant, cleaners map[cleanupKind]bool) (string, bool) {
	lines := strings.Split(script, "\n")
	hadTrailingNewline := strings.HasSuffix(script, "\n")
	filtered := make([]string, 0, len(lines))
	changed := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			filtered = append(filtered, line)
			continue
		}

		if isCacheCleanupCommand(trimmed, cleaners) {
			changed = true
			continue
		}

		if strings.Contains(trimmed, "&&") {
			updated, lineChanged := removeCacheCleanupFromChain(trimmed, variant, cleaners)
			if lineChanged {
				indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
				line = indent + updated
				changed = true
			}
		} else {
			updated, lineChanged := stripNoCacheFlags(trimmed, variant, cleaners)
			if lineChanged {
				indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
				line = indent + updated
				changed = true
			}
		}

		filtered = append(filtered, line)
	}

	if !changed {
		return script, false
	}

	updated := strings.Join(filtered, "\n")
	if hadTrailingNewline && !strings.HasSuffix(updated, "\n") {
		updated += "\n"
	}
	if strings.TrimSpace(updated) == "" {
		return script, false
	}

	return updated, true
}

func stripNoCacheFlags(command string, variant shell.Variant, cleaners map[cleanupKind]bool) (string, bool) {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return command, false
	}

	cmds := shell.FindCommands(command, variant, string(cleanupApk), string(cleanupPip), string(cleanupUV), string(cleanupBun))
	if len(cmds) == 0 {
		return command, false
	}

	mode := detectStripNoCacheFlags(cmds, cleaners)
	if mode.isEmpty() {
		return command, false
	}

	filtered := make([]string, 0, len(fields))
	removed := false
	for _, field := range fields {
		if shouldStripNoCacheFlag(field, mode) {
			removed = true
			continue
		}
		filtered = append(filtered, field)
	}

	if !removed || len(filtered) == 0 {
		return command, false
	}

	return strings.Join(filtered, " "), true
}

type stripNoCacheMode struct {
	apkNoCache    bool
	pipNoCacheDir bool
	toolNoCache   bool
}

func (m stripNoCacheMode) isEmpty() bool {
	return !m.apkNoCache && !m.pipNoCacheDir && !m.toolNoCache
}

func detectStripNoCacheFlags(cmds []shell.CommandInfo, cleaners map[cleanupKind]bool) stripNoCacheMode {
	mode := stripNoCacheMode{}

	for _, cmd := range cmds {
		switch cmd.Name {
		case string(cleanupApk):
			//nolint:customlint // "add" is a package manager subcommand, not Dockerfile ADD
			if cleaners[cleanupApk] && cmd.HasAnyArg("add", "update", "upgrade") {
				mode.apkNoCache = true
			}
		case string(cleanupPip):
			if cleaners[cleanupPip] && cmd.HasAnyArg("install") {
				mode.pipNoCacheDir = true
			}
		case string(cleanupUV):
			if cleaners[cleanupUV] && uvUsesCache(cmd) {
				mode.toolNoCache = true
			}
		case string(cleanupBun):
			if cleaners[cleanupBun] && cmd.HasAnyArg("install") {
				mode.toolNoCache = true
			}
		}
	}

	return mode
}

func shouldStripNoCacheFlag(field string, mode stripNoCacheMode) bool {
	if mode.pipNoCacheDir && isNoCacheDirFlag(field) {
		return true
	}
	if (mode.apkNoCache || mode.toolNoCache) && isNoCacheFlag(field) {
		return true
	}
	return false
}

func isNoCacheDirFlag(field string) bool {
	return field == noCacheDirFlag || strings.HasPrefix(field, noCacheDirFlag+"=")
}

func isNoCacheFlag(field string) bool {
	return field == noCacheFlag || strings.HasPrefix(field, noCacheFlag+"=")
}

type cleanupMatcher struct {
	kind cleanupKind
	fn   func(string) bool
}

var cleanupMatchers = []cleanupMatcher{
	{kind: cleanupApt, fn: isAptCleanupCommand},
	{kind: cleanupApk, fn: isApkCleanupCommand},
	{kind: cleanupDnf, fn: isDnfCleanupCommand},
	{kind: cleanupYum, fn: isYumCleanupCommand},
	{kind: cleanupZypper, fn: isZypperCleanupCommand},
	{kind: cleanupNpm, fn: isNpmCleanupCommand},
	{kind: cleanupPip, fn: isPipCleanupCommand},
	{kind: cleanupBundle, fn: isBundleCleanupCommand},
	{kind: cleanupYarn, fn: isYarnCleanupCommand},
	{kind: cleanupDotnet, fn: isDotnetCleanupCommand},
	{kind: cleanupPnpm, fn: isPnpmCleanupCommand},
	{kind: cleanupComposer, fn: isComposerCleanupCommand},
	{kind: cleanupUV, fn: isUVCleanupCommand},
	{kind: cleanupBun, fn: isBunCleanupCommand},
}

func isCacheCleanupCommand(command string, cleaners map[cleanupKind]bool) bool {
	normalized := normalizeCommand(command)

	for _, matcher := range cleanupMatchers {
		if cleaners[matcher.kind] && matcher.fn(normalized) {
			return true
		}
	}

	return false
}

func normalizeCommand(command string) string {
	return strings.ToLower(strings.Join(strings.Fields(command), " "))
}

func isAptListCleanup(command string) bool {
	return isPackageCacheDirCleanup(command, "/var/lib/apt/lists")
}

func isAptCleanupCommand(command string) bool {
	return strings.HasPrefix(command, "apt-get clean") ||
		strings.HasPrefix(command, "apt clean") ||
		isAptListCleanup(command)
}

func isApkCleanupCommand(command string) bool {
	return strings.HasPrefix(command, "apk cache clean") ||
		isPackageCacheDirCleanup(command, "/var/cache/apk")
}

func isDnfCleanupCommand(command string) bool {
	return strings.HasPrefix(command, "dnf clean") ||
		isPackageCacheDirCleanup(command, "/var/cache/dnf")
}

func isYumCleanupCommand(command string) bool {
	return strings.HasPrefix(command, "yum clean") ||
		isPackageCacheDirCleanup(command, "/var/cache/yum")
}

func isZypperCleanupCommand(command string) bool {
	return strings.HasPrefix(command, "zypper clean") ||
		isPackageCacheDirCleanup(command, "/var/cache/zypp")
}

func isNpmCleanupCommand(command string) bool {
	return strings.HasPrefix(command, "npm cache clean")
}

func isPipCleanupCommand(command string) bool {
	return strings.HasPrefix(command, "pip cache purge") ||
		strings.HasPrefix(command, "pip cache remove")
}

func isBundleCleanupCommand(command string) bool {
	return strings.HasPrefix(command, "bundle clean")
}

func isYarnCleanupCommand(command string) bool {
	return strings.HasPrefix(command, "yarn cache clean")
}

func isPnpmCleanupCommand(command string) bool {
	return strings.HasPrefix(command, "pnpm store prune")
}

func isDotnetCleanupCommand(command string) bool {
	return strings.HasPrefix(command, "dotnet nuget locals") &&
		strings.Contains(command, "--clear")
}

func isComposerCleanupCommand(command string) bool {
	return strings.HasPrefix(command, "composer clear-cache") ||
		strings.HasPrefix(command, "composer clearcache")
}

func isUVCleanupCommand(command string) bool {
	return strings.HasPrefix(command, "uv cache clean") ||
		strings.HasPrefix(command, "uv cache prune")
}

func isBunCleanupCommand(command string) bool {
	return strings.HasPrefix(command, "bun pm cache rm") ||
		strings.HasPrefix(command, "bun pm cache clean")
}

func isPackageCacheDirCleanup(command, cacheDir string) bool {
	fields := strings.Fields(command)
	if len(fields) < 3 || fields[0] != "rm" {
		return false
	}

	hasRecursive := false
	hasForce := false
	paths := make([]string, 0, len(fields))

	for _, field := range fields[1:] {
		if strings.HasPrefix(field, "-") {
			if strings.Contains(field, "r") {
				hasRecursive = true
			}
			if strings.Contains(field, "f") {
				hasForce = true
			}
			continue
		}
		paths = append(paths, field)
	}

	if !hasRecursive || !hasForce || len(paths) == 0 {
		return false
	}

	for _, path := range paths {
		if !strings.HasPrefix(path, cacheDir) {
			return false
		}
	}

	return true
}

func formatRunFlags(flagsUsed []string, mounts []*instructions.Mount) string {
	parts := make([]string, 0, len(flagsUsed)+len(mounts))

	for _, flag := range flagsUsed {
		if strings.HasPrefix(flag, "mount") {
			continue
		}
		parts = append(parts, "--"+flag)
	}

	if mountFlags := runmount.FormatMounts(mounts); mountFlags != "" {
		parts = append(parts, mountFlags)
	}

	return strings.Join(parts, " ")
}

func describeMounts(mounts []cacheMountSpec) []string {
	descriptions := make([]string, 0, len(mounts))
	for _, mount := range mounts {
		var attrs []string
		if mount.ID != "" {
			attrs = append(attrs, "id="+mount.ID)
		}
		if mount.Sharing != "" {
			attrs = append(attrs, "sharing="+string(mount.Sharing))
		}
		if len(attrs) == 0 {
			descriptions = append(descriptions, mount.Target)
		} else {
			descriptions = append(descriptions, fmt.Sprintf("%s (%s)", mount.Target, strings.Join(attrs, ", ")))
		}
	}
	return descriptions
}

func init() {
	rules.Register(NewPreferPackageCacheMountsRule())
}
