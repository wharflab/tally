package facts

import (
	"maps"
	"path"
	"regexp"
	"strings"
	"sync"

	"github.com/moby/buildkit/frontend/dockerfile/command"
	"github.com/moby/buildkit/frontend/dockerfile/instructions"
	"github.com/moby/buildkit/frontend/dockerfile/parser"

	"github.com/wharflab/tally/internal/dockerfile"
	"github.com/wharflab/tally/internal/semantic"
	"github.com/wharflab/tally/internal/shell"
	"github.com/wharflab/tally/internal/sourcemap"
)

// FileFacts contains cached, derived facts for one Dockerfile.
// The facts are computed once per file and then shared read-only by all rules.
type FileFacts struct {
	file            string
	parseResult     *dockerfile.ParseResult
	semantic        *semantic.Model
	shellDirectives []ShellDirective
	contextFiles    ContextFileReader

	once   sync.Once
	stages []*StageFacts
	runs   []*RunFacts
}

// StageFacts contains derived facts for a single build stage.
type StageFacts struct {
	Index       int
	IsLast      bool
	BaseImageOS semantic.BaseImageOS

	InitialShell ShellFacts
	FinalShell   ShellFacts
	EffectiveEnv EnvFacts
	Runs         []*RunFacts

	// EffectiveUser is the value from the last USER instruction in this stage.
	// Empty string means no USER instruction exists in this stage (inherits
	// from the base image).
	EffectiveUser string

	// UserCommands collects all USER instructions in this stage in order.
	// Useful for rules that need to track the progression of USER changes
	// or need the instruction location of the last USER.
	UserCommands []*instructions.UserCommand

	// Volumes collects all volume mount point paths declared by VOLUME
	// instructions in this stage.
	Volumes []string

	// FinalWorkdir is the effective WORKDIR at the end of this stage.
	FinalWorkdir string

	// HasPrivilegeDropEntrypoint is true when the stage's ENTRYPOINT either
	// directly references a known privilege-drop tool or resolves to an
	// observable script whose content invokes one.
	HasPrivilegeDropEntrypoint bool

	// HasPrivilegeDropCmd is true when the stage's CMD references a known
	// privilege-drop tool. Because CMD provides default arguments to
	// ENTRYPOINT when both are present, a privilege-drop tool in CMD only
	// indicates actual privilege dropping when no ENTRYPOINT is set.
	HasPrivilegeDropCmd bool

	// HasEntrypoint is true when the stage contains an ENTRYPOINT instruction.
	HasEntrypoint bool

	// ObservableFiles collects image files written in this stage whose content
	// can be observed directly or loaded lazily at lint time.
	ObservableFiles []*ObservableFile

	// BuildContextSources records COPY/ADD sources resolved from the Docker
	// build context, including .dockerignore evaluation results.
	BuildContextSources []*BuildContextSource

	cacheDisablingEnv []EnvBinding
	observableByPath  map[string]*ObservableFile
}

// RunFacts contains derived facts for a single RUN instruction.
type RunFacts struct {
	StageIndex   int
	CommandIndex int
	Run          *instructions.RunCommand
	UsesShell    bool

	Workdir       string
	CommandScript string
	SourceScript  string
	Shell         ShellFacts
	Env           EnvFacts

	CommandInfos       []shell.CommandInfo
	InstallCommands    []shell.InstallCommand
	CachePathOverrides map[string]string
	CacheDisablingEnv  []EnvBinding
}

// ShellFacts captures the effective shell state for a stage or RUN command.
type ShellFacts struct {
	Command []string

	Variant    shell.Variant
	Executable string
	HasParser  bool

	IsPowerShell         bool
	PowerShellMayMaskErr bool
}

// EnvFacts captures effective environment signals at a stage or RUN point.
type EnvFacts struct {
	Values   map[string]string
	Bindings map[string]EnvBinding

	DebianFrontend    string
	AptNonInteractive bool
}

// EnvBinding points a resolved env value back to the ENV instruction that set it.
type EnvBinding struct {
	Key     string
	Value   string
	Command *instructions.EnvCommand
}

type runFactBuildParams struct {
	run               *instructions.RunCommand
	stageIdx          int
	commandIdx        int
	workdir           string
	shell             ShellFacts
	envValues         map[string]string
	envBinding        map[string]EnvBinding
	cacheDisablingEnv []EnvBinding
	sm                *sourcemap.SourceMap
	escape            rune
}

