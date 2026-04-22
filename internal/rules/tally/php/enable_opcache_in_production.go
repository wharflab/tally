package php

import (
	"path"
	"slices"
	"strings"

	"github.com/distribution/reference"
	"github.com/moby/buildkit/frontend/dockerfile/instructions"
	"gopkg.in/ini.v1"

	"github.com/wharflab/tally/internal/facts"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/semantic"
	"github.com/wharflab/tally/internal/shell"
	"github.com/wharflab/tally/internal/sourcemap"
)

// EnableOpcacheInProductionRuleCode is the full rule code.
const EnableOpcacheInProductionRuleCode = rules.TallyRulePrefix + "php/enable-opcache-in-production"

// sortPackagesRuleCode identifies the tally/sort-packages rule. Referenced
// here (rather than imported from the parent tally package) to keep
// internal/rules/tally/php free of an upward dependency on its parent.
const sortPackagesRuleCode = rules.TallyRulePrefix + "sort-packages"

// EnableOpcacheInProductionRule flags production PHP web runtime images that
// do not enable OPcache.
type EnableOpcacheInProductionRule struct{}

// NewEnableOpcacheInProductionRule creates the rule.
func NewEnableOpcacheInProductionRule() *EnableOpcacheInProductionRule {
	return &EnableOpcacheInProductionRule{}
}

// Metadata returns the rule metadata.
func (r *EnableOpcacheInProductionRule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            EnableOpcacheInProductionRuleCode,
		Name:            "Enable OPcache in production PHP images",
		Description:     "Production PHP web runtime images should install and enable OPcache",
		DocURL:          rules.TallyDocURL(EnableOpcacheInProductionRuleCode),
		DefaultSeverity: rules.SeverityInfo,
		Category:        "performance",
		FixPriority:     88, //nolint:mnd // stable priority contract, consistent with companion PHP rules
	}
}

// Check runs the rule.
func (r *EnableOpcacheInProductionRule) Check(input rules.LintInput) []rules.Violation {
	meta := r.Metadata()

	if len(input.Stages) == 0 {
		return nil
	}
	finalIdx := len(input.Stages) - 1
	stage := input.Stages[finalIdx]
	if stageLooksLikeDev(stage.Name) {
		return nil
	}

	stageFacts := input.Facts.Stage(finalIdx)
	if stageFacts == nil || !stageFacts.IsLast {
		return nil
	}

	info := input.Semantic.StageInfo(finalIdx)
	if !stageLooksLikePHPWebRuntime(info, stage) {
		return nil
	}

	if stageHasOpcacheSignal(stageFacts) {
		return nil
	}

	loc := rules.NewLocationFromRanges(input.File, stage.Location)
	v := rules.NewViolation(
		loc,
		meta.Code,
		meta.Description,
		meta.DefaultSeverity,
	).WithDocURL(meta.DocURL).WithDetail(
		"OPcache stores precompiled PHP bytecode in shared memory, dramatically " +
			"reducing request-time overhead. Install and enable it in this runtime stage, " +
			"e.g. RUN docker-php-ext-install opcache.",
	)
	if fix := buildOpcacheInstallFix(input.File, stage, stageFacts, info, meta.FixPriority); fix != nil {
		v = v.WithSuggestedFix(fix)
	} else if fix := buildOpcachePackageManagerFix(
		input.File,
		stageFacts,
		input.SourceMap(),
		meta.FixPriority,
		input.IsRuleEnabled(sortPackagesRuleCode),
	); fix != nil {
		v = v.WithSuggestedFix(fix)
	}
	return []rules.Violation{v}
}

// buildOpcacheInstallFix returns a heuristic fix that inserts
// `RUN docker-php-ext-install opcache` immediately after the FROM instruction,
// but only for the common-case shape where that command is known to apply:
//
//   - base image is an official php:* tag (docker-php-ext-* helpers ship there)
//   - tag contains "fpm" or "apache" (web runtime variant)
//   - stage is not Windows (docker-php-ext-* is a Linux-only convention)
//
// Derivative images and generic Debian/Ubuntu bases are intentionally skipped:
// the correct command there is distro-specific (apt-get install php-opcache,
// apk add php83-opcache, ...) and picking the right package name requires
// knowing the distro + PHP version.
func buildOpcacheInstallFix(
	file string,
	stage instructions.Stage,
	sf *facts.StageFacts,
	info *semantic.StageInfo,
	priority int,
) *rules.SuggestedFix {
	if sf == nil || sf.BaseImageOS == semantic.BaseImageOSWindows {
		return nil
	}
	if !officialPHPWebRuntimeTag(stageBaseImageRaw(info)) {
		return nil
	}
	if len(stage.Location) == 0 {
		return nil
	}
	insertLine := stage.Location[len(stage.Location)-1].End.Line + 1
	return &rules.SuggestedFix{
		Description: "Add RUN docker-php-ext-install opcache after the final FROM",
		Safety:      rules.FixSuggestion,
		Priority:    priority,
		IsPreferred: true,
		Edits: []rules.TextEdit{{
			Location: rules.NewRangeLocation(file, insertLine, 0, insertLine, 0),
			NewText:  "RUN docker-php-ext-install opcache\n",
		}},
	}
}

