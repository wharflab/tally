package js

import (
	"encoding/json/v2"
	"fmt"
	"path"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/wharflab/tally/internal/facts"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/rules/runcheck"
	"github.com/wharflab/tally/internal/runmount"
	"github.com/wharflab/tally/internal/semantic"
	"github.com/wharflab/tally/internal/shell"
	"github.com/wharflab/tally/internal/sourcemap"
)

// NodeGypCacheMountsRuleCode is the full rule code.
const NodeGypCacheMountsRuleCode = rules.TallyRulePrefix + "js/node-gyp-cache-mounts"

const (
	defaultNpmCachePath           = "/root/.npm"
	defaultPnpmStorePath          = "/root/.pnpm-store"
	defaultYarnCachePath          = "/usr/local/share/.cache/yarn"
	defaultNodeGypDevDir          = "/root/.cache/node-gyp"
	preferPackageCacheMountsCode  = rules.TallyRulePrefix + "prefer-package-cache-mounts"
	nodeGypDevDirEnvAssignmentKey = "NPM_CONFIG_DEVDIR"
)

var nativeToolchainPackages = map[string]bool{
	"build-base":      true,
	"build-essential": true,
	"g++":             true,
	"gcc":             true,
	"make":            true,
	"python3":         true,
}

var nativePackageNames = []string{
	"better-sqlite3",
	"bcrypt",
	"canvas",
	"grpc",
	"isolated-vm",
	"node-rdkafka",
	"sharp",
	"sqlite3",
}

type packageManifest struct {
	Dependencies         map[string]string `json:"dependencies"`
	DevDependencies      map[string]string `json:"devDependencies"`
	OptionalDependencies map[string]string `json:"optionalDependencies"`
	PeerDependencies     map[string]string `json:"peerDependencies"`
}

// NodeGypCacheMountsRule suggests BuildKit caches for native Node addon builds.
type NodeGypCacheMountsRule struct{}

// NewNodeGypCacheMountsRule creates the rule.
func NewNodeGypCacheMountsRule() *NodeGypCacheMountsRule {
	return &NodeGypCacheMountsRule{}
}

// Metadata returns the rule metadata.
func (r *NodeGypCacheMountsRule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            NodeGypCacheMountsRuleCode,
		Name:            "Cache node-gyp native addon builds",
		Description:     "Native Node addon installs should cache node-gyp header downloads with BuildKit cache mounts",
		DocURL:          rules.TallyDocURL(NodeGypCacheMountsRuleCode),
		DefaultSeverity: rules.SeverityInfo,
		Category:        "performance",
		IsExperimental:  false,
		FixPriority:     91, // After package cache mounts (90), before structural rewrites.
	}
}

// Check runs the rule.
func (r *NodeGypCacheMountsRule) Check(input rules.LintInput) []rules.Violation {
	if input.Facts == nil {
		return nil
	}

	meta := r.Metadata()
	sm := input.SourceMap()
	preferPackageCacheMountsEnabled := input.IsRuleEnabled(preferPackageCacheMountsCode)

	var violations []rules.Violation
	for _, stageFacts := range input.Facts.Stages() {
		violations = append(
			violations,
			r.checkStage(input.File, stageFacts, meta, sm, preferPackageCacheMountsEnabled)...,
		)
	}
	return violations
}

func (r *NodeGypCacheMountsRule) checkStage(
	file string,
	stageFacts *facts.StageFacts,
	meta rules.RuleMetadata,
	sm *sourcemap.SourceMap,
	preferPackageCacheMountsEnabled bool,
) []rules.Violation {
	if stageFacts == nil || stageFacts.BaseImageOS == semantic.BaseImageOSWindows {
		return nil
	}
	if stageHasCustomNativeBuildCache(stageFacts) {
		return nil
	}

	signal, ok := stageNativeAddonSignal(stageFacts)
	if !ok {
		return nil
	}

	var violations []rules.Violation
	for _, runFacts := range stageFacts.Runs {
		manager, ok := jsInstallOrRebuildManager(runFacts)
		if !ok {
			continue
		}

		devdir, devdirConfigured := configuredNodeGypDevDir(runFacts.Env, runFacts.Workdir)
		existing := runmount.GetMounts(runFacts.Run)
		if hasNodeGypHeaderCache(existing, devdir) {
			continue
		}

		mounts := requiredNativeBuildMounts(
			manager,
			runFacts,
			existing,
			devdir,
			preferPackageCacheMountsEnabled,
		)
		if len(mounts) == 0 {
			continue
		}

		v := buildNodeGypViolation(file, runFacts, meta, signal, mounts)
		if fix := buildNodeGypCacheFix(
			file,
			runFacts,
			meta,
			sm,
			mounts,
			devdir,
			!devdirConfigured && !runScriptHasNodeGypDevDir(runFacts.CommandScript),
		); fix != nil {
			v = v.WithSuggestedFix(fix)
		}
		violations = append(violations, v)
	}
	return violations
}