type stageBuildState struct {
	currentEnvValues         map[string]string
	currentEnvBindings       map[string]EnvBinding
	currentCacheDisablingEnv []EnvBinding
	currentShell             ShellFacts
	workdir                  string
	fileTracker              *observableFileTracker
}

type stageEntrypointState struct {
	lastEntrypointCmdLine []string
	lastCmdCmdLine        []string
	sawLocalEntrypoint    bool
	sawLocalCmd           bool
}

// ShellDirective is the subset of directive metadata needed by the facts layer.
type ShellDirective struct {
	Line  int
	Shell string
}

// CacheLocationEnvVar defines an ENV variable that overrides a cache mount target.
type CacheLocationEnvVar struct {
	EnvKey          string
	CaseInsensitive bool
	MountID         string
	Suffix          string
}

// CacheLocationEnvVars lists ENV variables that override default cache mount targets.
// Callers must treat this slice as read-only.
var CacheLocationEnvVars = []CacheLocationEnvVar{
	{EnvKey: "PNPM_HOME", MountID: "pnpm", Suffix: "/store"},
	{EnvKey: "npm_config_cache", CaseInsensitive: true, MountID: "npm"},
	{EnvKey: "BUN_INSTALL_CACHE_DIR", MountID: "bun"},
}

// CacheDisablingEnvVars lists ENV variables that disable package-manager caches.
// Callers must treat this map as read-only.
var CacheDisablingEnvVars = map[string]bool{
	"UV_NO_CACHE":      true,
	"PIP_NO_CACHE_DIR": true,
}

// NewFileFacts creates a new fact store for a Dockerfile.
func NewFileFacts(
	file string,
	parseResult *dockerfile.ParseResult,
	sem *semantic.Model,
	shellDirectives []ShellDirective,
	contextFiles ContextFileReader,
) *FileFacts {
	return &FileFacts{
		file:            file,
		parseResult:     parseResult,
		semantic:        sem,
		shellDirectives: shellDirectives,
		contextFiles:    contextFiles,
	}
}

// Stage returns the facts for a single stage.
func (f *FileFacts) Stage(index int) *StageFacts {
	f.once.Do(f.build)
	if index < 0 || index >= len(f.stages) {
		return nil
	}
	return f.stages[index]
}

// DropsPrivilegesAtRuntime reports whether the stage effectively drops root
// privileges at runtime, respecting Docker's ENTRYPOINT/CMD interaction:
//   - A privilege-drop tool in ENTRYPOINT always counts.
//   - A privilege-drop tool in CMD counts only when no ENTRYPOINT is set,
//     because CMD provides default arguments to ENTRYPOINT when both exist.
func (s *StageFacts) DropsPrivilegesAtRuntime() bool {
	if s.HasPrivilegeDropEntrypoint {
		return true
	}
	return s.HasPrivilegeDropCmd && !s.HasEntrypoint
}

// FileContent returns the final observable content for a file path in this stage.
func (s *StageFacts) FileContent(filePath string) (string, bool) {
	if s == nil || filePath == "" {
		return "", false
	}
	file := s.observableByPath[normalizeObservablePath(filePath)]
	if file == nil {
		return "", false
	}
	return file.Content()
}

// Stages returns all stage facts.
func (f *FileFacts) Stages() []*StageFacts {
	f.once.Do(f.build)
	return append([]*StageFacts(nil), f.stages...)
}

// Runs returns all RUN facts across all stages.
func (f *FileFacts) Runs() []*RunFacts {
	f.once.Do(f.build)
	return append([]*RunFacts(nil), f.runs...)
}

func (f *FileFacts) build() {
	if f.parseResult == nil {
		return
	}

	stages := f.parseResult.Stages
	f.stages = make([]*StageFacts, len(stages))
	sm, escapeToken := factsBuildContext(f.parseResult)

	for stageIdx := range stages {
		f.stages[stageIdx] = f.buildStageFacts(stageIdx, &stages[stageIdx], len(stages), sm, escapeToken)
	}
}

func factsBuildContext(parseResult *dockerfile.ParseResult) (*sourcemap.SourceMap, rune) {
	sm := sourcemap.New(parseResult.Source)
	escapeToken := rune('\\')
	if parseResult.AST != nil {
		escapeToken = parseResult.AST.EscapeToken
	}
	return sm, escapeToken
}

