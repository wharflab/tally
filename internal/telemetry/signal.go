package telemetry

import (
	"encoding/json/v2"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/command"
	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/wharflab/tally/internal/facts"
	"github.com/wharflab/tally/internal/semantic"
	"github.com/wharflab/tally/internal/shell"
)

// SignalKind explains why a tool was detected for a stage.
type SignalKind string

const (
	SignalKindCommand  SignalKind = "command"
	SignalKindInstall  SignalKind = "install"
	SignalKindManifest SignalKind = "manifest"
)

const (
	cmdNpm                = "npm"
	cmdPnpm               = "pnpm"
	cmdStart              = "start"
	cmdBuild              = "build"
	cmdDev                = "dev"
	cmdPreview            = "preview"
	cmdYarn               = "yarn"
	cmdBun                = string(ToolBun)
	cmdBunX               = "bunx"
	cmdWrangler           = string(ToolWrangler)
	cmdNext               = "next"
	cmdNuxi               = "nuxi"
	cmdGatsby             = string(ToolGatsby)
	cmdAstro              = string(ToolAstro)
	cmdTurbo              = "turbo"
	pkgCloudflareWrangler = "@cloudflare/wrangler"
	fileVcpkgExe          = "vcpkg.exe"
	fileBootstrapVcpkgBat = "bootstrap-vcpkg.bat"
	fileBootstrapVcpkgSh  = "bootstrap-vcpkg.sh"
)

// Signal records the earliest stage evidence for one tool.
type Signal struct {
	ToolID  ToolID
	Kind    SignalKind
	Reason  string
	Line    int
	Command instructions.Command
}

// StageSignals contains the deduplicated tool evidence for one stage.
type StageSignals struct {
	byTool map[ToolID]Signal
	anchor *Signal
}

// Empty reports whether any telemetry-target tools were detected.
func (s *StageSignals) Empty() bool {
	if s == nil {
		return true
	}
	return len(s.byTool) == 0
}

// OrderedToolIDs returns detected tools in catalog order.
func (s *StageSignals) OrderedToolIDs() []ToolID {
	if s == nil {
		return nil
	}
	if len(s.byTool) == 0 {
		return nil
	}

	set := make(map[ToolID]bool, len(s.byTool))
	for toolID := range s.byTool {
		set[toolID] = true
	}
	return OrderedToolIDs(set)
}

// Anchor returns the earliest stage signal.
func (s *StageSignals) Anchor() (Signal, bool) {
	if s == nil || s.anchor == nil {
		return Signal{}, false
	}
	return *s.anchor, true
}

func (s *StageSignals) ensureMap() {
	if s.byTool == nil {
		s.byTool = make(map[ToolID]Signal)
	}
}

func (s *StageSignals) addSignal(toolID ToolID, kind SignalKind, reason string, candidate anchorCandidate) {
	if !candidate.valid() {
		return
	}

	s.ensureMap()
	next := Signal{
		ToolID:  toolID,
		Kind:    kind,
		Reason:  reason,
		Line:    candidate.line,
		Command: candidate.command,
	}

	if current, ok := s.byTool[toolID]; ok && current.Line > 0 && current.Line <= next.Line {
		return
	}
	s.byTool[toolID] = next

	if s.anchor == nil || next.Line < s.anchor.Line {
		s.anchor = &next
	}
}

type anchorCandidate struct {
	line    int
	command instructions.Command
}

func (c anchorCandidate) valid() bool {
	return c.line > 0
}

func earlierCandidate(a, b anchorCandidate) anchorCandidate {
	switch {
	case !a.valid():
		return b
	case !b.valid():
		return a
	case b.line < a.line:
		return b
	default:
		return a
	}
}

type packageManifest struct {
	PackageManager       string            `json:"packageManager"`
	Dependencies         map[string]string `json:"dependencies"`
	DevDependencies      map[string]string `json:"devDependencies"`
	OptionalDependencies map[string]string `json:"optionalDependencies"`
	PeerDependencies     map[string]string `json:"peerDependencies"`
}