func stageNativeAddonSignal(stageFacts *facts.StageFacts) (string, bool) {
	for _, runFacts := range stageFacts.Runs {
		if runFacts == nil {
			continue
		}
		for _, install := range runFacts.InstallCommands {
			if !isOSPackageInstallManager(install.Manager) {
				continue
			}
			for _, pkg := range install.Packages {
				name := strings.ToLower(shell.StripPackageVersion(pkg.Normalized))
				if nativeToolchainPackages[name] {
					return "stage installs native build toolchain package " + name, true
				}
			}
		}
		for _, cmd := range runFacts.CommandInfos {
			if isNativeAddonCommand(cmd) {
				return "stage runs " + cmd.Name, true
			}
		}
		if scriptMentionsNativeAddon(runFacts.CommandScript) {
			return "stage mentions native addon build files or helpers", true
		}
	}

	if dep, ok := stagePackageJSONNativeDependency(stageFacts); ok {
		return "package.json declares native addon dependency " + dep, true
	}
	return "", false
}

func isOSPackageInstallManager(manager string) bool {
	switch manager {
	case "apt", "apt-get", "apk", "dnf", "microdnf", "yum", "zypper":
		return true
	default:
		return false
	}
}

func isNativeAddonCommand(cmd shell.CommandInfo) bool {
	switch cmd.Name {
	case "node-gyp", "node-pre-gyp", "prebuild-install":
		return true
	case "npm", "pnpm", "yarn":
		return cmd.Subcommand == "rebuild"
	default:
		return false
	}
}

func scriptMentionsNativeAddon(script string) bool {
	script = strings.ToLower(script)
	return strings.Contains(script, "binding.gyp") ||
		strings.Contains(script, "node-gyp") ||
		strings.Contains(script, "node-pre-gyp") ||
		strings.Contains(script, "prebuild-install")
}

func stagePackageJSONNativeDependency(stageFacts *facts.StageFacts) (string, bool) {
	var dependency string
	stageFacts.ScanObservableFiles(func(file *facts.ObservableFile, pathView facts.ObservablePathView) bool {
		if pathView.Base() != "package.json" {
			return true
		}
		content, ok := file.Content()
		if !ok {
			return true
		}
		dep, ok := packageJSONNativeDependency(content)
		if ok {
			dependency = dep
			return false
		}
		return true
	})
	return dependency, dependency != ""
}

func packageJSONNativeDependency(content string) (string, bool) {
	var manifest packageManifest
	if err := json.Unmarshal([]byte(content), &manifest); err != nil {
		return "", false
	}

	for _, name := range nativePackageNames {
		if manifest.Dependencies[name] != "" ||
			manifest.DevDependencies[name] != "" ||
			manifest.OptionalDependencies[name] != "" ||
			manifest.PeerDependencies[name] != "" {
			return name, true
		}
	}
	return "", false
}

func stageHasCustomNativeBuildCache(stageFacts *facts.StageFacts) bool {
	for _, runFacts := range stageFacts.Runs {
		if runFacts == nil {
			continue
		}
		if runFacts.Env.Values["CCACHE_DIR"] != "" || runFacts.Env.Values["ccache_dir"] != "" {
			return true
		}
		for _, mount := range runmount.GetMounts(runFacts.Run) {
			if mount == nil || mount.Type != instructions.MountTypeCache {
				continue
			}
			key := strings.ToLower(mount.Target + " " + mount.CacheID)
			if strings.Contains(key, "ccache") || strings.Contains(key, "prebuild") {
				return true
			}
		}
	}
	return false
}

