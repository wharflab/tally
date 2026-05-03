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
	"time"

	"golang.org/x/exp/jsonrpc2"

	protocol "github.com/wharflab/tally/internal/lsp/protocol"

	"github.com/wharflab/tally/internal/async"
	"github.com/wharflab/tally/internal/config"
	"github.com/wharflab/tally/internal/dockerfile"
	"github.com/wharflab/tally/internal/invocation"
	"github.com/wharflab/tally/internal/linter"
	"github.com/wharflab/tally/internal/processor"
	"github.com/wharflab/tally/internal/rules"
)

const (
	// maxConcurrentDiagnosticsPasses limits concurrent diagnostics workers
	// across all documents.
	maxConcurrentDiagnosticsPasses = 2
)

type diagnosticsTask struct {
	docURI  string
	version int32
	content []byte
}

// lintResultCache caches lint results keyed by document URI + version
// to avoid redundant linting between publishDiagnostics and codeAction requests.
// In addition to violations, it stores the resolved config and parse result
// so that code action providers (e.g. suppress-rule) can reuse the already-parsed
// document without re-parsing.
type lintResultCache struct {
	mu      sync.Mutex
	entries map[string]lintCacheEntry
}

type lintCacheEntry struct {
	version     int32
	violations  []rules.Violation
	config      *config.Config
	parseResult *dockerfile.ParseResult
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

// getEntry returns the full cache entry for the given URI and version.
// Returns a zero entry and false if not cached or version doesn't match.
func (c *lintResultCache) getEntry(uri string, version int32) (lintCacheEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[uri]
	if !ok || entry.version != version {
		return lintCacheEntry{}, false
	}
	return entry, true
}

func (c *lintResultCache) set(
	uri string,
	version int32,
	violations []rules.Violation,
	cfg *config.Config,
	parseResult *dockerfile.ParseResult,
) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[uri] = lintCacheEntry{
		version:     version,
		violations:  violations,
		config:      cfg,
		parseResult: parseResult,
	}
}

func (c *lintResultCache) delete(uri string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, uri)
}

func (c *lintResultCache) clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	clear(c.entries)
}

// publishDiagnostics lints a document and publishes diagnostics to the client.
func (s *Server) publishDiagnostics(ctx context.Context, doc *Document) {
	if doc == nil {
		return
	}
	s.enqueueDiagnosticsTask(context.WithoutCancel(ctx), diagnosticsTask{
		docURI:  doc.URI,
		version: doc.Version,
		content: []byte(doc.Content),
	})
}

func (s *Server) enqueueDiagnosticsTask(ctx context.Context, task diagnosticsTask) {
	s.diagnosticsDispatchMu.Lock()
	if s.diagnosticsInFlightByURI[task.docURI] {
		s.diagnosticsPendingByURI[task.docURI] = task
		s.diagnosticsDispatchMu.Unlock()
		return
	}
	s.diagnosticsInFlightByURI[task.docURI] = true
	s.diagnosticsDispatchMu.Unlock()

	// Run linting asynchronously so that expensive analyzers (ShellCheck) don't
	// block the JSON-RPC message handling goroutine.
	go s.runDiagnosticsWorker(ctx, task)
}

func (s *Server) runDiagnosticsWorker(ctx context.Context, task diagnosticsTask) {
	for {
		s.acquireDiagnosticsSlot()
		s.runDiagnosticsTask(ctx, task)
		s.releaseDiagnosticsSlot()

		var ok bool
		task, ok = s.nextDiagnosticsTask(task.docURI)
		if !ok {
			return
		}
	}
}

func (s *Server) runDiagnosticsTask(ctx context.Context, task diagnosticsTask) {
	if s.diagnosticsRunFn != nil {
		s.diagnosticsRunFn(ctx, task.docURI, task.version, task.content)
		return
	}
	s.publishDiagnosticsForDocument(ctx, task.docURI, task.version, task.content)
}

func (s *Server) acquireDiagnosticsSlot() {
	s.diagnosticsConcurrencyGate <- struct{}{}
}

func (s *Server) releaseDiagnosticsSlot() {
	<-s.diagnosticsConcurrencyGate
}

func (s *Server) nextDiagnosticsTask(docURI string) (diagnosticsTask, bool) {
	s.diagnosticsDispatchMu.Lock()
	defer s.diagnosticsDispatchMu.Unlock()

	if next, ok := s.diagnosticsPendingByURI[docURI]; ok {
		delete(s.diagnosticsPendingByURI, docURI)
		return next, true
	}

	delete(s.diagnosticsInFlightByURI, docURI)
	return diagnosticsTask{}, false
}

func (s *Server) cancelPendingDiagnostics(docURI string) {
	s.diagnosticsDispatchMu.Lock()
	delete(s.diagnosticsPendingByURI, docURI)
	s.diagnosticsDispatchMu.Unlock()
}

