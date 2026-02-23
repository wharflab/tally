package semantic

import "strconv"

// StageGraph represents the dependency graph between stages.
// It tracks cross-stage relationships (COPY --from and FROM stage refs)
// to enable reachability analysis.
type StageGraph struct {
	// dependencies maps stage index -> list of stages it depends on.
	edges map[int][]int

	// reverseEdges maps stage index -> list of stages that depend on it.
	reverseEdges map[int][]int

	// externalRefs maps stage index -> list of external image refs.
	externalRefs map[int][]string

	// stageCount is the total number of stages.
	stageCount int
}

// DependsOn returns true if stageA depends on stageB (directly or transitively).
// A stage depends on another if it copies from it (COPY --from) or uses it
// as a base image (FROM <stage>).
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

// DirectDependencies returns the stages that stageIndex directly depends on
// (via COPY --from or FROM stage refs).
func (g *StageGraph) DirectDependencies(stageIndex int) []int {
	return g.edges[stageIndex]
}

// DirectDependents returns the stages that directly depend on stageIndex.
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

// DetectCycles finds cycles in the stage dependency graph.
// It returns at least one cycle per strongly connected component;
// overlapping or shorter cycles within the same SCC may not all be reported.
// Each cycle is a slice of stage indices forming a directed cycle
// (e.g., [0, 2, 1] means stage 0 → stage 2 → stage 1 → stage 0).
// Returns nil if the graph is acyclic.
//
// The algorithm uses DFS with 3-color marking. Cycles are returned in
// canonical form: rotated so the smallest index is first, then deduplicated.
func (g *StageGraph) DetectCycles() [][]int {
	const (
		white = 0 // unvisited
		gray  = 1 // on current DFS path
		black = 2 // fully processed
	)

	color := make([]int, g.stageCount)
	path := make([]int, 0, g.stageCount)
	var cycles [][]int
	seen := make(map[string]bool) // canonical key → already recorded

	var dfs func(node int)
	dfs = func(node int) {
		color[node] = gray
		path = append(path, node)

		for _, dep := range g.edges[node] {
			switch color[dep] {
			case gray:
				// Back-edge: extract cycle from path.
				cycle := extractCycle(path, dep)
				key := canonicalCycleKey(cycle)
				if !seen[key] {
					seen[key] = true
					cycles = append(cycles, cycle)
				}
			case white:
				dfs(dep)
			}
		}

		path = path[:len(path)-1]
		color[node] = black
	}

	for i := range g.stageCount {
		if color[i] == white {
			dfs(i)
		}
	}

	return cycles
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

// addDependency records that stageIndex depends on depStage.
// The dependency can originate from either COPY --from or FROM <stage>.
func (g *StageGraph) addDependency(depStage, stageIndex int) {
	// edges: which stages does stageIndex depend on?
	g.edges[stageIndex] = append(g.edges[stageIndex], depStage)

	// reverseEdges: which stages depend on depStage?
	g.reverseEdges[depStage] = append(g.reverseEdges[depStage], stageIndex)
}

// addExternalRef records an external image reference in a stage.
func (g *StageGraph) addExternalRef(stageIndex int, ref string) {
	g.externalRefs[stageIndex] = append(g.externalRefs[stageIndex], ref)
}

// extractCycle extracts the cycle portion from the DFS path.
// target is the node where the back-edge points; path contains
// the current DFS stack including target somewhere.
func extractCycle(path []int, target int) []int {
	for i, node := range path {
		if node == target {
			cycle := make([]int, len(path)-i)
			copy(cycle, path[i:])
			return cycle
		}
	}
	return nil
}

// canonicalCycleKey produces a deterministic string key for a cycle
// by rotating it so the smallest index is first.
func canonicalCycleKey(cycle []int) string {
	if len(cycle) == 0 {
		return ""
	}

	// Find position of minimum element.
	minIdx := 0
	for i, v := range cycle {
		if v < cycle[minIdx] {
			minIdx = i
		}
	}

	// Build key from rotated cycle.
	n := len(cycle)
	buf := make([]byte, 0, n*4)
	for i := range n {
		if i > 0 {
			buf = append(buf, ',')
		}
		buf = strconv.AppendInt(buf, int64(cycle[(minIdx+i)%n]), 10)
	}
	return string(buf)
}