func jsInstallOrRebuildManager(runFacts *facts.RunFacts) (string, bool) {
	if runFacts == nil || !runFacts.UsesShell || !runFacts.Shell.HasParser || runFacts.CommandScript == "" {
		return "", false
	}

	for _, cmd := range runFacts.CommandInfos {
		if manager, ok := jsInstallManager(cmd); ok {
			return manager, true
		}
	}
	return "", false
}

func jsInstallManager(cmd shell.CommandInfo) (string, bool) {
	switch cmd.Name {
	case "npm":
		if cmd.HasAnyArg("ci", "install", "i", "rebuild") {
			return "npm", true
		}
	case "pnpm":
		if cmd.HasAnyArg("install", "i", "rebuild") {
			return "pnpm", true
		}
	case "yarn":
		if cmd.HasAnyArg("install", "add", "rebuild") { //nolint:customlint // "add" is a package manager subcommand, not Dockerfile ADD.
			return "yarn", true
		}
	}
	return "", false
}

func configuredNodeGypDevDir(env facts.EnvFacts, workdir string) (string, bool) {
	for key, value := range env.Values {
		if !isNodeGypDevDirEnvKey(key) {
			continue
		}
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		if value == "" || strings.Contains(value, "$") {
			continue
		}
		if !path.IsAbs(value) {
			value = path.Join(workdir, value)
		}
		return path.Clean(value), true
	}
	return defaultNodeGypDevDir, false
}

func isNodeGypDevDirEnvKey(key string) bool {
	return key == "npm_package_config_node_gyp_devdir" ||
		strings.EqualFold(key, "NPM_CONFIG_DEVDIR") ||
		strings.EqualFold(key, "npm_config_devdir")
}

func runScriptHasNodeGypDevDir(script string) bool {
	script = strings.ToLower(script)
	return strings.Contains(script, "npm_package_config_node_gyp_devdir=") ||
		strings.Contains(script, "npm_config_devdir=")
}

func hasNodeGypHeaderCache(existing []*instructions.Mount, devdir string) bool {
	for _, mount := range existing {
		if mount == nil || mount.Type != instructions.MountTypeCache {
			continue
		}
		target := path.Clean(mount.Target)
		key := strings.ToLower(target + " " + mount.CacheID)
		if target == devdir || strings.Contains(key, "node-gyp") {
			return true
		}
	}
	return false
}

func requiredNativeBuildMounts(
	manager string,
	runFacts *facts.RunFacts,
	existing []*instructions.Mount,
	devdir string,
	preferPackageCacheMountsEnabled bool,
) []*instructions.Mount {
	var mounts []*instructions.Mount
	if !preferPackageCacheMountsEnabled {
		if mount := packageManagerCacheMount(manager, runFacts.CachePathOverrides, existing); mount != nil {
			mounts = append(mounts, mount)
		}
	}

	if !hasCacheMountTarget(existing, devdir) {
		mounts = append(mounts, &instructions.Mount{
			Type:         instructions.MountTypeCache,
			Target:       devdir,
			CacheID:      "node-gyp",
			CacheSharing: instructions.MountSharingLocked,
		})
	}
	if !hasMountTarget(existing, instructions.MountTypeTmpfs, "/tmp") {
		mounts = append(mounts, &instructions.Mount{
			Type:   instructions.MountTypeTmpfs,
			Target: "/tmp",
		})
	}
	return mounts
}

func packageManagerCacheMount(
	manager string,
	overrides map[string]string,
	existing []*instructions.Mount,
) *instructions.Mount {
	target, id, ok := packageManagerCacheTarget(manager, overrides)
	if !ok || hasCacheMountTarget(existing, target) {
		return nil
	}
	return &instructions.Mount{
		Type:    instructions.MountTypeCache,
		Target:  target,
		CacheID: id,
	}
}

func packageManagerCacheTarget(manager string, overrides map[string]string) (target, id string, ok bool) {
	switch manager {
	case "npm":
		target = defaultNpmCachePath
	case "pnpm":
		target = defaultPnpmStorePath
	case "yarn":
		target = defaultYarnCachePath
	default:
		return "", "", false
	}
	if override := overrides[manager]; override != "" {
		target = override
	}
	return target, manager, true
}

