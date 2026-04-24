package semantic

import (
	"slices"
	"strconv"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"
	"github.com/moby/buildkit/frontend/dockerfile/parser"
	dfshell "github.com/moby/buildkit/frontend/dockerfile/shell"

	"github.com/wharflab/tally/internal/dockerfile"
	"github.com/wharflab/tally/internal/runmount"
	"github.com/wharflab/tally/internal/shell"
)

// Builder constructs a semantic model from a parse result.
// It performs single-pass analysis and records structural build facts.
type Builder struct {
	parseResult     *dockerfile.ParseResult
	buildArgs       map[string]string
	targetStage     string
	file            string
	shellDirectives []ShellDirective

	// Accumulated during build
	globalScope  *VariableScope
	stagesByName map[string]int
}

// NewBuilder creates a new semantic model builder.
func NewBuilder(pr *dockerfile.ParseResult, buildArgs map[string]string, file string) *Builder {
	return &Builder{
		parseResult:  pr,
		buildArgs:    buildArgs,
		file:         file,
		globalScope:  NewGlobalScope(),
		stagesByName: make(map[string]int),
	}
}

// WithShellDirectives sets the shell directives to be applied during build.
func (b *Builder) WithShellDirectives(directives []ShellDirective) *Builder {
	b.shellDirectives = directives
	return b
}

// WithTargetStage sets the invocation-selected target stage name.
func (b *Builder) WithTargetStage(stage string) *Builder {
	b.targetStage = stage
	return b
}

// Build constructs the semantic model.
// This performs single-pass analysis of the Dockerfile and derives the
// cross-instruction state consumed by downstream rules.
func (b *Builder) Build() *Model {
	if b.parseResult == nil {
		return &Model{
			stagesByName:    make(map[string]int),
			graph:           newStageGraph(0),
			targetStageName: defaultTargetStageName,
			finalStageIndex: -1,
		}
	}

	stages := b.parseResult.Stages
	metaArgs := b.parseResult.MetaArgs

	fromEval := b.initFromArgEval(stages, metaArgs)

	// Build stage info and graph
	stageCount := len(stages)
	stageInfo := make([]*StageInfo, stageCount)
	graph := newStageGraph(stageCount)
	finalStageIdx := targetStageIndex(stages, b.targetStage)
	targetStageName := effectiveTargetStageName(stages, b.targetStage)

	for i := range stages {
		stage := &stages[i]
		isLast := i == finalStageIdx

		// Create stage info
		info := newStageInfo(i, stage, isLast)
		info.Variables = NewStageScope(b.globalScope)

		// Process stage name
		b.processStageNaming(stage, i)

		// Process base image
		info.BaseImage = b.processBaseImage(stage, i, graph)
		effectiveBaseName := resolveFromEvalWord(stage.BaseName, fromEval)
		effectivePlatform := resolveFromEvalWord(stage.Platform, fromEval)

		// Detect base image OS from name and platform heuristics.
		info.BaseImageOS = detectBaseImageOS(effectiveBaseName, effectivePlatform)
		// Strengthen the signal with the escape directive: backtick is a strong
		// Windows indicator when the image name alone is ambiguous.
		if info.BaseImageOS == BaseImageOSUnknown && b.parseResult != nil &&
			b.parseResult.AST != nil && b.parseResult.AST.EscapeToken == '`' {
			info.BaseImageOS = BaseImageOSWindows
		}

		// FROM ARG analysis (UndefinedArgInFrom, InvalidDefaultArgInFrom).
		b.applyFromArgAnalysis(info, stage, fromEval)

		// Apply shell directives that appear before this stage's FROM instruction
		b.applyShellDirectives(stage, info)

		applyDefaultShellSemantics(info, stage, effectiveBaseName)

		// Seed the environment used for undefined-var analysis.
		var stageEnv *fromEnv
		switch {
		case info.IsScratch():
			stageEnv = newFromEnv(nil)
		case info.BaseImage != nil && info.BaseImage.IsStageRef:
			base := stageInfo[info.BaseImage.StageIndex]
			if base != nil && base.EffectiveEnv != nil {
				stageEnv = newFromEnv(base.EffectiveEnv)
			} else {
				stageEnv = newFromEnv(nil)
			}
		default:
			stageEnv = newFromEnv(defaultExternalImageEnv())
		}

		// Process commands in the stage
		b.processStageCommands(stage, info, graph, stageEnv, fromEval.shlex)
		info.EffectiveEnv = stageEnv.vars

		stageInfo[i] = info
	}

	// Resolve forward references now that all stage names are registered.
	// The main loop only resolves backward references (idx < stageIndex)
	// because stage names are registered sequentially. Forward references
	// in COPY --from and RUN --mount from= are valid in BuildKit (resolved
	// at LLB construction time), so we add them to the graph here.
	b.resolveForwardRefs(stages, stageInfo, graph)

	return &Model{
		stages:          stages,
		metaArgs:        metaArgs,
		stagesByName:    b.stagesByName,
		stageInfo:       stageInfo,
		graph:           graph,
		buildArgs:       b.buildArgs,
		targetStageName: targetStageName,
		finalStageIndex: finalStageIdx,
	}
}

