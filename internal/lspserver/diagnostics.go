package lspserver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/sourcegraph/jsonrpc2"

	protocol "github.com/tinovyatkin/tally/internal/lsp/protocol"

	"github.com/tinovyatkin/tally/internal/config"
	"github.com/tinovyatkin/tally/internal/linter"
	"github.com/tinovyatkin/tally/internal/processor"
	"github.com/tinovyatkin/tally/internal/rules"
)

// lintResultCache caches lint results keyed by document URI + version
// to avoid redundant linting between publishDiagnostics and codeAction requests.
type lintResultCache struct {
	mu      sync.Mutex
	entries map[string]lintCacheEntry
}

type lintCacheEntry struct {
	version    int32
	violations []rules.Violation
}

func newLintResultCache() *lintResultCache {
	return &lintResultCache{entries: make(map[string]lintCacheEntry)}
}

func (c *lintResultCache) get(uri string, version int32) ([]rules.Violation, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[uri]
	if !ok || entry.version != version {
		return nil, false
	}
	return entry.violations, true
}

func (c *lintResultCache) set(uri string, version int32, violations []rules.Violation) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[uri] = lintCacheEntry{version: version, violations: violations}
}

func (c *lintResultCache) delete(uri string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, uri)
}

// publishDiagnostics lints a document and publishes diagnostics to the client.
func (s *Server) publishDiagnostics(ctx context.Context, conn *jsonrpc2.Conn, doc *Document) {
	docURI := doc.URI
	content := doc.Content

	violations := s.lintContent(docURI, []byte(content))
	s.lintCache.set(docURI, doc.Version, violations)
	diagnostics := convertDiagnostics(violations)

	version := doc.Version
	if err := lspNotify(ctx, conn, string(protocol.MethodTextDocumentPublishDiagnostics), &protocol.PublishDiagnosticsParams{
		Uri:         protocol.DocumentUri(docURI),
		Version:     &version,
		Diagnostics: diagnostics,
	}); err != nil {
		log.Printf("lsp: failed to publish diagnostics for %s: %v", docURI, err)
	}
}

// clearDiagnostics sends an empty diagnostics array to clear issues for a URI.
// version is the last known document version (nil if unknown).
func clearDiagnostics(ctx context.Context, conn *jsonrpc2.Conn, docURI string, version *int32) {
	if err := lspNotify(ctx, conn, string(protocol.MethodTextDocumentPublishDiagnostics), &protocol.PublishDiagnosticsParams{
		Uri:         protocol.DocumentUri(docURI),
		Version:     version,
		Diagnostics: []*protocol.Diagnostic{},
	}); err != nil {
		log.Printf("lsp: failed to clear diagnostics for %s: %v", docURI, err)
	}
}

// handleDiagnostic handles textDocument/diagnostic (pull diagnostics).
func (s *Server) handleDiagnostic(params *protocol.DocumentDiagnosticParams) (any, error) {
	uri := string(params.TextDocument.Uri)

	// Check if the document is open in the editor.
	if doc := s.documents.Get(uri); doc != nil {
		resultID := fmt.Sprintf("v%d", doc.Version)
		if params.PreviousResultId != nil && *params.PreviousResultId == resultID {
			return &protocol.DocumentDiagnosticResponse{
				UnchangedDocumentDiagnosticReport: &protocol.RelatedUnchangedDocumentDiagnosticReport{
					ResultId: resultID,
				},
			}, nil
		}

		violations := s.lintContent(uri, []byte(doc.Content))
		diagnostics := convertDiagnostics(violations)

		return &protocol.DocumentDiagnosticResponse{
			FullDocumentDiagnosticReport: &protocol.RelatedFullDocumentDiagnosticReport{
				ResultId: &resultID,
				Items:    diagnostics,
			},
		}, nil
	}

	// Document not open — read from disk.
	filePath := uriToPath(uri)
	return s.pullDiagnosticsFromDisk(uri, filePath, params.PreviousResultId)
}

// pullDiagnosticsFromDisk reads content from disk and returns a diagnostic report.
//
//nolint:nilerr // gracefully returns empty diagnostics for unreadable files
func (s *Server) pullDiagnosticsFromDisk(docURI, filePath string, previousResultID *string) (any, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		// Return empty full report if file cannot be read.
		return &protocol.DocumentDiagnosticResponse{
			FullDocumentDiagnosticReport: &protocol.RelatedFullDocumentDiagnosticReport{
				Items: []*protocol.Diagnostic{},
			},
		}, nil
	}

	resultID := contentHash(content)
	if previousResultID != nil && *previousResultID == resultID {
		return &protocol.DocumentDiagnosticResponse{
			UnchangedDocumentDiagnosticReport: &protocol.RelatedUnchangedDocumentDiagnosticReport{
				ResultId: resultID,
			},
		}, nil
	}

	violations := s.lintContent(docURI, content)
	diagnostics := convertDiagnostics(violations)

	return &protocol.DocumentDiagnosticResponse{
		FullDocumentDiagnosticReport: &protocol.RelatedFullDocumentDiagnosticReport{
			ResultId: &resultID,
			Items:    diagnostics,
		},
	}, nil
}

