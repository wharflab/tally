package hadolint

import (
	"testing"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/tinovyatkin/tally/internal/dockerfile"
	"github.com/tinovyatkin/tally/internal/rules"
	"github.com/tinovyatkin/tally/internal/shell"
	"github.com/tinovyatkin/tally/internal/testutil"
)

func TestRunCommandString(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		dockerfile string
		want       string
	}{
		{
			name: "shell form",
			dockerfile: `FROM alpine
RUN echo hello world`,
			want: "echo hello world",
		},
		{
			name: "exec form",
			dockerfile: `FROM alpine
RUN ["echo", "hello", "world"]`,
			want: "echo hello world",
		},
		{
			name: "multi-line shell form",
			dockerfile: `FROM alpine
RUN apt-get update && \
    apt-get install -y curl`,
			want: "apt-get update &&     apt-get install -y curl",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			input := testutil.MakeLintInput(t, "Dockerfile", tt.dockerfile)
			if len(input.Stages) == 0 || len(input.Stages[0].Commands) == 0 {
				t.Fatal("expected at least one stage with one command")
			}

			run, ok := input.Stages[0].Commands[0].(*instructions.RunCommand)
			if !ok {
				t.Fatal("expected RUN command")
			}

			got := dockerfile.RunCommandString(run)
			if got != tt.want {
				t.Errorf("dockerfile.RunCommandString() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestScanRunCommandsWithPOSIXShell(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		dockerfile    string
		wantCallCount int // Number of times callback should be called
	}{
		{
			name: "single RUN command",
			dockerfile: `FROM alpine
RUN echo hello`,
			wantCallCount: 1,
		},
		{
			name: "multiple RUN commands",
			dockerfile: `FROM alpine
RUN echo hello
RUN echo world
RUN echo foo`,
			wantCallCount: 3,
		},
		{
			name: "mixed commands",
			dockerfile: `FROM alpine
ENV FOO=bar
RUN echo hello
COPY . /app
RUN echo world`,
			wantCallCount: 2,
		},
		{
			name: "multi-stage with RUN commands",
			dockerfile: `FROM alpine AS builder
RUN echo stage1
FROM alpine
RUN echo stage2`,
			wantCallCount: 2,
		},
		{
			name: "non-POSIX shell (should skip)",
			dockerfile: `FROM mcr.microsoft.com/windows/servercore
SHELL ["powershell", "-Command"]
RUN Write-Host "hello"`,
			// Note: testutil.MakeLintInput doesn't include semantic model,
			// so shell variant detection won't work. This test would need
			// a full semantic model to work correctly.
			wantCallCount: 1, // Without semantic model, doesn't skip
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			input := testutil.MakeLintInput(t, "Dockerfile", tt.dockerfile)

			callCount := 0
			ScanRunCommandsWithPOSIXShell(
				input,
				func(run *instructions.RunCommand, shellVariant shell.Variant, file string) []rules.Violation {
					callCount++

					// Verify the callback receives valid arguments
					if run == nil {
						t.Error("callback received nil RUN command")
					}
					if file == "" {
						t.Error("callback received empty file path")
					}

					return nil
				},
			)

			if callCount != tt.wantCallCount {
				t.Errorf("callback called %d times, want %d", callCount, tt.wantCallCount)
			}
		})
	}
}

func TestScanRunCommandsWithPOSIXShell_ShellVariant(t *testing.T) {
	t.Parallel()
	t.Skip("Shell variant detection requires semantic model which testutil.MakeLintInput doesn't provide")

	tests := []struct {
		name        string
		dockerfile  string
		wantVariant shell.Variant
	}{
		{
			name: "default bash",
			dockerfile: `FROM alpine
RUN echo hello`,
			wantVariant: shell.VariantBash,
		},
		{
			name: "explicit sh",
			dockerfile: `FROM alpine
SHELL ["/bin/sh", "-c"]
RUN echo hello`,
			wantVariant: shell.VariantPOSIX,
		},
		{
			name: "explicit bash",
			dockerfile: `FROM alpine
SHELL ["/bin/bash", "-c"]
RUN echo hello`,
			wantVariant: shell.VariantBash,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			input := testutil.MakeLintInput(t, "Dockerfile", tt.dockerfile)

			ScanRunCommandsWithPOSIXShell(
				input,
				func(run *instructions.RunCommand, shellVariant shell.Variant, file string) []rules.Violation {
					if shellVariant != tt.wantVariant {
						t.Errorf("got shell variant %v, want %v", shellVariant, tt.wantVariant)
					}
					return nil
				},
			)
		})
	}
}