func applyDefaultShellSemantics(info *StageInfo, stage *instructions.Stage, effectiveBaseName string) {
	if info.BaseImageOS == BaseImageOSUnknown {
		info.BaseImageOS = inferStageOSHeuristically(stage)
	}

	// Set OS-appropriate default shell after image/directive/heuristic
	// detection. Windows containers default to cmd.exe, not /bin/sh.
	if info.BaseImageOS == BaseImageOSWindows && info.ShellSetting.Source == ShellSourceDefault {
		info.ShellSetting = ShellSetting{
			Shell:   DefaultWindowsShell(),
			Variant: shell.VariantCmd,
			Source:  ShellSourceDefault,
			Line:    -1,
		}
	}

	// Known PowerShell images (mcr.microsoft.com/powershell:*) ship with
	// pwsh as the default shell. Set it before the POSIX refinement so
	// PowerShell takes priority. On Windows PowerShell images the shell
	// executable is "powershell" (Windows PowerShell); on Linux it is "pwsh".
	if info.ShellSetting.Source == ShellSourceDefault &&
		isPowerShellImageName(effectiveBaseName) {
		exe := "pwsh"
		if info.BaseImageOS == BaseImageOSWindows {
			exe = windowsPowerShellExe
		}
		info.ShellSetting = ShellSetting{
			Shell:   []string{exe, "-Command"},
			Variant: shell.VariantPowerShell,
			Source:  ShellSourceDefault,
			Line:    -1,
		}
	}

	// Refine the default shell variant for Linux distros whose /bin/sh
	// is a strict POSIX shell (dash or ash) rather than bash.
	// The default is VariantBash (correct for most distros); narrow to
	// VariantPOSIX only when the base image is a known dash/ash distro.
	if info.ShellSetting.Source == ShellSourceDefault &&
		info.ShellSetting.Variant == shell.VariantBash &&
		isPOSIXShellDistro(effectiveBaseName) {
		info.ShellSetting.Variant = shell.VariantPOSIX
		info.DashDefault = IsDashDefaultShell(effectiveBaseName)
	}
}

type fromArgEval struct {
	shlex        *dfshell.Lex
	defaultsEnv  *fromEnv
	effectiveEnv *fromEnv
	defaultsOK   bool
	effectiveOK  bool
	knownKeys    []string
	knownSet     map[string]struct{}
}

func resolveFromEvalWord(word string, eval fromArgEval) string {
	if word == "" || !eval.effectiveOK || eval.shlex == nil || eval.effectiveEnv == nil {
		return word
	}

	res, err := eval.shlex.ProcessWordWithMatches(word, eval.effectiveEnv)
	if err != nil || len(res.Unmatched) != 0 || res.Result == "" {
		return word
	}
	return res.Result
}