func (s *Server) publishDiagnosticsForDocument(ctx context.Context, docURI string, version int32, content []byte) {
	lr := s.lintContent(ctx, docURI, content)
	if s.documentVersionCurrent(docURI, version) {
		s.lintCache.set(docURI, version, lr.violations, lr.config, lr.parseResult)
		s.notifyDiagnostics(ctx, docURI, version, lr.violations)
	}
}

func (s *Server) documentVersionCurrent(docURI string, version int32) bool {
	doc := s.documents.Get(docURI)
	return doc != nil && doc.Version == version
}

func (s *Server) notifyDiagnostics(ctx context.Context, docURI string, version int32, violations []rules.Violation) {
	diagnostics := convertDiagnostics(violations)

	notifyCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	if err := lspNotify(notifyCtx, s.conn, string(protocol.MethodTextDocumentPublishDiagnostics), &protocol.PublishDiagnosticsParams{
		Uri:         protocol.DocumentUri(docURI),
		Version:     &version,
		Diagnostics: diagnostics,
	}); err != nil {
		log.Printf("lsp: failed to publish diagnostics for %s: %v", docURI, err)
	}
}

// clearDiagnostics sends an empty diagnostics array to clear issues for a URI.
// version is the last known document version (nil if unknown).
func clearDiagnostics(ctx context.Context, conn *jsonrpc2.Connection, docURI string, version *int32) {
	if err := lspNotify(ctx, conn, string(protocol.MethodTextDocumentPublishDiagnostics), &protocol.PublishDiagnosticsParams{
		Uri:         protocol.DocumentUri(docURI),
		Version:     version,
		Diagnostics: []*protocol.Diagnostic{},
	}); err != nil {
		log.Printf("lsp: failed to clear diagnostics for %s: %v", docURI, err)
	}
}

// handleDiagnostic handles textDocument/diagnostic (pull diagnostics).
func (s *Server) handleDiagnostic(ctx context.Context, params *protocol.DocumentDiagnosticParams) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

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

		lr := s.lintContent(ctx, uri, []byte(doc.Content))
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if s.documentVersionCurrent(uri, doc.Version) {
			s.lintCache.set(uri, doc.Version, lr.violations, lr.config, lr.parseResult)
		}
		diagnostics := convertDiagnostics(lr.violations)

		return &protocol.DocumentDiagnosticResponse{
			FullDocumentDiagnosticReport: &protocol.RelatedFullDocumentDiagnosticReport{
				ResultId: &resultID,
				Items:    diagnostics,
			},
		}, nil
	}

	// Document not open — read from disk.
	// Untitled documents have no backing file; return empty diagnostics.
	if isVirtualURI(uri) {
		return &protocol.DocumentDiagnosticResponse{
			FullDocumentDiagnosticReport: &protocol.RelatedFullDocumentDiagnosticReport{
				Items: []*protocol.Diagnostic{},
			},
		}, nil
	}
	filePath := uriToPath(uri)
	return s.pullDiagnosticsFromDisk(ctx, uri, filePath, params.PreviousResultId)
}

// pullDiagnosticsFromDisk reads content from disk and returns a diagnostic report.
//

