package psanalyzer

import (
	"bufio"
	"bytes"
	"context"
	_ "embed"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultExecutable = "pwsh"
	executableEnv     = "TALLY_POWERSHELL"

	startupTimeout        = 5 * time.Minute
	progressNoticeDelay   = 3 * time.Second
	progressNoticeRepeat  = 15 * time.Second
	progressNoticeEnv     = "TALLY_POWERSHELL_PROGRESS"
	progressNoticeEnvMute = "0"
)

//go:embed sidecar/Tally.PSSA.Sidecar.ps1
var sidecarScript []byte

type Runner struct {
	mu sync.Mutex

	executable string

	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	waitCh chan error

	tempDir string
	nextID  int64

	stderr tailBuffer
}

type sidecarProcess struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdout  io.ReadCloser
	stderr  io.ReadCloser
	tempDir string
}

func NewRunner() *Runner {
	return &Runner{}
}

func (r *Runner) Analyze(ctx context.Context, req AnalyzeRequest) ([]Diagnostic, error) {
	if strings.TrimSpace(req.Path) == "" && strings.TrimSpace(req.ScriptDefinition) == "" {
		return nil, errors.New("path or script definition is required")
	}

	wireReq := request{
		Op:               "analyze",
		Path:             req.Path,
		ScriptDefinition: req.ScriptDefinition,
	}
	if !req.Settings.isZero() {
		settings := req.Settings
		wireReq.Settings = &settings
	}

	resp, err := r.sendRequest(ctx, wireReq)
	if err != nil {
		return nil, err
	}
	return resp.Diagnostics, nil
}

func (r *Runner) Format(ctx context.Context, req FormatRequest) (string, error) {
	if strings.TrimSpace(req.ScriptDefinition) == "" {
		return "", errors.New("script definition is required")
	}

	resp, err := r.sendRequest(ctx, request{
		Op:               "format",
		ScriptDefinition: req.ScriptDefinition,
	})
	if err != nil {
		return "", err
	}
	return resp.Formatted, nil
}

func (r *Runner) FormatPowerShell(ctx context.Context, script string) (string, error) {
	return r.Format(ctx, FormatRequest{ScriptDefinition: script})
}

func (r *Runner) Close(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.cmd == nil {
		r.cleanupTempDir()
		return nil
	}

	r.nextID++
	id := strconv.FormatInt(r.nextID, 10)
	shutdownErr := error(nil)
	if _, err := r.roundTrip(ctx, request{ID: id, Op: "shutdown"}); err != nil {
		shutdownErr = err
	}
	if r.stdin != nil {
		if err := r.stdin.Close(); err != nil && shutdownErr == nil {
			shutdownErr = err
		}
	}

	select {
	case err := <-r.waitCh:
		r.clearProcess()
		r.cleanupTempDir()
		if err != nil {
			return err
		}
		return shutdownErr
	case <-ctx.Done():
		r.stopProcess()
		r.cleanupTempDir()
		return ctx.Err()
	}
}

func (r *Runner) ensureStarted(ctx context.Context) error {
	if r.cmd != nil {
		return nil
	}

	exe, err := r.findExecutable()
	if err != nil {
		return err
	}

	proc, err := startSidecarProcess(exe)
	if err != nil {
		return err
	}
	r.attachProcess(proc)

	if err := r.awaitReady(ctx); err != nil {
		r.stopProcess()
		r.cleanupTempDir()
		return err
	}

	return nil
}

