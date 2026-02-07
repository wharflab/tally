package lspserver

import (
	"bytes"
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

	"github.com/sourcegraph/jsonrpc2"

	protocol "github.com/tinovyatkin/tally/internal/lsp/protocol"

	"github.com/tinovyatkin/tally/internal/config"
	"github.com/tinovyatkin/tally/internal/directive"
	"github.com/tinovyatkin/tally/internal/dockerfile"
	"github.com/tinovyatkin/tally/internal/processor"
	"github.com/tinovyatkin/tally/internal/rules"
	_ "github.com/tinovyatkin/tally/internal/rules/all" // Register all rules.
	"github.com/tinovyatkin/tally/internal/rules/buildkit/fixes"
	"github.com/tinovyatkin/tally/internal/semantic"
	"github.com/tinovyatkin/tally/internal/sourcemap"
)

// publishDiagnostics lints a document and publishes diagnostics to the client.
func (s *Server) publishDiagnostics(ctx context.Context, conn *jsonrpc2.Conn, doc *Document) {
	docURI := doc.URI
	content := doc.Content

	violations := s.lintContent(docURI, []byte(content))
	diagnostics := convertDiagnostics(violations)

	if err := lspNotify(ctx, conn, string(protocol.MethodTextDocumentPublishDiagnostics), &protocol.PublishDiagnosticsParams{
		Uri:         protocol.DocumentUri(docURI),
		Diagnostics: diagnostics,
	}); err != nil {
		log.Printf("lsp: failed to publish diagnostics for %s: %v", docURI, err)
	}
}

// clearDiagnostics sends an empty diagnostics array to clear issues for a URI.
func clearDiagnostics(ctx context.Context, conn *jsonrpc2.Conn, docURI string) {
	if err := lspNotify(ctx, conn, string(protocol.MethodTextDocumentPublishDiagnostics), &protocol.PublishDiagnosticsParams{
		Uri:         protocol.DocumentUri(docURI),
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
	return s.pullDiagnosticsFromDisk(filePath, params.PreviousResultId)
}

// pullDiagnosticsFromDisk reads content from disk and returns a diagnostic report.
//
//nolint:nilerr // gracefully returns empty diagnostics for unreadable files
func (s *Server) pullDiagnosticsFromDisk(filePath string, previousResultID *string) (any, error) {
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

	violations := lintFile(filePath, content)
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

// lintContent runs the full tally lint pipeline on in-memory content.
func (s *Server) lintContent(docURI string, content []byte) []rules.Violation {
	filePath := uriToPath(docURI)
	return lintFile(filePath, content)
}

// lintFile runs the full lint pipeline for a file path and content.
func lintFile(filePath string, content []byte) []rules.Violation {
	cfg, err := config.Load(filePath)
	if err != nil {
		log.Printf("lsp: config load error for %s: %v", filePath, err)
		cfg = config.Default()
	}

	parseResult, err := dockerfile.Parse(bytes.NewReader(content), cfg)
	if err != nil {
		log.Printf("lsp: parse error for %s: %v", filePath, err)
		return nil
	}

	sm := sourcemap.New(content)
	directiveResult := directive.Parse(sm, nil)

	sem := semantic.NewBuilder(parseResult, nil, filePath).
		WithShellDirectives(directiveResult.ShellDirectives).
		Build()

	enabledRules := computeEnabledRules(cfg)

	baseInput := rules.LintInput{
		File:               filePath,
		AST:                parseResult.AST,
		Stages:             parseResult.Stages,
		MetaArgs:           parseResult.MetaArgs,
		Source:             content,
		Semantic:           sem,
		EnabledRules:       enabledRules,
		HeredocMinCommands: getHeredocMinCommands(cfg),
	}

	violations := make([]rules.Violation, 0, len(sem.ConstructionIssues())+len(rules.All())+len(parseResult.Warnings))
	for _, issue := range sem.ConstructionIssues() {
		violations = append(violations, rules.NewViolation(
			rules.NewLocationFromRange(issue.File, issue.Location),
			issue.Code, issue.Message, rules.SeverityError,
		).WithDocURL(issue.DocURL))
	}

	for _, rule := range rules.All() {
		input := baseInput
		input.Config = cfg.Rules.GetOptions(rule.Metadata().Code)
		violations = append(violations, rule.Check(input)...)
	}

	for _, w := range parseResult.Warnings {
		violations = append(violations, rules.NewViolationFromBuildKitWarning(
			filePath, w.RuleName, w.Description, w.URL, w.Message, w.Location,
		))
	}

	fixes.EnrichBuildKitFixes(violations, sem, content)

	fileConfigs := map[string]*config.Config{filePath: cfg}
	fileSources := map[string][]byte{filePath: content}
	chain := processor.NewChain(
		processor.NewSeverityOverride(),
		processor.NewEnableFilter(),
		processor.NewInlineDirectiveFilter(),
		processor.NewDeduplication(),
		processor.NewSorting(),
	)
	procCtx := processor.NewContext(fileConfigs, cfg, fileSources)
	violations = chain.Process(violations, procCtx)

	return violations
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
		return protocol.DiagnosticSeverityHint
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
	// On Windows, file URIs look like file:///C:/path, so Path is /C:/path.
	if runtime.GOOS == "windows" && len(path) > 2 && path[0] == '/' && path[2] == ':' {
		path = path[1:]
	}
	return filepath.FromSlash(path)
}

// computeEnabledRules returns a sorted list of enabled rule codes.
func computeEnabledRules(cfg *config.Config) []string {
	var enabled []string
	for _, rule := range rules.All() {
		meta := rule.Metadata()
		if isRuleEnabled(meta.Code, meta.DefaultSeverity, cfg) {
			enabled = append(enabled, meta.Code)
		}
	}
	return enabled
}

// isRuleEnabled checks if a rule is enabled based on config.
func isRuleEnabled(ruleCode string, defaultSeverity rules.Severity, cfg *config.Config) bool {
	if cfg == nil {
		return defaultSeverity != rules.SeverityOff
	}
	if enabled := cfg.Rules.IsEnabled(ruleCode); enabled != nil {
		return *enabled
	}
	if sev := cfg.Rules.GetSeverity(ruleCode); sev == "off" {
		return false
	}
	if defaultSeverity == rules.SeverityOff {
		ruleConfig := cfg.Rules.Get(ruleCode)
		return ruleConfig != nil && len(ruleConfig.Options) > 0
	}
	return defaultSeverity != rules.SeverityOff
}

// getHeredocMinCommands extracts the min-commands setting from config.
func getHeredocMinCommands(cfg *config.Config) int {
	if cfg == nil {
		return 0
	}
	opts := cfg.Rules.GetOptions(rules.HeredocRuleCode)
	if len(opts) == 0 {
		return 0
	}
	if minCmds, ok := opts["min-commands"]; ok {
		switch v := minCmds.(type) {
		case int:
			return v
		case float64:
			return int(v)
		}
	}
	return 0
}