func (b *Builder) initFromArgEval(stages []instructions.Stage, metaArgs []instructions.ArgCommand) fromArgEval {
	// BuildKit-style word expander for ARG evaluation in FROM/meta scope.
	escapeToken := rune('\\')
	if b.parseResult != nil && b.parseResult.AST != nil {
		escapeToken = b.parseResult.AST.EscapeToken
	}
	shlex := dfshell.NewLex(escapeToken)

	// Automatic platform ARGs are available in FROM without explicit declaration.
	// Match BuildKit behavior by seeding:
	// - defaultsEnv with the automatic args without --build-arg overrides
	// - effectiveEnv and the semantic global scope with override-aware values
	targetStage := effectiveTargetStageName(stages, b.targetStage)
	autoArgsNoOverrides := defaultFromArgs(targetStage, nil)
	autoArgsWithOverrides := defaultFromArgs(targetStage, b.buildArgs)
	b.addAutoArgsToGlobalScope(autoArgsWithOverrides)

	// Build environments for FROM evaluation.
	// - defaultsEnv: automatic args + meta ARG defaults only (no --build-arg overrides)
	// - effectiveEnv: automatic args + meta ARG defaults + --build-arg overrides
	defaultsEnv := newFromEnv(autoArgsNoOverrides)
	effectiveEnv := newFromEnv(autoArgsWithOverrides)

	defaultsOK, effectiveOK := b.processMetaArgsForFrom(metaArgs, shlex, defaultsEnv, effectiveEnv)
	knownKeys, knownSet := scopeArgKeys(b.globalScope)

	return fromArgEval{
		shlex:        shlex,
		defaultsEnv:  defaultsEnv,
		effectiveEnv: effectiveEnv,
		defaultsOK:   defaultsOK,
		effectiveOK:  effectiveOK,
		knownKeys:    knownKeys,
		knownSet:     knownSet,
	}
}

func effectiveTargetStageName(stages []instructions.Stage, override string) string {
	if override != "" {
		return override
	}
	targetStage := defaultTargetStageName
	if len(stages) > 0 && stages[len(stages)-1].Name != "" {
		targetStage = stages[len(stages)-1].Name
	}
	return targetStage
}

func targetStageIndex(stages []instructions.Stage, override string) int {
	if len(stages) == 0 {
		return -1
	}
	if override != "" {
		normalized := normalizeStageRef(override)
		for i := range stages {
			if normalizeStageRef(stages[i].Name) == normalized {
				return i
			}
		}
	}
	return len(stages) - 1
}

func (b *Builder) addAutoArgsToGlobalScope(autoArgs map[string]string) {
	// Add automatic args to the global scope in deterministic order.
	autoKeys := make([]string, 0, len(autoArgs))
	for k := range autoArgs {
		autoKeys = append(autoKeys, k)
	}
	slices.Sort(autoKeys)
	for _, k := range autoKeys {
		v := autoArgs[k]
		b.globalScope.AddArg(k, &v, nil)
	}
}

func (b *Builder) processMetaArgsForFrom(
	metaArgs []instructions.ArgCommand,
	shlex *dfshell.Lex,
	defaultsEnv, effectiveEnv *fromEnv,
) (bool, bool) {
	defaultsOK := true
	effectiveOK := true

	// Process global ARGs (before first FROM) with proper default expansion.
	for i := range metaArgs {
		cmd := &metaArgs[i]
		for _, kv := range cmd.Args {
			effectiveVal, ok := effectiveArgValue(kv.Key, kv.Value, b.buildArgs, shlex, effectiveEnv)
			if !ok {
				effectiveOK = false
			}

			defaultVal, ok := defaultArgValue(kv.Value, shlex, defaultsEnv)
			if !ok {
				defaultsOK = false
			}

			// Record in global semantic scope using the effective value.
			// If this ARG has no value, VariableScope preserves any previously-set
			// value, matching Docker/BuildKit semantics.
			b.globalScope.AddArg(kv.Key, effectiveVal, cmd.Location())

			// Update FROM evaluation envs only when a value is set.
			if effectiveVal != nil {
				effectiveEnv.Set(kv.Key, *effectiveVal)
			}
			if defaultVal != nil {
				defaultsEnv.Set(kv.Key, *defaultVal)
			}
		}
	}

	return defaultsOK, effectiveOK
}

func effectiveArgValue(key string, value *string, buildArgs map[string]string, shlex *dfshell.Lex, env *fromEnv) (*string, bool) {
	// Compute effective value (build-arg override > default expansion).
	if buildArgs != nil {
		if v, ok := buildArgs[key]; ok {
			vv := v
			return &vv, true
		}
	}

	if value == nil {
		return nil, true
	}

	res, err := shlex.ProcessWordWithMatches(*value, env)
	if err != nil {
		return nil, false
	}
	vv := res.Result
	return &vv, true
}

func defaultArgValue(value *string, shlex *dfshell.Lex, env *fromEnv) (*string, bool) {
	// Compute defaults-only value (default expansion only).
	if value == nil {
		return nil, true
	}
	res, err := shlex.ProcessWordWithMatches(*value, env)
	if err != nil {
		return nil, false
	}
	vv := res.Result
	return &vv, true
}

