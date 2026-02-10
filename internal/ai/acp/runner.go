package acp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"time"

	acpsdk "github.com/coder/acp-go-sdk"
)

const (
	defaultMaxAgentOutputBytes = 2 * 1024 * 1024
	defaultStderrTailBytes     = 32 * 1024
	defaultTerminateGrace      = 250 * time.Millisecond
)

type Runner struct {
	maxOutputBytes int
	stderrTail     int
	terminateGrace time.Duration
}

type Option func(*Runner)

func WithMaxOutputBytes(n int) Option {
	return func(r *Runner) { r.maxOutputBytes = n }
}

func WithStderrTailBytes(n int) Option {
	return func(r *Runner) { r.stderrTail = n }
}

func WithTerminateGrace(d time.Duration) Option {
	return func(r *Runner) { r.terminateGrace = d }
}

func NewRunner(opts ...Option) *Runner {
	r := &Runner{
		maxOutputBytes: defaultMaxAgentOutputBytes,
		stderrTail:     defaultStderrTailBytes,
		terminateGrace: defaultTerminateGrace,
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

type Stats struct {
	PromptBytes   int
	ResponseBytes int
	Duration      time.Duration
}

type RunRequest struct {
	Command []string
	Cwd     string
	Timeout time.Duration
	Prompt  string
}

type RunResponse struct {
	Text  string
	Stats Stats
}

type agentProcess struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr *tailBuffer

	terminateGrace time.Duration

	termOnce sync.Once
	termExit *int
	termErr  error
}

type readGate struct {
	r     io.Reader
	once  sync.Once
	ready chan struct{}
}

func newReadGate(r io.Reader) *readGate {
	return &readGate{
		r:     r,
		ready: make(chan struct{}),
	}
}

func (g *readGate) Open() { g.once.Do(func() { close(g.ready) }) }

func (g *readGate) Read(p []byte) (int, error) {
	<-g.ready
	return g.r.Read(p)
}

func startAgentProcess(absCwd string, command []string, stderrTailBytes int, grace time.Duration) (*agentProcess, error) {
	if len(command) == 0 {
		return nil, errors.New("agent command is empty")
	}

	p := &agentProcess{
		cmd:            exec.Command(command[0], command[1:]...), //nolint:gosec // Command is explicit user configuration.
		stderr:         newTailBuffer(stderrTailBytes),
		terminateGrace: grace,
	}
	p.cmd.Dir = absCwd
	configureProcessGroup(p.cmd)
	p.cmd.Stderr = p.stderr

	var err error
	p.stdin, err = p.cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	p.stdout, err = p.cmd.StdoutPipe()
	if err != nil {
		if p.stdin != nil {
			_ = p.stdin.Close()
		}
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	if err := p.cmd.Start(); err != nil {
		if p.stdin != nil {
			_ = p.stdin.Close()
		}
		if p.stdout != nil {
			_ = p.stdout.Close()
		}
		return nil, err
	}
	return p, nil
}

func (p *agentProcess) terminate() (*int, error) {
	p.termOnce.Do(func() {
		p.termExit, p.termErr = terminateAgent(p.cmd, p.terminateGrace)
	})
	return p.termExit, p.termErr
}

func (r *Runner) Run(ctx context.Context, req RunRequest) (RunResponse, error) {
	start := time.Now()

	if len(req.Command) == 0 {
		return RunResponse{}, &RunnerError{Op: "acp run", Err: errors.New("agent command is empty")}
	}

	absCwd, err := makeAbsDir(req.Cwd)
	if err != nil {
		return RunResponse{}, &RunnerError{Op: "acp run", Err: err}
	}

	runCtx := ctx
	var cancel context.CancelFunc
	if req.Timeout > 0 {
		runCtx, cancel = context.WithTimeout(runCtx, req.Timeout)
		defer cancel()
	}
	runCtx, cancelCause := context.WithCancelCause(runCtx)
	defer func() {
		// Ensure any in-flight RPC waits are released.
		cancelCause(context.Canceled)
	}()

	proc, err := startAgentProcess(absCwd, req.Command, r.stderrTail, r.terminateGrace)
	if err != nil {
		return RunResponse{}, &RunnerError{Op: "acp start", Err: err}
	}
	defer func() {
		_, terr := proc.terminate()
		_ = terr
	}()

	client := newRunClient(cancelCause, r.maxOutputBytes)
	stdoutGate := newReadGate(proc.stdout)
	defer stdoutGate.Open()

	conn := acpsdk.NewClientSideConnection(client, proc.stdin, stdoutGate)
	conn.SetLogger(slog.New(slog.DiscardHandler))
	stdoutGate.Open()

	if _, err := conn.Initialize(runCtx, acpsdk.InitializeRequest{
		ProtocolVersion: acpsdk.ProtocolVersionNumber,
		ClientCapabilities: acpsdk.ClientCapabilities{
			Fs:       acpsdk.FileSystemCapability{ReadTextFile: false, WriteTextFile: false},
			Terminal: false,
		},
	}); err != nil {
		exit, termErr := proc.terminate()
		return RunResponse{}, r.wrapErr("acp initialize", errors.Join(err, termErr), proc.stderr, exit)
	}

	sess, err := conn.NewSession(runCtx, acpsdk.NewSessionRequest{Cwd: absCwd, McpServers: []acpsdk.McpServer{}})
	if err != nil {
		exit, termErr := proc.terminate()
		return RunResponse{}, r.wrapErr("acp session", errors.Join(err, termErr), proc.stderr, exit)
	}

	if _, err := conn.Prompt(runCtx, acpsdk.PromptRequest{
		SessionId: sess.SessionId,
		Prompt:    []acpsdk.ContentBlock{acpsdk.TextBlock(req.Prompt)},
	}); err != nil {
		exit, termErr := proc.terminate()
		return RunResponse{}, r.wrapErr("acp prompt", errors.Join(err, termErr), proc.stderr, exit)
	}

	respText := client.outputText()
	stats := Stats{
		PromptBytes:   len(req.Prompt),
		ResponseBytes: client.outputBytes(),
		Duration:      time.Since(start),
	}

	// Always terminate (start-per-fix model).
	if exit, termErr := proc.terminate(); termErr != nil {
		return RunResponse{}, r.wrapErr("acp terminate", termErr, proc.stderr, exit)
	}

	return RunResponse{Text: respText, Stats: stats}, nil
}

func (r *Runner) wrapErr(op string, err error, stderr *tailBuffer, exitCode *int) error {
	return &RunnerError{
		Op:       op,
		Err:      err,
		ExitCode: exitCode,
		Stderr:   stderr.String(),
	}
}

func terminateAgent(cmd *exec.Cmd, grace time.Duration) (*int, error) {
	if cmd == nil || cmd.Process == nil {
		code := 0
		return &code, nil
	}

	if runtime.GOOS == "windows" {
		if err := cmd.Process.Kill(); err != nil && !isNoSuchProcess(err) {
			waitErr := cmd.Wait()
			return exitCodeFromWaitErr(waitErr), err
		}
		waitErr := cmd.Wait()
		return exitCodeFromWaitErr(waitErr), nil
	}

	pid := cmd.Process.Pid

	var termErr error

	// First try a graceful termination.
	if err := killProcessGroup(pid, syscall.SIGTERM); err != nil && !isNoSuchProcess(err) {
		termErr = err
		if killErr := cmd.Process.Kill(); killErr != nil && !isNoSuchProcess(killErr) {
			termErr = errors.Join(termErr, killErr)
		}
	}

	waitCh := make(chan error, 1)
	go func() { waitCh <- cmd.Wait() }()

	if grace > 0 {
		timer := time.NewTimer(grace)
		defer timer.Stop()
		select {
		case waitErr := <-waitCh:
			return exitCodeFromWaitErr(waitErr), termErr
		case <-timer.C:
		}
	}

	// Escalate.
	if err := killProcessGroup(pid, syscall.SIGKILL); err != nil && !isNoSuchProcess(err) {
		termErr = errors.Join(termErr, err)
		if killErr := cmd.Process.Kill(); killErr != nil && !isNoSuchProcess(killErr) {
			termErr = errors.Join(termErr, killErr)
		}
	}

	waitErr := <-waitCh
	return exitCodeFromWaitErr(waitErr), termErr
}

func makeAbsDir(dir string) (string, error) {
	if dir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("getwd: %w", err)
		}
		return cwd, nil
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("abs cwd: %w", err)
	}
	return abs, nil
}

func exitCodeFromWaitErr(err error) *int {
	if err == nil {
		code := 0
		return &code
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		code := ee.ExitCode()
		return &code
	}
	return nil
}
