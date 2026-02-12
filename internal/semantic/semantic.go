// Package semantic provides a semantic model for Dockerfiles that enables
// cross-instruction analysis such as stage resolution, variable scoping,
// and COPY --from validation.
//
// The semantic model is built in a single pass from a ParseResult and is
// immutable after construction. Construction-time violations are accumulated
// and returned with the model.
package semantic

import (
	"github.com/moby/buildkit/frontend/dockerfile/instructions"
	dfshell "github.com/moby/buildkit/frontend/dockerfile/shell"

	"github.com/tinovyatkin/tally/internal/dockerfile"
)

// Model represents the semantic analysis of a Dockerfile.
// It provides O(1) lookups for stages, variable resolution with proper
// precedence, and dependency graph analysis for COPY --from.
//
// The model is immutable after construction. All methods are safe for
// concurrent read access.
type Model struct {
	// stages is a reference to the parsed stages (from BuildKit).
	stages []instructions.Stage

	// metaArgs contains global ARG instructions before the first FROM.
	metaArgs []instructions.ArgCommand

	// stagesByName provides O(1) lookup of stage index by name.
	// Only named stages are included.
	stagesByName map[string]int

	// stageInfo contains enhanced per-stage information.
	stageInfo []*StageInfo

	// graph tracks COPY --from dependencies between stages.
	graph *StageGraph

	// buildArgs are CLI --build-arg values.
	buildArgs map[string]string

	// file is the path to the Dockerfile (for violation locations).
	file string

	// issues accumulated during construction.
	issues []Issue
}

// NewModel creates a semantic model from a parse result.
// This is a convenience wrapper around NewBuilder().Build().
func NewModel(pr *dockerfile.ParseResult, buildArgs map[string]string, file string) *Model {
	return NewBuilder(pr, buildArgs, file).Build()
}

// StageCount returns the number of stages in the Dockerfile.
func (m *Model) StageCount() int {
	return len(m.stages)
}

// Stage returns the stage at the given index (0-based).
// Returns nil if the index is out of bounds.
func (m *Model) Stage(index int) *instructions.Stage {
	if index < 0 || index >= len(m.stages) {
		return nil
	}
	return &m.stages[index]
}

// StageByName returns the stage with the given name.
// Returns nil if no stage with that name exists.
// Stage names are case-insensitive per Docker semantics.
func (m *Model) StageByName(name string) *instructions.Stage {
	idx, found := m.StageIndexByName(name)
	if !found {
		return nil
	}
	return &m.stages[idx]
}

// StageIndexByName returns the index of the stage with the given name.
// Returns -1 and false if no stage with that name exists.
func (m *Model) StageIndexByName(name string) (int, bool) {
	// Stage names are stored lowercase in stagesByName
	idx, found := m.stagesByName[normalizeStageRef(name)]
	return idx, found
}

// StageInfo returns enhanced information for the stage at the given index.
// Returns nil if the index is out of bounds.
func (m *Model) StageInfo(index int) *StageInfo {
	if index < 0 || index >= len(m.stageInfo) {
		return nil
	}
	return m.stageInfo[index]
}

// ResolveVariable resolves a variable name in the context of a stage.
// Resolution precedence: BuildArgs > Stage ENV > Stage ARG > Global ARG.
// Returns the value and true if found, or empty string and false if not.
func (m *Model) ResolveVariable(stageIndex int, name string) (string, bool) {
	info := m.StageInfo(stageIndex)
	if info == nil {
		return "", false
	}
	return info.Variables.Resolve(name, m.buildArgs)
}

// Graph returns the stage dependency graph.
func (m *Model) Graph() *StageGraph {
	return m.graph
}

// ConstructionIssues returns issues detected during model construction.
// The caller should convert these to rules.Violation for output.
func (m *Model) ConstructionIssues() []Issue {
	return m.issues
}

// MetaArgs returns the global ARG instructions before the first FROM.
func (m *Model) MetaArgs() []instructions.ArgCommand {
	return m.metaArgs
}

