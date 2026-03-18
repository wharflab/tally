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
	m, ok := extract.ExtractRunScript(sm, node, escapeToken)
	if ok {
		m.Script = extract.NormalizeContinuation(m.Script, escapeToken, '\\')
	}
	return m, ok
}

func extractOnbuildRunScript(
	sm *sourcemap.SourceMap,
	node *dfparser.Node,
	escapeToken rune,
) (scriptMapping, bool) {
	m, ok := extract.ExtractOnbuildRunScript(sm, node, escapeToken)
	if ok {
		m.Script = extract.NormalizeContinuation(m.Script, escapeToken, '\\')
	}
	return m, ok
}

func extractShellFormScript(
	sm *sourcemap.SourceMap,
	node *dfparser.Node,
	escapeToken rune,
	keyword string,
) (scriptMapping, bool) {
	m, ok := extract.ExtractShellFormScript(sm, node, escapeToken, keyword)
	if ok {
		m.Script = extract.NormalizeContinuation(m.Script, escapeToken, '\\')
	}
	return m, ok
}

func extractHealthcheckCmdShellScript(
	sm *sourcemap.SourceMap,
	node *dfparser.Node,
	escapeToken rune,
) (scriptMapping, bool) {
	m, ok := extract.ExtractHealthcheckCmdShellScript(sm, node, escapeToken)
	if ok {
		m.Script = extract.NormalizeContinuation(m.Script, escapeToken, '\\')
	}
	return m, ok
}
