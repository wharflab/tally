package js

import (
	"encoding/json/v2"
	"fmt"
	"path"
	"slices"
	"strings"
	"unicode"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"
	"mvdan.cc/sh/v3/syntax"

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
	nodeGypPackageConfigDevDirKey = "npm_package_config_node_gyp_devdir"
	nodeGypLowerDevDirEnvKey      = "npm_config_devdir"
	npmManager                    = "npm"
	pnpmManager                   = "pnpm"
	yarnManager                   = "yarn"
)

var nodeGypDevDirEnvKeyPrecedence = []string{
	nodeGypPackageConfigDevDirKey,
	nodeGypDevDirEnvAssignmentKey,
	nodeGypLowerDevDirEnvKey,
}

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

type nativePackageDependency struct {
	Name    string
	DevOnly bool
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

	stageSignal, hasStageSignal := stageNativeAddonSignal(stageFacts)
	manifestSignal, hasManifestSignal := stagePackageJSONNativeDependency(stageFacts)
	if !hasStageSignal && !hasManifestSignal {
		return nil
	}

	var violations []rules.Violation
	for _, runFacts := range stageFacts.Runs {
		manager, ok := jsInstallOrRebuildManager(runFacts)
		if !ok {
			continue
		}

		signal, ok := nativeAddonSignalForRun(stageSignal, hasStageSignal, manifestSignal, hasManifestSignal, runFacts)
		if !ok {
			continue
		}

		devdirs, devdirConfigured, devdirKnown := nodeGypDevDirsForRun(runFacts)
		if !devdirKnown {
			continue
		}
		existing := runmount.GetMounts(runFacts.Run)
		if hasNodeGypHeaderCaches(existing, devdirs) {
			continue
		}

		mounts := requiredNativeBuildMounts(
			manager,
			runFacts,
			existing,
			devdirs,
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
			devdirs[0],
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

	return "", false
}

func nativeAddonSignalForRun(
	stageSignal string,
	hasStageSignal bool,
	manifestSignal nativePackageDependency,
	hasManifestSignal bool,
	runFacts *facts.RunFacts,
) (string, bool) {
	if hasStageSignal {
		return stageSignal, true
	}
	if !hasManifestSignal {
		return "", false
	}
	if manifestSignal.DevOnly && !runInstallsDevDependencies(runFacts) {
		return "", false
	}
	return "package.json declares native addon dependency " + manifestSignal.Name, true
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
	case npmManager, pnpmManager, yarnManager:
		return cmd.Subcommand == "rebuild" && !jsInstallIgnoresLifecycleScripts(cmd)
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

func stagePackageJSONNativeDependency(stageFacts *facts.StageFacts) (nativePackageDependency, bool) {
	var dependency nativePackageDependency
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
	return dependency, dependency.Name != ""
}

func packageJSONNativeDependency(content string) (nativePackageDependency, bool) {
	var manifest packageManifest
	if err := json.Unmarshal([]byte(content), &manifest); err != nil {
		return nativePackageDependency{}, false
	}

	for _, name := range nativePackageNames {
		if manifest.Dependencies[name] != "" ||
			manifest.OptionalDependencies[name] != "" ||
			manifest.PeerDependencies[name] != "" {
			return nativePackageDependency{Name: name}, true
		}
	}
	for _, name := range nativePackageNames {
		if manifest.DevDependencies[name] != "" {
			return nativePackageDependency{Name: name, DevOnly: true}, true
		}
	}
	return nativePackageDependency{}, false
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
	if runFacts == nil || runFacts.CommandScript == "" {
		return "", false
	}
	if runFacts.UsesShell && !runFacts.Shell.HasParser {
		return "", false
	}

	for _, cmd := range jsInstallCommandInfos(runFacts) {
		if manager, ok := jsInstallManager(cmd); ok {
			return manager, true
		}
	}
	return "", false
}

func jsInstallCommandInfos(runFacts *facts.RunFacts) []shell.CommandInfo {
	if runFacts == nil {
		return nil
	}
	if runFacts.UsesShell {
		return runFacts.CommandInfos
	}
	if cmd, ok := execFormCommandInfo(runFacts.Run); ok {
		return []shell.CommandInfo{cmd}
	}
	return nil
}

func execFormCommandInfo(run *instructions.RunCommand) (shell.CommandInfo, bool) {
	if run == nil || run.PrependShell || len(run.CmdLine) == 0 {
		return shell.CommandInfo{}, false
	}

	name := path.Base(run.CmdLine[0])
	if name == "" {
		return shell.CommandInfo{}, false
	}

	cmd := shell.CommandInfo{Name: name}
	for _, arg := range run.CmdLine[1:] {
		cmd.Args = append(cmd.Args, arg)
		cmd.ArgLiteral = append(cmd.ArgLiteral, true)
		if cmd.Subcommand == "" && !strings.HasPrefix(arg, "-") {
			cmd.Subcommand = arg
		}
	}
	return cmd, true
}

func jsInstallManager(cmd shell.CommandInfo) (string, bool) {
	switch cmd.Name {
	case npmManager:
		if cmd.HasAnyArg("ci", "install", "i", "rebuild") && !jsInstallIgnoresLifecycleScripts(cmd) {
			return npmManager, true
		}
	case pnpmManager:
		if cmd.HasAnyArg("install", "i", "rebuild") && !jsInstallIgnoresLifecycleScripts(cmd) {
			return pnpmManager, true
		}
	case yarnManager:
		hasInstallSubcommand := cmd.HasAnyArg(
			"install",
			"add", //nolint:customlint // Package manager subcommand, not Dockerfile ADD.
			"rebuild",
		)
		if (yarnBareInstall(cmd) || hasInstallSubcommand) && !jsInstallIgnoresLifecycleScripts(cmd) {
			return yarnManager, true
		}
	}
	return "", false
}

func jsInstallIgnoresLifecycleScripts(cmd shell.CommandInfo) bool {
	value, found := commandBoolFlagValue(cmd, "--ignore-scripts")
	return found && value
}

func yarnBareInstall(cmd shell.CommandInfo) bool {
	if cmd.Subcommand != "" {
		return false
	}
	for _, arg := range cmd.Args {
		arg = strings.ToLower(shell.DropQuotes(arg))
		if arg == "-h" || arg == "--help" || arg == "-v" || arg == "--version" ||
			strings.HasPrefix(arg, "--help=") || strings.HasPrefix(arg, "--version=") {
			return false
		}
	}
	return true
}

func runInstallsDevDependencies(runFacts *facts.RunFacts) bool {
	if runFacts == nil {
		return false
	}
	for _, cmd := range jsInstallCommandInfos(runFacts) {
		manager, ok := jsInstallManager(cmd)
		if !ok {
			continue
		}
		if !jsInstallOmitsDevDependencies(manager, cmd) {
			return true
		}
	}
	return false
}

func jsInstallOmitsDevDependencies(manager string, cmd shell.CommandInfo) bool {
	if manager != npmManager && manager != pnpmManager && manager != yarnManager {
		return false
	}
	if commandHasDevDependencyTypeFlag(cmd, "--include") ||
		commandHasAnyFlag(cmd, "--no-production", "--no-prod") ||
		commandHasBoolFlagValue(cmd, false, "--production", "--prod") {
		return false
	}
	return commandHasDevDependencyTypeFlag(cmd, "--omit") ||
		commandHasProductionOnlyFlag(cmd) ||
		commandHasBoolFlagValue(cmd, true, "--production", "--prod") ||
		commandHasAnyFlag(cmd, "--production", "--prod")
}

func commandHasAnyFlag(cmd shell.CommandInfo, flags ...string) bool {
	for _, arg := range cmd.Args {
		arg = shell.DropQuotes(arg)
		if slices.Contains(flags, arg) {
			return true
		}
	}
	return false
}

func commandHasDevDependencyTypeFlag(cmd shell.CommandInfo, flag string) bool {
	for _, value := range commandFlagValues(cmd, flag) {
		for _, part := range strings.FieldsFunc(value, func(r rune) bool {
			return r == ',' || unicode.IsSpace(r)
		}) {
			if strings.EqualFold(part, "dev") {
				return true
			}
		}
	}
	return false
}

func commandHasProductionOnlyFlag(cmd shell.CommandInfo) bool {
	for _, value := range commandFlagValues(cmd, "--only") {
		switch strings.ToLower(value) {
		case "prod", "production":
			return true
		}
	}
	return false
}

func commandHasBoolFlagValue(cmd shell.CommandInfo, want bool, flags ...string) bool {
	for _, flag := range flags {
		for _, value := range commandFlagValues(cmd, flag) {
			if got, ok := parseBoolFlagValue(value); ok && got == want {
				return true
			}
		}
	}
	return false
}

func commandBoolFlagValue(cmd shell.CommandInfo, flag string) (bool, bool) {
	negativeFlag := "--no-" + strings.TrimPrefix(flag, "--")
	value := false
	found := false
	for i, arg := range cmd.Args {
		arg = shell.DropQuotes(arg)
		switch arg {
		case negativeFlag:
			value = false
			found = true
		case flag:
			value = true
			found = true
			if i+1 >= len(cmd.Args) {
				continue
			}
			next := shell.DropQuotes(cmd.Args[i+1])
			if next == "" || strings.HasPrefix(next, "-") {
				continue
			}
			if parsed, ok := parseBoolFlagValue(next); ok {
				value = parsed
			}
		default:
			raw, ok := strings.CutPrefix(arg, flag+"=")
			if !ok {
				continue
			}
			if parsed, ok := parseBoolFlagValue(shell.DropQuotes(raw)); ok {
				value = parsed
				found = true
			}
		}
	}
	return value, found
}

func commandFlagValues(cmd shell.CommandInfo, flag string) []string {
	var values []string
	for i, arg := range cmd.Args {
		arg = shell.DropQuotes(arg)
		if arg == flag {
			if i+1 >= len(cmd.Args) {
				continue
			}
			next := shell.DropQuotes(cmd.Args[i+1])
			if next != "" && !strings.HasPrefix(next, "-") {
				values = append(values, next)
			}
			continue
		}
		if value, ok := strings.CutPrefix(arg, flag+"="); ok {
			values = append(values, shell.DropQuotes(value))
		}
	}
	return values
}

func parseBoolFlagValue(value string) (bool, bool) {
	switch strings.ToLower(value) {
	case "1", "true", "yes":
		return true, true
	case "0", "false", "no":
		return false, true
	default:
		return false, false
	}
}

func nodeGypDevDirsForRun(runFacts *facts.RunFacts) (devdirs []string, configured, known bool) {
	if runFacts == nil {
		return []string{defaultNodeGypDevDir}, false, true
	}
	fallbackDevDir, envConfigured := configuredNodeGypDevDir(runFacts.Env, runFacts.Workdir)
	inlineDevDirs, inlineConfigured, inlineKnown := inlineNodeGypDevDirs(
		runFacts.CommandScript,
		runFacts.Shell.Variant,
		runFacts.Workdir,
		fallbackDevDir,
	)
	if !inlineKnown {
		return nil, false, false
	}
	if len(inlineDevDirs) > 0 {
		return inlineDevDirs, envConfigured || inlineConfigured, true
	}
	return []string{fallbackDevDir}, envConfigured, true
}

func configuredNodeGypDevDir(env facts.EnvFacts, workdir string) (string, bool) {
	for _, key := range nodeGypDevDirEnvKeyPrecedence {
		if devdir, ok := normalizeNodeGypDevDir(env.Values[key], workdir); ok {
			return devdir, true
		}
	}
	return defaultNodeGypDevDir, false
}

func normalizeNodeGypDevDir(value, workdir string) (string, bool) {
	value = shell.DropQuotes(strings.TrimSpace(value))
	if value == "" || strings.Contains(value, "$") || hasControlChar(value) {
		return "", false
	}
	if !path.IsAbs(value) {
		value = path.Join(workdir, value)
	}
	return path.Clean(value), true
}

func hasControlChar(value string) bool {
	for _, r := range value {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}

func inlineNodeGypDevDirs(
	script string,
	variant shell.Variant,
	workdir string,
	fallbackDevDir string,
) (devdirs []string, configured, known bool) {
	if strings.TrimSpace(script) == "" || !variant.SupportsPOSIXShellAST() {
		return nil, false, true
	}

	parser := syntax.NewParser(
		syntax.Variant(nodeGypSyntaxVariant(variant)),
		syntax.KeepComments(false),
	)
	prog, err := parser.Parse(strings.NewReader(script), "")
	if err != nil {
		hasDevDir := runScriptHasNodeGypDevDir(script)
		return nil, hasDevDir, !hasDevDir
	}

	known = true
	var exportedDevDir string
	exportedFound := false
	exportedKnown := true
	syntax.Walk(prog, func(node syntax.Node) bool {
		switch n := node.(type) {
		case *syntax.DeclClause:
			if value, ok, valueKnown := declarationNodeGypDevDir(n, workdir); ok {
				exportedDevDir = value
				exportedFound = true
				exportedKnown = valueKnown
				configured = true
			}
			return true
		case *syntax.CallExpr:
			if value, ok, valueKnown := exportCallNodeGypDevDir(n, workdir); ok {
				exportedDevDir = value
				exportedFound = true
				exportedKnown = valueKnown
				configured = true
				return true
			}
			if !callIsJSInstallManager(n) {
				return true
			}
			if value, ok, valueKnown := callNodeGypDevDir(n, workdir); ok {
				configured = true
				if !valueKnown {
					known = false
					return false
				}
				devdirs = appendUniqueDevDir(devdirs, value)
				return true
			}
			if exportedFound {
				if !exportedKnown {
					known = false
					return false
				}
				devdirs = appendUniqueDevDir(devdirs, exportedDevDir)
				return true
			}
			devdirs = appendUniqueDevDir(devdirs, fallbackDevDir)
			return true
		default:
			return true
		}
	})
	return devdirs, configured, known
}

func appendUniqueDevDir(devdirs []string, devdir string) []string {
	devdir = path.Clean(devdir)
	if slices.Contains(devdirs, devdir) {
		return devdirs
	}
	return append(devdirs, devdir)
}

func nodeGypSyntaxVariant(variant shell.Variant) syntax.LangVariant {
	switch variant {
	case shell.VariantBash:
		return syntax.LangBash
	case shell.VariantPOSIX:
		return syntax.LangPOSIX
	case shell.VariantMksh:
		return syntax.LangMirBSDKorn
	case shell.VariantBats:
		return syntax.LangBats
	case shell.VariantZsh:
		return syntax.LangZsh
	case shell.VariantPowerShell, shell.VariantCmd, shell.VariantUnknown:
		return syntax.LangBash
	default:
		return syntax.LangBash
	}
}

func callIsJSInstallManager(call *syntax.CallExpr) bool {
	if call == nil || len(call.Args) == 0 {
		return false
	}
	name := path.Base(call.Args[0].Lit())
	if name == "" {
		return false
	}

	cmd := shell.CommandInfo{Name: name}
	for _, arg := range call.Args[1:] {
		lit := arg.Lit()
		if lit == "" {
			continue
		}
		cmd.Args = append(cmd.Args, lit)
		if cmd.Subcommand == "" && !strings.HasPrefix(lit, "-") {
			cmd.Subcommand = lit
		}
	}
	_, ok := jsInstallManager(cmd)
	return ok
}

func callNodeGypDevDir(call *syntax.CallExpr, workdir string) (string, bool, bool) {
	return assignmentNodeGypDevDir(call.Assigns, workdir)
}

func declarationNodeGypDevDir(decl *syntax.DeclClause, workdir string) (string, bool, bool) {
	if decl == nil || decl.Variant == nil || decl.Variant.Value != "export" {
		return "", false, true
	}
	return assignmentNodeGypDevDir(decl.Args, workdir)
}

func assignmentNodeGypDevDir(assigns []*syntax.Assign, workdir string) (string, bool, bool) {
	for _, key := range nodeGypDevDirEnvKeyPrecedence {
		for _, assign := range assigns {
			if assign.Name == nil || assign.Name.Value != key {
				continue
			}
			if assign.Append || assign.Naked || assign.Index != nil || assign.Array != nil || assign.Value == nil {
				return "", true, false
			}
			value, ok := staticShellWordValue(assign.Value)
			if !ok {
				return "", true, false
			}
			devdir, ok := normalizeNodeGypDevDir(value, workdir)
			return devdir, true, ok
		}
	}
	return "", false, true
}

func exportCallNodeGypDevDir(call *syntax.CallExpr, workdir string) (string, bool, bool) {
	if call == nil || len(call.Args) == 0 || call.Args[0].Lit() != "export" {
		return "", false, true
	}
	return exportedArgNodeGypDevDir(call.Args[1:], workdir)
}

func exportedArgNodeGypDevDir(args []*syntax.Word, workdir string) (string, bool, bool) {
	for _, key := range nodeGypDevDirEnvKeyPrecedence {
		for _, arg := range args {
			value, ok, known := exportedArgValue(arg, key)
			if !ok {
				if !known {
					return "", true, false
				}
				continue
			}
			devdir, ok := normalizeNodeGypDevDir(value, workdir)
			return devdir, true, ok
		}
	}
	return "", false, true
}

func exportedArgValue(arg *syntax.Word, key string) (string, bool, bool) {
	word, literal := staticShellWordValue(arg)
	if !literal {
		raw, ok := renderShellWord(arg)
		if !ok {
			return "", false, true
		}
		if strings.HasPrefix(raw, key+"=") {
			return "", true, false
		}
		return "", false, true
	}
	gotKey, value, ok := strings.Cut(word, "=")
	if !ok || gotKey != key {
		return "", false, true
	}
	return value, true, true
}

func staticShellWordValue(word *syntax.Word) (string, bool) {
	if word == nil {
		return "", false
	}
	var b strings.Builder
	for _, part := range word.Parts {
		value, ok := staticShellWordPartValue(part)
		if !ok {
			return "", false
		}
		b.WriteString(value)
	}
	return b.String(), true
}

func staticShellWordPartValue(part syntax.WordPart) (string, bool) {
	switch p := part.(type) {
	case *syntax.Lit:
		return p.Value, true
	case *syntax.SglQuoted:
		return p.Value, true
	case *syntax.DblQuoted:
		var b strings.Builder
		for _, nested := range p.Parts {
			value, ok := staticShellWordPartValue(nested)
			if !ok {
				return "", false
			}
			b.WriteString(value)
		}
		return b.String(), true
	default:
		return "", false
	}
}

func renderShellWord(word *syntax.Word) (string, bool) {
	if word == nil {
		return "", false
	}
	var b strings.Builder
	if err := syntax.NewPrinter().Print(&b, word); err != nil {
		return "", false
	}
	return b.String(), true
}

func runScriptHasNodeGypDevDir(script string) bool {
	script = strings.ToLower(script)
	return strings.Contains(script, nodeGypPackageConfigDevDirKey+"=") ||
		strings.Contains(script, strings.ToLower(nodeGypDevDirEnvAssignmentKey)+"=") ||
		strings.Contains(script, nodeGypLowerDevDirEnvKey+"=")
}

// hasNodeGypHeaderCaches reports whether `existing` already mounts caches at
// all resolved node-gyp devdirs. The match is strict on `target` rather than
// loose on the cache id, because a mount like
// `--mount=type=cache,target=/tmp,id=node-gyp` carries a node-gyp-flavored
// id while caching the wrong directory — node-gyp would still write to its
// real devdir and the rule must keep reporting.
func hasNodeGypHeaderCaches(existing []*instructions.Mount, devdirs []string) bool {
	if len(devdirs) == 0 {
		return false
	}
	for _, devdir := range devdirs {
		if !hasCacheMountTarget(existing, devdir) {
			return false
		}
	}
	return true
}

func requiredNativeBuildMounts(
	manager string,
	runFacts *facts.RunFacts,
	existing []*instructions.Mount,
	devdirs []string,
	preferPackageCacheMountsEnabled bool,
) []*instructions.Mount {
	var mounts []*instructions.Mount
	if !preferPackageCacheMountsEnabled {
		if mount := packageManagerCacheMount(manager, runFacts.CachePathOverrides, existing); mount != nil {
			mounts = append(mounts, mount)
		}
	}

	for _, devdir := range devdirs {
		if hasCacheMountTarget(existing, devdir) {
			continue
		}
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
	case npmManager:
		target = defaultNpmCachePath
	case pnpmManager:
		target = defaultPnpmStorePath
	case yarnManager:
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
		if envEdits, ok := buildNodeGypDevDirEnvEdits(file, runFacts.Run, runFacts.Shell.Variant, sm, devdir); ok {
			for _, edit := range envEdits {
				if edit.Location == mountEdit.Location {
					edits[0].NewText += edit.NewText
				} else {
					edits = append(edits, edit)
				}
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

func buildNodeGypDevDirEnvEdits(
	file string,
	run *instructions.RunCommand,
	shellVariant shell.Variant,
	sm *sourcemap.SourceMap,
	devdir string,
) ([]rules.TextEdit, bool) {
	if run == nil || sm == nil || !run.PrependShell || len(run.Files) > 0 {
		return nil, false
	}
	// The inline assignment `NPM_CONFIG_DEVDIR="…" npm install …` is POSIX
	// shell syntax. PowerShell and other non-POSIX shells parse it as a bare
	// token and the resulting RUN fails to execute, so skip the env edits
	// when the active shell variant is not POSIX-compatible.
	if !shellVariant.SupportsPOSIXShellAST() {
		return nil, false
	}
	envAssignment, ok := nodeGypDevDirEnvAssignment(devdir)
	if !ok {
		return nil, false
	}

	cmds, runStartLine := runcheck.FindCommands(run, shellVariant, sm, npmManager, pnpmManager, yarnManager)
	if runStartLine == 0 {
		return nil, false
	}
	var edits []rules.TextEdit
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
			return nil, false
		}
		edits = append(edits, rules.TextEdit{
			Location: rules.NewRangeLocation(file, editLine, cmd.StartCol, editLine, cmd.StartCol),
			NewText:  envAssignment,
		})
	}
	return edits, len(edits) > 0
}

func nodeGypDevDirEnvAssignment(devdir string) (string, bool) {
	devdir = shell.DropQuotes(strings.TrimSpace(devdir))
	if devdir == "" || hasControlChar(devdir) {
		return "", false
	}
	return fmt.Sprintf("%s=%q ", nodeGypDevDirEnvAssignmentKey, devdir), true
}

func init() {
	rules.Register(NewNodeGypCacheMountsRule())
}
