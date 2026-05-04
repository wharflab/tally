package cmd

import (
	"bytes"
	"context"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/wharflab/tally/internal/invocation"
	"github.com/wharflab/tally/internal/psanalyzer"
	"github.com/wharflab/tally/internal/rules"
)

type recordingPowerShellRunnerCloser struct {
	closed          bool
	receivedTimeout bool
}

func (r *recordingPowerShellRunnerCloser) Close(ctx context.Context) error {
	r.closed = true
	_, r.receivedTimeout = ctx.Deadline()
	return nil
}

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

func TestPowerShellUnavailableReporterWritesInstallNoteOnce(t *testing.T) {
	var stderr bytes.Buffer
	restore := installPowerShellUnavailableReporter(&stderr)
	defer restore()

	t.Setenv("TALLY_POWERSHELL", filepath.Join(t.TempDir(), "missing-pwsh"))

	runner := psanalyzer.NewRunner()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	_, err := runner.Analyze(ctx, psanalyzer.AnalyzeRequest{ScriptDefinition: "Write-Host hi\n"})
	if !psanalyzer.IsUnavailable(err) {
		t.Fatalf("Analyze() error = %v, want unavailable", err)
	}
	_, err = runner.Format(ctx, psanalyzer.FormatRequest{ScriptDefinition: "Write-Host hi\n"})
	if !psanalyzer.IsUnavailable(err) {
		t.Fatalf("Format() error = %v, want unavailable", err)
	}

	got := stderr.String()
	if strings.Count(got, "PowerShell script linting/formatting skipped") != 1 {
		t.Fatalf("expected one PowerShell skip note, got:\n%s", got)
	}
	for _, want := range []string{
		"note: PowerShell script linting/formatting skipped",
		"PowerShell 7+",
		installPowerShellURL,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected note to contain %q, got:\n%s", want, got)
		}
	}
}

func TestClosePowerShellRunnerUsesTimeout(t *testing.T) {
	t.Parallel()

	runner := &recordingPowerShellRunnerCloser{}
	closePowerShellRunner(context.Background(), func() powerShellRunnerCloser {
		return runner
	})

	if !runner.closed {
		t.Fatal("expected PowerShell runner to be closed")
	}
	if !runner.receivedTimeout {
		t.Fatal("expected PowerShell runner close to use a timeout context")
	}
}

func TestFailFastViolationsExcludeSlowGatedPowerShell(t *testing.T) {
	t.Parallel()

	file := "Dockerfile"
	powerShellError := rules.NewViolation(
		rules.NewLineLocation(file, 1),
		rules.PowerShellRulePrefix+"PSAvoidUsingPlainTextForPassword",
		"message",
		rules.SeverityError,
	)
	fastError := rules.NewViolation(
		rules.NewLineLocation(file, 2),
		"buildkit/InvalidDefaultArgInFrom",
		"message",
		rules.SeverityError,
	)

	onlyPowerShell := filesWithErrors(failFastViolations([]rules.Violation{powerShellError}))
	if onlyPowerShell[asyncErrorKey(file, "")] {
		t.Fatalf("PowerShell analyzer error should not trigger async fail-fast")
	}

	withFastError := filesWithErrors(failFastViolations([]rules.Violation{powerShellError, fastError}))
	if !withFastError[asyncErrorKey(file, "")] {
		t.Fatalf("non-slow error should still trigger async fail-fast")
	}
}