// Stages returns all stages (read-only reference).
func (m *Model) Stages() []instructions.Stage {
	return m.stages
}

// ExternalImageStages returns an iterator over stages that use external images
// (not "scratch" and not referencing another stage in the Dockerfile).
// This is useful for rules that need to check image tags/versions.
func (m *Model) ExternalImageStages() func(yield func(*StageInfo) bool) {
	return func(yield func(*StageInfo) bool) {
		for _, info := range m.stageInfo {
			if info != nil && info.IsExternalImage() {
				if !yield(info) {
					return
				}
			}
		}
	}
}

// StageUndefinedVars groups undefined variable results by stage index.
type StageUndefinedVars struct {
	StageIdx int
	Undefs   []UndefinedVarRef
}

// RecheckUndefinedVars re-runs the undefined-var analysis for the specified stage
// and all stages that transitively inherit from it (via FROM <stage>).
// It uses the provided base image environment instead of the static approximation.
// This is used by the async pipeline when base image env has been resolved from
// the registry.
func (m *Model) RecheckUndefinedVars(stageIdx int, resolvedEnv map[string]string) []StageUndefinedVars {
	if stageIdx < 0 || stageIdx >= len(m.stageInfo) || stageIdx >= len(m.stages) {
		return nil
	}

	var results []StageUndefinedVars
	m.recheckStageChain(stageIdx, resolvedEnv, &results)
	return results
}

// recheckStageChain rechecks a single stage and recursively processes stages
// that inherit from it, propagating the effective env through the chain.
func (m *Model) recheckStageChain(stageIdx int, baseEnv map[string]string, results *[]StageUndefinedVars) {
	undefs, effectiveEnv := m.recheckSingleStage(stageIdx, baseEnv)
	*results = append(*results, StageUndefinedVars{StageIdx: stageIdx, Undefs: undefs})

	// Find stages that inherit from this one and recheck them too.
	for i, info := range m.stageInfo {
		if info != nil && info.BaseImage != nil && info.BaseImage.IsStageRef && info.BaseImage.StageIndex == stageIdx {
			m.recheckStageChain(i, effectiveEnv, results)
		}
	}
}

// recheckSingleStage re-runs the undefined-var analysis for a single stage
// and returns both the undefined vars and the effective env at stage end.
func (m *Model) recheckSingleStage(stageIdx int, baseEnv map[string]string) ([]UndefinedVarRef, map[string]string) {
	stage := &m.stages[stageIdx]

	// Seed environment with provided base env.
	env := newFromEnv(baseEnv)

	escapeToken := rune('\\')
	shlex := dfshell.NewLex(escapeToken)

	declaredArgs := make(map[string]struct{})

	var undefs []UndefinedVarRef
	for _, cmd := range stage.Commands {
		switch c := cmd.(type) {
		case *instructions.ArgCommand:
			undefs = append(undefs,
				applyArgCommandToEnv(c, shlex, env, declaredArgs, m.buildArgs, m.globalScope())...)
		default:
			undefs = append(undefs, undefinedVarsInCommand(cmd, shlex, env, declaredArgs)...)
		}

		// Apply env mutations (ENV instructions change the scope for subsequent commands).
		if ec, ok := cmd.(*instructions.EnvCommand); ok {
			applyEnvCommandToEnv(ec, shlex, env)
		}
	}

	return undefs, env.vars
}

// globalScope returns the builder's global scope, reconstructed from metaArgs.
func (m *Model) globalScope() *VariableScope {
	scope := NewGlobalScope()
	for _, ma := range m.metaArgs {
		for _, kv := range ma.Args {
			val := kv.Value
			if m.buildArgs != nil {
				if ov, ok := m.buildArgs[kv.Key]; ok {
					v := ov
					val = &v
				}
			}
			scope.AddArg(kv.Key, val, ma.Location())
		}
	}
	return scope
}