func (b *Builder) applyFromArgAnalysis(info *StageInfo, stage *instructions.Stage, eval fromArgEval) {
	if eval.effectiveOK {
		info.FromArgs.UndefinedBaseName = undefinedFromArgs(
			stage.BaseName,
			eval.shlex,
			eval.effectiveEnv,
			eval.knownSet,
			eval.knownKeys,
		)
		if stage.Platform != "" {
			info.FromArgs.UndefinedPlatform = undefinedFromArgs(
				stage.Platform,
				eval.shlex,
				eval.effectiveEnv,
				eval.knownSet,
				eval.knownKeys,
			)
		}
	}

	if eval.defaultsOK {
		invalid, err := invalidDefaultBaseName(stage.BaseName, eval.shlex, eval.defaultsEnv)
		if err == nil {
			info.FromArgs.InvalidDefaultBaseName = invalid
		}
	}
}

// applyShellDirectives applies shell directives that appear before the stage's FROM.
// The most recent directive before FROM wins.
func (b *Builder) applyShellDirectives(stage *instructions.Stage, info *StageInfo) {
	if len(b.shellDirectives) == 0 {
		return
	}

	// Get the line number of this stage's FROM instruction (0-based)
	var fromLine int
	if len(stage.Location) > 0 {
		fromLine = stage.Location[0].Start.Line - 1 // Convert 1-based to 0-based
	}

	// Find the most recent shell directive before FROM
	var activeDirective *ShellDirective
	for i := range b.shellDirectives {
		sd := &b.shellDirectives[i]
		if sd.Line < fromLine && (activeDirective == nil || sd.Line > activeDirective.Line) {
			activeDirective = sd
		}
	}

	if activeDirective != nil {
		// Apply the directive: set both variant and shell name so that
		// ShellNameAtLine can return the directive's shell for dialect
		// selection in downstream consumers (shellcheck, highlight).
		info.ShellSetting.Variant = shell.VariantFromShell(activeDirective.Shell)
		info.ShellSetting.Source = ShellSourceDirective
		info.ShellSetting.Line = activeDirective.Line
		info.ShellSetting.Shell = []string{activeDirective.Shell, "-c"}
	}
}

// buildShellLookupsByLine pre-computes the effective shell variant and shell
// executable name at each instruction's start line within a stage, tracking
// SHELL instruction transitions.
func buildShellLookupsByLine(stage *instructions.Stage, info *StageInfo) {
	activeVariant := info.ShellSetting.Variant
	activeName := DefaultShell[0]
	if len(info.ShellSetting.Shell) > 0 {
		activeName = info.ShellSetting.Shell[0]
	}

	info.shellVariantByLine = make(map[int]shell.Variant, len(stage.Commands))
	info.shellNameByLine = make(map[int]string, len(stage.Commands))

	for _, cmd := range stage.Commands {
		if locs := cmd.Location(); len(locs) > 0 && locs[0].Start.Line > 0 {
			line := locs[0].Start.Line
			info.shellVariantByLine[line] = activeVariant
			info.shellNameByLine[line] = activeName
		}
		if sc, ok := cmd.(*instructions.ShellCommand); ok && len(sc.Shell) > 0 {
			activeVariant = shell.VariantFromShellCmd(sc.Shell)
			activeName = sc.Shell[0]
		}
	}
}

// processStageNaming registers stage names for stage reference resolution.
func (b *Builder) processStageNaming(stage *instructions.Stage, index int) {
	if stage.Name == "" {
		return
	}

	normalized := normalizeStageRef(stage.Name)

	if _, exists := b.stagesByName[normalized]; !exists {
		b.stagesByName[normalized] = index
	}
}

// processBaseImage analyzes the FROM instruction's base image.
func (b *Builder) processBaseImage(stage *instructions.Stage, stageIndex int, graph *StageGraph) *BaseImageRef {
	ref := &BaseImageRef{
		Raw:      stage.BaseName,
		Platform: stage.Platform,
		Location: stage.Location,
	}

	// Check if base name references another stage
	normalized := normalizeStageRef(stage.BaseName)
	if idx, found := b.stagesByName[normalized]; found {
		ref.IsStageRef = true
		ref.StageIndex = idx
		// FROM another stage creates a base dependency - track it in the graph
		// This is important for reachability analysis
		graph.addDependency(idx, stageIndex)
	} else {
		ref.StageIndex = -1
	}

	return ref
}

