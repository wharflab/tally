package semantic

// StageGraph represents the dependency graph between stages.
// It tracks COPY --from relationships to enable reachability analysis.
type StageGraph struct {
	// edges maps stage index -> list of stages it copies from.
	edges map[int][]int

	// reverseEdges maps stage index -> list of stages that copy from it.
	reverseEdges map[int][]int

	// externalRefs maps stage index -> list of external image refs.
	externalRefs map[int][]string

	// stageCount is the total number of stages.
	stageCount int
}

// DependsOn returns true if stageA depends on stageB (directly or transitively).
// A stage depends on another if it copies from it (COPY --from) or if any
// stage it depends on copies from it.
func (g *StageGraph) DependsOn(stageA, stageB int) bool {
	// BFS to find if stageB is reachable from stageA's dependencies
	visited := make(map[int]bool)
	queue := []int{stageA}

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		if visited[current] {
			continue
		}
		visited[current] = true

		for _, dep := range g.edges[current] {
			if dep == stageB {
				return true
			}
			if !visited[dep] {
				queue = append(queue, dep)
			}
		}
	}
	return false
}

// IsReachable returns true if stageIndex is reachable from finalStageIndex.
// A stage is reachable if:
//  1. It is the final stage itself
//  2. The final stage (or any reachable stage) depends on it
//  3. It is a base image for a reachable stage
func (g *StageGraph) IsReachable(stageIndex, finalStageIndex int) bool {
	if stageIndex == finalStageIndex {
		return true
	}

	// BFS backwards from final stage
	visited := make(map[int]bool)
	queue := []int{finalStageIndex}

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		if visited[current] {
			continue
		}
		visited[current] = true

		// Check direct dependencies (COPY --from)
		for _, dep := range g.edges[current] {
			if dep == stageIndex {
				return true
			}
			if !visited[dep] {
				queue = append(queue, dep)
			}
		}
	}

	return false
}

// UnreachableStages returns indices of stages that are not reachable from the final stage.
// These are stages that don't contribute to the final image.
func (g *StageGraph) UnreachableStages() []int {
	if g.stageCount == 0 {
		return nil
	}

	finalStage := g.stageCount - 1
	var unreachable []int

	for i := range g.stageCount {
		if !g.IsReachable(i, finalStage) {
			unreachable = append(unreachable, i)
		}
	}

	return unreachable
}

// DirectDependencies returns the stages that stageIndex directly copies from.
func (g *StageGraph) DirectDependencies(stageIndex int) []int {
	return g.edges[stageIndex]
}

// DirectDependents returns the stages that directly copy from stageIndex.
func (g *StageGraph) DirectDependents(stageIndex int) []int {
	return g.reverseEdges[stageIndex]
}

// ExternalRefs returns the external image references in stageIndex.
func (g *StageGraph) ExternalRefs(stageIndex int) []string {
	return g.externalRefs[stageIndex]
}

// StageCount returns the total number of stages.
func (g *StageGraph) StageCount() int {
	return g.stageCount
}

// newStageGraph creates a new empty stage graph.
func newStageGraph(stageCount int) *StageGraph {
	return &StageGraph{
		edges:        make(map[int][]int),
		reverseEdges: make(map[int][]int),
		externalRefs: make(map[int][]string),
		stageCount:   stageCount,
	}
}

// addEdge adds a dependency from fromStage to toStage (toStage copies from fromStage).
func (g *StageGraph) addEdge(fromStage, toStage int) {
	// edges: which stages does toStage copy from?
	g.edges[toStage] = append(g.edges[toStage], fromStage)

	// reverseEdges: which stages copy from fromStage?
	g.reverseEdges[fromStage] = append(g.reverseEdges[fromStage], toStage)
}

// addExternalRef records an external image reference in a stage.
func (g *StageGraph) addExternalRef(stageIndex int, ref string) {
	g.externalRefs[stageIndex] = append(g.externalRefs[stageIndex], ref)
}