func (f *FileFacts) buildStageFacts(
	stageIdx int,
	stage *instructions.Stage,
	stageCount int,
	sm *sourcemap.SourceMap,
	escapeToken rune,
) *StageFacts {
	semInfo := f.stageInfo(stageIdx)
	knownVars := makeStageKnownVarsChecker(semInfo)
	state := newStageBuildState(stage, semInfo, f.stages, f.shellDirectives)
	stageFacts := newStageFacts(stageIdx, stageCount, state.currentShell, semInfo)
	seedStageEntrypointState(semInfo, f.stages, stageFacts)
	entrypointState := f.processStageCommands(stageFacts, stage, stageIdx, sm, escapeToken, knownVars, state)
	finalizeStageFacts(stageFacts, semInfo, state, entrypointState)
	return stageFacts
}

func newStageBuildState(
	stage *instructions.Stage,
	semInfo *semantic.StageInfo,
	stages []*StageFacts,
	shellDirectives []ShellDirective,
) *stageBuildState {
	currentEnvValues, currentEnvBindings := seedStageEnv(semInfo, stages)
	observableFiles := seedStageObservableFiles(semInfo, stages)
	return &stageBuildState{
		currentEnvValues:         currentEnvValues,
		currentEnvBindings:       currentEnvBindings,
		currentCacheDisablingEnv: seedStageCacheDisablingEnv(semInfo, stages),
		currentShell:             initialStageShell(stage, semInfo, shellDirectives),
		workdir:                  "/",
		fileTracker:              newObservableFileTracker(observableFiles),
	}
}

func newStageFacts(stageIdx, stageCount int, currentShell ShellFacts, semInfo *semantic.StageInfo) *StageFacts {
	stageFacts := &StageFacts{
		Index:        stageIdx,
		IsLast:       stageIdx == stageCount-1,
		InitialShell: currentShell,
		FinalShell:   currentShell,
	}
	if semInfo != nil {
		stageFacts.BaseImageOS = semInfo.BaseImageOS
	}
	return stageFacts
}

func (f *FileFacts) processStageCommands(
	stageFacts *StageFacts,
	stage *instructions.Stage,
	stageIdx int,
	sm *sourcemap.SourceMap,
	escapeToken rune,
	knownVars func(string) bool,
	state *stageBuildState,
) stageEntrypointState {
	var entrypointState stageEntrypointState

	for cmdIdx, cmd := range stage.Commands {
		switch c := cmd.(type) {
		case *instructions.WorkdirCommand:
			state.workdir = ResolveWorkdir(state.workdir, c.Path)
		case *instructions.EnvCommand:
			state.currentCacheDisablingEnv = applyEnvCommand(
				c,
				state.currentEnvValues,
				state.currentEnvBindings,
				state.currentCacheDisablingEnv,
			)
		case *instructions.ShellCommand:
			state.currentShell = newShellFacts(c.Shell)
			stageFacts.FinalShell = state.currentShell
		case *instructions.RunCommand:
			runFacts := buildRunFacts(runFactBuildParams{
				run:               c,
				stageIdx:          stageIdx,
				commandIdx:        cmdIdx,
				workdir:           state.workdir,
				shell:             state.currentShell,
				envValues:         state.currentEnvValues,
				envBinding:        state.currentEnvBindings,
				cacheDisablingEnv: state.currentCacheDisablingEnv,
				sm:                sm,
				escape:            escapeToken,
			})
			stageFacts.Runs = append(stageFacts.Runs, runFacts)
			f.runs = append(f.runs, runFacts)
			recordRunObservableFile(stageFacts, state.fileTracker, runFacts, knownVars)
		case *instructions.CopyCommand:
			recordCopyObservableFiles(stageFacts, state.fileTracker, c, state.workdir, f.contextFiles)
		case *instructions.AddCommand:
			recordAddObservableFiles(stageFacts, state.fileTracker, c, state.workdir, f.contextFiles)
		case *instructions.UserCommand:
			stageFacts.UserCommands = append(stageFacts.UserCommands, c)
			stageFacts.EffectiveUser = c.User
		case *instructions.VolumeCommand:
			stageFacts.Volumes = append(stageFacts.Volumes, c.Volumes...)
		case *instructions.EntrypointCommand:
			stageFacts.HasEntrypoint = true
			entrypointState.sawLocalEntrypoint = true
			entrypointState.lastEntrypointCmdLine = append([]string(nil), c.CmdLine...)
		case *instructions.CmdCommand:
			entrypointState.sawLocalCmd = true
			entrypointState.lastCmdCmdLine = append([]string(nil), c.CmdLine...)
		}
	}

	return entrypointState
}

