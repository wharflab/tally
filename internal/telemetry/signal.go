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
	scanner.scanStageCommands(stage.Commands)
	scanner.finalizeManifestSignals()

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

func (s *stageScanner) recordCommandActivity(cmd shell.CommandInfo, candidate anchorCandidate) {
	name := normalizeCommandName(cmd.Name)
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
}

func (s *stageScanner) scanCommandInfo(cmd shell.CommandInfo, candidate anchorCandidate) {
	s.recordCommandActivity(cmd, candidate)
	name := normalizeCommandName(cmd.Name)

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

func (s *stageScanner) scanContainerCommandInfo(cmd shell.CommandInfo, candidate anchorCandidate) {
	s.recordCommandActivity(cmd, candidate)

	if isPythonModuleCommand(cmd, "huggingface_hub") {
		s.result.addSignal(
			ToolHuggingFace,
			SignalKindCommand,
			"stage runs python -m huggingface_hub at container start",
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
		case *instructions.RunCommand:
			if s.stageFacts == nil {
				s.scanRawRunCommand(c, candidate)
			}
		case *instructions.CmdCommand:
			s.scanDockerCommand(c.CmdLine, c.PrependShell, candidate)
		case *instructions.EntrypointCommand:
			s.scanDockerCommand(c.CmdLine, c.PrependShell, candidate)
		case *instructions.ShellCommand:
			if len(c.Shell) == 0 {
				continue
			}
			switch directToolFromCommandName(c.Shell[0]) {
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

func (s *stageScanner) scanRawRunCommand(run *instructions.RunCommand, candidate anchorCandidate) {
	script := rawRunScript(run)
	if script == "" {
		return
	}

	variant := s.shellVariantAtLine(candidate.line)
	for _, cmd := range shell.FindCommands(script, variant) {
		s.scanCommandInfo(cmd, candidate)
	}

	installVariant := variant
	if !installVariant.SupportsPOSIXShellAST() {
		installVariant = shell.VariantBash
	}
	for _, install := range shell.FindInstallPackages(script, installVariant) {
		s.scanInstallCommand(install, candidate)
	}
}

func (s *stageScanner) scanDockerCommand(cmdLine []string, prependShell bool, candidate anchorCandidate) {
	variant := s.shellVariantAtLine(candidate.line)

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

	for _, cmd := range dockerCommandInfos(cmdLine, prependShell, variant) {
		s.scanContainerCommandInfo(cmd, candidate)
	}
}

func (s *stageScanner) shellVariantAtLine(line int) shell.Variant {
	if s.semInfo != nil {
		return s.semInfo.ShellVariantAtLine(line)
	}
	if s.stageFacts != nil {
		return s.stageFacts.FinalShell.Variant
	}
	return shell.VariantBash
}

func dockerCommandInfos(cmdLine []string, prependShell bool, variant shell.Variant) []shell.CommandInfo {
	if len(cmdLine) == 0 {
		return nil
	}
	if prependShell {
		return shell.FindCommands(strings.Join(cmdLine, " "), variant)
	}

	cmd, ok := commandInfoFromArgv(cmdLine)
	if !ok {
		return nil
	}
	return []shell.CommandInfo{cmd}
}

func commandInfoFromArgv(argv []string) (shell.CommandInfo, bool) {
	if len(argv) == 0 || strings.TrimSpace(argv[0]) == "" {
		return shell.CommandInfo{}, false
	}

	cmd := shell.CommandInfo{
		Name: normalizeCommandName(argv[0]),
		Args: append([]string(nil), argv[1:]...),
	}
	for _, arg := range significantCommandArgs(cmd.Name, cmd.Args) {
		if arg == "" || strings.HasPrefix(arg, "-") {
			continue
		}
		cmd.Subcommand = arg
		break
	}
	return cmd, true
}

func rawRunScript(run *instructions.RunCommand) string {
	if run == nil {
		return ""
	}
	if len(run.Files) > 0 && run.Files[0].Data != "" {
		return run.Files[0].Data
	}
	return strings.Join(run.CmdLine, " ")
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
	name = normalizeCommandName(name)
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
	case "vcpkg", "bootstrap-vcpkg", fileBootstrapVcpkgBat, fileBootstrapVcpkgSh:
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
	args := significantCommandArgs(cmd.Name, cmd.Args)
	switch normalizeCommandName(cmd.Name) {
	case "npx":
		return firstExecutableArg(args)
	case cmdNpm:
		if len(args) > 0 && (strings.EqualFold(args[0], "exec") || strings.EqualFold(args[0], "x")) {
			return firstExecutableArg(args[1:])
		}
	case cmdPnpm:
		if len(args) > 0 && (strings.EqualFold(args[0], "exec") || strings.EqualFold(args[0], "dlx")) {
			return firstExecutableArg(args[1:])
		}
		if subcommand := commandSubcommand(cmd); subcommand != "" && toolFromExecLikeSubcommand(subcommand) {
			return subcommand, true
		}
	case "yarn":
		if len(args) > 0 && strings.EqualFold(args[0], "dlx") {
			return firstExecutableArg(args[1:])
		}
	case "bunx":
		return firstExecutableArg(args)
	case "bun":
		if len(args) > 0 && strings.EqualFold(args[0], "x") {
			return firstExecutableArg(args[1:])
		}
	}
	return "", false
}

func execPackageFromArgv(argv []string) (string, bool) {
	cmd, ok := commandInfoFromArgv(argv)
	if !ok {
		return "", false
	}
	return execPackageFromCommand(cmd)
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
	if len(argv) < 3 || !isPythonCommandName(argv[0]) {
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
	switch normalizeCommandName(name) {
	case "python", "python3", "py":
		return true
	default:
		return false
	}
}

func isNodeScriptCommand(cmd shell.CommandInfo) bool {
	subcommand := commandSubcommand(cmd)
	switch normalizeCommandName(cmd.Name) {
	case "npm":
		return strings.EqualFold(subcommand, command.Run) || strings.EqualFold(subcommand, cmdStart)
	case "pnpm":
		switch strings.ToLower(subcommand) {
		case command.Run, cmdStart, cmdBuild, cmdDev, cmdPreview:
			return true
		default:
			return false
		}
	case cmdYarn:
		if subcommand == "" {
			return false
		}
		switch strings.ToLower(subcommand) {
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
		return strings.EqualFold(subcommand, command.Run)
	default:
		return false
	}
}

func isExplicitBerryCommand(cmd shell.CommandInfo) bool {
	switch normalizeCommandName(cmd.Name) {
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

func commandSubcommand(cmd shell.CommandInfo) string {
	if cmd.Subcommand != "" && !packageManagerSupportsGlobalPrefix(cmd.Name) {
		return cmd.Subcommand
	}
	for _, arg := range significantCommandArgs(cmd.Name, cmd.Args) {
		if arg == "" || arg == "--" || strings.HasPrefix(arg, "-") {
			continue
		}
		return arg
	}
	return cmd.Subcommand
}

func significantCommandArgs(name string, args []string) []string {
	if !packageManagerSupportsGlobalPrefix(name) {
		return args
	}

	for i := 0; i < len(args); {
		arg := strings.TrimSpace(args[i])
		switch {
		case arg == "":
			i++
		case arg == "--":
			return args[i:]
		case !strings.HasPrefix(arg, "-"):
			return args[i:]
		case packageManagerGlobalOptionConsumesValue(name, arg):
			if strings.Contains(arg, "=") {
				i++
				continue
			}
			i += 2
		default:
			i++
		}
	}
	return nil
}

func packageManagerSupportsGlobalPrefix(name string) bool {
	switch normalizeCommandName(name) {
	case "npx", cmdNpm, cmdPnpm, cmdYarn, cmdBun:
		return true
	default:
		return false
	}
}

func packageManagerGlobalOptionConsumesValue(name, arg string) bool {
	name = normalizeCommandName(name)
	switch normalizedPackageManagerOption(arg) {
	case "--workspace":
		return name == cmdNpm
	case "-w":
		return name == cmdNpm
	case "--prefix":
		return name == cmdNpm
	case "--dir", "--workspace-dir":
		return name == cmdPnpm
	case "-c":
		return name == cmdPnpm
	case "--filter":
		return name == cmdPnpm
	case "-f":
		return name == cmdPnpm
	case "--cwd":
		return name == cmdYarn || name == cmdBun
	default:
		return false
	}
}

func normalizedPackageManagerOption(arg string) string {
	if eq := strings.Index(arg, "="); eq >= 0 {
		arg = arg[:eq]
	}
	return strings.ToLower(strings.TrimSpace(arg))
}

func normalizeCommandName(name string) string {
	name = shell.NormalizeShellExecutableName(strings.TrimSpace(name))
	return strings.TrimSuffix(name, ".cmd")
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
	for line := range strings.SplitSeq(content, "\n") {
		line = stripPythonManifestComment(strings.ToLower(line))
		for _, token := range strings.FieldsFunc(line, isPackageTokenDelimiter) {
			switch canonicalPackageSpec(token) {
			case "huggingface-hub", "transformers", "datasets", "diffusers", "gradio":
				return true
			}
		}
	}
	return false
}

func stripPythonManifestComment(line string) string {
	trimmed := strings.TrimLeft(line, " \t")
	if strings.HasPrefix(trimmed, "#") {
		return ""
	}
	for i := 1; i < len(line); i++ {
		if line[i] == '#' && (line[i-1] == ' ' || line[i-1] == '\t') {
			return line[:i]
		}
	}
	return line
}

func isPackageTokenDelimiter(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z':
		return false
	case r >= '0' && r <= '9':
		return false
	case r == '-', r == '_':
		return false
	default:
		return true
	}
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
