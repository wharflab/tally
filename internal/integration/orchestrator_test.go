package integration

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"encoding/json/v2"
)

type orchestratorJSONOutput struct {
	Files []struct {
		File       string `json:"file"`
		Violations []struct {
			Rule       string `json:"rule"`
			Invocation struct {
				Kind string `json:"kind"`
				Name string `json:"name"`
			} `json:"invocation"`
		} `json:"violations"`
	} `json:"files"`
	Summary struct {
		Total       int `json:"total"`
		Files       int `json:"files"`
		Invocations int `json:"invocations"`
	} `json:"summary"`
	FilesScanned       int `json:"files_scanned"`
	InvocationsScanned int `json:"invocations_scanned"`
}

func TestOrchestratorEntrypoints(t *testing.T) {
	t.Parallel()

	t.Run("compose fans out services", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeOrchestratorFile(t, filepath.Join(dir, "Dockerfile"), "FROM alpine:3.20\nRUN echo one\nRUN echo two\n")
		writeOrchestratorFile(t, filepath.Join(dir, "compose.yaml"), `services:
  api:
    build:
      context: .
      dockerfile: Dockerfile
  worker:
    build:
      context: .
      dockerfile: Dockerfile
`)

		out := runOrchestratorLint(t, filepath.Join(dir, "compose.yaml"))
		assertOrchestratorOutput(t, out, "compose", []string{"api", "worker"}, 1, 2)
	})

	t.Run("bake fans out targets", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeOrchestratorFile(t, filepath.Join(dir, "Dockerfile"), "FROM alpine:3.20\nRUN echo one\nRUN echo two\n")
		writeOrchestratorFile(t, filepath.Join(dir, "docker-bake.hcl"), `group "default" {
  targets = ["api", "worker"]
}

target "api" {
  context = "."
  dockerfile = "Dockerfile"
}

target "worker" {
  context = "."
  dockerfile = "Dockerfile"
}
`)

		out := runOrchestratorLint(t, filepath.Join(dir, "docker-bake.hcl"))
		assertOrchestratorOutput(t, out, "bake", []string{"api", "worker"}, 1, 2)
	})

	t.Run("orchestrator rejects fix", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeOrchestratorFile(t, filepath.Join(dir, "Dockerfile"), "FROM alpine:3.20\n")
		writeOrchestratorFile(t, filepath.Join(dir, "compose.yaml"), `services:
  api:
    build: .
`)

		var stderr bytes.Buffer
		cmd := exec.Command(binaryPath, "lint", "--fix", filepath.Join(dir, "compose.yaml"))
		cmd.Env = append(os.Environ(), "GOCOVERDIR="+coverageDir)
		cmd.Stderr = &stderr
		err := cmd.Run()
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("expected exit error, got %v", err)
		}
		if exitErr.ExitCode() != 2 {
			t.Fatalf("exit code = %d, want 2; stderr:\n%s", exitErr.ExitCode(), stderr.String())
		}
		if got := stderr.String(); !strings.Contains(got, "--fix is not supported") {
			t.Fatalf("stderr did not mention --fix rejection:\n%s", got)
		}
	})
}

func writeOrchestratorFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func runOrchestratorLint(t *testing.T, entrypoint string) orchestratorJSONOutput {
	t.Helper()
	args := []string{
		"lint",
		"--format", "json",
		"--fail-level", "none",
		"--ignore", "*",
		"--select", "tally/max-lines",
		"--max-lines", "1",
		entrypoint,
	}
	cmd := exec.Command(binaryPath, args...)
	cmd.Env = append(os.Environ(), "GOCOVERDIR="+coverageDir)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("lint failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
	}

	var out orchestratorJSONOutput
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("decode JSON: %v\nstdout:\n%s", err, stdout.String())
	}
	return out
}

func assertOrchestratorOutput(
	t *testing.T,
	out orchestratorJSONOutput,
	wantKind string,
	wantNames []string,
	wantFiles, wantInvocations int,
) {
	t.Helper()
	if out.FilesScanned != wantFiles {
		t.Fatalf("files_scanned = %d, want %d", out.FilesScanned, wantFiles)
	}
	if out.InvocationsScanned != wantInvocations {
		t.Fatalf("invocations_scanned = %d, want %d", out.InvocationsScanned, wantInvocations)
	}
	if out.Summary.Invocations != wantInvocations {
		t.Fatalf("summary.invocations = %d, want %d", out.Summary.Invocations, wantInvocations)
	}
	if out.Summary.Total != wantInvocations {
		t.Fatalf("summary.total = %d, want %d", out.Summary.Total, wantInvocations)
	}

	var gotNames []string
	for _, file := range out.Files {
		for _, violation := range file.Violations {
			if violation.Rule != "tally/max-lines" {
				t.Fatalf("rule = %q, want tally/max-lines", violation.Rule)
			}
			if violation.Invocation.Kind != wantKind {
				t.Fatalf("invocation.kind = %q, want %q", violation.Invocation.Kind, wantKind)
			}
			gotNames = append(gotNames, violation.Invocation.Name)
		}
	}
	if !slices.Equal(gotNames, wantNames) {
		t.Fatalf("invocation names = %v, want %v", gotNames, wantNames)
	}
}