type stageScanner struct {
	result StageSignals

	stageFacts *facts.StageFacts
	semInfo    *semantic.StageInfo

	commandsByLine map[int]instructions.Command

	pythonActivity     anchorCandidate
	nodeScriptActivity anchorCandidate
	yarnActivity       anchorCandidate
	berryEvidence      anchorCandidate
	corepackEnable     anchorCandidate

	nextManifest   anchorCandidate
	nuxtManifest   anchorCandidate
	gatsbyManifest anchorCandidate
	astroManifest  anchorCandidate
	turboManifest  anchorCandidate
	hfManifest     anchorCandidate
}

// DetectStage scans one stage for telemetry-aware tool usage.
func DetectStage(
	stage instructions.Stage,
	stageFacts *facts.StageFacts,
	semInfo *semantic.StageInfo,
) StageSignals {
	scanner := stageScanner{
		stageFacts:     stageFacts,
		semInfo:        semInfo,
		commandsByLine: indexCommandsByLine(stage.Commands),
	}

	scanner.scanRunFacts()
	scanner.scanObservableFiles()
	scanner.scanBuildContextSources()
	scanner.finalizeManifestSignals()
	scanner.scanStageCommands(stage.Commands)

	return scanner.result
}

func indexCommandsByLine(commands []instructions.Command) map[int]instructions.Command {
	index := make(map[int]instructions.Command, len(commands))
	for _, cmd := range commands {
		loc := cmd.Location()
		if len(loc) == 0 {
			continue
		}
		index[loc[0].Start.Line] = cmd
	}
	return index
}

func (s *stageScanner) scanRunFacts() {
	if s.stageFacts == nil {
		return
	}

	for _, runFacts := range s.stageFacts.Runs {
		if runFacts == nil || runFacts.Run == nil {
			continue
		}

		candidate := candidateFromCommand(runFacts.Run)

		for _, cmd := range runFacts.CommandInfos {
			s.scanCommandInfo(cmd, candidate)
		}
		for _, install := range runFacts.InstallCommands {
			s.scanInstallCommand(install, candidate)
		}
	}
}

func (s *stageScanner) scanCommandInfo(cmd shell.CommandInfo, candidate anchorCandidate) {
	name := strings.ToLower(cmd.Name)
	switch name {
	case "python", "python3", "py", "pip", "pip3", "uv", "hf", "huggingface-cli":
		s.pythonActivity = earlierCandidate(s.pythonActivity, candidate)
	case cmdYarn:
		s.yarnActivity = earlierCandidate(s.yarnActivity, candidate)
	case "corepack":
		if hasAnyArgFold(cmd.Args, "enable") {
			s.corepackEnable = earlierCandidate(s.corepackEnable, candidate)
		}
	case cmdNpm, cmdPnpm, cmdBun:
		if isNodeScriptCommand(cmd) {
			s.nodeScriptActivity = earlierCandidate(s.nodeScriptActivity, candidate)
		}
	}

	switch directToolFromCommandName(name) {
	case ToolBun:
		s.result.addSignal(ToolBun, SignalKindCommand, "stage invokes Bun", candidate)
	case ToolAzureCLI:
		s.result.addSignal(ToolAzureCLI, SignalKindCommand, "stage invokes Azure CLI", candidate)
	case ToolWrangler:
		s.result.addSignal(ToolWrangler, SignalKindCommand, "stage invokes Wrangler", candidate)
	case ToolHuggingFace:
		s.result.addSignal(ToolHuggingFace, SignalKindCommand, "stage invokes the Hugging Face CLI", candidate)
	case ToolYarnBerry:
		// Plain yarn invocations are ambiguous; explicit Berry handling lives below.
	case ToolNextJS:
		s.result.addSignal(ToolNextJS, SignalKindCommand, "stage invokes Next.js", candidate)
	case ToolNuxt:
		s.result.addSignal(ToolNuxt, SignalKindCommand, "stage invokes Nuxt", candidate)
	case ToolGatsby:
		s.result.addSignal(ToolGatsby, SignalKindCommand, "stage invokes Gatsby", candidate)
	case ToolAstro:
		s.result.addSignal(ToolAstro, SignalKindCommand, "stage invokes Astro", candidate)
	case ToolTurborepo:
		s.result.addSignal(ToolTurborepo, SignalKindCommand, "stage invokes Turborepo", candidate)
	case ToolDotNetCLI:
		s.result.addSignal(ToolDotNetCLI, SignalKindCommand, "stage invokes the .NET CLI", candidate)
	case ToolPowerShell:
		s.result.addSignal(ToolPowerShell, SignalKindCommand, "stage invokes PowerShell", candidate)
	case ToolVcpkg:
		s.result.addSignal(ToolVcpkg, SignalKindCommand, "stage invokes vcpkg", candidate)
	case ToolHomebrew:
		s.result.addSignal(ToolHomebrew, SignalKindCommand, "stage invokes Homebrew", candidate)
	}

	if isPythonModuleCommand(cmd, "huggingface_hub") {
		s.result.addSignal(
			ToolHuggingFace,
			SignalKindCommand,
			"stage runs python -m huggingface_hub",
			candidate,
		)
	}

	if packageName, ok := execPackageFromCommand(cmd); ok {
		if toolID, reason, ok := toolFromExecPackage(packageName); ok {
			s.result.addSignal(toolID, SignalKindCommand, reason, candidate)
		}
	}

	if isExplicitBerryCommand(cmd) {
		s.berryEvidence = earlierCandidate(s.berryEvidence, candidate)
		s.yarnActivity = earlierCandidate(s.yarnActivity, candidate)
		s.result.addSignal(ToolYarnBerry, SignalKindCommand, "stage configures or invokes Yarn Berry", candidate)
	}
}

