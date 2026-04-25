package lspserver

import (
	"cmp"
	"context"
	"log"
	"path/filepath"
	"slices"
	"strings"
	"time"

	jsonv2 "encoding/json/v2"

	protocol "github.com/wharflab/tally/internal/lsp/protocol"

	"github.com/wharflab/tally/internal/config"
)

type clientSettings struct {
	Global     folderSettings
	Workspaces []workspaceFolderSettings
}

type workspaceFolderSettings struct {
	Root     string
	Settings folderSettings
}

type folderSettings struct {
	ConfigurationPreference config.ConfigurationPreference
	ConfigurationOverrides  map[string]any
	WorkspaceTrusted        bool
	SuppressRuleEnabled     bool
	ShowDocEnabled          bool
	FixAllMode              string // "all" (iterative) or "problems" (single-pass)
	InvocationEntrypoints   []string
}

func applyDefaultPreference(pref config.ConfigurationPreference) config.ConfigurationPreference {
	if pref == "" {
		return config.ConfigurationPreferenceEditorFirst
	}
	return pref
}

const (
	fixAllModeAll      = "all"
	fixAllModeProblems = "problems"

	watchedFilesDebounceDelay = 250 * time.Millisecond
)

func defaultClientSettings() clientSettings {
	return clientSettings{
		Global: folderSettings{
			ConfigurationPreference: config.ConfigurationPreferenceEditorFirst,
			WorkspaceTrusted:        false,
			SuppressRuleEnabled:     true,
			ShowDocEnabled:          true,
			FixAllMode:              fixAllModeAll,
		},
	}
}

func (s *Server) handleDidChangeConfiguration(
	ctx context.Context,
	params *protocol.DidChangeConfigurationParams,
) {
	next, ok := parseClientSettings(params.Settings)
	if !ok {
		log.Printf("lsp: didChangeConfiguration: unable to parse settings payload")
		return
	}

	s.settingsMu.Lock()
	s.settings = next
	s.settingsMu.Unlock()

	// Settings affect lint results, so clear caches.
	s.lintCache.clear()

	// Push model: recompute and publish diagnostics immediately.
	if s.pushDiagnosticsEnabled() {
		for _, doc := range s.documents.All() {
			s.publishDiagnostics(ctx, doc)
		}
		return
	}

	// Pull model: request a refresh so the client re-pulls diagnostics.
	// Run asynchronously to avoid blocking the message delivery goroutine,
	// which could deadlock if the client needs to send requests before
	// responding to the refresh.
	if s.diagnosticRefreshSupported() {
		s.requestDiagnosticRefresh(context.WithoutCancel(ctx))
	}
}

func (s *Server) handleDidChangeWatchedFiles(ctx context.Context, _ *protocol.DidChangeWatchedFilesParams) {
	rootCtx := context.WithoutCancel(ctx)
	s.watchedFilesMu.Lock()
	s.watchedFilesSeq++
	seq := s.watchedFilesSeq
	if s.watchedFilesTimer != nil {
		s.watchedFilesTimer.Stop()
	}
	s.watchedFilesTimer = time.AfterFunc(watchedFilesDebounceDelay, func() {
		s.runDidChangeWatchedFiles(rootCtx, seq)
	})
	s.watchedFilesMu.Unlock()
}

func (s *Server) runDidChangeWatchedFiles(ctx context.Context, seq uint64) {
	s.watchedFilesMu.Lock()
	if seq != s.watchedFilesSeq {
		s.watchedFilesMu.Unlock()
		return
	}
	s.watchedFilesTimer = nil
	s.watchedFilesMu.Unlock()

	s.lintCache.clear()
	if s.pushDiagnosticsEnabled() {
		for _, doc := range s.documents.All() {
			s.publishDiagnostics(ctx, doc)
		}
		return
	}
	if s.diagnosticRefreshSupported() {
		s.requestDiagnosticRefresh(context.WithoutCancel(ctx))
	}
}

func (s *Server) requestDiagnosticRefresh(rootCtx context.Context) {
	conn := s.conn
	go func() {
		refreshCtx, cancel := context.WithTimeout(rootCtx, 2*time.Second)
		defer cancel()
		if err := conn.Call(refreshCtx, string(protocol.MethodWorkspaceDiagnosticRefresh), nil).Await(refreshCtx, nil); err != nil {
			log.Printf("lsp: workspace/diagnostic/refresh failed: %v", err)
		}
	}()
}

func (s *Server) settingsForFile(filePath string) folderSettings {
	_, settings := s.workspaceSettingsForFile(filePath)
	return settings
}

