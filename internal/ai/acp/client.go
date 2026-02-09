package acp

import (
	"context"
	"sync"

	acpsdk "github.com/coder/acp-go-sdk"
)

type runClient struct {
	cancel context.CancelCauseFunc

	maxOutputBytes int

	mu  sync.Mutex
	out []byte
}

var _ acpsdk.Client = (*runClient)(nil)

func newRunClient(cancel context.CancelCauseFunc, maxOutputBytes int) *runClient {
	return &runClient{
		cancel:         cancel,
		maxOutputBytes: maxOutputBytes,
	}
}

func (c *runClient) ReadTextFile(ctx context.Context, params acpsdk.ReadTextFileRequest) (acpsdk.ReadTextFileResponse, error) {
	return acpsdk.ReadTextFileResponse{}, acpsdk.NewInvalidRequest(map[string]any{"error": "filesystem is disabled"})
}

func (c *runClient) WriteTextFile(ctx context.Context, params acpsdk.WriteTextFileRequest) (acpsdk.WriteTextFileResponse, error) {
	return acpsdk.WriteTextFileResponse{}, acpsdk.NewInvalidRequest(map[string]any{"error": "filesystem is disabled"})
}

func (c *runClient) RequestPermission(
	ctx context.Context,
	params acpsdk.RequestPermissionRequest,
) (acpsdk.RequestPermissionResponse, error) {
	// Default: deny. Prefer an explicit reject option if one is provided.
	for _, opt := range params.Options {
		if opt.Kind == acpsdk.PermissionOptionKindRejectAlways ||
			opt.Kind == acpsdk.PermissionOptionKindRejectOnce {
			return acpsdk.RequestPermissionResponse{
				Outcome: acpsdk.RequestPermissionOutcome{
					Selected: &acpsdk.RequestPermissionOutcomeSelected{OptionId: opt.OptionId},
				},
			}, nil
		}
	}
	return acpsdk.RequestPermissionResponse{
		Outcome: acpsdk.RequestPermissionOutcome{
			Cancelled: &acpsdk.RequestPermissionOutcomeCancelled{},
		},
	}, nil
}

func (c *runClient) SessionUpdate(ctx context.Context, params acpsdk.SessionNotification) error {
	u := params.Update
	if u.AgentMessageChunk != nil {
		if u.AgentMessageChunk.Content.Text != nil {
			c.appendOutput(u.AgentMessageChunk.Content.Text.Text)
		}
	}
	return nil
}

func (c *runClient) CreateTerminal(
	ctx context.Context,
	params acpsdk.CreateTerminalRequest,
) (acpsdk.CreateTerminalResponse, error) {
	return acpsdk.CreateTerminalResponse{}, acpsdk.NewInvalidRequest(map[string]any{"error": "terminal is disabled"})
}

func (c *runClient) KillTerminalCommand(
	ctx context.Context,
	params acpsdk.KillTerminalCommandRequest,
) (acpsdk.KillTerminalCommandResponse, error) {
	return acpsdk.KillTerminalCommandResponse{}, acpsdk.NewInvalidRequest(map[string]any{"error": "terminal is disabled"})
}

func (c *runClient) TerminalOutput(
	ctx context.Context,
	params acpsdk.TerminalOutputRequest,
) (acpsdk.TerminalOutputResponse, error) {
	return acpsdk.TerminalOutputResponse{}, acpsdk.NewInvalidRequest(map[string]any{"error": "terminal is disabled"})
}

func (c *runClient) ReleaseTerminal(
	ctx context.Context,
	params acpsdk.ReleaseTerminalRequest,
) (acpsdk.ReleaseTerminalResponse, error) {
	return acpsdk.ReleaseTerminalResponse{}, acpsdk.NewInvalidRequest(map[string]any{"error": "terminal is disabled"})
}

func (c *runClient) WaitForTerminalExit(
	ctx context.Context,
	params acpsdk.WaitForTerminalExitRequest,
) (acpsdk.WaitForTerminalExitResponse, error) {
	return acpsdk.WaitForTerminalExitResponse{}, acpsdk.NewInvalidRequest(map[string]any{"error": "terminal is disabled"})
}

func (c *runClient) outputBytes() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.out)
}

func (c *runClient) outputText() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return string(c.out)
}

func (c *runClient) appendOutput(text string) {
	if text == "" {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.maxOutputBytes > 0 && len(c.out)+len(text) > c.maxOutputBytes {
		c.cancel(ErrOutputLimitExceeded)
		return
	}
	c.out = append(c.out, text...)
}