// processShellCommand updates the stage's shell setting from a SHELL instruction
// and strengthens BaseImageOS when the shell is Windows-specific.
func (b *Builder) processShellCommand(c *instructions.ShellCommand, info *StageInfo) {
	shellCmd := make([]string, len(c.Shell))
	copy(shellCmd, c.Shell)

	shellLine := -1
	if len(c.Location()) > 0 {
		shellLine = c.Location()[0].Start.Line - 1 // Convert 1-based to 0-based
	}
	variant := shell.VariantFromShellCmd(shellCmd)
	info.ShellSetting = ShellSetting{
		Shell:   shellCmd,
		Variant: variant,
		Source:  ShellSourceInstruction,
		Line:    shellLine,
	}

	// SHELL ["powershell"...] or SHELL ["cmd"...] is a Windows signal.
	// pwsh is cross-platform and must not imply Windows on its own.
	if info.BaseImageOS == BaseImageOSUnknown && len(shellCmd) > 0 {
		switch shell.NormalizeShellExecutableName(shellCmd[0]) {
		case windowsCmdShellName, windowsPowerShellExe:
			info.BaseImageOS = BaseImageOSWindows
		}
	}
}

// processStageCommands analyzes commands within a stage.
func (b *Builder) processStageCommands(stage *instructions.Stage, info *StageInfo, graph *StageGraph, env *fromEnv, shlex *dfshell.Lex) {
	declaredArgs := make(map[string]struct{})

	buildShellLookupsByLine(stage, info)

	for _, cmd := range stage.Commands {
		// UndefinedVar analysis must observe the environment at the point of use,
		// before this command mutates the environment.
		switch c := cmd.(type) {
		case *instructions.ArgCommand:
			info.UndefinedVars = append(
				info.UndefinedVars,
				applyArgCommandToEnv(c, shlex, env, declaredArgs, b.buildArgs, b.globalScope)...,
			)
		default:
			info.UndefinedVars = append(info.UndefinedVars, undefinedVarsInCommand(cmd, shlex, env, declaredArgs)...)
		}

		switch c := cmd.(type) {
		case *instructions.ArgCommand:
			info.Variables.AddArgCommand(c)

		case *instructions.EnvCommand:
			applyEnvCommandToEnv(c, shlex, env)
			info.Variables.AddEnvCommand(c)

		case *instructions.ShellCommand:
			b.processShellCommand(c, info)

		case *instructions.RunCommand:
			// Extract package installations from RUN commands
			b.extractPackageInstalls(c, info)

			// Track mount-based stage dependencies (RUN --mount=from=...).
			b.processMountDependencies(c, info.Index, graph)

			// Detect heredoc shebang for per-instruction shell override.
			if len(c.Files) > 0 && c.Files[0].Data != "" {
				firstLine, _, _ := strings.Cut(c.Files[0].Data, "\n")
				if shellName, ok := shell.ShellFromShebang(firstLine); ok {
					line := 0
					if locs := c.Location(); len(locs) > 0 {
						line = locs[0].Start.Line
					}
					info.HeredocShellOverrides = append(info.HeredocShellOverrides, HeredocShellOverride{
						Line:    line,
						Shell:   shellName,
						Variant: shell.VariantFromShell(shellName),
					})
				}
			}

		case *instructions.CopyCommand:
			if c.From != "" {
				copyRef := b.processCopyFrom(c, info.Index, graph)
				info.CopyFromRefs = append(info.CopyFromRefs, copyRef)
			}

		case *instructions.OnbuildCommand:
			b.processOnbuildCommand(c, info)
		}
	}
}

// processOnbuildCommand parses an ONBUILD expression into a typed command and
// stores it in the stage info. ONBUILD instructions execute when the image is
// used as a base for another build, not in the current build.
func (b *Builder) processOnbuildCommand(c *instructions.OnbuildCommand, info *StageInfo) {
	sourceLine := 0
	if loc := c.Location(); len(loc) > 0 {
		sourceLine = loc[0].Start.Line
	}

	parsed := parseOnbuildExpression(c.Expression, sourceLine)
	if parsed == nil {
		return
	}

	info.OnbuildInstructions = append(info.OnbuildInstructions, OnbuildInstruction{
		Command:    parsed,
		SourceLine: sourceLine,
	})

	// Extract COPY --from references from parsed ONBUILD commands
	if copyCmd, ok := parsed.(*instructions.CopyCommand); ok && copyCmd.From != "" {
		copyRef := b.processOnbuildCopyFrom(copyCmd, info.Index)
		info.OnbuildCopyFromRefs = append(info.OnbuildCopyFromRefs, copyRef)
	}
}

