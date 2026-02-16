package fixes

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/semantic"
)

// stageCasingRegex extracts the stage name from BuildKit's warning message.
// Message format: "Stage name 'Builder' should be lowercase"
var stageCasingRegex = regexp.MustCompile(`Stage name '([^']+)' should be lowercase`)

// enrichStageNameCasingFix adds auto-fix for BuildKit's StageNameCasing rule.
// This fixes stage names that should be lowercase, updating both the definition
// and all references (FROM and COPY --from).
//
// Example:
//
//	FROM alpine AS Builder    -> FROM alpine AS builder
//	COPY --from=Builder ...   -> COPY --from=builder ...
//	FROM Builder              -> FROM builder
func enrichStageNameCasingFix(v *rules.Violation, sem *semantic.Model, source []byte) {
	// Extract stage name from message
	matches := stageCasingRegex.FindStringSubmatch(v.Message)
	if len(matches) < 2 || sem == nil {
		return
	}

	stageName := matches[1]
	lowerName := strings.ToLower(stageName)

	// Find the stage by name
	stageIdx, found := sem.StageIndexByName(stageName)
	if !found {
		return
	}

	file := v.Location.File
	var edits []rules.TextEdit

	// 1. Fix the stage definition (FROM ... AS stagename)
	if edit := createStageDefEdit(sem.Stage(stageIdx), stageName, lowerName, file, source); edit != nil {
		edits = append(edits, *edit)
	}

	// 2. Fix all references to this stage
	edits = append(edits, collectStageRefEdits(sem, stageIdx, stageName, lowerName, file, source)...)

	if len(edits) > 0 {
		v.SuggestedFix = &rules.SuggestedFix{
			Description: fmt.Sprintf("Rename stage '%s' to '%s'", stageName, lowerName),
			Safety:      rules.FixSafe,
			Edits:       edits,
			IsPreferred: true,
		}
	}
}

// createStageDefEdit creates an edit for the stage definition (FROM ... AS stagename).
func createStageDefEdit(stage *instructions.Stage, stageName, lowerName, file string, source []byte) *rules.TextEdit {
	if stage == nil || len(stage.Location) == 0 {
		return nil
	}

	lineIdx := stage.Location[0].Start.Line - 1
	line := getLine(source, lineIdx)
	if line == nil {
		return nil
	}

	// Use tokenizer to find the stage name after AS
	it := ParseInstruction(line)
	asKeyword := it.FindKeyword("AS")
	if asKeyword == nil {
		return nil
	}

	nameToken := it.TokenAfter(asKeyword)
	if nameToken == nil {
		return nil
	}

	// Verify the name matches what we expect
	if !strings.EqualFold(nameToken.Value, stageName) {
		return nil
	}

	return &rules.TextEdit{
		Location: createEditLocation(file, stage.Location[0].Start.Line, nameToken.Start, nameToken.End),
		NewText:  lowerName,
	}
}

// collectStageRefEdits collects edits for all references to a stage.
func collectStageRefEdits(sem *semantic.Model, stageIdx int, stageName, lowerName, file string, source []byte) []rules.TextEdit {
	var edits []rules.TextEdit

	for i := range sem.StageCount() {
		info := sem.StageInfo(i)
		if info == nil {
			continue
		}

		// Check FROM <stagename> references (multi-stage builds)
		if edit := createFromRefEdit(info, stageIdx, stageName, lowerName, file, source); edit != nil {
			edits = append(edits, *edit)
		}

		// Check COPY --from=<stagename> references
		edits = append(edits, createCopyFromEdits(info, stageIdx, stageName, lowerName, file, source)...)
	}

	return edits
}

// createFromRefEdit creates an edit for FROM <stagename> references.
func createFromRefEdit(info *semantic.StageInfo, stageIdx int, stageName, lowerName, file string, source []byte) *rules.TextEdit {
	if info.BaseImage == nil || !info.BaseImage.IsStageRef || info.BaseImage.StageIndex != stageIdx {
		return nil
	}
	if len(info.BaseImage.Location) == 0 {
		return nil
	}

	lineIdx := info.BaseImage.Location[0].Start.Line - 1
	line := getLine(source, lineIdx)
	if line == nil {
		return nil
	}

	// Use tokenizer to find the base image (first argument after FROM and any flags)
	it := ParseInstruction(line)
	args := it.Arguments()
	if len(args) == 0 {
		return nil
	}

	// First argument is the base image name
	baseNameToken := &args[0]
	if !strings.EqualFold(baseNameToken.Value, stageName) {
		return nil
	}

	return &rules.TextEdit{
		Location: createEditLocation(file, info.BaseImage.Location[0].Start.Line, baseNameToken.Start, baseNameToken.End),
		NewText:  lowerName,
	}
}

// createCopyFromEdits creates edits for COPY --from=<stagename> references.
func createCopyFromEdits(info *semantic.StageInfo, stageIdx int, stageName, lowerName, file string, source []byte) []rules.TextEdit {
	edits := make([]rules.TextEdit, 0, len(info.CopyFromRefs))

	for _, ref := range info.CopyFromRefs {
		if !ref.IsStageRef || ref.StageIndex != stageIdx || len(ref.Location) == 0 {
			continue
		}

		lineIdx := ref.Location[0].Start.Line - 1
		line := getLine(source, lineIdx)
		if line == nil {
			continue
		}

		// Use tokenizer to find --from flag value
		it := ParseInstruction(line)
		fromFlag := it.FindFlag("from")
		if fromFlag == nil {
			continue
		}

		valueStart, valueEnd, value := it.FlagValue(fromFlag)
		if valueStart < 0 {
			continue
		}

		if !strings.EqualFold(value, stageName) {
			continue
		}

		edits = append(edits, rules.TextEdit{
			Location: createEditLocation(file, ref.Location[0].Start.Line, valueStart, valueEnd),
			NewText:  lowerName,
		})
	}

	return edits
}