func (s *Server) pullDiagnosticsFromDisk(ctx context.Context, docURI, filePath string, previousResultID *string) (any, error) {
	// Bail out early if the request has been cancelled.
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	content, ok := s.readValidatedFileContent(filePath)
	if !ok {
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

	// Check cancellation before the expensive lint pass.
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	lr := s.lintContent(ctx, docURI, content)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	diagnostics := convertDiagnostics(lr.violations)

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

// lintInput builds a linter.Input for the given document. Config comes from
// s.resolveConfig() — currently disk-only, but designed for future
// workspace/didChangeConfiguration support.
func (s *Server) lintInput(docURI string, content []byte) linter.Input {
	filePath := uriToPath(docURI)
	return linter.Input{
		FilePath: filePath,
		Content:  content,
		Config:   s.resolveConfig(filePath),
	}
}

// resolveConfig returns the effective config for a file path.
// The editor can provide configuration overrides via workspace/didChangeConfiguration.
// The server merges those overrides with discovered `.tally.toml` / `tally.toml`
// based on configurationPreference.
func (s *Server) resolveConfig(filePath string) *config.Config {
	settings := s.settingsForFile(filePath)
	cfg, err := config.LoadWithOverrides(filePath, settings.ConfigurationOverrides, settings.ConfigurationPreference)
	if err != nil {
		log.Printf("lsp: config load error for %s: %v", filePath, err)
		return nil
	}
	if cfg.SlowChecks.Mode == "auto" {
		if settings.WorkspaceTrusted {
			cfg.SlowChecks.Mode = "on"
		} else {
			cfg.SlowChecks.Mode = "off"
		}
	}
	return cfg
}

// lintResult holds the full output of a lint pass: violations plus the resolved
// config and parse result that produced them.
type lintResult struct {
	violations  []rules.Violation
	config      *config.Config
	parseResult *dockerfile.ParseResult
}

// lintContent runs the shared lint pipeline and applies LSP-specific processors.
func (s *Server) lintContent(ctx context.Context, docURI string, content []byte) lintResult {
	filePath := uriToPath(docURI)
	cfg := s.resolveConfig(filePath)
	return s.lintContentWithConfig(ctx, docURI, content, cfg, nil)
}

func (s *Server) lintContentWithConfig(
	ctx context.Context,
	docURI string,
	content []byte,
	cfg *config.Config,
	parseResult *dockerfile.ParseResult,
) lintResult {
	filePath := uriToPath(docURI)
	if invocations := s.invocationsForDocument(ctx, filePath); len(invocations) > 0 {
		return s.lintContentWithInvocations(ctx, docURI, content, cfg, parseResult, invocations)
	}

	input := linter.Input{
		FilePath:    filePath,
		Content:     content,
		Config:      cfg,
		ParseResult: parseResult,
	}

	result, err := linter.LintFileContext(ctx, input)
	if err != nil {
		log.Printf("lsp: lint error for %s: %v", input.FilePath, err)
		return lintResult{}
	}

	violations := result.Violations
	asyncResult := s.runAsyncChecks(ctx, filePath, content, result.Config, violations, result.AsyncPlan)
	if asyncResult != nil {
		violations = linter.MergeAsyncViolations(violations, asyncResult)
	}

	chain := linter.LSPProcessors()
	procCtx := processor.NewContext(
		map[string]*config.Config{input.FilePath: result.Config},
		result.Config,
		map[string][]byte{input.FilePath: content},
	)
	return lintResult{
		violations:  chain.Process(violations, procCtx),
		config:      result.Config,
		parseResult: result.ParseResult,
	}
}

func (s *Server) lintContentWithInvocations(
	ctx context.Context,
	docURI string,
	content []byte,
	cfg *config.Config,
	parseResult *dockerfile.ParseResult,
	invocations []invocation.BuildInvocation,
) lintResult {
	filePath := uriToPath(docURI)
	var (
		violations []rules.Violation
		asyncPlans []async.CheckRequest
		parsed     = parseResult
		resolved   = cfg
	)

	for _, inv := range invocations {
		input := linter.Input{
			FilePath:    filePath,
			Content:     content,
			Config:      cfg,
			ParseResult: parsed,
			Invocation:  &inv,
		}
		result, err := linter.LintFileContext(ctx, input)
		if err != nil {
			log.Printf("lsp: lint error for %s (%s): %v", input.FilePath, invocation.LabelForSource(&inv.Source), err)
			continue
		}
		if parsed == nil {
			parsed = result.ParseResult
		}
		resolved = result.Config
		violations = append(violations, result.Violations...)
		asyncPlans = append(asyncPlans, result.AsyncPlan...)
	}

	if len(violations) == 0 && parsed == nil {
		return lintResult{config: resolved}
	}
	asyncResult := s.runAsyncChecks(ctx, filePath, content, resolved, violations, asyncPlans)
	if asyncResult != nil {
		violations = linter.MergeAsyncViolations(violations, asyncResult)
	}

	chain := linter.LSPProcessors()
	procCtx := processor.NewContext(
		map[string]*config.Config{filePath: resolved},
		resolved,
		map[string][]byte{filePath: content},
	)
	return lintResult{
		violations:  chain.Process(violations, procCtx),
		config:      resolved,
		parseResult: parsed,
	}
}

func (s *Server) invocationsForDocument(ctx context.Context, filePath string) []invocation.BuildInvocation {
	root, settings := s.workspaceSettingsForFile(filePath)
	if len(settings.InvocationEntrypoints) == 0 {
		return nil
	}
	if root == "" {
		root = filepath.Dir(filePath)
	}
	filePath = filepath.Clean(filePath)

	var out []invocation.BuildInvocation
	for _, entrypoint := range settings.InvocationEntrypoints {
		if !filepath.IsAbs(entrypoint) {
			entrypoint = filepath.Join(root, entrypoint)
		}
		result, err := discoverLSPEntrypoint(ctx, entrypoint)
		if err != nil {
			log.Printf("lsp: invocation entrypoint %s: %v", entrypoint, err)
			continue
		}
		for _, inv := range result.Invocations {
			if filepath.Clean(inv.DockerfilePath) == filePath {
				out = append(out, inv)
			}
		}
	}
	return out
}

func discoverLSPEntrypoint(ctx context.Context, entrypoint string) (*invocation.DiscoveryResult, error) {
	ext := strings.ToLower(filepath.Ext(entrypoint))
	switch ext {
	case ".hcl":
		return (invocation.BakeProvider{}).Discover(ctx, invocation.ResolveOptions{Path: entrypoint})
	case ".json":
		if kind, ok := invocation.ProbeEntrypointKind(entrypoint); ok {
			return discoverLSPEntrypointByKind(ctx, entrypoint, kind)
		}
		result, err := (invocation.BakeProvider{}).Discover(ctx, invocation.ResolveOptions{Path: entrypoint})
		if err == nil && len(result.Invocations) > 0 {
			return result, nil
		}
		return (invocation.ComposeProvider{}).Discover(ctx, invocation.ResolveOptions{Path: entrypoint})
	case ".yml", ".yaml":
		return (invocation.ComposeProvider{}).Discover(ctx, invocation.ResolveOptions{Path: entrypoint})
	default:
		kind, ok := invocation.ProbeEntrypointKind(entrypoint)
		if !ok {
			return nil, fmt.Errorf("%s is not a Dockerfile, Compose, or Bake file", entrypoint)
		}
		return discoverLSPEntrypointByKind(ctx, entrypoint, kind)
	}
}

func discoverLSPEntrypointByKind(ctx context.Context, entrypoint, kind string) (*invocation.DiscoveryResult, error) {
	switch kind {
	case invocation.KindCompose:
		result, composeErr := (invocation.ComposeProvider{}).Discover(ctx, invocation.ResolveOptions{Path: entrypoint})
		if composeErr != nil {
			return nil, composeErr
		}
		if len(result.Invocations) > 0 {
			return result, nil
		}
		if bakeResult, bakeErr := (invocation.BakeProvider{}).Discover(ctx, invocation.ResolveOptions{Path: entrypoint}); bakeErr == nil {
			return bakeResult, nil
		}
		return result, nil
	case invocation.KindBake:
		return (invocation.BakeProvider{}).Discover(ctx, invocation.ResolveOptions{Path: entrypoint})
	default:
		return nil, fmt.Errorf("%s is not a Dockerfile, Compose, or Bake file", entrypoint)
	}
}

// convertDiagnostics converts tally violations to LSP diagnostics.
func convertDiagnostics(violations []rules.Violation) []*protocol.Diagnostic {
	diagnostics := make([]*protocol.Diagnostic, 0, len(violations))
	for _, v := range violations {
		source := "tally"
		if v.Invocation != nil {
			source = "tally/" + v.Invocation.Kind
			if v.Invocation.Name != "" {
				source += ":" + v.Invocation.Name
			}
		}
		d := &protocol.Diagnostic{
			Range:    violationRange(v),
			Severity: ptrTo(severityToLSP(v.Severity)),
			Source:   ptrTo(source),
			Code:     &protocol.IntegerOrString{String: ptrTo(v.RuleCode)},
			Message:  v.Message,
		}
		if v.Invocation != nil {
			data := any(map[string]any{
				"invocation": map[string]string{
					"kind": v.Invocation.Kind,
					"file": v.Invocation.File,
					"name": v.Invocation.Name,
					"key":  v.InvocationKey,
				},
			})
			d.Data = &data
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
	return uint32(v)
}

// isVirtualURI reports whether docURI refers to a virtual document that doesn't
// have a backing file on disk (e.g. untitled:, vscode-notebook-cell:).
func isVirtualURI(docURI string) bool {
	// Fast path for the most common case.
	if strings.HasPrefix(docURI, "file:") {
		return false
	}
	parsed, err := url.Parse(docURI)
	if err != nil {
		// If parsing fails, it's unlikely to be a URI with a scheme.
		// Treat as a file path.
		return false
	}
	return parsed.Scheme != "" && parsed.Scheme != "file"
}

// uriToPath converts a document URI to a local file path for linting purposes.
//
// For file:// URIs this returns the real filesystem path.
// For non-file URIs (e.g. untitled://) this returns a synthetic path anchored
// at the working directory so that config discovery finds the project-level
// .tally.toml. The synthetic name is always "Dockerfile" because tally only
// lints Dockerfiles.
func uriToPath(docURI string) string {
	parsed, err := url.Parse(docURI)
	if err != nil {
		return strings.TrimPrefix(docURI, "file://")
	}

	// Non-file URIs (e.g. untitled://) represent unsaved documents with no
	// on-disk path. Return a synthetic path so config discovery and settings
	// matching work relative to the project root.
	if parsed.Scheme != "" && parsed.Scheme != "file" {
		wd, err := os.Getwd()
		if err != nil {
			return "Dockerfile"
		}
		return filepath.Join(wd, "Dockerfile")
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