// processCopyFrom analyzes a COPY --from reference.
func (b *Builder) processCopyFrom(cmd *instructions.CopyCommand, stageIndex int, graph *StageGraph) CopyFromRef {
	ref := CopyFromRef{
		From:     cmd.From,
		Command:  cmd,
		Location: cmd.Location(),
	}

	// Try to resolve as stage reference
	// First check if it's a numeric index
	if idx, err := strconv.Atoi(cmd.From); err == nil {
		// Numeric reference
		if idx >= 0 && idx < graph.stageCount && idx < stageIndex {
			ref.IsStageRef = true
			ref.StageIndex = idx
			graph.addDependency(idx, stageIndex)
		} else {
			// Invalid numeric reference - will be caught by other rules
			ref.StageIndex = -1
		}
	} else {
		// Named reference
		normalized := normalizeStageRef(cmd.From)
		if idx, found := b.stagesByName[normalized]; found && idx < stageIndex {
			ref.IsStageRef = true
			ref.StageIndex = idx
			graph.addDependency(idx, stageIndex)
		} else if _, found := b.stagesByName[normalized]; found {
			// Forward reference to a later stage - invalid but not external
			ref.StageIndex = -1
		} else {
			// External image reference
			ref.StageIndex = -1
			graph.addExternalRef(stageIndex, cmd.From)
		}
	}

	return ref
}

// processMountDependencies tracks stage dependencies from RUN --mount=from=... references.
func (b *Builder) processMountDependencies(cmd *instructions.RunCommand, stageIndex int, graph *StageGraph) {
	for _, m := range runmount.GetMounts(cmd) {
		if m.From == "" {
			continue
		}

		// Try to resolve as numeric index first.
		if idx, err := strconv.Atoi(m.From); err == nil {
			if idx >= 0 && idx < graph.stageCount && idx < stageIndex {
				graph.addDependency(idx, stageIndex)
			}
			continue
		}

		// Named reference.
		normalized := normalizeStageRef(m.From)
		if idx, found := b.stagesByName[normalized]; found && idx < stageIndex {
			graph.addDependency(idx, stageIndex)
		}
	}
}

// resolveForwardRefs resolves stage references that couldn't be resolved during
// the main single-pass build because the referenced stage hadn't been registered yet.
// This handles COPY --from=<later-stage> and RUN --mount from=<later-stage>,
// which are valid in BuildKit (resolved at LLB construction time).
func (b *Builder) resolveForwardRefs(stages []instructions.Stage, stageInfo []*StageInfo, graph *StageGraph) {
	for i, info := range stageInfo {
		if info == nil {
			continue
		}

		// Resolve forward COPY --from references.
		for j := range info.CopyFromRefs {
			ref := &info.CopyFromRefs[j]
			if ref.IsStageRef {
				continue // already resolved
			}
			idx := b.resolveStageRef(ref.From, i)
			if idx < 0 {
				continue
			}
			ref.IsStageRef = true
			ref.StageIndex = idx
			graph.addDependency(idx, i)
		}

		// Resolve forward RUN --mount from= references.
		for _, cmd := range stages[i].Commands {
			run, ok := cmd.(*instructions.RunCommand)
			if !ok {
				continue
			}
			for _, m := range runmount.GetMounts(run) {
				if m.From == "" {
					continue
				}
				idx := b.resolveStageRef(m.From, i)
				if idx < 0 || idx < i {
					continue // already handled in first pass or not a stage
				}
				graph.addDependency(idx, i)
			}
		}

		// Resolve forward FROM base references.
		// FROM forward refs are invalid in Docker, but modeling them
		// allows cycle detection to catch the error.
		if info.BaseImage != nil && !info.BaseImage.IsStageRef {
			idx := b.resolveStageRef(info.BaseImage.Raw, i)
			if idx >= 0 && idx > i { // only forward; backward already handled
				info.BaseImage.IsStageRef = true
				info.BaseImage.StageIndex = idx
				graph.addDependency(idx, i)
			}
		}
	}
}

