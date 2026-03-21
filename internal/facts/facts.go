package facts

import (
	"maps"
	"path"
	"strings"
	"sync"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"

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
	CacheDisablingEnv  map[string]EnvBinding
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
	run        *instructions.RunCommand
	stageIdx   int
	commandIdx int
	workdir    string
	shell      ShellFacts
	envValues  map[string]string
	envBinding map[string]EnvBinding
	sm         *sourcemap.SourceMap
	escape     rune
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
) *FileFacts {
	return &FileFacts{
		file:            file,
		parseResult:     parseResult,
		semantic:        sem,
		shellDirectives: shellDirectives,
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

	sm := sourcemap.New(f.parseResult.Source)
	escapeToken := rune('\\')
	if f.parseResult.AST != nil {
		escapeToken = f.parseResult.AST.EscapeToken
	}

	stages := f.parseResult.Stages
	f.stages = make([]*StageFacts, len(stages))

	for stageIdx := range stages {
		stage := &stages[stageIdx]
		semInfo := f.stageInfo(stageIdx)

		currentEnvValues, currentEnvBindings := seedStageEnv(semInfo, f.stages)
		currentShell := initialStageShell(stage, semInfo, f.shellDirectives)
		workdir := "/"

		stageFacts := &StageFacts{
			Index:        stageIdx,
			IsLast:       stageIdx == len(stages)-1,
			InitialShell: currentShell,
			FinalShell:   currentShell,
		}
		if semInfo != nil {
			stageFacts.BaseImageOS = semInfo.BaseImageOS
		}

		for cmdIdx, cmd := range stage.Commands {
			switch c := cmd.(type) {
			case *instructions.WorkdirCommand:
				workdir = ResolveWorkdir(workdir, c.Path)
			case *instructions.EnvCommand:
				applyEnvCommand(c, currentEnvValues, currentEnvBindings)
			case *instructions.ShellCommand:
				currentShell = newShellFacts(c.Shell)
				stageFacts.FinalShell = currentShell
			case *instructions.RunCommand:
				runFacts := buildRunFacts(runFactBuildParams{
					run:        c,
					stageIdx:   stageIdx,
					commandIdx: cmdIdx,
					workdir:    workdir,
					shell:      currentShell,
					envValues:  currentEnvValues,
					envBinding: currentEnvBindings,
					sm:         sm,
					escape:     escapeToken,
				})
				stageFacts.Runs = append(stageFacts.Runs, runFacts)
				f.runs = append(f.runs, runFacts)
			}
		}

		finalEnvValues := maps.Clone(currentEnvValues)
		if semInfo != nil && semInfo.EffectiveEnv != nil {
			finalEnvValues = maps.Clone(semInfo.EffectiveEnv)
		}
		stageFacts.EffectiveEnv = buildEnvFacts(finalEnvValues, currentEnvBindings)
		f.stages[stageIdx] = stageFacts
	}
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
	cmd := append([]string(nil), shellCmd...)
	var executable string
	if len(cmd) > 0 {
		executable = cmd[0]
	}
	variant := shell.VariantFromShellCmd(cmd)
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

func applyEnvCommand(cmd *instructions.EnvCommand, values map[string]string, bindings map[string]EnvBinding) {
	if cmd == nil {
		return
	}
	for _, kv := range cmd.Env {
		value := Unquote(kv.Value)
		values[kv.Key] = value
		bindings[kv.Key] = EnvBinding{
			Key:     kv.Key,
			Value:   value,
			Command: cmd,
		}
	}
}

func buildRunFacts(params runFactBuildParams) *RunFacts {
	envFacts := buildEnvFacts(params.envValues, params.envBinding)
	sourceScript := resolveRunSourceScript(params.run, params.sm, params.escape)
	commandScript := resolveRunCommandScript(params.run)

	commandInfos := shell.FindCommands(commandScript, params.shell.Variant)

	installVariant := params.shell.Variant
	if !installVariant.SupportsPOSIXShellAST() {
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
		CacheDisablingEnv:  deriveCacheDisablingEnv(envFacts.Bindings),
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

func deriveCacheDisablingEnv(bindings map[string]EnvBinding) map[string]EnvBinding {
	derived := make(map[string]EnvBinding, len(CacheDisablingEnvVars))
	for key, binding := range bindings {
		if CacheDisablingEnvVars[key] {
			derived[key] = binding
		}
	}
	return derived
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

// Unquote strips a single layer of matching double or single quotes.
func Unquote(s string) string {
	if len(s) >= 2 && ((s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'')) {
		return s[1 : len(s)-1]
	}
	return s
}
