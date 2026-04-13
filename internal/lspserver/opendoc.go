package lspserver

import (
	"context"
	"log"
	"time"

	protocol "github.com/wharflab/tally/internal/lsp/protocol"

	"github.com/wharflab/tally/internal/rules"
)

const openRuleDocCommand = "tally.openRuleDoc"

// showDocActions generates "Show documentation for {ruleCode}" CodeActions
// for violations that have a non-empty DocURL.
// Each action is command-based: it triggers tally.openRuleDoc which sends
// window/showDocument to the client to open the URL in a browser.
func showDocActions(violations []rules.Violation, params *protocol.CodeActionParams) []protocol.CodeAction {
	seen := make(map[string]struct{})
	var actions []protocol.CodeAction

	for _, v := range violations {
		if v.DocURL == "" {
			continue
		}

		vRange := violationRange(v)
		if !rangesOverlap(vRange, params.Range) {
			continue
		}

		if _, ok := seen[v.RuleCode]; ok {
			continue
		}
		seen[v.RuleCode] = struct{}{}

		args := []any{map[string]any{"url": v.DocURL}}
		actions = append(actions, protocol.CodeAction{
			Title: "Show documentation for " + v.RuleCode,
			// Kind intentionally nil: appears in full lightbulb but not in
			// quickfix-only filtered requests (matching ESLint's pattern).
			Command: &protocol.Command{
				Title:     "Show documentation for " + v.RuleCode,
				Command:   openRuleDocCommand,
				Arguments: &args,
			},
		})
	}

	return actions
}

// handleOpenRuleDoc handles the tally.openRuleDoc command.
// It sends a window/showDocument request to the client to open the rule
// documentation URL in the system browser.
func (s *Server) handleOpenRuleDoc(args *[]any) (any, error) {
	docURL := parseOpenRuleDocArgs(args)
	if docURL == "" {
		return nil, nil //nolint:nilnil // LSP: no URL, no action
	}

	// Send window/showDocument asynchronously. If the client doesn't respond
	// in time or doesn't support it, we log and move on.
	conn := s.conn
	go func() {
		reqCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		if err := lspCall(reqCtx, conn, string(protocol.MethodWindowShowDocument),
			&protocol.ShowDocumentParams{
				Uri:      protocol.URI(docURL),
				External: ptrTo(true),
			}); err != nil {
			log.Printf("lsp: window/showDocument failed: %v", err)
		}
	}()

	return nil, nil //nolint:nilnil // LSP: command produces no direct result
}

func parseOpenRuleDocArgs(args *[]any) string {
	if args == nil || len(*args) == 0 {
		return ""
	}
	m, ok := (*args)[0].(map[string]any)
	if !ok {
		return ""
	}
	url, ok := m["url"].(string)
	if !ok {
		return ""
	}
	return url
}