func (s *Server) workspaceSettingsForFile(filePath string) (string, folderSettings) {
	s.settingsMu.RLock()
	defer s.settingsMu.RUnlock()

	filePath = filepath.Clean(filePath)

	root := ""
	best := s.settings.Global
	for _, ws := range s.settings.Workspaces {
		if ws.Root == "" {
			continue
		}
		if pathWithin(ws.Root, filePath) {
			root = ws.Root
			best = ws.Settings
			break
		}
	}
	return root, best
}

func pathWithin(root, filePath string) bool {
	root = filepath.Clean(root)
	rel, err := filepath.Rel(root, filePath)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	if strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
		return false
	}
	return !filepath.IsAbs(rel)
}

type settingsEnvelopeWire struct {
	Version    int                     `json:"version"`
	Global     folderSettingsWire      `json:"global"`
	Workspaces []workspaceSettingsWire `json:"workspaces"`
}

type workspaceSettingsWire struct {
	URI      string             `json:"uri"`
	Settings folderSettingsWire `json:"settings"`
}

type folderSettingsWire struct {
	ConfigurationPreference config.ConfigurationPreference `json:"configurationPreference"`
	Configuration           any                            `json:"configuration"`
	WorkspaceTrusted        bool                           `json:"workspaceTrusted"`
	SuppressRuleEnabled     *bool                          `json:"suppressRuleEnabled"`
	ShowDocEnabled          *bool                          `json:"showDocumentationEnabled"`
	FixAllMode              string                         `json:"fixAllMode"`
	InvocationEntrypoints   []string                       `json:"invocationEntrypoints"`
}

func parseClientSettings(settings any) (clientSettings, bool) {
	inner := settings
	if m, ok := settings.(map[string]any); ok {
		if v, ok := m["tally"]; ok {
			inner = v
		}
	}

	raw, err := jsonv2.Marshal(inner)
	if err != nil {
		return clientSettings{}, false
	}

	var wire settingsEnvelopeWire
	if err := jsonv2.Unmarshal(raw, &wire); err != nil {
		return clientSettings{}, false
	}

	out := clientSettings{
		Global: folderSettings{
			ConfigurationPreference: applyDefaultPreference(wire.Global.ConfigurationPreference),
			ConfigurationOverrides:  toOverridesMap(wire.Global.Configuration),
			WorkspaceTrusted:        wire.Global.WorkspaceTrusted,
			SuppressRuleEnabled:     boolPtrTrue(wire.Global.SuppressRuleEnabled),
			ShowDocEnabled:          boolPtrTrue(wire.Global.ShowDocEnabled),
			FixAllMode:              fixAllModeOrDefault(wire.Global.FixAllMode),
			InvocationEntrypoints:   cleanEntrypoints(wire.Global.InvocationEntrypoints),
		},
	}

	for _, ws := range wire.Workspaces {
		out.Workspaces = append(out.Workspaces, workspaceFolderSettings{
			Root: uriToPath(ws.URI),
			Settings: folderSettings{
				ConfigurationPreference: applyDefaultPreference(ws.Settings.ConfigurationPreference),
				ConfigurationOverrides:  toOverridesMap(ws.Settings.Configuration),
				WorkspaceTrusted:        ws.Settings.WorkspaceTrusted,
				SuppressRuleEnabled:     boolPtrTrue(ws.Settings.SuppressRuleEnabled),
				ShowDocEnabled:          boolPtrTrue(ws.Settings.ShowDocEnabled),
				FixAllMode:              fixAllModeOrDefault(ws.Settings.FixAllMode),
				InvocationEntrypoints:   cleanEntrypoints(ws.Settings.InvocationEntrypoints),
			},
		})
	}

	slices.SortFunc(out.Workspaces, func(a, b workspaceFolderSettings) int {
		// Prefer longer roots first so nested workspaces win.
		return cmp.Compare(len(b.Root), len(a.Root))
	})

	return out, true
}

func cleanEntrypoints(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	seen := map[string]struct{}{}
	for _, path := range in {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		path = filepath.Clean(path)
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		out = append(out, path)
	}
	return out
}

// boolPtrTrue returns the value pointed to by p, or true if p is nil.
func boolPtrTrue(p *bool) bool {
	if p == nil {
		return true
	}
	return *p
}

func fixAllModeOrDefault(mode string) string {
	switch mode {
	case fixAllModeAll, fixAllModeProblems:
		return mode
	default:
		return fixAllModeAll
	}
}

func toOverridesMap(v any) map[string]any {
	if v == nil {
		return nil
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	if len(m) == 0 {
		return nil
	}
	return m
}