func (s *stageScanner) scanInstallCommand(install shell.InstallCommand, candidate anchorCandidate) {
	manager := strings.ToLower(install.Manager)
	switch manager {
	case "pip", "pip3", "uv":
		s.pythonActivity = earlierCandidate(s.pythonActivity, candidate)
	case cmdYarn:
		s.yarnActivity = earlierCandidate(s.yarnActivity, candidate)
	}

	for _, pkg := range install.Packages {
		switch installedToolFromPackage(pkg.Normalized) {
		case ToolBun, ToolYarnBerry, ToolHomebrew:
			// No package-install signal for these tools in v1.
		case ToolAzureCLI:
			s.result.addSignal(ToolAzureCLI, SignalKindInstall, "stage installs Azure CLI", candidate)
		case ToolWrangler:
			s.result.addSignal(ToolWrangler, SignalKindInstall, "stage installs Wrangler", candidate)
		case ToolHuggingFace:
			s.result.addSignal(ToolHuggingFace, SignalKindInstall, "stage installs Hugging Face Python packages", candidate)
			s.pythonActivity = earlierCandidate(s.pythonActivity, candidate)
		case ToolNextJS:
			s.result.addSignal(ToolNextJS, SignalKindInstall, "stage installs Next.js", candidate)
		case ToolNuxt:
			s.result.addSignal(ToolNuxt, SignalKindInstall, "stage installs Nuxt", candidate)
		case ToolGatsby:
			s.result.addSignal(ToolGatsby, SignalKindInstall, "stage installs Gatsby", candidate)
		case ToolAstro:
			s.result.addSignal(ToolAstro, SignalKindInstall, "stage installs Astro", candidate)
		case ToolTurborepo:
			s.result.addSignal(ToolTurborepo, SignalKindInstall, "stage installs Turborepo", candidate)
		case ToolDotNetCLI:
			s.result.addSignal(ToolDotNetCLI, SignalKindInstall, "stage installs the .NET SDK", candidate)
		case ToolPowerShell:
			s.result.addSignal(ToolPowerShell, SignalKindInstall, "stage installs PowerShell", candidate)
		case ToolVcpkg:
			s.result.addSignal(ToolVcpkg, SignalKindInstall, "stage installs vcpkg", candidate)
		}
	}
}