func finalizeStageFacts(
	stageFacts *StageFacts,
	semInfo *semantic.StageInfo,
	state *stageBuildState,
	entrypointState stageEntrypointState,
) {
	stageFacts.FinalWorkdir = state.workdir
	stageFacts.observableByPath = state.fileTracker.snapshot()
	if entrypointState.sawLocalEntrypoint {
		stageFacts.HasPrivilegeDropEntrypoint = commandDropsPrivileges(entrypointState.lastEntrypointCmdLine, stageFacts)
	}
	if entrypointState.sawLocalCmd {
		stageFacts.HasPrivilegeDropCmd = commandDropsPrivileges(entrypointState.lastCmdCmdLine, stageFacts)
	}

	finalEnvValues := maps.Clone(state.currentEnvValues)
	if semInfo != nil && semInfo.EffectiveEnv != nil {
		finalEnvValues = maps.Clone(semInfo.EffectiveEnv)
	}
	stageFacts.EffectiveEnv = buildEnvFacts(finalEnvValues, state.currentEnvBindings)
	stageFacts.cacheDisablingEnv = append([]EnvBinding(nil), state.currentCacheDisablingEnv...)
}

func (f *FileFacts) stageInfo(index int) *semantic.StageInfo {
	if f.semantic == nil {
		return nil
	}
	return f.semantic.StageInfo(index)
}

func seedStageEnv(semInfo *semantic.StageInfo, stages []*StageFacts) (map[string]string, map[string]EnvBinding) {
	if semInfo == nil || semInfo.BaseImage == nil || !semInfo.BaseImage.IsStageRef {
		return map[string]string{}, map[string]EnvBinding{}
	}

	baseIdx := semInfo.BaseImage.StageIndex
	if baseIdx < 0 || baseIdx >= len(stages) || stages[baseIdx] == nil {
		return map[string]string{}, map[string]EnvBinding{}
	}

	return maps.Clone(stages[baseIdx].EffectiveEnv.Values), maps.Clone(stages[baseIdx].EffectiveEnv.Bindings)
}

func seedStageCacheDisablingEnv(semInfo *semantic.StageInfo, stages []*StageFacts) []EnvBinding {
	if semInfo == nil || semInfo.BaseImage == nil || !semInfo.BaseImage.IsStageRef {
		return nil
	}

	baseIdx := semInfo.BaseImage.StageIndex
	if baseIdx < 0 || baseIdx >= len(stages) || stages[baseIdx] == nil {
		return nil
	}

	return append([]EnvBinding(nil), stages[baseIdx].cacheDisablingEnv...)
}

// seedStageEntrypointState inherits the entrypoint/cmd privilege-drop state
// from a parent stage when the base is a local stage ref. The inherited values
// are overridden if the child stage has its own ENTRYPOINT/CMD instructions.
func seedStageEntrypointState(semInfo *semantic.StageInfo, stages []*StageFacts, target *StageFacts) {
	if semInfo == nil || semInfo.BaseImage == nil || !semInfo.BaseImage.IsStageRef {
		return
	}

	baseIdx := semInfo.BaseImage.StageIndex
	if baseIdx < 0 || baseIdx >= len(stages) || stages[baseIdx] == nil {
		return
	}

	parent := stages[baseIdx]
	target.HasEntrypoint = parent.HasEntrypoint
	target.HasPrivilegeDropEntrypoint = parent.HasPrivilegeDropEntrypoint
	target.HasPrivilegeDropCmd = parent.HasPrivilegeDropCmd
}

func seedStageObservableFiles(semInfo *semantic.StageInfo, stages []*StageFacts) map[string]*ObservableFile {
	if semInfo == nil || semInfo.BaseImage == nil || !semInfo.BaseImage.IsStageRef {
		return nil
	}

	baseIdx := semInfo.BaseImage.StageIndex
	if baseIdx < 0 || baseIdx >= len(stages) || stages[baseIdx] == nil {
		return nil
	}

	return maps.Clone(stages[baseIdx].observableByPath)
}

func makeStageKnownVarsChecker(semInfo *semantic.StageInfo) func(string) bool {
	if semInfo == nil || semInfo.Variables == nil {
		return nil
	}
	return func(name string) bool {
		return semInfo.Variables.HasArg(name) || semInfo.Variables.GetEnv(name) != nil
	}
}

