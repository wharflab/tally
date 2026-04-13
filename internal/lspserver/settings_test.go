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

func TestParseClientSettings_NewFields_Defaults(t *testing.T) {
	t.Parallel()

	// When the new fields are omitted, they should default to sensible values.
	settings, ok := parseClientSettings(map[string]any{
		"tally": map[string]any{
			"version": 1,
			"global":  map[string]any{},
		},
	})
	require.True(t, ok)
	require.True(t, settings.Global.SuppressRuleEnabled, "SuppressRuleEnabled should default to true")
	require.True(t, settings.Global.ShowDocEnabled, "ShowDocEnabled should default to true")
	require.Equal(t, fixAllModeAll, settings.Global.FixAllMode, "FixAllMode should default to 'all'")
}

func TestParseClientSettings_NewFields_Explicit(t *testing.T) {
	t.Parallel()

	settings, ok := parseClientSettings(map[string]any{
		"tally": map[string]any{
			"version": 1,
			"global": map[string]any{
				"suppressRuleEnabled":      false,
				"showDocumentationEnabled": false,
				"fixAllMode":               "problems",
			},
		},
	})
	require.True(t, ok)
	require.False(t, settings.Global.SuppressRuleEnabled)
	require.False(t, settings.Global.ShowDocEnabled)
	require.Equal(t, fixAllModeProblems, settings.Global.FixAllMode)
}

func TestParseClientSettings_FixAllMode_InvalidDefaultsToAll(t *testing.T) {
	t.Parallel()

	settings, ok := parseClientSettings(map[string]any{
		"tally": map[string]any{
			"version": 1,
			"global": map[string]any{
				"fixAllMode": "invalid-value",
			},
		},
	})
	require.True(t, ok)
	require.Equal(t, fixAllModeAll, settings.Global.FixAllMode, "unknown fixAllMode should default to 'all'")
}