func (s *stageScanner) scanObservableFiles() {
	if s.stageFacts == nil {
		return
	}

	for _, file := range s.stageFacts.ObservableFiles {
		if file == nil || file.Line <= 0 {
			continue
		}

		candidate := anchorCandidate{
			line:    file.Line,
			command: s.commandsByLine[file.Line],
		}
		base := strings.ToLower(path.Base(file.Path))

		switch {
		case base == "package.json":
			if content, ok := file.Content(); ok {
				s.scanPackageJSON(content, candidate)
			}
		case isPythonManifestFile(base):
			if content, ok := file.Content(); ok && contentMentionsHFPackage(content) {
				s.hfManifest = earlierCandidate(s.hfManifest, candidate)
			}
		case base == ".yarnrc.yml":
			s.berryEvidence = earlierCandidate(s.berryEvidence, candidate)
		case base == fileVcpkgExe || base == fileBootstrapVcpkgBat || base == fileBootstrapVcpkgSh:
			s.result.addSignal(ToolVcpkg, SignalKindManifest, "stage copies vcpkg tooling", candidate)
		}
	}
}

func (s *stageScanner) scanBuildContextSources() {
	if s.stageFacts == nil {
		return
	}

	for _, source := range s.stageFacts.BuildContextSources {
		if source == nil || source.Line <= 0 {
			continue
		}

		candidate := anchorCandidate{
			line:    source.Line,
			command: s.commandsByLine[source.Line],
		}
		base := strings.ToLower(path.Base(source.NormalizedSourcePath))

		switch {
		case base == ".yarnrc.yml" || pathHasSegment(source.NormalizedSourcePath, ".yarn"):
			s.berryEvidence = earlierCandidate(s.berryEvidence, candidate)
		case base == fileVcpkgExe || base == fileBootstrapVcpkgBat || base == fileBootstrapVcpkgSh:
			s.result.addSignal(ToolVcpkg, SignalKindManifest, "stage copies vcpkg tooling", candidate)
		}
	}
}

func (s *stageScanner) finalizeManifestSignals() {
	if s.hfManifest.valid() && s.pythonActivity.valid() {
		s.result.addSignal(
			ToolHuggingFace,
			SignalKindManifest,
			"stage copies Python manifests that declare Hugging Face packages",
			earlierCandidate(s.hfManifest, s.pythonActivity),
		)
	}

	if s.berryEvidence.valid() && (s.yarnActivity.valid() || s.corepackEnable.valid()) {
		candidate := s.yarnActivity
		if !candidate.valid() {
			candidate = s.corepackEnable
		}
		s.result.addSignal(
			ToolYarnBerry,
			SignalKindManifest,
			"stage uses Yarn Berry project metadata",
			earlierCandidate(s.berryEvidence, candidate),
		)
	}

	if s.nextManifest.valid() && s.nodeScriptActivity.valid() {
		s.result.addSignal(
			ToolNextJS,
			SignalKindManifest,
			"package.json declares Next.js and the stage runs project scripts",
			earlierCandidate(s.nextManifest, s.nodeScriptActivity),
		)
	}
	if s.nuxtManifest.valid() && s.nodeScriptActivity.valid() {
		s.result.addSignal(
			ToolNuxt,
			SignalKindManifest,
			"package.json declares Nuxt and the stage runs project scripts",
			earlierCandidate(s.nuxtManifest, s.nodeScriptActivity),
		)
	}
	if s.gatsbyManifest.valid() && s.nodeScriptActivity.valid() {
		s.result.addSignal(
			ToolGatsby,
			SignalKindManifest,
			"package.json declares Gatsby and the stage runs project scripts",
			earlierCandidate(s.gatsbyManifest, s.nodeScriptActivity),
		)
	}
	if s.astroManifest.valid() && s.nodeScriptActivity.valid() {
		s.result.addSignal(
			ToolAstro,
			SignalKindManifest,
			"package.json declares Astro and the stage runs project scripts",
			earlierCandidate(s.astroManifest, s.nodeScriptActivity),
		)
	}
	if s.turboManifest.valid() && s.nodeScriptActivity.valid() {
		s.result.addSignal(
			ToolTurborepo,
			SignalKindManifest,
			"package.json declares Turborepo and the stage runs project scripts",
			earlierCandidate(s.turboManifest, s.nodeScriptActivity),
		)
	}
}

