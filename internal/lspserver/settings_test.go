package lspserver

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/exp/jsonrpc2"

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

func TestDidChangeConfigurationDiagnosticRefreshIgnoresCanceledContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	listener, err := jsonrpc2.NetPipe(ctx)
	require.NoError(t, err)

	s := New()
	s.diagMu.Lock()
	s.pushDiagnostics = false
	s.supportsDiagnosticPullMode = true
	s.supportsDiagnosticRefresh = true
	s.diagMu.Unlock()

	server, err := jsonrpc2.Serve(ctx, listener, &serverBinder{server: s})
	require.NoError(t, err)

	refreshCh := make(chan struct{}, 1)
	client, err := jsonrpc2.Dial(ctx, listener.Dialer(), jsonrpc2.ConnectionOptions{
		Framer: jsonrpc2.HeaderFramer(),
		Handler: jsonrpc2.HandlerFunc(func(_ context.Context, req *jsonrpc2.Request) (any, error) {
			if req.Method == string(protocol.MethodWorkspaceDiagnosticRefresh) {
				refreshCh <- struct{}{}
				return jsonNull, nil
			}
			return nil, jsonrpc2.ErrNotHandled
		}),
	})
	require.NoError(t, err)

	var shutdownResult any
	require.NoError(t, client.Call(ctx, string(protocol.MethodShutdown), nil).Await(ctx, &shutdownResult))

	t.Cleanup(func() {
		require.NoError(t, client.Close())
		require.NoError(t, listener.Close())
		if err := server.Wait(); err != nil {
			t.Logf("jsonrpc2 server wait: %v", err)
		}
	})

	notificationCtx, cancelNotification := context.WithCancel(context.Background())
	cancelNotification()
	s.handleDidChangeConfiguration(notificationCtx, &protocol.DidChangeConfigurationParams{
		Settings: map[string]any{
			"tally": map[string]any{
				"version": 1,
				"global":  map[string]any{},
			},
		},
	})

	select {
	case <-refreshCh:
	case <-time.After(time.Second):
		t.Fatal("expected workspace/diagnostic/refresh despite canceled notification context")
	}
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