// contentHash returns a truncated SHA-256 hex digest of content (16 hex chars).
func contentHash(content []byte) string {
	h := sha256.Sum256(content)
	return hex.EncodeToString(h[:8])
}

// lintInput builds a linter.Input for the given document.
// Config comes from s.resolveConfig() — currently disk-only,
// but designed for future workspace/didChangeConfiguration support.
func (s *Server) lintInput(docURI string, content []byte) linter.Input {
	filePath := uriToPath(docURI)
	return linter.Input{
		FilePath: filePath,
		Content:  content,
		Config:   s.resolveConfig(filePath),
	}
}

// resolveConfig returns the effective config for a file path.
// Currently returns nil (LintFile discovers from disk).
// When workspace/didChangeConfiguration is implemented, this will merge
// editor settings with filesystem config per configurationPreference.
func (s *Server) resolveConfig(_ string) *config.Config {
	return nil
}

// lintContent runs the shared lint pipeline and applies LSP-specific processors.
func (s *Server) lintContent(docURI string, content []byte) []rules.Violation {
	input := s.lintInput(docURI, content)
	result, err := linter.LintFile(input)
	if err != nil {
		log.Printf("lsp: lint error for %s: %v", input.FilePath, err)
		return nil
	}
	chain := linter.LSPProcessors()
	ctx := processor.NewContext(
		map[string]*config.Config{input.FilePath: result.Config},
		result.Config,
		map[string][]byte{input.FilePath: content},
	)
	return chain.Process(result.Violations, ctx)
}

// convertDiagnostics converts tally violations to LSP diagnostics.
func convertDiagnostics(violations []rules.Violation) []*protocol.Diagnostic {
	diagnostics := make([]*protocol.Diagnostic, 0, len(violations))
	for _, v := range violations {
		d := &protocol.Diagnostic{
			Range:    violationRange(v),
			Severity: ptrTo(severityToLSP(v.Severity)),
			Source:   ptrTo("tally"),
			Code:     &protocol.IntegerOrString{String: ptrTo(v.RuleCode)},
			Message:  v.Message,
		}
		if v.DocURL != "" {
			d.CodeDescription = &protocol.CodeDescription{
				Href: protocol.URI(v.DocURL),
			}
		}
		diagnostics = append(diagnostics, d)
	}
	return diagnostics
}

// violationRange converts a tally Location to an LSP Range.
// tally uses 1-based lines, 0-based columns.
// LSP uses 0-based lines, 0-based characters.
func violationRange(v rules.Violation) protocol.Range {
	loc := v.Location
	if loc.IsFileLevel() {
		return protocol.Range{
			Start: protocol.Position{Line: 0, Character: 0},
			End:   protocol.Position{Line: 0, Character: 0},
		}
	}

	startLine := clampUint32(loc.Start.Line - 1)
	startChar := clampUint32(loc.Start.Column)

	endLine := startLine
	endChar := startChar
	if !loc.IsPointLocation() {
		endLine = clampUint32(loc.End.Line - 1)
		endChar = clampUint32(loc.End.Column)
	}

	// For point locations, extend to end of line to make the diagnostic visible.
	if endLine == startLine && endChar == startChar {
		endChar = startChar + 1000 // VS Code will clamp to actual line length.
	}

	return protocol.Range{
		Start: protocol.Position{Line: startLine, Character: startChar},
		End:   protocol.Position{Line: endLine, Character: endChar},
	}
}

// severityToLSP converts a tally Severity to an LSP DiagnosticSeverity.
// SeverityOff violations should be filtered by the processor chain (EnableFilter)
// before reaching this function; it falls through to the default Warning mapping.
func severityToLSP(s rules.Severity) protocol.DiagnosticSeverity {
	switch s {
	case rules.SeverityError:
		return protocol.DiagnosticSeverityError
	case rules.SeverityWarning:
		return protocol.DiagnosticSeverityWarning
	case rules.SeverityInfo:
		return protocol.DiagnosticSeverityInformation
	case rules.SeverityStyle:
		return protocol.DiagnosticSeverityHint
	case rules.SeverityOff:
		return protocol.DiagnosticSeverityWarning // filtered upstream; defensive fallback
	default:
		return protocol.DiagnosticSeverityWarning
	}
}

// clampUint32 safely converts an int to uint32, clamping negative values to 0.
func clampUint32(v int) uint32 {
	if v < 0 {
		return 0
	}
	return uint32(v) //nolint:gosec // line/column numbers are well within uint32 range
}

// uriToPath converts a file:// URI to a local file path.
func uriToPath(docURI string) string {
	parsed, err := url.Parse(docURI)
	if err != nil {
		return strings.TrimPrefix(docURI, "file://")
	}
	path := parsed.Path
	if runtime.GOOS == "windows" {
		// UNC paths: file://server/share/path → \\server\share\path
		if parsed.Host != "" {
			path = `//` + parsed.Host + path
		}
		// Drive-letter paths: file:///C:/path → Path is /C:/path, strip leading /.
		if len(path) > 2 && path[0] == '/' && path[2] == ':' {
			path = path[1:]
		}
	}
	return filepath.FromSlash(path)
}