func (s *stageScanner) scanStageCommands(commands []instructions.Command) {
	for _, cmd := range commands {
		loc := cmd.Location()
		if len(loc) == 0 {
			continue
		}

		candidate := anchorCandidate{
			line:    loc[0].Start.Line,
			command: cmd,
		}

		switch c := cmd.(type) {
		case *instructions.CmdCommand:
			s.scanDockerCommand(c.CmdLine, c.PrependShell, candidate)
		case *instructions.EntrypointCommand:
			s.scanDockerCommand(c.CmdLine, c.PrependShell, candidate)
		case *instructions.ShellCommand:
			if len(c.Shell) == 0 {
				continue
			}
			switch directToolFromCommandName(strings.ToLower(path.Base(c.Shell[0]))) {
			case ToolPowerShell:
				s.result.addSignal(
					ToolPowerShell,
					SignalKindCommand,
					"stage sets a PowerShell SHELL instruction",
					candidate,
				)
			case ToolBun, ToolAzureCLI, ToolWrangler, ToolHuggingFace, ToolYarnBerry, ToolNextJS, ToolNuxt,
				ToolGatsby, ToolAstro, ToolTurborepo, ToolDotNetCLI, ToolVcpkg, ToolHomebrew:
				// No stage-level SHELL signal for these tools.
			}
		}
	}
}

func (s *stageScanner) scanDockerCommand(cmdLine []string, prependShell bool, candidate anchorCandidate) {
	variant := shell.VariantBash
	if s.semInfo != nil {
		variant = s.semInfo.ShellVariantAtLine(candidate.line)
	}
	if s.stageFacts != nil && s.semInfo == nil {
		variant = s.stageFacts.FinalShell.Variant
	}

	for _, name := range shell.DockerCommandNames(cmdLine, prependShell, variant) {
		switch directToolFromCommandName(strings.ToLower(name)) {
		case ToolBun:
			s.result.addSignal(ToolBun, SignalKindCommand, "stage runs Bun at container start", candidate)
		case ToolAzureCLI, ToolYarnBerry:
			// Not a supported container-start signal in v1.
		case ToolWrangler:
			s.result.addSignal(ToolWrangler, SignalKindCommand, "stage runs Wrangler at container start", candidate)
		case ToolHuggingFace:
			s.result.addSignal(ToolHuggingFace, SignalKindCommand, "stage runs the Hugging Face CLI at container start", candidate)
		case ToolNextJS:
			s.result.addSignal(ToolNextJS, SignalKindCommand, "stage runs Next.js at container start", candidate)
		case ToolNuxt:
			s.result.addSignal(ToolNuxt, SignalKindCommand, "stage runs Nuxt at container start", candidate)
		case ToolGatsby:
			s.result.addSignal(ToolGatsby, SignalKindCommand, "stage runs Gatsby at container start", candidate)
		case ToolAstro:
			s.result.addSignal(ToolAstro, SignalKindCommand, "stage runs Astro at container start", candidate)
		case ToolTurborepo:
			s.result.addSignal(ToolTurborepo, SignalKindCommand, "stage runs Turborepo at container start", candidate)
		case ToolDotNetCLI:
			s.result.addSignal(ToolDotNetCLI, SignalKindCommand, "stage runs the .NET CLI at container start", candidate)
		case ToolPowerShell:
			s.result.addSignal(ToolPowerShell, SignalKindCommand, "stage runs PowerShell at container start", candidate)
		case ToolVcpkg:
			s.result.addSignal(ToolVcpkg, SignalKindCommand, "stage runs vcpkg at container start", candidate)
		case ToolHomebrew:
			s.result.addSignal(ToolHomebrew, SignalKindCommand, "stage runs Homebrew at container start", candidate)
		}
	}

	if !prependShell {
		if packageName, ok := execPackageFromArgv(cmdLine); ok {
			if toolID, reason, ok := toolFromExecPackage(packageName); ok {
				s.result.addSignal(toolID, SignalKindCommand, reason, candidate)
			}
		}
		if isPythonModuleArgv(cmdLine, "huggingface_hub") {
			s.result.addSignal(
				ToolHuggingFace,
				SignalKindCommand,
				"stage runs python -m huggingface_hub at container start",
				candidate,
			)
		}
	}
}

