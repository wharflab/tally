package tally

import (
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/runmount"
	"github.com/wharflab/tally/internal/semantic"
	"github.com/wharflab/tally/internal/shell"
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

	sem, _ := input.Semantic.(*semantic.Model) //nolint:errcheck // Safe assertion with nil fallback

	var violations []rules.Violation

	for stageIdx, stage := range input.Stages {
		shellVariant := shell.VariantBash
		if sem != nil {
			if info := sem.StageInfo(stageIdx); info != nil {
				shellVariant = info.ShellSetting.Variant
				if shellVariant.IsNonPOSIX() {
					continue
				}
			}
		}

		workdir := "/"
		pnpmStorePath := defaultPnpmStorePath
		for _, cmd := range stage.Commands {
			if wd, ok := cmd.(*instructions.WorkdirCommand); ok {
				workdir = resolveWorkdir(workdir, wd.Path)
				continue
			}

			if env, ok := cmd.(*instructions.EnvCommand); ok {
				pnpmStorePath = resolvePnpmStorePath(env, pnpmStorePath)
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

			required, cleaners := detectRequiredCacheMounts(script, shellVariant, workdir, pnpmStorePath)
			if len(required) == 0 {
				continue
			}

			existing := runmount.GetMounts(run)
			mergedMounts, mountChanged := mergeCacheMounts(existing, required)
			if !mountChanged {
				continue
			}

			updatedScript, cleaned := removeCacheCleanup(run, script, shellVariant, cleaners)
			replacement := formatUpdatedRun(run, mergedMounts, updatedScript)
			if replacement == "" {
				continue
			}

			runLoc := run.Location()
			if len(runLoc) == 0 {
				continue
			}

			endLine, endCol := resolveRunEndPosition(runLoc, sm, run)
			fixDescription := "Add package cache mount(s)"
			if cleaned {
				fixDescription = "Add package cache mount(s) and remove cache cleanup commands"
			}

			mountDescriptions := describeMounts(required)
			v := rules.NewViolation(
				rules.NewLocationFromRanges(input.File, runLoc),
				meta.Code,
				"use cache mounts for package manager cache directories",
				meta.DefaultSeverity,
			).WithDocURL(meta.DocURL).WithDetail(
				"Detected package install/build command; add cache mount(s): " + strings.Join(mountDescriptions, ", "),
			).WithSuggestedFix(&rules.SuggestedFix{
				Description: fixDescription,
				Safety:      rules.FixSuggestion,
				Priority:    meta.FixPriority,
				Edits: []rules.TextEdit{{
					Location: rules.NewRangeLocation(
						input.File,
						runLoc[0].Start.Line,
						runLoc[0].Start.Character,
						endLine,
						endCol,
					),
					NewText: replacement,
				}},
			})

			violations = append(violations, v)
		}
	}

	return violations
}

type cacheMountSpec struct {
	Target  string
	ID      string
	Sharing instructions.ShareMode
}

type cleanupKind string

const (
	cargoOrderPlaceholder = "__cargo_target_order__"
	pnpmOrderPlaceholder  = "__pnpm_store_order__"
	composerCacheTarget   = "/root/.cache/composer"
	defaultPnpmStorePath  = "/root/.pnpm-store"
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
	{Target: "/root/.npm", ID: "npm"},
	{Target: "/go/pkg/mod", ID: "gomod"},
	{Target: "/root/.cache/go-build", ID: "gobuild"},
	{Target: "/var/cache/apt", ID: "apt", Sharing: instructions.MountSharingLocked},
	{Target: "/var/lib/apt", ID: "aptlib", Sharing: instructions.MountSharingLocked},
	{Target: "/var/cache/apk", ID: "apk", Sharing: instructions.MountSharingLocked},
	{Target: "/var/cache/dnf", ID: "dnf", Sharing: instructions.MountSharingLocked},
	{Target: "/var/cache/yum", ID: "yum", Sharing: instructions.MountSharingLocked},
	{Target: "/var/cache/zypp", ID: "zypper", Sharing: instructions.MountSharingLocked},
	{Target: "/usr/local/share/.cache/yarn", ID: "yarn"},
	{Target: pnpmOrderPlaceholder, ID: "pnpm"},
	{Target: "/root/.cache/pip", ID: "pip"},
	{Target: "/root/.gem", ID: "gem"},
	{Target: cargoOrderPlaceholder, ID: "cargo-target"},
	{Target: "/usr/local/cargo/git/db", ID: "cargo-git"},
	{Target: "/usr/local/cargo/registry", ID: "cargo-registry"},
	{Target: "/root/.nuget/packages", ID: "nuget"},
	{Target: composerCacheTarget, ID: "composer"},
	{Target: "/root/.cache/uv", ID: "uv"},
	{Target: "/root/.bun/install/cache", ID: "bun"},
}

func detectRequiredCacheMounts(
	script string, variant shell.Variant, workdir, pnpmStorePath string,
) ([]cacheMountSpec, map[cleanupKind]bool) {
	requiredByTarget := make(map[string]cacheMountSpec)
	cleaners := make(map[cleanupKind]bool)
	cargoTarget := ""

	cmds := shell.FindCommands(
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
	)

	for _, cmd := range cmds {
		if addOSPackageManagerCacheMounts(cmd, requiredByTarget, cleaners) {
			continue
		}
		cargoTarget = addLanguagePackageManagerCacheMounts(cmd, workdir, cargoTarget, pnpmStorePath, requiredByTarget, cleaners)
	}

	return orderedRequiredMounts(requiredByTarget, cargoTarget, pnpmStorePath), cleaners
}

func orderedRequiredMounts(requiredByTarget map[string]cacheMountSpec, cargoTarget, pnpmStorePath string) []cacheMountSpec {
	required := make([]cacheMountSpec, 0, len(requiredByTarget))
	seen := make(map[string]bool, len(requiredByTarget))
	for _, mount := range orderedCacheMounts {
		target := mount.Target
		if target == cargoOrderPlaceholder && cargoTarget != "" {
			target = cargoTarget
		}
		if target == pnpmOrderPlaceholder {
			target = pnpmStorePath
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
	workdir, cargoTarget, pnpmStorePath string,
	requiredByTarget map[string]cacheMountSpec,
	cleaners map[cleanupKind]bool,
) string {
	switch cmd.Name {
	case string(cleanupNpm):
		if cmd.HasAnyArg("install", "ci", "i") {
			addRequiredMount(requiredByTarget, cacheMountSpec{Target: "/root/.npm", ID: "npm"})
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
		if cmd.HasAnyArg("install", "add") {
			addRequiredMount(requiredByTarget, cacheMountSpec{Target: "/usr/local/share/.cache/yarn", ID: "yarn"})
			cleaners[cleanupYarn] = true
		}
	case string(cleanupPnpm):
		if cmd.HasAnyArg("install", "add", "i") {
			addRequiredMount(requiredByTarget, cacheMountSpec{Target: pnpmStorePath, ID: "pnpm"})
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
			addRequiredMount(requiredByTarget, cacheMountSpec{Target: "/root/.bun/install/cache", ID: "bun"})
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
		if !cmd.HasAnyArg("add", "update", "upgrade") {
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

func resolveWorkdir(currentWorkdir, nextPath string) string {
	if nextPath == "" {
		return currentWorkdir
	}
	if path.IsAbs(nextPath) {
		return path.Clean(nextPath)
	}
	return path.Clean(path.Join(currentWorkdir, nextPath))
}

func hasUnresolvedWorkdirReference(workdir string) bool {
	return strings.Contains(workdir, "$")
}

// resolvePnpmStorePath updates the pnpm store path if PNPM_HOME is set in the ENV instruction.
func resolvePnpmStorePath(env *instructions.EnvCommand, current string) string {
	for _, kv := range env.Env {
		if kv.Key == "PNPM_HOME" {
			val := unquote(kv.Value)
			if !strings.Contains(val, "$") {
				return path.Join(path.Clean(val), "store")
			}
		}
	}
	return current
}

// unquote strips a single layer of matching double or single quotes.
func unquote(s string) string {
	if len(s) >= 2 && ((s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'')) {
		return s[1 : len(s)-1]
	}
	return s
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
	case "pip", "tool":
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

func formatUpdatedRun(run *instructions.RunCommand, mounts []*instructions.Mount, script string) string {
	var sb strings.Builder
	sb.WriteString("RUN")

	if flags := formatRunFlags(run.FlagsUsed, mounts); flags != "" {
		sb.WriteString(" ")
		sb.WriteString(flags)
	}

	if len(run.Files) > 0 {
		cmdLine := strings.Join(run.CmdLine, " ")
		if cmdLine != "" {
			sb.WriteString(" ")
			sb.WriteString(cmdLine)
		}

		for i, file := range run.Files {
			sb.WriteString("\n")
			content := file.Data
			if i == 0 {
				content = script
			}
			sb.WriteString(content)
			if !strings.HasSuffix(content, "\n") {
				sb.WriteString("\n")
			}
			sb.WriteString(file.Name)
		}

		return sb.String()
	}

	if script != "" {
		sb.WriteString(" ")
		sb.WriteString(script)
	}

	return sb.String()
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
