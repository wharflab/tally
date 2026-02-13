package semantic

import (
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"
	"github.com/moby/buildkit/frontend/dockerfile/parser"
	dfshell "github.com/moby/buildkit/frontend/dockerfile/shell"

	"github.com/tinovyatkin/tally/internal/directive"
	"github.com/tinovyatkin/tally/internal/dockerfile"
	"github.com/tinovyatkin/tally/internal/rules"
	"github.com/tinovyatkin/tally/internal/shell"
)

// Builder constructs a semantic model from a parse result.
// It performs single-pass analysis and accumulates violations.
type Builder struct {
	parseResult     *dockerfile.ParseResult
	buildArgs       map[string]string
	file            string
	shellDirectives []directive.ShellDirective

	// Accumulated during build
	issues       []Issue
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
func (b *Builder) WithShellDirectives(directives []directive.ShellDirective) *Builder {
	b.shellDirectives = directives
	return b
}

// Build constructs the semantic model.
// This performs single-pass analysis of the Dockerfile, detecting
// construction-time violations (e.g., instruction order issues).
func (b *Builder) Build() *Model {
	if b.parseResult == nil {
		return &Model{
			stagesByName: make(map[string]int),
			graph:        newStageGraph(0),
		}
	}

	// Construction-time semantic issues (based on AST, not just parsed instructions).
	// Note: This must run even if instruction parsing was sanitized to continue.
	b.checkDL3061InstructionOrder()
	b.checkDL3043ForbiddenOnbuildTriggers()

	stages := b.parseResult.Stages
	metaArgs := b.parseResult.MetaArgs

	fromEval := b.initFromArgEval(stages, metaArgs)

	// Build stage info and graph
	stageCount := len(stages)
	stageInfo := make([]*StageInfo, stageCount)
	graph := newStageGraph(stageCount)

	for i := range stages {
		stage := &stages[i]
		isLast := i == stageCount-1

		// Create stage info
		info := newStageInfo(i, stage, isLast)
		info.Variables = NewStageScope(b.globalScope)

		// Process stage name
		b.processStageNaming(stage, i)

		// Process base image
		info.BaseImage = b.processBaseImage(stage, i, graph)

		// FROM ARG analysis (UndefinedArgInFrom, InvalidDefaultArgInFrom).
		b.applyFromArgAnalysis(info, stage, fromEval)

		// Apply shell directives that appear before this stage's FROM instruction
		b.applyShellDirectives(stage, info)

		// Seed the environment used for undefined-var analysis.
		var stageEnv *fromEnv
		switch {
		case stage.BaseName == "scratch":
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

	return &Model{
		stages:       stages,
		metaArgs:     metaArgs,
		stagesByName: b.stagesByName,
		stageInfo:    stageInfo,
		graph:        graph,
		buildArgs:    b.buildArgs,
		file:         b.file,
		issues:       b.issues,
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
	targetStage := targetStageName(stages)
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

func targetStageName(stages []instructions.Stage) string {
	targetStage := defaultTargetStageName
	if len(stages) > 0 && stages[len(stages)-1].Name != "" {
		targetStage = stages[len(stages)-1].Name
	}
	return targetStage
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
	var activeDirective *directive.ShellDirective
	for i := range b.shellDirectives {
		sd := &b.shellDirectives[i]
		if sd.Line < fromLine && (activeDirective == nil || sd.Line > activeDirective.Line) {
			activeDirective = sd
		}
	}

	if activeDirective != nil {
		// Apply the directive (it only hints at shell variant for linting).
		info.ShellSetting.Variant = shell.VariantFromShell(activeDirective.Shell)
		info.ShellSetting.Source = ShellSourceDirective
		info.ShellSetting.Line = activeDirective.Line
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

// checkDuplicateInstruction reports a MultipleInstructionsDisallowed violation for the
// previous location when a duplicate instruction is found. Returns the updated location.
// Following BuildKit convention: the previous instruction is reported (the one Docker ignores).
func (b *Builder) checkDuplicateInstruction(prevLoc *parser.Range, cmd instructions.Command) *parser.Range {
	instrName := cmd.Name()
	if prevLoc != nil {
		issue := newIssue(
			b.file,
			*prevLoc,
			rules.BuildKitRulePrefix+"MultipleInstructionsDisallowed",
			"Multiple "+instrName+" instructions should not be used in the same stage because only the last one will be used",
			"https://docs.docker.com/go/dockerfile/rule/multiple-instructions-disallowed/",
		)
		issue.Severity = rules.SeverityWarning
		b.issues = append(b.issues, issue)
	}
	if ranges := cmd.Location(); len(ranges) > 0 {
		loc := ranges[0]
		return &loc
	}
	return prevLoc
}

// processStageCommands analyzes commands within a stage.
func (b *Builder) processStageCommands(stage *instructions.Stage, info *StageInfo, graph *StageGraph, env *fromEnv, shlex *dfshell.Lex) {
	var lastCmdLoc, lastEntrypointLoc, lastHealthcheckLoc *parser.Range
	normalizedStageName := normalizeStageRef(stage.Name)

	declaredArgs := make(map[string]struct{})

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

		case *instructions.CmdCommand:
			lastCmdLoc = b.checkDuplicateInstruction(lastCmdLoc, c)

		case *instructions.EntrypointCommand:
			lastEntrypointLoc = b.checkDuplicateInstruction(lastEntrypointLoc, c)

		case *instructions.HealthCheckCommand:
			lastHealthcheckLoc = b.checkDuplicateInstruction(lastHealthcheckLoc, c)

		case *instructions.ShellCommand:
			// Update active shell for this stage.
			shellCmd := make([]string, len(c.Shell))
			copy(shellCmd, c.Shell)

			// Also update ShellSetting.
			shellLine := -1
			if len(c.Location()) > 0 {
				shellLine = c.Location()[0].Start.Line - 1 // Convert 1-based to 0-based
			}
			info.ShellSetting = ShellSetting{
				Shell:   shellCmd,
				Variant: shell.VariantFromShellCmd(shellCmd),
				Source:  ShellSourceInstruction,
				Line:    shellLine,
			}

		case *instructions.RunCommand:
			// Extract package installations from RUN commands
			b.extractPackageInstalls(c, info)

		case *instructions.CopyCommand:
			if c.From != "" {
				// DL3023: COPY --from cannot reference its own FROM alias.
				if stage.Name != "" && normalizeStageRef(c.From) == normalizedStageName {
					var loc parser.Range
					if ranges := c.Location(); len(ranges) > 0 {
						loc = ranges[0]
					}
					b.issues = append(b.issues, newIssue(
						b.file,
						loc,
						rules.HadolintRulePrefix+"DL3023",
						"`COPY --from` cannot reference its own `FROM` alias",
						"https://github.com/hadolint/hadolint/wiki/DL3023",
					))
				}
				copyRef := b.processCopyFrom(c, info.Index, graph)
				info.CopyFromRefs = append(info.CopyFromRefs, copyRef)

				// DL3022: COPY --from should reference a previously defined FROM alias.
				// If the reference didn't resolve to a stage and doesn't look like
				// an external image (contains ":"), it's an undefined reference.
				if !copyRef.IsStageRef && !strings.Contains(c.From, ":") {
					var loc parser.Range
					if ranges := c.Location(); len(ranges) > 0 {
						loc = ranges[0]
					}
					b.issues = append(b.issues, newIssue(
						b.file,
						loc,
						rules.HadolintRulePrefix+"DL3022",
						"`COPY --from` should reference a previously defined `FROM` alias",
						"https://github.com/hadolint/hadolint/wiki/DL3022",
					))
				}
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

// checkDL3061InstructionOrder detects DL3061: Invalid instruction order.
// Dockerfile must begin with FROM, ARG, or comment.
//
// We detect this from the raw AST because BuildKit's instruction parser may
// reject invalid order and prevent downstream linting.
func (b *Builder) checkDL3061InstructionOrder() {
	if b.parseResult == nil || b.parseResult.AST == nil || b.parseResult.AST.AST == nil {
		return
	}

	nodes := topLevelInstructionNodes(b.parseResult.AST.AST)
	for _, node := range nodes {
		if node == nil {
			continue
		}
		if strings.EqualFold(node.Value, "FROM") {
			return
		}
		if strings.EqualFold(node.Value, "ARG") {
			continue
		}

		var loc parser.Range
		if ranges := node.Location(); len(ranges) > 0 {
			loc = ranges[0]
		}
		b.issues = append(b.issues, newIssue(
			b.file,
			loc,
			rules.HadolintRulePrefix+"DL3061",
			"Invalid instruction order. Dockerfile must begin with `FROM`, `ARG` or comment.",
			"https://github.com/hadolint/hadolint/wiki/DL3061",
		))
	}
}

// checkDL3043ForbiddenOnbuildTriggers detects DL3043: ONBUILD must not trigger
// ONBUILD/FROM/MAINTAINER.
//
// We detect this from the raw AST because BuildKit's instruction parser rejects
// these constructs, which would otherwise prevent semantic model construction.
func (b *Builder) checkDL3043ForbiddenOnbuildTriggers() {
	if b.parseResult == nil || b.parseResult.AST == nil || b.parseResult.AST.AST == nil {
		return
	}

	nodes := topLevelInstructionNodes(b.parseResult.AST.AST)
	for _, node := range nodes {
		if node == nil || !strings.EqualFold(node.Value, "ONBUILD") {
			continue
		}

		trigger := onbuildTriggerKeyword(node)
		if trigger == "" {
			continue
		}

		if strings.EqualFold(trigger, "ONBUILD") ||
			strings.EqualFold(trigger, "FROM") ||
			strings.EqualFold(trigger, "MAINTAINER") {
			var loc parser.Range
			if ranges := node.Location(); len(ranges) > 0 {
				loc = ranges[0]
			}
			b.issues = append(b.issues, newIssue(
				b.file,
				loc,
				rules.HadolintRulePrefix+"DL3043",
				"`ONBUILD`, `FROM` or `MAINTAINER` triggered from within `ONBUILD` instruction.",
				"https://github.com/hadolint/hadolint/wiki/DL3043",
			))
		}
	}
}

func topLevelInstructionNodes(root *parser.Node) []*parser.Node {
	if root == nil {
		return nil
	}

	// BuildKit parser stores Dockerfile instructions under root.Children.
	nodes := make([]*parser.Node, 0, len(root.Children))
	for _, child := range root.Children {
		if child != nil {
			nodes = append(nodes, child)
		}
	}

	sort.Slice(nodes, func(i, j int) bool {
		return nodes[i].StartLine < nodes[j].StartLine
	})

	return nodes
}

func onbuildTriggerKeyword(node *parser.Node) string {
	// BuildKit parser represents ONBUILD like: (ONBUILD (TRIGGER ...))
	if node == nil || node.Next == nil || len(node.Next.Children) == 0 || node.Next.Children[0] == nil {
		return ""
	}
	return node.Next.Children[0].Value
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