func recordRunObservableFile(
	stageFacts *StageFacts,
	tracker *observableFileTracker,
	runFacts *RunFacts,
	knownVars func(string) bool,
) {
	if stageFacts == nil || tracker == nil || runFacts == nil || runFacts.Run == nil {
		return
	}

	info := shell.DetectFileCreation(reconstructRunShellScript(runFacts.Run), runFacts.Shell.Variant, knownVars)
	if info == nil {
		return
	}

	targetPath := normalizeObservablePath(info.TargetPath)
	if targetPath == "" {
		return
	}

	if info.HasUnsafeVariables {
		tracker.invalidate(targetPath)
		return
	}

	file := literalObservableFile(
		targetPath,
		ObservableFileSourceRun,
		instructionStartLine(runFacts.Run.Location()),
		info.IsAppend,
		info.RawChmodMode,
		"",
		info.Content,
	)
	stageFacts.ObservableFiles = append(stageFacts.ObservableFiles, file)
	if info.IsAppend {
		tracker.append(file)
		return
	}
	tracker.overwrite(file)
}

func recordCopyObservableFiles(
	stageFacts *StageFacts,
	tracker *observableFileTracker,
	cmd *instructions.CopyCommand,
	workdir string,
	contextFiles ContextFileReader,
) {
	if stageFacts == nil || tracker == nil || cmd == nil {
		return
	}

	line := instructionStartLine(cmd.Location())
	totalSources := len(cmd.SourcePaths) + len(cmd.SourceContents)
	heredocPaths := dockerfile.CollectHeredocPaths(cmd.SourceContents)

	for _, content := range cmd.SourceContents {
		destPath, ok := resolveCopyDestPath(cmd.DestPath, content.Path, workdir, totalSources)
		if !ok {
			continue
		}
		file := literalObservableFile(
			destPath,
			ObservableFileSourceCopyHeredoc,
			line,
			false,
			cmd.Chmod,
			cmd.Chown,
			content.Data,
		)
		stageFacts.ObservableFiles = append(stageFacts.ObservableFiles, file)
		tracker.overwrite(file)
	}

	for _, src := range cmd.SourcePaths {
		if heredocPaths[src] {
			continue
		}

		var sourceFact *BuildContextSource
		if cmd.From == "" {
			sourceFact = analyzeBuildContextSource(command.Copy, src, line, cmd.Location(), contextFiles)
		}
		if sourceFact != nil {
			stageFacts.BuildContextSources = append(stageFacts.BuildContextSources, sourceFact)
		}

		destPath, ok := resolveCopyDestPath(cmd.DestPath, src, workdir, totalSources)
		if !ok {
			continue
		}
		if cmd.From != "" || !canObserveContextSource(src, sourceFact, contextFiles) {
			tracker.invalidate(destPath)
			continue
		}
		sourcePath := src
		if sourceFact != nil && sourceFact.NormalizedSourcePath != "" {
			sourcePath = sourceFact.NormalizedSourcePath
		}
		file := contextObservableFile(destPath, line, cmd.Chmod, cmd.Chown, sourcePath, contextFiles)
		stageFacts.ObservableFiles = append(stageFacts.ObservableFiles, file)
		tracker.overwrite(file)
	}
}

func recordAddObservableFiles(
	stageFacts *StageFacts,
	tracker *observableFileTracker,
	cmd *instructions.AddCommand,
	workdir string,
	contextFiles ContextFileReader,
) {
	if stageFacts == nil || tracker == nil || cmd == nil {
		return
	}

	line := instructionStartLine(cmd.Location())
	totalSources := len(cmd.SourcePaths) + len(cmd.SourceContents)
	heredocPaths := dockerfile.CollectHeredocPaths(cmd.SourceContents)

	for _, content := range cmd.SourceContents {
		destPath, ok := resolveCopyDestPath(cmd.DestPath, content.Path, workdir, totalSources)
		if !ok {
			continue
		}
		file := literalObservableFile(
			destPath,
			ObservableFileSourceAddHeredoc,
			line,
			false,
			cmd.Chmod,
			cmd.Chown,
			content.Data,
		)
		stageFacts.ObservableFiles = append(stageFacts.ObservableFiles, file)
		tracker.overwrite(file)
	}

	for _, src := range cmd.SourcePaths {
		if heredocPaths[src] {
			continue
		}

		sourceFact := analyzeBuildContextSource(command.Add, src, line, cmd.Location(), contextFiles)
		if sourceFact != nil {
			stageFacts.BuildContextSources = append(stageFacts.BuildContextSources, sourceFact)
		}
		destPath, ok := resolveCopyDestPath(cmd.DestPath, src, workdir, totalSources)
		if ok {
			tracker.invalidate(destPath)
		}
	}
}

