package lspserver

import (
	"testing"

	"github.com/stretchr/testify/require"

	protocol "github.com/wharflab/tally/internal/lsp/protocol"
)

func TestParseClientSettings_TracksWorkspaceTrust(t *testing.T) {
	t.Parallel()

	settings, ok := parseClientSettings(map[string]any{
		"tally": map[string]any{
			"version": 1,
			"global": map[string]any{
				"configurationPreference": "editorFirst",
				"workspaceTrusted":        true,
			},
		},
	})
	require.True(t, ok)
	require.True(t, settings.Global.WorkspaceTrusted)
}

func TestHandleInitialize_ConsumesInitializationOptions(t *testing.T) {
	t.Parallel()

	s := New()
	initOpts := any(map[string]any{
		"tally": map[string]any{
			"version": 1,
			"global": map[string]any{
				"configurationPreference": "editorFirst",
				"workspaceTrusted":        true,
			},
		},
	})

	_, err := s.handleInitialize(&protocol.InitializeParams{
		InitializationOptions: &initOpts,
	})
	require.NoError(t, err)
	require.True(t, s.settings.Global.WorkspaceTrusted)
}
