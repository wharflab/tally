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
		Key:            "compose\x00compose.yaml\x00api\x00Dockerfile",
		DockerfilePath: file,
		ContextRef: invocation.ContextRef{
			Kind:  invocation.ContextKindDir,
			Value: "/workspace/api",
		},
	}
	second := &invocation.BuildInvocation{
		Key:            "compose\x00compose.yaml\x00worker\x00Dockerfile",
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

func TestAddFileInvocationUsesInvocationKey(t *testing.T) {
	t.Parallel()

	inv := &invocation.BuildInvocation{
		Key:            "dockerfile\x00/workspace/Dockerfile\x00\x00/workspace/Dockerfile",
		DockerfilePath: "/workspace/Dockerfile",
	}
	fileInvocations := make(map[string]*invocation.BuildInvocation)

	addFileInvocation(fileInvocations, inv)

	if got := fileInvocations[inv.Key]; got != inv {
		t.Fatalf("fileInvocations[inv.Key] = %#v, want %#v", got, inv)
	}
	if got := fileInvocations[inv.DockerfilePath]; got != nil {
		t.Fatalf("fileInvocations[inv.DockerfilePath] = %#v, want nil", got)
	}
}

func TestContextDirForViolationUsesDockerfileInvocationKey(t *testing.T) {
	t.Parallel()

	file := "Dockerfile"
	inv := &invocation.BuildInvocation{
		Key:            "dockerfile\x00Dockerfile\x00\x00Dockerfile",
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
	violation.InvocationKey = inv.Key

	got := contextDirForViolation(violation, map[string]*invocation.BuildInvocation{
		inv.Key: inv,
	})
	if got != "/workspace/app" {
		t.Fatalf("contextDirForViolation() = %q, want %q", got, "/workspace/app")
	}
}

func TestContextDirForViolationRequiresInvocationKey(t *testing.T) {
	t.Parallel()

	file := "Dockerfile"
	inv := &invocation.BuildInvocation{
		Key:            "dockerfile\x00Dockerfile\x00\x00Dockerfile",
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
	if got != "" {
		t.Fatalf("contextDirForViolation() = %q, want empty without invocation key", got)
	}
}

func TestContextDirForViolationDoesNotUseCleanFileFallback(t *testing.T) {
	t.Parallel()

	inv := &invocation.BuildInvocation{
		Key:            "dockerfile\x00Dockerfile\x00\x00Dockerfile",
		DockerfilePath: "Dockerfile",
		ContextRef: invocation.ContextRef{
			Kind:  invocation.ContextKindDir,
			Value: "/workspace/app",
		},
	}
	violation := rules.NewViolation(
		rules.NewLineLocation("./Dockerfile", 1),
		"test/rule",
		"message",
		rules.SeverityWarning,
	)

	got := contextDirForViolation(violation, map[string]*invocation.BuildInvocation{
		"Dockerfile": inv,
	})
	if got != "" {
		t.Fatalf("contextDirForViolation() = %q, want empty without invocation key", got)
	}
}

func TestContextDirForViolationIgnoresNonLocalContext(t *testing.T) {
	t.Parallel()

	inv := &invocation.BuildInvocation{
		Key:            "bake\x00docker-bake.hcl\x00api\x00Dockerfile",
		DockerfilePath: "Dockerfile",
		ContextRef: invocation.ContextRef{
			Kind:  invocation.ContextKindGit,
			Value: "https://github.com/wharflab/tally.git",
		},
	}
	violation := rules.NewViolation(
		rules.NewLineLocation("Dockerfile", 1),
		"test/rule",
		"message",
		rules.SeverityWarning,
	)
	violation.InvocationKey = inv.Key

	got := contextDirForViolation(violation, map[string]*invocation.BuildInvocation{
		inv.Key: inv,
	})
	if got != "" {
		t.Fatalf("contextDirForViolation() = %q, want empty for non-dir context", got)
	}
}
