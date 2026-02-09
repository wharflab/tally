package acp

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

var testAgentBin string

func TestMain(m *testing.M) {
	bin, err := buildTestAgent()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	testAgentBin = bin
	os.Exit(m.Run())
}

func buildTestAgent() (string, error) {
	tmp, err := os.MkdirTemp("", "tally-acp-testagent-*")
	if err != nil {
		return "", fmt.Errorf("mkdtemp: %w", err)
	}
	binName := "acp-testagent"
	if runtime.GOOS == "windows" {
		binName += ".exe"
	}
	out := filepath.Join(tmp, binName)

	cmd := exec.Command("go", "build", "-trimpath", "-o", out, "./testdata/testagent")
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("build test agent: %w", err)
	}
	return out, nil
}

func TestRunner_HappyPath(t *testing.T) {
	t.Parallel()

	r := NewRunner(
		WithTerminateGrace(50*time.Millisecond),
		WithMaxOutputBytes(64*1024),
	)

	resp, err := r.Run(context.Background(), RunRequest{
		Command: []string{testAgentBin, "-mode=happy"},
		Cwd:     t.TempDir(),
		Timeout: 2 * time.Second,
		Prompt:  "hi",
	})
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if !strings.Contains(resp.Text, "hello from test agent") {
		t.Fatalf("unexpected response text: %q", resp.Text)
	}
	if resp.Stats.PromptBytes != len("hi") {
		t.Fatalf("PromptBytes=%d, want %d", resp.Stats.PromptBytes, len("hi"))
	}
	if resp.Stats.ResponseBytes == 0 {
		t.Fatalf("ResponseBytes=0, want >0")
	}
	if resp.Stats.Duration <= 0 {
		t.Fatalf("Duration=%v, want >0", resp.Stats.Duration)
	}
}

func TestRunner_StderrTailIncludedInError(t *testing.T) {
	t.Parallel()

	r := NewRunner(
		WithTerminateGrace(50*time.Millisecond),
		WithStderrTailBytes(128),
	)

	_, err := r.Run(context.Background(), RunRequest{
		Command: []string{testAgentBin, "-mode=stderr-exit", "-stderr-bytes=8192"},
		Cwd:     t.TempDir(),
		Timeout: 2 * time.Second,
		Prompt:  "ignored",
	})
	if err == nil {
		t.Fatalf("Run() expected error, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "END_STDER") {
		t.Fatalf("error missing stderr tail marker: %q", msg)
	}
	if strings.Contains(msg, "BEGIN_STDER") {
		t.Fatalf("error unexpectedly contains full stderr (expected tail only): %q", msg)
	}
}
