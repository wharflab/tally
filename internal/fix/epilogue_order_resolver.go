package fix

import (
	"bytes"
	"cmp"
	"context"
	"slices"
	"strconv"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"
	"github.com/moby/buildkit/frontend/dockerfile/parser"

	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/sourcemap"
)

// epilogueOrderResolver implements FixResolver for epilogue-order fixes.
// It re-parses the modified content, determines applicable stages, and
// generates delete/insert edits to move epilogue instructions to the end
// of each stage in canonical order.
type epilogueOrderResolver struct{}

// ID returns the resolver identifier.
func (r *epilogueOrderResolver) ID() string {
	return rules.EpilogueOrderResolverID
}

// epilogueInstr tracks an epilogue instruction's location in the source.
type epilogueInstr struct {
	name      string // lowercase instruction name
	rank      int    // canonical order position
	startLine int    // 1-based, including preceding comments
	endLine   int    // 1-based, inclusive, accounting for continuations
}

// rankedText pairs instruction text with its canonical rank for sorting.
type rankedText struct {
	rank int
	text string
}

// Resolve re-parses the current content and generates all epilogue-reordering edits.
// Because this resolver produces the complete set of edits for the whole file,
// only the first invocation per file generates edits; subsequent calls find the
// content already correct and return nil.
func (r *epilogueOrderResolver) Resolve(
	_ context.Context,
	resolveCtx ResolveContext,
	fix *rules.SuggestedFix,
) ([]rules.TextEdit, error) {
	if _, ok := fix.ResolverData.(*rules.EpilogueOrderResolveData); !ok {
		return nil, nil
	}

	// Parse the modified content.
	dockerfile, err := parser.Parse(bytes.NewReader(resolveCtx.Content))
	if err != nil {
		return nil, nil //nolint:nilerr // Skip silently - don't fail fix process
	}

	stages, _, err := instructions.Parse(dockerfile.AST, nil)
	if err != nil {
		return nil, nil //nolint:nilerr // Skip silently - don't fail fix process
	}

	if len(stages) == 0 {
		return nil, nil
	}

	sm := sourcemap.New(resolveCtx.Content)
	dependents := buildDependentsMap(stages)

	var allEdits []rules.TextEdit

	for stageIdx, stage := range stages {
		isLast := stageIdx == len(stages)-1
		// Only process applicable stages: final stage or stages with no dependents.
		if !isLast && len(dependents[stageIdx]) > 0 {
			continue
		}

		edits := r.fixStage(stage, stageIdx, dockerfile, sm, resolveCtx.FilePath)
		allEdits = append(allEdits, edits...)
	}

	if len(allEdits) == 0 {
		return nil, nil
	}
	return allEdits, nil
}

// fixStage generates edits for a single stage that needs epilogue reordering.
func (r *epilogueOrderResolver) fixStage(
	stage instructions.Stage,
	stageIdx int,
	dockerfile *parser.Result,
	sm *sourcemap.SourceMap,
	file string,
) []rules.TextEdit {
	// Find AST children belonging to this stage.
	stageNodes := stageASTChildren(stageIdx, dockerfile)
	if len(stageNodes) == 0 {
		return nil
	}

	// Collect epilogue instructions with their source locations.
	var epilogues []epilogueInstr
	for i, cmd := range stage.Commands {
		if _, isOnbuild := cmd.(*instructions.OnbuildCommand); isOnbuild {
			continue
		}
		name := strings.ToLower(cmd.Name())
		rank, ok := rules.EpilogueOrderRank[name]
		if !ok {
			continue
		}

		// The node index in stageNodes is offset by 1 (first node is FROM).
		// stage.Commands doesn't include the FROM instruction.
		nodeIdx := i + 1
		if nodeIdx >= len(stageNodes) {
			continue
		}
		node := stageNodes[nodeIdx]

		startLine := node.StartLine - len(node.PrevComment)
		endLine := sm.ResolveEndLine(node.EndLine)

		epilogues = append(epilogues, epilogueInstr{
			name:      name,
			rank:      rank,
			startLine: startLine,
			endLine:   endLine,
		})
	}

	if len(epilogues) == 0 {
		return nil
	}

	// Check for duplicate instruction types — skip fix for safety.
	names := make([]string, len(epilogues))
	for i, ep := range epilogues {
		names[i] = ep.name
	}
	if rules.HasDuplicateEpilogueNames(names) {
		return nil
	}

	// Check if already correct: all at end and in order.
	if rules.CheckEpilogueOrder(stage.Commands) {
		return nil
	}

	// Find the last non-epilogue instruction's end line.
	// This is where we'll insert the reordered epilogues.
	insertAfterLine := r.lastNonEpilogueLine(stage, stageNodes, sm)
	if insertAfterLine == 0 {
		// All instructions are epilogue — just need to reorder, no position change.
		// Use the line before the first epilogue as the insert point.
		insertAfterLine = epilogues[0].startLine - 1
	}

	// Extract source text for each epilogue instruction.
	instrTexts := make([]rankedText, 0, len(epilogues))
	for _, ep := range epilogues {
		// Extract lines from startLine to endLine (1-based, inclusive).
		text := sm.Snippet(ep.startLine-1, ep.endLine-1)
		instrTexts = append(instrTexts, rankedText{rank: ep.rank, text: text})
	}

	// Sort by canonical order.
	slices.SortStableFunc(instrTexts, func(a, b rankedText) int {
		return cmp.Compare(a.rank, b.rank)
	})

	// Build the combined insert text.
	var insertBuf strings.Builder
	for _, it := range instrTexts {
		insertBuf.WriteString("\n")
		insertBuf.WriteString(it.text)
	}
	insertText := insertBuf.String()

	// Generate edits: delete originals, then insert at end.
	var edits []rules.TextEdit

	// Delete each epilogue instruction from its original position.
	for _, ep := range epilogues {
		edits = append(edits, rules.TextEdit{
			Location: rules.NewRangeLocation(file, ep.startLine, 0, ep.endLine+1, 0),
			NewText:  "",
		})
	}

	// Insert all epilogues in canonical order after the last non-epilogue instruction.
	edits = append(edits, rules.TextEdit{
		Location: rules.NewRangeLocation(file, insertAfterLine+1, 0, insertAfterLine+1, 0),
		NewText:  insertText + "\n",
	})

	return edits
}