func (s *stageScanner) scanPackageJSON(content string, candidate anchorCandidate) {
	var manifest packageManifest
	if err := json.Unmarshal([]byte(content), &manifest); err != nil {
		return
	}

	if isYarnBerryPackageManager(manifest.PackageManager) {
		s.berryEvidence = earlierCandidate(s.berryEvidence, candidate)
	}

	if hasDependency(manifest, "next") {
		s.nextManifest = earlierCandidate(s.nextManifest, candidate)
	}
	if hasDependency(manifest, "nuxt") {
		s.nuxtManifest = earlierCandidate(s.nuxtManifest, candidate)
	}
	if hasDependency(manifest, "gatsby") {
		s.gatsbyManifest = earlierCandidate(s.gatsbyManifest, candidate)
	}
	if hasDependency(manifest, "astro") {
		s.astroManifest = earlierCandidate(s.astroManifest, candidate)
	}
	if hasDependency(manifest, "turbo") {
		s.turboManifest = earlierCandidate(s.turboManifest, candidate)
	}
}

func candidateFromCommand(cmd instructions.Command) anchorCandidate {
	loc := cmd.Location()
	if len(loc) == 0 {
		return anchorCandidate{}
	}
	return anchorCandidate{
		line:    loc[0].Start.Line,
		command: cmd,
	}
}

func directToolFromCommandName(name string) ToolID {
	switch name {
	case cmdBun, cmdBunX:
		return ToolBun
	case "az":
		return ToolAzureCLI
	case cmdWrangler:
		return ToolWrangler
	case "hf", "huggingface-cli":
		return ToolHuggingFace
	case cmdNext:
		return ToolNextJS
	case string(ToolNuxt), cmdNuxi:
		return ToolNuxt
	case cmdGatsby:
		return ToolGatsby
	case cmdAstro:
		return ToolAstro
	case cmdTurbo:
		return ToolTurborepo
	case "dotnet":
		return ToolDotNetCLI
	case "pwsh", "powershell":
		return ToolPowerShell
	case "vcpkg", "vcpkg.exe", "bootstrap-vcpkg", fileBootstrapVcpkgBat, fileBootstrapVcpkgSh:
		return ToolVcpkg
	case "brew":
		return ToolHomebrew
	default:
		return ""
	}
}

func installedToolFromPackage(pkg string) ToolID {
	spec := canonicalPackageSpec(pkg)
	switch {
	case spec == "azure-cli":
		return ToolAzureCLI
	case spec == cmdWrangler || spec == pkgCloudflareWrangler:
		return ToolWrangler
	case isHuggingFacePackage(spec):
		return ToolHuggingFace
	case spec == cmdNext:
		return ToolNextJS
	case spec == string(ToolNuxt):
		return ToolNuxt
	case spec == "gatsby":
		return ToolGatsby
	case spec == "astro":
		return ToolAstro
	case spec == "turbo":
		return ToolTurborepo
	case spec == "powershell" || spec == "powershell-preview":
		return ToolPowerShell
	case spec == "vcpkg":
		return ToolVcpkg
	case strings.HasPrefix(spec, "dotnet-sdk"):
		return ToolDotNetCLI
	default:
		return ""
	}
}

func toolFromExecPackage(pkg string) (ToolID, string, bool) {
	spec := canonicalPackageSpec(pkg)
	switch spec {
	case cmdWrangler, pkgCloudflareWrangler:
		return ToolWrangler, "stage executes Wrangler via a package manager", true
	case cmdNext:
		return ToolNextJS, "stage executes Next.js via a package manager", true
	case "nuxt", cmdNuxi:
		return ToolNuxt, "stage executes Nuxt via a package manager", true
	case cmdGatsby:
		return ToolGatsby, "stage executes Gatsby via a package manager", true
	case cmdAstro:
		return ToolAstro, "stage executes Astro via a package manager", true
	case cmdTurbo:
		return ToolTurborepo, "stage executes Turborepo via a package manager", true
	default:
		return "", "", false
	}
}

