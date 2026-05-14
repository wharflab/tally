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

func TestBridgeDockerfileCommentContinuations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		lines       []string
		escapeToken rune
		target      rune
		want        []string
	}{
		{
			name: "comment between continued shell lines",
			lines: []string{
				`RUN echo one \`,
				`    # Dockerfile comment`,
				`    && echo two`,
			},
			escapeToken: '\\',
			target:      '\\',
			want: []string{
				`RUN echo one \`,
				`    \`,
				`    && echo two`,
			},
		},
		{
			name: "consecutive comments stay in continued span",
			lines: []string{
				`RUN echo one \`,
				`    # first`,
				`    # second`,
				`    && echo two`,
			},
			escapeToken: '\\',
			target:      '\\',
			want: []string{
				`RUN echo one \`,
				`    \`,
				`    \`,
				`    && echo two`,
			},
		},
		{
			name: "comment outside continuation is unchanged",
			lines: []string{
				`RUN echo one`,
				`# normal Dockerfile comment`,
			},
			escapeToken: '\\',
			target:      '\\',
			want: []string{
				`RUN echo one`,
				`# normal Dockerfile comment`,
			},
		},
		{
			name: "rewrites to requested target continuation",
			lines: []string{
				"RUN Write-Host one `",
				"    # Dockerfile comment",
				"    ; Write-Host two",
			},
			escapeToken: '`',
			target:      '`',
			want: []string{
				"RUN Write-Host one `",
				"    `",
				"    ; Write-Host two",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := BridgeDockerfileCommentContinuations(tt.lines, tt.escapeToken, tt.target)
			if len(got) != len(tt.want) {
				t.Fatalf("line count = %d, want %d", len(got), len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("line %d = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}