func canObserveContextSource(sourcePath string, sourceFact *BuildContextSource, contextFiles ContextFileReader) bool {
	if contextFiles == nil || sourcePath == "" {
		return false
	}
	if sourceFact != nil && (sourceFact.IgnoredByDockerignore || sourceFact.IgnoreErr != nil) {
		return false
	}
	if strings.ContainsAny(sourcePath, "*?[") {
		return false
	}
	if sourceFact != nil && sourceFact.NormalizedSourcePath != "" {
		sourcePath = sourceFact.NormalizedSourcePath
	}
	return contextFiles.FileExists(sourcePath)
}

func analyzeBuildContextSource(
	instruction, sourcePath string,
	line int,
	location []parser.Range,
	contextFiles ContextFileReader,
) *BuildContextSource {
	if contextFiles == nil || sourcePath == "" {
		return nil
	}
	if isBuildContextURLSource(sourcePath) || contextFiles.IsHeredocFile(sourcePath) {
		return nil
	}

	normalized := normalizeBuildContextSourcePath(sourcePath)
	ignored, err := contextFiles.IsIgnored(normalized)
	return &BuildContextSource{
		Instruction:           instruction,
		SourcePath:            sourcePath,
		NormalizedSourcePath:  normalized,
		Line:                  line,
		Location:              location,
		IgnoredByDockerignore: ignored,
		IgnoreErr:             err,
	}
}

func commandDropsPrivileges(cmdLine []string, stageFacts *StageFacts) bool {
	if containsPrivilegeDropPattern(cmdLine) {
		return true
	}
	if stageFacts == nil {
		return false
	}

	scriptPath := referencedScriptPath(cmdLine)
	if scriptPath == "" {
		return false
	}

	content, ok := stageFacts.FileContent(resolveRuntimeScriptPath(scriptPath, stageFacts.FinalWorkdir))
	if !ok {
		return false
	}
	return privilegeDropToolsRe.MatchString(strings.ToLower(content))
}