func execPackageFromCommand(cmd shell.CommandInfo) (string, bool) {
	switch strings.ToLower(cmd.Name) {
	case "npx":
		return firstExecutableArg(cmd.Args)
	case cmdNpm:
		if len(cmd.Args) > 0 && (strings.EqualFold(cmd.Args[0], "exec") || strings.EqualFold(cmd.Args[0], "x")) {
			return firstExecutableArg(cmd.Args[1:])
		}
	case cmdPnpm:
		if len(cmd.Args) > 0 && (strings.EqualFold(cmd.Args[0], "exec") || strings.EqualFold(cmd.Args[0], "dlx")) {
			return firstExecutableArg(cmd.Args[1:])
		}
		if cmd.Subcommand != "" && toolFromExecLikeSubcommand(cmd.Subcommand) {
			return cmd.Subcommand, true
		}
	case "yarn":
		if len(cmd.Args) > 0 && strings.EqualFold(cmd.Args[0], "dlx") {
			return firstExecutableArg(cmd.Args[1:])
		}
	case "bunx":
		return firstExecutableArg(cmd.Args)
	case "bun":
		if len(cmd.Args) > 0 && strings.EqualFold(cmd.Args[0], "x") {
			return firstExecutableArg(cmd.Args[1:])
		}
	}
	return "", false
}

func execPackageFromArgv(argv []string) (string, bool) {
	if len(argv) == 0 {
		return "", false
	}
	switch strings.ToLower(path.Base(argv[0])) {
	case "npx":
		return firstExecutableArg(argv[1:])
	case cmdNpm:
		if len(argv) > 1 && (strings.EqualFold(argv[1], "exec") || strings.EqualFold(argv[1], "x")) {
			return firstExecutableArg(argv[2:])
		}
	case cmdPnpm:
		if len(argv) > 1 && (strings.EqualFold(argv[1], "exec") || strings.EqualFold(argv[1], "dlx")) {
			return firstExecutableArg(argv[2:])
		}
		if len(argv) > 1 && toolFromExecLikeSubcommand(argv[1]) {
			return argv[1], true
		}
	case "yarn":
		if len(argv) > 1 && strings.EqualFold(argv[1], "dlx") {
			return firstExecutableArg(argv[2:])
		}
	case "bunx":
		return firstExecutableArg(argv[1:])
	case "bun":
		if len(argv) > 1 && strings.EqualFold(argv[1], "x") {
			return firstExecutableArg(argv[2:])
		}
	}
	return "", false
}

func toolFromExecLikeSubcommand(name string) bool {
	switch canonicalPackageSpec(name) {
	case "wrangler", "@cloudflare/wrangler", "next", "nuxt", "nuxi", "gatsby", "astro", "turbo":
		return true
	default:
		return false
	}
}

func firstExecutableArg(args []string) (string, bool) {
	for _, arg := range args {
		if arg == "" || strings.HasPrefix(arg, "-") {
			continue
		}
		return arg, true
	}
	return "", false
}

func canonicalPackageSpec(spec string) string {
	spec = strings.ToLower(strings.TrimSpace(spec))
	if spec == "" {
		return ""
	}

	if strings.HasPrefix(spec, "@") {
		if slash := strings.Index(spec, "/"); slash > 0 {
			if at := strings.Index(spec[slash+1:], "@"); at >= 0 {
				spec = spec[:slash+1+at]
			}
		}
	} else {
		if at := strings.Index(spec, "@"); at >= 0 {
			spec = spec[:at]
		}
	}

	if cut := strings.IndexAny(spec, "<>=!~[ "); cut >= 0 {
		spec = spec[:cut]
	}

	return strings.ReplaceAll(spec, "_", "-")
}

func isHuggingFacePackage(spec string) bool {
	switch canonicalPackageSpec(spec) {
	case "huggingface-hub", "transformers", "datasets", "diffusers", "gradio":
		return true
	default:
		return false
	}
}

func isPythonModuleCommand(cmd shell.CommandInfo, module string) bool {
	if !isPythonCommandName(cmd.Name) {
		return false
	}
	for i := range len(cmd.Args) - 1 {
		if cmd.Args[i] == "-m" && strings.EqualFold(cmd.Args[i+1], module) {
			return true
		}
	}
	return false
}

