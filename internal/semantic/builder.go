package semantic

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"
	"github.com/moby/buildkit/frontend/dockerfile/parser"

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
// construction-time violations like DL3024 (duplicate stage names).
func (b *Builder) Build() *Model {
	if b.parseResult == nil {
		return &Model{
			stagesByName: make(map[string]int),
			graph:        newStageGraph(0),
		}
	}

	stages := b.parseResult.Stages
	metaArgs := b.parseResult.MetaArgs

	// Process global ARGs (before first FROM)
	for i := range metaArgs {
		b.globalScope.AddArgCommand(&metaArgs[i])
	}

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

		// Apply shell directives that appear before this stage's FROM instruction
		b.applyShellDirectives(stage, info)

		// Process commands in the stage
		b.processStageCommands(stage, info, graph)

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
		if sd.Line < fromLine {
			activeDirective = sd
		}
	}

	if activeDirective != nil {
		// Apply the directive
		info.ShellSetting = ShellSetting{
			Shell:   info.Shell, // Keep the shell command array (directive only hints at variant)
			Variant: shell.VariantFromShell(activeDirective.Shell),
			Source:  ShellSourceDirective,
			Line:    activeDirective.Line,
		}
	}
}

// processStageNaming registers stage names and detects DL3024 (duplicate names).
func (b *Builder) processStageNaming(stage *instructions.Stage, index int) {
	if stage.Name == "" {
		return
	}

	normalized := normalizeStageRef(stage.Name)

	if existingIdx, exists := b.stagesByName[normalized]; exists {
		// DL3024: Duplicate stage name
		var loc parser.Range
		if len(stage.Location) > 0 {
			loc = stage.Location[0]
		}
		b.issues = append(b.issues, newIssue(
			b.file,
			loc,
			rules.HadolintRulePrefix+"DL3024",
			fmt.Sprintf("Stage name %q is already used on stage %d", stage.Name, existingIdx),
			"https://github.com/hadolint/hadolint/wiki/DL3024",
		))
	} else {
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
		graph.addEdge(idx, stageIndex)
	} else {
		ref.StageIndex = -1
	}

	return ref
}

// processStageCommands analyzes commands within a stage.
func (b *Builder) processStageCommands(stage *instructions.Stage, info *StageInfo, graph *StageGraph) {
	for _, cmd := range stage.Commands {
		switch c := cmd.(type) {
		case *instructions.ArgCommand:
			info.Variables.AddArgCommand(c)

		case *instructions.EnvCommand:
			info.Variables.AddEnvCommand(c)

		case *instructions.ShellCommand:
			// Update active shell for this stage
			info.Shell = make([]string, len(c.Shell))
			copy(info.Shell, c.Shell)

			// Also update ShellSetting
			shellLine := -1
			if len(c.Location()) > 0 {
				shellLine = c.Location()[0].Start.Line - 1 // Convert 1-based to 0-based
			}
			info.ShellSetting = ShellSetting{
				Shell:   info.Shell,
				Variant: shell.VariantFromShellCmd(c.Shell),
				Source:  ShellSourceInstruction,
				Line:    shellLine,
			}

		case *instructions.RunCommand:
			// Extract package installations from RUN commands
			b.extractPackageInstalls(c, info)

		case *instructions.CopyCommand:
			if c.From != "" {
				copyRef := b.processCopyFrom(c, info.Index, graph)
				info.CopyFromRefs = append(info.CopyFromRefs, copyRef)
			}

		case *instructions.OnbuildCommand:
			// Parse ONBUILD expression to extract COPY --from references
			// Note: ONBUILD instructions execute when image is used as a base for another build,
			// not in the current build, so we don't add edges to the graph here.
			if copyCmd := b.parseOnbuildCopy(c.Expression); copyCmd != nil {
				copyRef := b.processOnbuildCopyFrom(copyCmd, info.Index)
				info.OnbuildCopyFromRefs = append(info.OnbuildCopyFromRefs, copyRef)
			}
		}
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
			graph.addEdge(idx, stageIndex)
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
			graph.addEdge(idx, stageIndex)
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

// parseOnbuildCopy parses an ONBUILD expression to extract a COPY command.
// Returns nil if the expression is not a COPY with --from.
func (b *Builder) parseOnbuildCopy(expr string) *instructions.CopyCommand {
	// Parse by wrapping in a minimal Dockerfile
	dummyDockerfile := "FROM scratch\n" + expr + "\n"
	result, err := parser.Parse(strings.NewReader(dummyDockerfile))
	if err != nil {
		return nil
	}

	stages, _, err := instructions.Parse(result.AST, nil)
	if err != nil || len(stages) == 0 {
		return nil
	}

	// Extract the COPY command with --from
	for _, cmd := range stages[0].Commands {
		if copyCmd, ok := cmd.(*instructions.CopyCommand); ok && copyCmd.From != "" {
			return copyCmd
		}
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