func referencedScriptPath(cmdLine []string) string {
	if len(cmdLine) == 0 {
		return ""
	}

	first := cmdLine[0]
	if len(cmdLine) == 1 {
		fields := strings.Fields(first)
		if len(fields) == 0 {
			return ""
		}
		if fields[0] == "exec" && len(fields) > 1 {
			first = fields[1]
		} else {
			first = fields[0]
		}
	}

	first = strings.Trim(first, `"'`)
	if first == "" {
		return ""
	}
	if path.IsAbs(first) || strings.Contains(first, "/") || strings.Contains(first, `\`) {
		return first
	}
	switch {
	case strings.HasSuffix(first, ".sh"),
		strings.HasSuffix(first, ".bash"),
		strings.HasSuffix(first, ".ps1"),
		strings.HasSuffix(first, ".cmd"),
		strings.HasSuffix(first, ".bat"):
		return first
	default:
		return ""
	}
}

func reconstructRunShellScript(run *instructions.RunCommand) string {
	if run == nil {
		return ""
	}
	if len(run.CmdLine) == 0 {
		return ""
	}

	script := strings.Join(run.CmdLine, " ")
	if len(run.Files) == 0 {
		return script
	}

	var sb strings.Builder
	sb.WriteString(script)
	for _, file := range run.Files {
		sb.WriteString("\n")
		sb.WriteString(file.Data)
		sb.WriteString(file.Name)
	}
	return sb.String()
}

func instructionStartLine(location []parser.Range) int {
	if len(location) == 0 {
		return 0
	}
	return location[0].Start.Line
}

func initialStageShell(
	stage *instructions.Stage,
	semInfo *semantic.StageInfo,
	shellDirectives []ShellDirective,
) ShellFacts {
	if shellName, ok := activeShellDirective(stage, shellDirectives); ok {
		return newShellFacts([]string{shellName, "-c"})
	}
	if semInfo != nil && semInfo.BaseImageOS == semantic.BaseImageOSWindows {
		return newShellFacts(semantic.DefaultWindowsShell())
	}

	// Use the semantic model's ShellSetting when available — it already
	// accounts for distro-aware variant refinement (e.g. VariantBash for
	// most Linux distros, VariantPOSIX for Alpine/Debian/Ubuntu).
	if semInfo != nil {
		return newShellFactsWithVariant(semInfo.ShellSetting.Shell, semInfo.ShellSetting.Variant)
	}

	defaultShell := append([]string(nil), semantic.DefaultShell...)
	return newShellFacts(defaultShell)
}

func activeShellDirective(stage *instructions.Stage, shellDirectives []ShellDirective) (string, bool) {
	if len(shellDirectives) == 0 || stage == nil || len(stage.Location) == 0 {
		return "", false
	}

	fromLine := stage.Location[0].Start.Line - 1
	var active *ShellDirective
	for i := range shellDirectives {
		sd := &shellDirectives[i]
		if sd.Line < fromLine && (active == nil || sd.Line > active.Line) {
			active = sd
		}
	}
	if active == nil {
		return "", false
	}
	return active.Shell, true
}

func newShellFacts(shellCmd []string) ShellFacts {
	variant := shell.VariantFromShellCmd(shellCmd)
	return newShellFactsWithVariant(shellCmd, variant)
}

func newShellFactsWithVariant(shellCmd []string, variant shell.Variant) ShellFacts {
	cmd := append([]string(nil), shellCmd...)
	var executable string
	if len(cmd) > 0 {
		executable = cmd[0]
	}
	return ShellFacts{
		Command:              cmd,
		Variant:              variant,
		Executable:           executable,
		HasParser:            variant.HasParser(),
		IsPowerShell:         variant.IsPowerShell(),
		PowerShellMayMaskErr: powerShellMayMaskErrors(cmd, variant),
	}
}

func powerShellMayMaskErrors(shellCmd []string, variant shell.Variant) bool {
	if !variant.IsPowerShell() {
		return false
	}
	if len(shellCmd) <= 1 {
		return true
	}

	lower := strings.ToLower(strings.Join(shellCmd[1:], " "))
	if strings.Contains(lower, "$erroractionpreference") {
		return !strings.Contains(lower, "stop")
	}
	return true
}

func applyEnvCommand(
	cmd *instructions.EnvCommand,
	values map[string]string,
	bindings map[string]EnvBinding,
	cacheDisablingEnv []EnvBinding,
) []EnvBinding {
	if cmd == nil {
		return cacheDisablingEnv
	}
	for _, kv := range cmd.Env {
		value := Unquote(kv.Value)
		binding := EnvBinding{
			Key:     kv.Key,
			Value:   value,
			Command: cmd,
		}
		values[kv.Key] = value
		bindings[kv.Key] = binding
		if CacheDisablingEnvVars[kv.Key] {
			cacheDisablingEnv = append(cacheDisablingEnv, binding)
		}
	}
	return cacheDisablingEnv
}

func buildRunFacts(params runFactBuildParams) *RunFacts {
	envFacts := buildEnvFacts(params.envValues, params.envBinding)
	sourceScript := resolveRunSourceScript(params.run, params.sm, params.escape)
	commandScript := resolveRunCommandScript(params.run)

	commandInfos := shell.FindCommands(commandScript, params.shell.Variant)

	installVariant := params.shell.Variant
	if !installVariant.SupportsPOSIXShellAST() {
		// FindInstallPackages is POSIX-oriented. For non-POSIX shells we still
		// run the lightweight extractor through a Bash-compatible parser as a
		// best-effort fallback; unsupported scripts fail closed and produce no
		// package installs rather than a false positive.
		installVariant = shell.VariantBash
	}

	return &RunFacts{
		StageIndex:         params.stageIdx,
		CommandIndex:       params.commandIdx,
		Run:                params.run,
		UsesShell:          params.run != nil && params.run.PrependShell,
		Workdir:            params.workdir,
		CommandScript:      commandScript,
		SourceScript:       sourceScript,
		Shell:              params.shell,
		Env:                envFacts,
		CommandInfos:       commandInfos,
		InstallCommands:    shell.FindInstallPackages(sourceScript, installVariant),
		CachePathOverrides: deriveCachePathOverrides(envFacts.Values, params.workdir),
		CacheDisablingEnv:  append([]EnvBinding(nil), params.cacheDisablingEnv...),
	}
}

func buildEnvFacts(values map[string]string, bindings map[string]EnvBinding) EnvFacts {
	clonedValues := maps.Clone(values)
	clonedBindings := maps.Clone(bindings)
	debianFrontend := clonedValues["DEBIAN_FRONTEND"]

	return EnvFacts{
		Values:            clonedValues,
		Bindings:          clonedBindings,
		DebianFrontend:    debianFrontend,
		AptNonInteractive: strings.EqualFold(strings.TrimSpace(debianFrontend), "noninteractive"),
	}
}

func deriveCachePathOverrides(values map[string]string, workdir string) map[string]string {
	overrides := map[string]string{}

	for key, value := range values {
		for _, loc := range CacheLocationEnvVars {
			match := key == loc.EnvKey
			if loc.CaseInsensitive {
				match = strings.EqualFold(key, loc.EnvKey)
			}
			if !match || value == "" || strings.Contains(value, "$") {
				continue
			}

			target := path.Clean(value)
			if !path.IsAbs(target) {
				target = path.Clean(path.Join(workdir, target))
			}
			if loc.Suffix != "" {
				target = path.Join(target, loc.Suffix)
			}
			overrides[loc.MountID] = target
		}
	}

	return overrides
}

func resolveRunSourceScript(run *instructions.RunCommand, sm *sourcemap.SourceMap, escapeToken rune) string {
	if run == nil {
		return ""
	}
	if len(run.Files) > 0 {
		return run.Files[0].Data
	}
	if !run.PrependShell {
		return strings.Join(run.CmdLine, " ")
	}
	if sm == nil || len(run.Location()) == 0 {
		return strings.Join(run.CmdLine, " ")
	}

	startLine := run.Location()[0].Start.Line
	endLine := sm.ResolveEndLineWithEscape(run.Location()[0].End.Line, escapeToken)

	instrLines := make([]string, 0, endLine-startLine+1)
	for line := startLine; line <= endLine; line++ {
		instrLines = append(instrLines, sm.Line(line-1))
	}

	return shell.ReconstructSourceText(instrLines, shell.DockerfileRunCommandStartCol(instrLines[0]), escapeToken)
}

func resolveRunCommandScript(run *instructions.RunCommand) string {
	if run == nil {
		return ""
	}
	if len(run.Files) > 0 {
		return run.Files[0].Data
	}
	return strings.Join(run.CmdLine, " ")
}

// ResolveWorkdir resolves a WORKDIR path against the current effective workdir.
func ResolveWorkdir(currentWorkdir, nextPath string) string {
	if nextPath == "" {
		return currentWorkdir
	}
	if path.IsAbs(nextPath) {
		return path.Clean(nextPath)
	}
	return path.Clean(path.Join(currentWorkdir, nextPath))
}

// IsRootUser checks if a USER instruction value refers to the root user.
// The USER instruction can specify: username, uid, username:group, or uid:gid.
// This is an exported shared helper so multiple rules can reuse the same logic.
func IsRootUser(user string) bool {
	// Strip group if present (user:group format).
	if idx := strings.Index(user, ":"); idx != -1 {
		user = user[:idx]
	}

	user = strings.TrimSpace(strings.ToLower(user))

	// root by name or UID 0.
	return user == "root" || user == "0"
}

// privilegeDropTools lists executables whose sole purpose is dropping root
// privileges at runtime. Generic script names are intentionally excluded;
// those are handled separately through observable script-content inspection.
var privilegeDropTools = []string{"gosu", "su-exec", "suexec", "setpriv"}

// privilegeDropToolsRe matches any of the privilege-drop tool names as whole
// words so that substrings like "gosuper" do not false-positive.
var privilegeDropToolsRe = regexp.MustCompile(`\b(` + strings.Join(privilegeDropTools, "|") + `)\b`)

// containsPrivilegeDropPattern checks whether a command line (from ENTRYPOINT
// or CMD) references a known privilege-drop tool.
func containsPrivilegeDropPattern(cmdLine []string) bool {
	joined := strings.ToLower(strings.Join(cmdLine, " "))
	return privilegeDropToolsRe.MatchString(joined)
}

// Unquote strips a single layer of matching double or single quotes.
func Unquote(s string) string {
	if len(s) >= 2 && ((s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'')) {
		return s[1 : len(s)-1]
	}
	return s
}
