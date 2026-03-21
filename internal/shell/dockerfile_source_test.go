package shell

import "testing"

func TestDockerfileRunCommandStartCol(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		line string
		want int
	}{
		{
			name: "simple run",
			line: "RUN echo hello",
			want: 4,
		},
		{
			name: "leading whitespace and flags",
			line: `  RUN --mount=type=cache,target="/tmp cache" --network=none apt-get update`,
			want: 60,
		},
		{
			name: "line continuation after flag",
			line: `RUN --mount=type=cache,target=/var/cache/apt \`,
			want: 45,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := DockerfileRunCommandStartCol(tt.line); got != tt.want {
				t.Fatalf("DockerfileRunCommandStartCol() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestSkipDockerfileFlagValue(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                   string
		line                   string
		offset                 int
		stopAtLineContinuation bool
		want                   int
	}{
		{
			name:                   "quoted value",
			line:                   `--mount=type=cache,target="/tmp cache" next`,
			offset:                 0,
			stopAtLineContinuation: false,
			want:                   38,
		},
		{
			name:                   "keep scanning past backslash when disabled",
			line:                   `--flag=value\ more`,
			offset:                 0,
			stopAtLineContinuation: false,
			want:                   13,
		},
		{
			name:                   "stop at backslash when enabled",
			line:                   `--flag=value\`,
			offset:                 0,
			stopAtLineContinuation: true,
			want:                   12,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := SkipDockerfileFlagValue(tt.line, tt.offset, tt.stopAtLineContinuation); got != tt.want {
				t.Fatalf("SkipDockerfileFlagValue() = %d, want %d", got, tt.want)
			}
		})
	}
}