func stageBaseImageRaw(info *semantic.StageInfo) string {
	if info == nil || info.BaseImage == nil || info.BaseImage.IsStageRef {
		return ""
	}
	return info.BaseImage.Raw
}

// buildOpcachePackageManagerFix emits an insertion that adds the matching
// OPcache package next to an existing PHP FPM package in the stage's
// install commands. Fires only when exactly one php*-fpm package is present
// across the stage's runs, so the distro/version naming is unambiguous.
//
// Fix priority: 88.
//
// Insertion anchor depends on whether tally/sort-packages is also enabled
// in the current run:
//
//   - sort-packages enabled: anchor is the LAST package in the install
//     command. sort-packages edits replace the first literal with the
//     sorted block and delete the rest; our zero-width insertion at the
//     final literal's EndCol lands at the trailing boundary of that
//     deletion (adjacency, not overlap) and ends up appended to the
//     sorted block, preserving sort order in one --fix pass for the
//     common case (packages alphabetically before "opcache").
//   - sort-packages NOT enabled: anchor is the php*-fpm token itself.
//     The new opcache package is inserted directly after its sibling,
//     keeping related PHP extensions visually grouped.
//
// Both branches produce a single " <pkg>" (one leading space) zero-width
// insertion, so the fix never creates a multi-space run and does not
// conflict with tally/no-multi-spaces. The fix also does not overlap
// with tally/prefer-package-cache-mounts (which operates on RUN flags).
func buildOpcachePackageManagerFix(
	file string,
	sf *facts.StageFacts,
	sm *sourcemap.SourceMap,
	priority int,
	sortPackagesEnabled bool,
) *rules.SuggestedFix {
	if sf == nil || sm == nil {
		return nil
	}

	var (
		targetRun   *facts.RunFacts
		targetIC    shell.InstallCommand
		targetPkg   shell.PackageArg
		opcacheName string
		found       int
	)
	for _, run := range sf.Runs {
		if run == nil {
			continue
		}
		for _, ic := range run.InstallCommands {
			for _, pkg := range ic.Packages {
				derived := derivePHPOpcachePackage(pkg.Normalized, ic.Manager)
				if derived == "" {
					continue
				}
				found++
				if found > 1 {
					return nil
				}
				targetRun = run
				targetIC = ic
				targetPkg = pkg
				opcacheName = derived
			}
		}
	}
	if found != 1 || targetRun == nil || targetRun.Run == nil || !targetRun.UsesShell {
		return nil
	}
	anchor := targetPkg
	if sortPackagesEnabled && len(targetIC.Packages) > 0 {
		anchor = targetIC.Packages[len(targetIC.Packages)-1]
	}
	locs := targetRun.Run.Location()
	if len(locs) == 0 {
		return nil
	}
	runStartLine := locs[0].Start.Line // 1-based
	editLine := runStartLine + anchor.Line
	editCol := anchor.EndCol
	if anchor.Line == 0 {
		lineIdx := runStartLine - 1
		if lineIdx < 0 || lineIdx >= sm.LineCount() {
			return nil
		}
		editCol += shell.DockerfileRunCommandStartCol(sm.Line(lineIdx))
	}
	return &rules.SuggestedFix{
		Description: "Add " + opcacheName + " to the existing " + targetIC.Manager + " " + targetIC.Subcommand,
		Safety:      rules.FixSuggestion,
		Priority:    priority,
		IsPreferred: true,
		Edits: []rules.TextEdit{{
			Location: rules.NewRangeLocation(file, editLine, editCol, editLine, editCol),
			NewText:  " " + opcacheName,
		}},
	}
}

