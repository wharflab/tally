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
	"github.com/sourcegraph/jsonrpc2"

	protocol "github.com/tinovyatkin/tally/internal/lsp/protocol"

	"github.com/tinovyatkin/tally/internal/config"
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
}

func applyDefaultPreference(pref config.ConfigurationPreference) config.ConfigurationPreference {
	if pref == "" {
		return config.ConfigurationPreferenceEditorFirst
	}
	return pref
}

func defaultClientSettings() clientSettings {
	return clientSettings{
		Global: folderSettings{
			ConfigurationPreference: config.ConfigurationPreferenceEditorFirst,
		},
	}
}

func (s *Server) handleDidChangeConfiguration(
	ctx context.Context,
	conn *jsonrpc2.Conn,
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
			s.publishDiagnostics(ctx, conn, doc)
		}
		return
	}

	// Pull model: request a refresh so the client re-pulls diagnostics.
	if s.diagnosticRefreshSupported() {
		reqCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		if err := conn.Call(reqCtx, string(protocol.MethodWorkspaceDiagnosticRefresh), nil, nil); err != nil {
			log.Printf("lsp: workspace/diagnostic/refresh failed: %v", err)
		}
	}
}

func (s *Server) settingsForFile(filePath string) folderSettings {
	s.settingsMu.RLock()
	defer s.settingsMu.RUnlock()

	filePath = filepath.Clean(filePath)

	best := s.settings.Global
	for _, ws := range s.settings.Workspaces {
		if ws.Root == "" {
			continue
		}
		if pathWithin(ws.Root, filePath) {
			best = ws.Settings
			break
		}
	}
	return best
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
		},
	}

	for _, ws := range wire.Workspaces {
		out.Workspaces = append(out.Workspaces, workspaceFolderSettings{
			Root: uriToPath(ws.URI),
			Settings: folderSettings{
				ConfigurationPreference: applyDefaultPreference(ws.Settings.ConfigurationPreference),
				ConfigurationOverrides:  toOverridesMap(ws.Settings.Configuration),
			},
		})
	}

	slices.SortFunc(out.Workspaces, func(a, b workspaceFolderSettings) int {
		// Prefer longer roots first so nested workspaces win.
		return cmp.Compare(len(b.Root), len(a.Root))
	})

	return out, true
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
