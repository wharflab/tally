package acp

import (
	"context"
	"errors"
	"testing"

	acpsdk "github.com/coder/acp-go-sdk"
)

func TestRunClient_FileSystemDisabled(t *testing.T) {
	t.Parallel()

	_, cancel := context.WithCancelCause(context.Background())
	c := newRunClient(cancel, 0)

	_, err := c.ReadTextFile(context.Background(), acpsdk.ReadTextFileRequest{})
	assertInvalidRequestError(t, err, "filesystem is disabled")

	_, err = c.WriteTextFile(context.Background(), acpsdk.WriteTextFileRequest{})
	assertInvalidRequestError(t, err, "filesystem is disabled")
}

func TestRunClient_TerminalDisabled(t *testing.T) {
	t.Parallel()

	_, cancel := context.WithCancelCause(context.Background())
	c := newRunClient(cancel, 0)

	_, err := c.CreateTerminal(context.Background(), acpsdk.CreateTerminalRequest{})
	assertInvalidRequestError(t, err, "terminal is disabled")

	_, err = c.KillTerminalCommand(context.Background(), acpsdk.KillTerminalCommandRequest{})
	assertInvalidRequestError(t, err, "terminal is disabled")

	_, err = c.TerminalOutput(context.Background(), acpsdk.TerminalOutputRequest{})
	assertInvalidRequestError(t, err, "terminal is disabled")

	_, err = c.ReleaseTerminal(context.Background(), acpsdk.ReleaseTerminalRequest{})
	assertInvalidRequestError(t, err, "terminal is disabled")

	_, err = c.WaitForTerminalExit(context.Background(), acpsdk.WaitForTerminalExitRequest{})
	assertInvalidRequestError(t, err, "terminal is disabled")
}

func TestRunClient_RequestPermission_PrefersRejectAlways(t *testing.T) {
	t.Parallel()

	_, cancel := context.WithCancelCause(context.Background())
	c := newRunClient(cancel, 0)

	resp, err := c.RequestPermission(context.Background(), acpsdk.RequestPermissionRequest{
		Options: []acpsdk.PermissionOption{
			{
				Kind:     acpsdk.PermissionOptionKindRejectAlways,
				OptionId: acpsdk.PermissionOptionId("reject-always"),
			},
			{
				Kind:     acpsdk.PermissionOptionKindAllowOnce,
				OptionId: acpsdk.PermissionOptionId("allow-once"),
			},
		},
	})
	if err != nil {
		t.Fatalf("RequestPermission() error: %v", err)
	}
	if resp.Outcome.Selected == nil || resp.Outcome.Selected.OptionId != "reject-always" {
		t.Fatalf("Selected=%+v, want reject-always", resp.Outcome.Selected)
	}
	if resp.Outcome.Cancelled != nil {
		t.Fatalf("Cancelled=%+v, want nil", resp.Outcome.Cancelled)
	}
}

func TestRunClient_RequestPermission_PrefersRejectOnce(t *testing.T) {
	t.Parallel()

	_, cancel := context.WithCancelCause(context.Background())
	c := newRunClient(cancel, 0)

	resp, err := c.RequestPermission(context.Background(), acpsdk.RequestPermissionRequest{
		Options: []acpsdk.PermissionOption{
			{
				Kind:     acpsdk.PermissionOptionKindAllowOnce,
				OptionId: acpsdk.PermissionOptionId("allow-once"),
			},
			{
				Kind:     acpsdk.PermissionOptionKindRejectOnce,
				OptionId: acpsdk.PermissionOptionId("reject-once"),
			},
		},
	})
	if err != nil {
		t.Fatalf("RequestPermission() error: %v", err)
	}
	if resp.Outcome.Selected == nil || resp.Outcome.Selected.OptionId != "reject-once" {
		t.Fatalf("Selected=%+v, want reject-once", resp.Outcome.Selected)
	}
	if resp.Outcome.Cancelled != nil {
		t.Fatalf("Cancelled=%+v, want nil", resp.Outcome.Cancelled)
	}
}

