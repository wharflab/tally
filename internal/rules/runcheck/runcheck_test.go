package runcheck

import (
	"testing"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/shell"
	"github.com/wharflab/tally/internal/testutil"
)

func TestScanRunCommandsWithPOSIXShell(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		dockerfile    string
		wantCallCount int
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
			name: "non-POSIX shell skips shell-form RUN",
			dockerfile: `FROM mcr.microsoft.com/windows/servercore
SHELL ["powershell", "-Command"]
RUN Write-Host "hello"`,
			wantCallCount: 0,
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

	tests := []struct {
		name        string
		dockerfile  string
		wantVariant shell.Variant
	}{
		{
			name: "default sh",
			dockerfile: `FROM alpine
RUN echo hello`,
			wantVariant: shell.VariantPOSIX,
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