// resolveStageRef attempts to resolve a stage reference string to a stage index.
// Returns the stage index, or -1 if not resolvable.
// Excludes self-references (returns -1 if resolved index equals stageIndex).
func (b *Builder) resolveStageRef(ref string, stageIndex int) int {
	stageCount := len(b.parseResult.Stages)

	// Try numeric index first.
	if idx, err := strconv.Atoi(ref); err == nil {
		if idx >= 0 && idx < stageCount && idx != stageIndex {
			return idx
		}
		return -1
	}

	// Named reference.
	normalized := normalizeStageRef(ref)
	if idx, found := b.stagesByName[normalized]; found && idx != stageIndex {
		return idx
	}
	return -1
}

// processOnbuildCopyFrom analyzes a COPY --from reference from an ONBUILD instruction.
// Unlike processCopyFrom, this does NOT add edges to the graph because ONBUILD
// instructions only execute when the image is used as a base for another build.
func (b *Builder) processOnbuildCopyFrom(cmd *instructions.CopyCommand, stageIndex int) CopyFromRef {
	ref := CopyFromRef{
		From:     cmd.From,
		Command:  cmd,
		Location: cmd.Location(),
	}

	// Try to resolve as stage reference (for informational purposes only)
	if idx, err := strconv.Atoi(cmd.From); err == nil {
		// Numeric reference
		if idx >= 0 && idx < stageIndex {
			ref.IsStageRef = true
			ref.StageIndex = idx
		} else {
			ref.StageIndex = -1
		}
	} else {
		// Named reference
		normalized := normalizeStageRef(cmd.From)
		if idx, found := b.stagesByName[normalized]; found && idx < stageIndex {
			ref.IsStageRef = true
			ref.StageIndex = idx
		} else {
			ref.StageIndex = -1
		}
	}

	return ref
}

// parseOnbuildExpression parses an ONBUILD expression string into a typed
// instructions.Command by wrapping it in a minimal Dockerfile and parsing
// with BuildKit. sourceLine is the 1-based line number of the original
// ONBUILD instruction; the parsed command's Location() will report this line.
// Returns nil if parsing fails or the expression is not a recognized instruction.
func parseOnbuildExpression(expr string, sourceLine int) instructions.Command {
	// Parse by wrapping in a minimal Dockerfile
	dummyDockerfile := "FROM scratch\n" + expr + "\n"
	result, err := parser.Parse(strings.NewReader(dummyDockerfile))
	if err != nil {
		return nil
	}

	// Patch the AST node's location to the original ONBUILD line before
	// instructions.Parse bakes it into the command. The expression node is
	// the second child (index 1, after "FROM scratch").
	if sourceLine > 0 && len(result.AST.Children) >= 2 {
		node := result.AST.Children[1]
		node.StartLine = sourceLine
		node.EndLine = sourceLine
	}

	stages, _, err := instructions.Parse(result.AST, nil)
	if err != nil || len(stages) == 0 {
		return nil
	}

	// Return the first (and only) command from the parsed stage
	if len(stages[0].Commands) > 0 {
		return stages[0].Commands[0]
	}

	return nil
}

// normalizeStageRef normalizes a stage reference for comparison.
// Stage names are case-insensitive in Docker.
func normalizeStageRef(name string) string {
	return strings.ToLower(name)
}

// extractPackageInstalls extracts package installations from a RUN command.
func (b *Builder) extractPackageInstalls(run *instructions.RunCommand, info *StageInfo) {
	// Build the full command string including heredocs
	var cmdBuilder strings.Builder
	cmdBuilder.WriteString(strings.Join(run.CmdLine, " "))
	for _, f := range run.Files {
		cmdBuilder.WriteByte('\n')
		cmdBuilder.WriteString(f.Data)
	}
	cmdStr := cmdBuilder.String()

	// Extract package installations using the shell parser
	installs := shell.ExtractPackageInstalls(cmdStr, info.ShellSetting.Variant)

	// Get the line number for this RUN command
	line := 0
	if len(run.Location()) > 0 {
		line = run.Location()[0].Start.Line
	}

	// Convert shell.PackageInstallInfo to semantic.PackageInstall
	for _, install := range installs {
		info.InstalledPackages = append(info.InstalledPackages, PackageInstall{
			Manager:  install.Manager,
			Packages: install.Packages,
			Line:     line,
		})
	}
}