// derivePHPOpcachePackage maps a PHP FPM package name to the matching OPcache
// package name on the same distro family. Returns "" if the package is not a
// recognized php*-fpm form.
//
// Supported forms:
//   - Debian/Ubuntu:    phpMAJOR.MINOR-fpm       -> phpMAJOR.MINOR-opcache
//   - Debian/Ubuntu:    php-fpm (unversioned)    -> php-opcache
//   - Alpine:           phpMAJORMINOR-fpm        -> phpMAJORMINOR-opcache
//   - RHEL/Fedora/UBI:  php-fpm                  -> php-opcache
//   - Remi SCL:         phpMAJORMINOR-php-fpm    -> phpMAJORMINOR-php-opcache
func derivePHPOpcachePackage(pkgName, manager string) string {
	name := strings.ToLower(shell.StripPackageVersion(pkgName))
	switch {
	case name == "php-fpm":
		return "php-opcache"
	case strings.HasSuffix(name, "-php-fpm"):
		// e.g. php83-php-fpm (Remi)
		return strings.TrimSuffix(name, "-php-fpm") + "-php-opcache"
	case strings.HasSuffix(name, "-fpm") && strings.HasPrefix(name, "php"):
		// e.g. php8.3-fpm (Debian), php83-fpm (Alpine)
		return strings.TrimSuffix(name, "-fpm") + "-opcache"
	default:
		_ = manager
		return ""
	}
}

// phpWebRuntimeCommands are command basenames that indicate a long-running
// PHP web runtime when seen as the effective ENTRYPOINT or CMD of the final
// stage. Matched case-insensitively against the last path segment; any
// executable starting with "php-fpm" is also accepted (covers php-fpmNN
// variants used by Debian/Ubuntu packages).
var phpWebRuntimeCommands = map[string]bool{
	"apache2-foreground": true,
	"httpd-foreground":   true,
	"frankenphp":         true,
	"rr":                 true, // roadrunner
}

// phpWebRuntimeDerivativeImages lists non-official image repositories that
// are widely used as PHP web runtime bases. Matched against the familiar
// image name (no domain, no tag).
var phpWebRuntimeDerivativeImages = map[string]bool{
	"serversideup/php":     true,
	"dunglas/frankenphp":   true,
	"bitnami/php-fpm":      true,
	"trafex/php-nginx":     true,
	"webdevops/php-nginx":  true,
	"webdevops/php-apache": true,
}

// stageLooksLikePHPWebRuntime reports whether the final stage behaves like a
// long-running PHP web runtime. True when any of:
//   - base image is official php:* with a tag containing "fpm" or "apache"
//   - base image is a known PHP web runtime derivative
//   - effective ENTRYPOINT or CMD starts a known PHP web server wrapper
func stageLooksLikePHPWebRuntime(info *semantic.StageInfo, stage instructions.Stage) bool {
	if info != nil && info.BaseImage != nil && !info.BaseImage.IsStageRef {
		if baseImageIsPHPWebRuntime(info.BaseImage.Raw) {
			return true
		}
	}
	if slices.ContainsFunc(stageRuntimeCommandNames(stage), isPHPWebRuntimeCommand) {
		return true
	}
	return false
}

func baseImageIsPHPWebRuntime(raw string) bool {
	if officialPHPWebRuntimeTag(raw) {
		return true
	}
	named, err := reference.ParseNormalizedNamed(strings.ToLower(raw))
	if err != nil {
		return false
	}
	return phpWebRuntimeDerivativeImages[reference.FamiliarName(named)]
}

// officialPHPWebRuntimeTag reports whether raw is an official php:* image
// tagged for a web runtime (fpm/apache).
func officialPHPWebRuntimeTag(raw string) bool {
	named, err := reference.ParseNormalizedNamed(strings.ToLower(raw))
	if err != nil {
		return false
	}
	if reference.FamiliarName(named) != "php" {
		return false
	}
	tagged, ok := named.(reference.Tagged)
	if !ok {
		return false
	}
	tag := strings.ToLower(tagged.Tag())
	return strings.Contains(tag, "fpm") || strings.Contains(tag, "apache")
}

// stageRuntimeCommandNames returns the executable names of the last
// ENTRYPOINT and CMD in the stage (both are considered, since a common
// PHP web runtime pattern is ENTRYPOINT ["docker-entrypoint.sh"] +
// CMD ["php-fpm"] — the actual runtime process is php-fpm, started via
// the entrypoint wrapper). For shell-form instructions, parses the script
// to find the first command name. Returns lowercased path basenames; an
// empty slice when no ENTRYPOINT or CMD is present.
func stageRuntimeCommandNames(stage instructions.Stage) []string {
	var lastEntrypoint *instructions.EntrypointCommand
	var lastCmd *instructions.CmdCommand
	for _, c := range stage.Commands {
		switch cc := c.(type) {
		case *instructions.EntrypointCommand:
			lastEntrypoint = cc
		case *instructions.CmdCommand:
			lastCmd = cc
		}
	}

	names := make([]string, 0, 2) //nolint:mnd // up to ENTRYPOINT + CMD
	if lastEntrypoint != nil {
		if name := commandNameFromCmdLine(lastEntrypoint.CmdLine, lastEntrypoint.PrependShell); name != "" {
			names = append(names, name)
		}
	}
	if lastCmd != nil {
		if name := commandNameFromCmdLine(lastCmd.CmdLine, lastCmd.PrependShell); name != "" {
			names = append(names, name)
		}
	}
	return names
}