func startSidecarProcess(exe string) (*sidecarProcess, error) {
	tempDir, err := os.MkdirTemp("", "tally-psanalyzer-*")
	if err != nil {
		return nil, fmt.Errorf("create psanalyzer temp dir: %w", err)
	}
	sidecarPath := filepath.Join(tempDir, "Tally.PSSA.Sidecar.ps1")
	if err := os.WriteFile(sidecarPath, sidecarScript, 0o600); err != nil {
		_ = os.RemoveAll(tempDir)
		return nil, fmt.Errorf("write psanalyzer sidecar: %w", err)
	}

	//nolint:gosec // G204: exe is pwsh from PATH or explicit TALLY_POWERSHELL configuration.
	cmd := exec.Command(
		exe,
		"-NoLogo",
		"-NoProfile",
		"-NonInteractive",
		"-File",
		sidecarPath,
	)
	cmd.Env = normalizePowerShellEnv(runtime.GOOS, os.Environ())

	stdin, err := cmd.StdinPipe()
	if err != nil {
		_ = os.RemoveAll(tempDir)
		return nil, fmt.Errorf("open psanalyzer stdin: %w", err)
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		_ = os.RemoveAll(tempDir)
		return nil, fmt.Errorf("open psanalyzer stdout: %w", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		_ = os.RemoveAll(tempDir)
		return nil, fmt.Errorf("open psanalyzer stderr: %w", err)
	}

	if err := cmd.Start(); err != nil {
		_ = os.RemoveAll(tempDir)
		return nil, fmt.Errorf("start %s: %w", exe, err)
	}

	return &sidecarProcess{
		cmd:     cmd,
		stdin:   stdin,
		stdout:  stdoutPipe,
		stderr:  stderrPipe,
		tempDir: tempDir,
	}, nil
}

func (r *Runner) attachProcess(proc *sidecarProcess) {
	r.stderr.reset()
	r.cmd = proc.cmd
	r.stdin = proc.stdin
	r.stdout = bufio.NewReader(proc.stdout)
	r.waitCh = make(chan error, 1)
	r.tempDir = proc.tempDir

	go r.stderr.capture(proc.stderr)
	go func() {
		r.waitCh <- proc.cmd.Wait()
	}()
}

func (r *Runner) awaitReady(ctx context.Context) error {
	handshakeCtx := ctx
	if _, hasDeadline := handshakeCtx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		handshakeCtx, cancel = context.WithTimeout(ctx, startupTimeout)
		defer cancel()
	}

	progress := newDelayedProgressNotice(progressNoticeDelay, progressNoticeRepeat)
	defer progress.stop()

	for {
		line, err := r.readLine(handshakeCtx)
		if err != nil {
			return fmt.Errorf("read psanalyzer handshake: %w%s", err, r.stderrSuffix())
		}

		var resp response
		if err := json.Unmarshal(bytes.TrimSpace(line), &resp); err != nil {
			return fmt.Errorf("parse psanalyzer handshake: %w%s", err, r.stderrSuffix())
		}
		if resp.Progress {
			progress.start(resp.Message)
			continue
		}
		if !resp.Ready {
			if resp.Error != "" {
				return errors.New(resp.Error)
			}
			return errors.New("psanalyzer sidecar did not report ready")
		}

		return nil
	}
}

func (r *Runner) findExecutable() (string, error) {
	if r.executable != "" {
		return r.executable, nil
	}

	candidate := os.Getenv(executableEnv)
	if candidate == "" {
		candidate = defaultExecutable
	}

	exe, err := exec.LookPath(candidate)
	if err != nil {
		return "", fmt.Errorf("PowerShell 7 executable %q not found; install pwsh or set %s", candidate, executableEnv)
	}
	r.executable = exe
	return exe, nil
}

func (r *Runner) sendRequest(ctx context.Context, wireReq request) (response, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := r.ensureStarted(ctx); err != nil {
		return response{}, err
	}

	r.nextID++
	id := strconv.FormatInt(r.nextID, 10)
	wireReq.ID = id

	resp, err := r.roundTrip(ctx, wireReq)
	if err != nil {
		r.stopProcess()
		return response{}, err
	}
	if resp.ID != id {
		r.stopProcess()
		return response{}, fmt.Errorf("psanalyzer sidecar returned response id %q for request %q", resp.ID, id)
	}
	if !resp.OK {
		if resp.Error == "" {
			resp.Error = "unknown sidecar error"
		}
		return response{}, errors.New(resp.Error)
	}
	return resp, nil
}

func (r *Runner) roundTrip(ctx context.Context, req request) (response, error) {
	data, err := json.Marshal(req)
	if err != nil {
		return response{}, fmt.Errorf("marshal psanalyzer request: %w", err)
	}
	data = append(data, '\n')
	if _, err := r.stdin.Write(data); err != nil {
		return response{}, fmt.Errorf("write psanalyzer request: %w%s", err, r.stderrSuffix())
	}

	line, err := r.readLine(ctx)
	if err != nil {
		return response{}, fmt.Errorf("read psanalyzer response: %w%s", err, r.stderrSuffix())
	}

	var resp response
	if err := json.Unmarshal(bytes.TrimSpace(line), &resp); err != nil {
		return response{}, fmt.Errorf("parse psanalyzer response: %w%s", err, r.stderrSuffix())
	}
	return resp, nil
}

func (r *Runner) readLine(ctx context.Context) ([]byte, error) {
	type readResult struct {
		line []byte
		err  error
	}

	ch := make(chan readResult, 1)
	go func() {
		line, err := r.stdout.ReadBytes('\n')
		ch <- readResult{line: line, err: err}
	}()

	select {
	case res := <-ch:
		if res.err != nil {
			return res.line, res.err
		}
		return res.line, nil
	case <-ctx.Done():
		killProcess(r.cmd)
		return nil, ctx.Err()
	}
}

