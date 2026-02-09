package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"

	acpsdk "github.com/coder/acp-go-sdk"
)

// Test helper ACP agent used by internal/ai/acp unit tests.
//
// This binary is not part of the tally CLI. It intentionally supports a handful
// of deterministic "modes" to exercise lifecycle and error handling:
// - happy: ACP handshake + prompt emits a short agent message
// - hang-prompt: ACP handshake + prompt blocks until cancelled
// - error-newsession: NewSession returns an error
// - error-prompt: Prompt returns an error
// - stderr-exit: write to stderr and exit non-zero (no ACP)
// - malformed: write invalid JSON-RPC to stdout and exit (no ACP)
//
// Some modes can also spawn a long-lived child process (sleep) to validate
// process-group cleanup.
func main() {
	mode := flag.String("mode", "happy", "test agent mode")
	spawnChild := flag.Bool("spawn-child", false, "spawn a long-lived child process")
	stderrBytes := flag.Int("stderr-bytes", 0, "write N bytes to stderr before exiting (stderr-exit mode)")
	flag.Parse()

	switch *mode {
	case "stderr-exit":
		if *spawnChild {
			if err := startChild(); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(2)
			}
		}
		if *stderrBytes > 0 {
			writeStderrPayload(*stderrBytes)
		} else {
			fmt.Fprintln(os.Stderr, "BEGIN_STDER")
			fmt.Fprintln(os.Stderr, "END_STDER")
		}
		os.Exit(42)
	case "malformed":
		if *spawnChild {
			if err := startChild(); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(2)
			}
		}
		// Emit garbage to stdout (ACP uses line-delimited JSON). Then exit.
		fmt.Fprintln(os.Stdout, "{this is not valid jsonrpc}")
		os.Exit(9)
	default:
		runACP(*mode, *spawnChild)
	}
}

func writeStderrPayload(n int) {
	if _, err := os.Stderr.WriteString("BEGIN_STDER\n"); err != nil {
		return
	}
	if n > 0 {
		buf := make([]byte, n)
		for i := range buf {
			buf[i] = 'x'
		}
		if _, err := os.Stderr.Write(buf); err != nil {
			return
		}
		if _, err := os.Stderr.Write([]byte{'\n'}); err != nil {
			return
		}
	}
	if _, err := os.Stderr.WriteString("END_STDER\n"); err != nil {
		return
	}
}

func runACP(mode string, spawnChild bool) {
	if spawnChild {
		if err := startChild(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
	}
	ag := &testAgent{mode: mode, spawnChild: spawnChild}
	asc := acpsdk.NewAgentSideConnection(ag, os.Stdout, os.Stdin)
	ag.conn = asc
	// Block until the peer disconnects.
	<-asc.Done()
}

type testAgent struct {
	mode       string
	spawnChild bool

	conn *acpsdk.AgentSideConnection
}

var _ acpsdk.Agent = (*testAgent)(nil)

func (a *testAgent) Authenticate(ctx context.Context, params acpsdk.AuthenticateRequest) (acpsdk.AuthenticateResponse, error) {
	return acpsdk.AuthenticateResponse{}, nil
}

func (a *testAgent) Initialize(ctx context.Context, params acpsdk.InitializeRequest) (acpsdk.InitializeResponse, error) {
	return acpsdk.InitializeResponse{
		ProtocolVersion:   acpsdk.ProtocolVersionNumber,
		AgentCapabilities: acpsdk.AgentCapabilities{LoadSession: false},
	}, nil
}

func (a *testAgent) Cancel(ctx context.Context, params acpsdk.CancelNotification) error { return nil }

func (a *testAgent) NewSession(ctx context.Context, params acpsdk.NewSessionRequest) (acpsdk.NewSessionResponse, error) {
	if a.mode == "error-newsession" {
		return acpsdk.NewSessionResponse{}, errors.New("forced NewSession failure")
	}
	return acpsdk.NewSessionResponse{SessionId: acpsdk.SessionId("sess_test")}, nil
}

func (a *testAgent) Prompt(ctx context.Context, params acpsdk.PromptRequest) (acpsdk.PromptResponse, error) {
	if a.mode == "error-prompt" {
		return acpsdk.PromptResponse{}, errors.New("forced Prompt failure")
	}
	if a.mode == "hang-prompt" {
		<-ctx.Done()
		return acpsdk.PromptResponse{StopReason: acpsdk.StopReasonCancelled}, nil
	}

	// happy
	if err := a.conn.SessionUpdate(ctx, acpsdk.SessionNotification{
		SessionId: params.SessionId,
		Update:    acpsdk.UpdateAgentMessageText("hello from test agent"),
	}); err != nil {
		return acpsdk.PromptResponse{}, err
	}
	time.Sleep(10 * time.Millisecond)
	return acpsdk.PromptResponse{StopReason: acpsdk.StopReasonEndTurn}, nil
}

func (a *testAgent) SetSessionMode(ctx context.Context, params acpsdk.SetSessionModeRequest) (acpsdk.SetSessionModeResponse, error) {
	return acpsdk.SetSessionModeResponse{}, nil
}

func startChild() error {
	cmd := exec.Command("sleep", "10000")
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		_, werr := fmt.Fprintf(os.Stderr, "FAILED_TO_START_CHILD: %v\n", err)
		_ = werr
		return err
	}
	if _, err := fmt.Fprintf(os.Stderr, "TEST_CHILD_PID=%d\n", cmd.Process.Pid); err != nil {
		return err
	}
	return nil
}
