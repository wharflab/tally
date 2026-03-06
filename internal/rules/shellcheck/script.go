package shellcheck

import (
	dfparser "github.com/moby/buildkit/frontend/dockerfile/parser"

	"github.com/wharflab/tally/internal/highlight/extract"
	"github.com/wharflab/tally/internal/sourcemap"
)

type scriptMapping = extract.Mapping

func extractRunScript(
	sm *sourcemap.SourceMap,
	node *dfparser.Node,
	escapeToken rune,
) (scriptMapping, bool) {
	return extract.ExtractRunScript(sm, node, escapeToken)
}

func extractOnbuildRunScript(
	sm *sourcemap.SourceMap,
	node *dfparser.Node,
	escapeToken rune,
) (scriptMapping, bool) {
	return extract.ExtractOnbuildRunScript(sm, node, escapeToken)
}

func extractShellFormScript(
	sm *sourcemap.SourceMap,
	node *dfparser.Node,
	escapeToken rune,
	keyword string,
) (scriptMapping, bool) {
	return extract.ExtractShellFormScript(sm, node, escapeToken, keyword)
}

func extractHealthcheckCmdShellScript(
	sm *sourcemap.SourceMap,
	node *dfparser.Node,
	escapeToken rune,
) (scriptMapping, bool) {
	return extract.ExtractHealthcheckCmdShellScript(sm, node, escapeToken)
}