func (r *Runner) stopProcess() {
	if r.stdin != nil {
		_ = r.stdin.Close()
	}
	killProcess(r.cmd)
	if r.waitCh != nil {
		select {
		case <-r.waitCh:
		case <-time.After(2 * time.Second):
		}
	}
	r.clearProcess()
	r.cleanupTempDir()
}

func killProcess(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	if err := cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return
	}
}

func (r *Runner) clearProcess() {
	r.cmd = nil
	r.stdin = nil
	r.stdout = nil
	r.waitCh = nil
}

func (r *Runner) cleanupTempDir() {
	if r.tempDir != "" {
		_ = os.RemoveAll(r.tempDir)
		r.tempDir = ""
	}
}

func (r *Runner) stderrSuffix() string {
	tail := strings.TrimSpace(r.stderr.string())
	if tail == "" {
		return ""
	}
	return ": " + tail
}

func (s Settings) isZero() bool {
	return len(s.IncludeRules) == 0 && len(s.ExcludeRules) == 0 && len(s.Severity) == 0
}

func normalizePowerShellEnv(goos string, env []string) []string {
	out := append([]string(nil), env...)
	if goos != "windows" {
		return out
	}

	get := func(key string) string {
		for _, entry := range out {
			k, v, ok := strings.Cut(entry, "=")
			if ok && strings.EqualFold(k, key) {
				return v
			}
		}
		return ""
	}
	setDefault := func(key, value string) {
		if value == "" || get(key) != "" {
			return
		}
		out = append(out, key+"="+value)
	}

	windir := get("WINDIR")
	if windir == "" {
		windir = get("SystemRoot")
	}
	if windir == "" {
		windir = `C:\WINDOWS`
	}
	setDefault("WINDIR", windir)
	setDefault("SystemRoot", windir)

	userProfile := get("USERPROFILE")
	if userProfile != "" {
		setDefault("APPDATA", userProfile+`\AppData\Roaming`)
		setDefault("LOCALAPPDATA", userProfile+`\AppData\Local`)
	}

	return out
}

type tailBuffer struct {
	mu  sync.Mutex
	buf []byte
}

func (b *tailBuffer) capture(r io.Reader) {
	tmp := make([]byte, 1024)
	for {
		n, err := r.Read(tmp)
		if n > 0 {
			b.append(tmp[:n])
		}
		if err != nil {
			return
		}
	}
}

func (b *tailBuffer) append(p []byte) {
	b.mu.Lock()
	defer b.mu.Unlock()

	const maxTail = 8192
	b.buf = append(b.buf, p...)
	if len(b.buf) > maxTail {
		b.buf = append([]byte(nil), b.buf[len(b.buf)-maxTail:]...)
	}
}

func (b *tailBuffer) string() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.buf)
}

func (b *tailBuffer) reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf = nil
}

type delayedProgressNotice struct {
	delay  time.Duration
	repeat time.Duration

	mu     sync.Mutex
	stopCh chan struct{}
	done   chan struct{}
}

func newDelayedProgressNotice(delay, repeat time.Duration) *delayedProgressNotice {
	return &delayedProgressNotice{delay: delay, repeat: repeat}
}

func (n *delayedProgressNotice) start(message string) {
	if os.Getenv(progressNoticeEnv) == progressNoticeEnvMute {
		return
	}
	message = strings.TrimSpace(message)
	if message == "" {
		return
	}

	n.mu.Lock()
	defer n.mu.Unlock()
	if n.stopCh != nil {
		return
	}

	stopCh := make(chan struct{})
	done := make(chan struct{})
	n.stopCh = stopCh
	n.done = done

	go func() {
		defer close(done)

		timer := time.NewTimer(n.delay)
		defer timer.Stop()

		select {
		case <-timer.C:
			fmt.Fprintf(os.Stderr, "note: %s\n", message)
		case <-stopCh:
			return
		}

		if n.repeat <= 0 {
			return
		}

		ticker := time.NewTicker(n.repeat)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				fmt.Fprintf(os.Stderr, "note: %s\n", message)
			case <-stopCh:
				return
			}
		}
	}()
}

func (n *delayedProgressNotice) stop() {
	n.mu.Lock()
	stopCh := n.stopCh
	done := n.done
	n.stopCh = nil
	n.done = nil
	n.mu.Unlock()

	if stopCh != nil {
		close(stopCh)
		<-done
	}
}
