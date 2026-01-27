// Package semantic provides a semantic model for Dockerfiles that enables
// cross-instruction analysis such as stage resolution, variable scoping,
// and COPY --from validation.
//
// The semantic model is built in a single pass from a ParseResult and is
// immutable after construction. Construction-time violations (e.g., DL3024
// for duplicate stage names) are accumulated and returned with the model.
package semantic

import (
	"github.com/moby/buildkit/frontend/dockerfile/instructions"

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

	// issues accumulated during construction (e.g., DL3024).
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
// These include semantic errors like duplicate stage names (DL3024).
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
