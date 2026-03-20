package autofix

import (
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/sourcemap"
)

func wholeFileReplacement(filePath string, original []byte, newText string) rules.TextEdit {
	sm := sourcemap.New(original)
	endLine := sm.LineCount()
	endCol := 0
	if endLine > 0 {
		endCol = len(sm.Line(endLine - 1))
	}
	return rules.TextEdit{
		Location: rules.NewRangeLocation(filePath, 1, 0, endLine, endCol),
		NewText:  newText,
	}
}
