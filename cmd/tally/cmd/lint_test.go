package cmd

import (
	"slices"
	"testing"

	"github.com/wharflab/tally/internal/invocation"
	"github.com/wharflab/tally/internal/rules"
)

func TestParseACPCmd(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    []string
		wantErr bool
	}{
		{
			name:    "empty",
			input:   "",
			wantErr: true,
		},
		{
			name:    "whitespace_only",
			input:   "  \t\n",
			wantErr: true,
		},
		{
			name:  "simple_fields",
			input: "gemini --experimental-acp",
			want:  []string{"gemini", "--experimental-acp"},
		},
		{
			name:  "double_quotes",
			input: `gemini --model "gemini 3 flash"`,
			want:  []string{"gemini", "--model", "gemini 3 flash"},
		},
		{
			name:  "single_quotes",
			input: `gemini --model 'gemini 3 flash'`,
			want:  []string{"gemini", "--model", "gemini 3 flash"},
		},
		{
			name:  "empty_quoted_arg",
			input: `cmd ""`,
			want:  []string{"cmd", ""},
		},
		{
			name:  "escaped_space",
			input: `cmd foo\ bar`,
			want:  []string{"cmd", "foo bar"},
		},
		{
			name:  "preserve_backslash_before_letter",
			input: `cmd foo\bar`,
			want:  []string{"cmd", `foo\bar`},
		},
		{
			name:  "escape_backslash",
			input: `cmd foo\\bar`,
			want:  []string{"cmd", `foo\bar`},
		},
		{
			name:  "windows_path_unquoted",
			input: `cmd C:\Tools\Gemini\gemini.exe --flag`,
			want:  []string{"cmd", `C:\Tools\Gemini\gemini.exe`, "--flag"},
		},
		{
			name:  "windows_path_quoted_with_spaces",
			input: `cmd "C:\Program Files\Gemini\config.json"`,
			want:  []string{"cmd", `C:\Program Files\Gemini\config.json`},
		},
		{
			name:    "unterminated_double_quote",
			input:   `cmd "oops`,
			wantErr: true,
		},
		{
			name:    "unterminated_single_quote",
			input:   `cmd 'oops`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseACPCmd(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseACPCmd(%q) error = %v, wantErr=%v", tt.input, err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if !slices.Equal(got, tt.want) {
				t.Fatalf("parseACPCmd(%q) = %#v, want %#v", tt.input, got, tt.want)
			}
		})
	}
}

func TestContextDirForViolationUsesInvocationKey(t *testing.T) {
	t.Parallel()

	file := "Dockerfile"
	first := &invocation.BuildInvocation{
		Key:            "compose|compose.yaml|api|Dockerfile",
		DockerfilePath: file,
		ContextRef: invocation.ContextRef{
			Kind:  invocation.ContextKindDir,
			Value: "/workspace/api",
		},
	}
	second := &invocation.BuildInvocation{
		Key:            "compose|compose.yaml|worker|Dockerfile",
		DockerfilePath: file,
		ContextRef: invocation.ContextRef{
			Kind:  invocation.ContextKindDir,
			Value: "/workspace/worker",
		},
	}
	violation := rules.NewViolation(
		rules.NewLineLocation(file, 1),
		"test/rule",
		"message",
		rules.SeverityWarning,
	)
	violation.InvocationKey = first.Key

	got := contextDirForViolation(violation, map[string]*invocation.BuildInvocation{
		first.Key:  first,
		second.Key: second,
	})
	if got != "/workspace/api" {
		t.Fatalf("contextDirForViolation() = %q, want %q", got, "/workspace/api")
	}
}

func TestContextDirForViolationFallsBackToFileForDockerfileContext(t *testing.T) {
	t.Parallel()

	file := "Dockerfile"
	inv := &invocation.BuildInvocation{
		Key:            "dockerfile|Dockerfile||Dockerfile",
		DockerfilePath: file,
		ContextRef: invocation.ContextRef{
			Kind:  invocation.ContextKindDir,
			Value: "/workspace/app",
		},
	}
	violation := rules.NewViolation(
		rules.NewLineLocation(file, 1),
		"test/rule",
		"message",
		rules.SeverityWarning,
	)

	got := contextDirForViolation(violation, map[string]*invocation.BuildInvocation{
		file: inv,
	})
	if got != "/workspace/app" {
		t.Fatalf("contextDirForViolation() = %q, want %q", got, "/workspace/app")
	}
}
