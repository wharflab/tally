package hadolint

import (
	"testing"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/wharflab/tally/internal/dockerfile"
	"github.com/wharflab/tally/internal/testutil"
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