func hasCacheMountTarget(existing []*instructions.Mount, target string) bool {
	return hasMountTarget(existing, instructions.MountTypeCache, target)
}

func hasMountTarget(existing []*instructions.Mount, mountType instructions.MountType, target string) bool {
	target = path.Clean(target)
	for _, mount := range existing {
		if mount != nil && mount.Type == mountType && path.Clean(mount.Target) == target {
			return true
		}
	}
	return false
}

func buildNodeGypViolation(
	file string,
	runFacts *facts.RunFacts,
	meta rules.RuleMetadata,
	signal string,
	mounts []*instructions.Mount,
) rules.Violation {
	mountDescriptions := make([]string, 0, len(mounts))
	for _, mount := range mounts {
		mountDescriptions = append(mountDescriptions, runmount.FormatMount(mount))
	}

	v := rules.NewViolation(
		rules.NewLocationFromRanges(file, runFacts.Run.Location()),
		meta.Code,
		"native Node addon builds should cache node-gyp headers",
		meta.DefaultSeverity,
	).WithDocURL(meta.DocURL).WithDetail(
		fmt.Sprintf("%s; add %s", signal, strings.Join(mountDescriptions, ", ")),
	)
	v.StageIndex = runFacts.StageIndex
	return v
}

func buildNodeGypCacheFix(
	file string,
	runFacts *facts.RunFacts,
	meta rules.RuleMetadata,
	sm *sourcemap.SourceMap,
	mounts []*instructions.Mount,
	devdir string,
	addDevDirEnv bool,
) *rules.SuggestedFix {
	if runFacts == nil || runFacts.Run == nil || len(mounts) == 0 {
		return nil
	}
	runLoc := runFacts.Run.Location()
	if len(runLoc) == 0 {
		return nil
	}

	insertCol := runmount.RunKeywordEndColumn(runLoc, sm)
	mountEdit := rules.TextEdit{
		Location: rules.NewRangeLocation(file, runLoc[0].Start.Line, insertCol, runLoc[0].Start.Line, insertCol),
		NewText:  runmount.FormatMounts(mounts) + " ",
	}
	edits := []rules.TextEdit{mountEdit}

	if addDevDirEnv {
		if edit, ok := buildNodeGypDevDirEnvEdit(file, runFacts.Run, runFacts.Shell.Variant, sm, devdir); ok {
			if edit.Location == mountEdit.Location {
				edits[0].NewText += edit.NewText
			} else {
				edits = append(edits, edit)
			}
		}
	}

	return &rules.SuggestedFix{
		Description: "Add node-gyp native build cache mount(s)",
		Safety:      rules.FixSuggestion,
		Priority:    meta.FixPriority,
		Edits:       edits,
	}
}

func buildNodeGypDevDirEnvEdit(
	file string,
	run *instructions.RunCommand,
	shellVariant shell.Variant,
	sm *sourcemap.SourceMap,
	devdir string,
) (rules.TextEdit, bool) {
	if run == nil || sm == nil || !run.PrependShell || len(run.Files) > 0 {
		return rules.TextEdit{}, false
	}

	cmds, runStartLine := runcheck.FindCommands(run, shellVariant, sm, "npm", "pnpm", "yarn")
	if runStartLine == 0 {
		return rules.TextEdit{}, false
	}
	for _, cmd := range cmds {
		if cmd.SourceKind != shell.CommandSourceKindDirect {
			continue
		}
		if _, ok := jsInstallManager(cmd); !ok {
			continue
		}

		editLine := runStartLine + cmd.Line
		lineIdx := editLine - 1
		if lineIdx < 0 || lineIdx >= sm.LineCount() || cmd.StartCol < 0 || cmd.StartCol > len(sm.Line(lineIdx)) {
			return rules.TextEdit{}, false
		}
		return rules.TextEdit{
			Location: rules.NewRangeLocation(file, editLine, cmd.StartCol, editLine, cmd.StartCol),
			NewText:  nodeGypDevDirEnvAssignmentKey + "=" + devdir + " ",
		}, true
	}
	return rules.TextEdit{}, false
}

func init() {
	rules.Register(NewNodeGypCacheMountsRule())
}