// lastNonEpilogueLine finds the end line of the last non-epilogue instruction in the stage.
// Returns 0 if all instructions in the stage are epilogues.
func (r *epilogueOrderResolver) lastNonEpilogueLine(
	stage instructions.Stage,
	stageNodes []*parser.Node,
	sm *sourcemap.SourceMap,
) int {
	lastLine := 0
	for i, cmd := range stage.Commands {
		if _, isOnbuild := cmd.(*instructions.OnbuildCommand); isOnbuild {
			nodeIdx := i + 1
			if nodeIdx < len(stageNodes) {
				endLine := sm.ResolveEndLine(stageNodes[nodeIdx].EndLine)
				if endLine > lastLine {
					lastLine = endLine
				}
			}
			continue
		}
		name := strings.ToLower(cmd.Name())
		if rules.IsEpilogueInstruction(name) {
			continue
		}
		nodeIdx := i + 1
		if nodeIdx < len(stageNodes) {
			endLine := sm.ResolveEndLine(stageNodes[nodeIdx].EndLine)
			if endLine > lastLine {
				lastLine = endLine
			}
		}
	}
	return lastLine
}

// stageASTChildren returns the AST children that belong to a specific stage.
// The first node is the FROM instruction; subsequent nodes match stage.Commands.
func stageASTChildren(stageIdx int, dockerfile *parser.Result) []*parser.Node {
	children := dockerfile.AST.Children

	// Count FROM instructions to find the stage boundaries.
	fromCount := 0
	startIdx := -1
	for i, node := range children {
		if strings.EqualFold(node.Value, "from") {
			if fromCount == stageIdx {
				startIdx = i
			}
			if fromCount == stageIdx+1 {
				return children[startIdx:i]
			}
			fromCount++
		}
	}

	if startIdx >= 0 {
		return children[startIdx:]
	}
	return nil
}

// buildDependentsMap builds a map of stage index → list of stages that depend on it.
// This is a lightweight alternative to the full semantic graph for use in the resolver.
func buildDependentsMap(stages []instructions.Stage) map[int][]int {
	// Build name → index lookup.
	nameToIdx := make(map[string]int, len(stages))
	for i, stage := range stages {
		if stage.Name != "" {
			nameToIdx[strings.ToLower(stage.Name)] = i
		}
	}

	dependents := make(map[int][]int)

	for stageIdx, stage := range stages {
		// Check FROM reference to another stage.
		if stage.BaseName != "" {
			ref := strings.ToLower(stage.BaseName)
			if depIdx, ok := nameToIdx[ref]; ok {
				dependents[depIdx] = append(dependents[depIdx], stageIdx)
			} else if idx, err := strconv.Atoi(ref); err == nil && idx >= 0 && idx < len(stages) {
				dependents[idx] = append(dependents[idx], stageIdx)
			}
		}

		// Check COPY --from references.
		for _, cmd := range stage.Commands {
			cp, ok := cmd.(*instructions.CopyCommand)
			if !ok || cp.From == "" {
				continue
			}
			ref := strings.ToLower(cp.From)
			if depIdx, ok := nameToIdx[ref]; ok {
				dependents[depIdx] = append(dependents[depIdx], stageIdx)
			} else if idx, err := strconv.Atoi(ref); err == nil && idx >= 0 && idx < len(stages) {
				dependents[idx] = append(dependents[idx], stageIdx)
			}
		}
	}

	return dependents
}

// init registers the epilogue-order resolver.
func init() {
	RegisterResolver(&epilogueOrderResolver{})
}