func commandNameFromCmdLine(cmdLine []string, prependShell bool) string {
	if len(cmdLine) == 0 {
		return ""
	}
	if prependShell {
		names := shell.CommandNamesWithVariant(cmdLine[0], shell.VariantBash)
		if len(names) == 0 {
			return ""
		}
		return strings.ToLower(path.Base(names[0]))
	}
	return strings.ToLower(path.Base(cmdLine[0]))
}

func isPHPWebRuntimeCommand(name string) bool {
	if phpWebRuntimeCommands[name] {
		return true
	}
	// Match php-fpm, php-fpm7.4, php-fpm8.3, php-fpm83, etc.
	return strings.HasPrefix(name, "php-fpm")
}

// stageHasOpcacheSignal reports whether the stage has any signal that OPcache
// is installed, enabled, or configured.
func stageHasOpcacheSignal(sf *facts.StageFacts) bool {
	if sf == nil {
		return false
	}
	if envHasOpcacheSignal(sf.EffectiveEnv) {
		return true
	}
	for _, run := range sf.Runs {
		if run != nil && runHasOpcacheSignal(run) {
			return true
		}
	}
	return slices.ContainsFunc(sf.ObservableFiles, observableFileHasOpcacheSignal)
}

func envHasOpcacheSignal(env facts.EnvFacts) bool {
	for key := range env.Values {
		if strings.HasPrefix(strings.ToUpper(key), "PHP_OPCACHE_") {
			return true
		}
	}
	return false
}

func runHasOpcacheSignal(run *facts.RunFacts) bool {
	if slices.ContainsFunc(run.CommandInfos, commandReferencesOpcacheExt) {
		return true
	}
	return slices.ContainsFunc(run.InstallCommands, installCommandInstallsOpcache)
}

// commandReferencesOpcacheExt detects PHP-specific extension tooling that
// installs, enables, or configures OPcache. General package-manager installs
// are handled separately via facts.RunFacts.InstallCommands.
func commandReferencesOpcacheExt(cmd shell.CommandInfo) bool {
	switch cmd.Name {
	case cmdDockerPHPExtInstall, cmdDockerPHPExtEnable, "docker-php-ext-configure":
		return argsContainOpcache(cmd.Args)
	case cmdPecl:
		return cmd.Subcommand == subcommandInstall && argsContainOpcache(cmd.Args)
	default:
		return false
	}
}

// installCommandInstallsOpcache reports whether a normalized package-manager
// install references an OPcache package (e.g., php-opcache, php8.3-opcache).
func installCommandInstallsOpcache(ic shell.InstallCommand) bool {
	for _, pkg := range ic.Packages {
		name := strings.ToLower(shell.StripPackageVersion(pkg.Normalized))
		if strings.Contains(name, "opcache") {
			return true
		}
	}
	return false
}

// argsContainOpcache checks if any non-flag arg is "opcache" or starts with
// "opcache-" (used by docker-php-ext-* and `pecl install`).
func argsContainOpcache(args []string) bool {
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			continue
		}
		lower := strings.ToLower(arg)
		if lower == "opcache" || strings.HasPrefix(lower, "opcache-") {
			return true
		}
	}
	return false
}

// observableFileHasOpcacheSignal reports whether an image file looks like an
// OPcache ini configuration or contains a directive that enables OPcache.
// Parses the content as an ini file (comments and whitespace are ignored by
// the parser) and inspects keys/values semantically:
//
//   - any section with an opcache.* key (opcache.enable, opcache.memory_consumption, ...)
//   - zend_extension value referencing opcache (opcache, opcache.so)
func observableFileHasOpcacheSignal(f *facts.ObservableFile) bool {
	if f == nil {
		return false
	}
	lowerPath := strings.ToLower(f.Path)
	if strings.Contains(lowerPath, "opcache") && strings.HasSuffix(lowerPath, ".ini") {
		return true
	}
	content, ok := f.Content()
	if !ok || content == "" {
		return false
	}
	cfg, err := ini.LoadSources(ini.LoadOptions{
		Loose:                      true,
		Insensitive:                true,
		IgnoreInlineComment:        true,
		AllowBooleanKeys:           true,
		AllowPythonMultilineValues: false,
	}, []byte(content))
	if err != nil {
		return false
	}
	for _, section := range cfg.Sections() {
		for _, key := range section.Keys() {
			name := strings.ToLower(key.Name())
			if strings.HasPrefix(name, "opcache.") {
				return true
			}
			if name == "zend_extension" && strings.Contains(strings.ToLower(key.Value()), "opcache") {
				return true
			}
		}
	}
	return false
}

func init() {
	rules.Register(NewEnableOpcacheInProductionRule())
}