func TestRunClient_RequestPermission_CancelsWhenNoRejectOption(t *testing.T) {
	t.Parallel()

	_, cancel := context.WithCancelCause(context.Background())
	c := newRunClient(cancel, 0)

	resp, err := c.RequestPermission(context.Background(), acpsdk.RequestPermissionRequest{
		Options: []acpsdk.PermissionOption{
			{
				Kind:     acpsdk.PermissionOptionKindAllowOnce,
				OptionId: acpsdk.PermissionOptionId("allow-once"),
			},
			{
				Kind:     acpsdk.PermissionOptionKindAllowAlways,
				OptionId: acpsdk.PermissionOptionId("allow-always"),
			},
		},
	})
	if err != nil {
		t.Fatalf("RequestPermission() error: %v", err)
	}
	if resp.Outcome.Selected != nil {
		t.Fatalf("Selected=%+v, want nil", resp.Outcome.Selected)
	}
	if resp.Outcome.Cancelled == nil {
		t.Fatalf("Cancelled=nil, want non-nil")
	}
}

func TestRunClient_SessionUpdate_AppendsAgentTextOnly(t *testing.T) {
	t.Parallel()

	_, cancel := context.WithCancelCause(context.Background())
	c := newRunClient(cancel, 0)

	if err := c.SessionUpdate(context.Background(), acpsdk.SessionNotification{
		Update: acpsdk.UpdateAgentMessageText("hello"),
	}); err != nil {
		t.Fatalf("SessionUpdate() error: %v", err)
	}
	if got := c.outputText(); got != "hello" {
		t.Fatalf("outputText=%q, want %q", got, "hello")
	}
	if got := c.outputBytes(); got != len("hello") {
		t.Fatalf("outputBytes=%d, want %d", got, len("hello"))
	}

	// Non-agent updates should not affect output.
	if err := c.SessionUpdate(context.Background(), acpsdk.SessionNotification{
		Update: acpsdk.UpdateUserMessageText("ignored"),
	}); err != nil {
		t.Fatalf("SessionUpdate() error: %v", err)
	}
	if err := c.SessionUpdate(context.Background(), acpsdk.SessionNotification{
		Update: acpsdk.UpdateAgentMessage(acpsdk.ImageBlock("abc", "image/png")),
	}); err != nil {
		t.Fatalf("SessionUpdate() error: %v", err)
	}

	// Empty agent text should be ignored.
	if err := c.SessionUpdate(context.Background(), acpsdk.SessionNotification{Update: acpsdk.UpdateAgentMessageText("")}); err != nil {
		t.Fatalf("SessionUpdate() error: %v", err)
	}

	if got := c.outputText(); got != "hello" {
		t.Fatalf("outputText=%q, want unchanged %q", got, "hello")
	}
}

func TestRunClient_AppendOutput_EnforcesMaxBytes(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancelCause(context.Background())
	c := newRunClient(cancel, 5)

	c.appendOutput("hello")
	if ctx.Err() != nil {
		t.Fatalf("context unexpectedly cancelled: %v", context.Cause(ctx))
	}

	// No-op.
	c.appendOutput("")

	// Would exceed max output bytes (5).
	c.appendOutput("!")

	if ctx.Err() == nil {
		t.Fatalf("context not cancelled, want cancellation due to output limit")
	}
	if !errors.Is(context.Cause(ctx), ErrOutputLimitExceeded) {
		t.Fatalf("cancel cause=%v, want %v", context.Cause(ctx), ErrOutputLimitExceeded)
	}
	if got := c.outputText(); got != "hello" {
		t.Fatalf("outputText=%q, want %q", got, "hello")
	}
}

func assertInvalidRequestError(t *testing.T, err error, wantMsg string) {
	t.Helper()

	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	var re *acpsdk.RequestError
	if !errors.As(err, &re) {
		t.Fatalf("error type=%T, want *acp.RequestError", err)
	}
	if re.Code != -32600 {
		t.Fatalf("RequestError.Code=%d, want -32600", re.Code)
	}
	data, ok := re.Data.(map[string]any)
	if !ok {
		t.Fatalf("RequestError.Data type=%T, want map[string]any", re.Data)
	}
	if got, ok := data["error"].(string); !ok || got != wantMsg {
		t.Fatalf("RequestError.Data[\"error\"]=%v (type %T), want %q", data["error"], data["error"], wantMsg)
	}
}