func isPythonModuleArgv(argv []string, module string) bool {
	if len(argv) < 3 || !isPythonCommandName(path.Base(argv[0])) {
		return false
	}
	for i := 1; i < len(argv)-1; i++ {
		if argv[i] == "-m" && strings.EqualFold(argv[i+1], module) {
			return true
		}
	}
	return false
}

func isPythonCommandName(name string) bool {
	switch strings.ToLower(name) {
	case "python", "python3", "py":
		return true
	default:
		return false
	}
}

func isNodeScriptCommand(cmd shell.CommandInfo) bool {
	switch strings.ToLower(cmd.Name) {
	case "npm":
		return strings.EqualFold(cmd.Subcommand, command.Run) || strings.EqualFold(cmd.Subcommand, cmdStart)
	case "pnpm":
		switch strings.ToLower(cmd.Subcommand) {
		case command.Run, cmdStart, cmdBuild, cmdDev, cmdPreview:
			return true
		default:
			return false
		}
	case cmdYarn:
		if cmd.Subcommand == "" {
			return false
		}
		switch strings.ToLower(cmd.Subcommand) {
		case command.Add,
			"bin",
			"install",
			"set",
			"dlx",
			"exec",
			"explain",
			"config",
			"constraints",
			"cache",
			"global",
			"info",
			"node",
			"npm",
			"pack",
			"patch",
			"patch-commit",
			"plugin",
			"search",
			"stage",
			"tag",
			"version",
			"workspaces",
			"workspace",
			"why",
			"up",
			"upgrade",
			"remove",
			"unlink",
			"link":
			return false
		default:
			return true
		}
	case cmdBun:
		return strings.EqualFold(cmd.Subcommand, command.Run)
	default:
		return false
	}
}

func isExplicitBerryCommand(cmd shell.CommandInfo) bool {
	switch strings.ToLower(cmd.Name) {
	case "corepack":
		return containsBerryYarnSpecifier(cmd.Args)
	case cmdYarn:
		return len(cmd.Args) >= 2 && strings.EqualFold(cmd.Args[0], "set") && strings.EqualFold(cmd.Args[1], "version")
	default:
		return false
	}
}

func containsBerryYarnSpecifier(args []string) bool {
	return slices.ContainsFunc(args, isYarnBerryPackageManager)
}

func isYarnBerryPackageManager(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	if !strings.HasPrefix(value, "yarn@") {
		return false
	}

	version := strings.TrimPrefix(value, "yarn@")
	switch version {
	case "berry", "stable", "latest", "canary":
		return true
	}

	digits := version
	if dot := strings.IndexByte(digits, '.'); dot >= 0 {
		digits = digits[:dot]
	}
	major, err := strconv.Atoi(digits)
	return err == nil && major >= 2
}

func hasDependency(manifest packageManifest, name string) bool {
	return manifest.Dependencies[name] != "" ||
		manifest.DevDependencies[name] != "" ||
		manifest.OptionalDependencies[name] != "" ||
		manifest.PeerDependencies[name] != ""
}

func contentMentionsHFPackage(content string) bool {
	content = strings.ToLower(content)
	return strings.Contains(content, "huggingface_hub") ||
		strings.Contains(content, "huggingface-hub") ||
		strings.Contains(content, "transformers") ||
		strings.Contains(content, "datasets") ||
		strings.Contains(content, "diffusers") ||
		strings.Contains(content, "gradio")
}

func isPythonManifestFile(base string) bool {
	switch {
	case strings.HasPrefix(base, "requirements") && strings.HasSuffix(base, ".txt"):
		return true
	case base == "pyproject.toml":
		return true
	case base == "uv.lock":
		return true
	default:
		return false
	}
}

func hasAnyArgFold(args []string, target string) bool {
	for _, arg := range args {
		if strings.EqualFold(arg, target) {
			return true
		}
	}
	return false
}

func pathHasSegment(value, segment string) bool {
	return slices.Contains(strings.Split(path.Clean(value), "/"), segment)
}
