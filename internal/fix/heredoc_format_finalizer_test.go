package fix

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wharflab/tally/internal/rules"
)

type fakePowerShellFormatter struct {
	formatted string
	calls     []string
}

func (f *fakePowerShellFormatter) FormatPowerShell(_ context.Context, script string) (string, error) {
	f.calls = append(f.calls, script)
	return f.formatted, nil
}

func TestFormattedHeredocsFinalizerFormatsGeneratedHeredoc(t *testing.T) {
	t.Parallel()

	file := filepath.Join(t.TempDir(), "Dockerfile")
	src := []byte("FROM alpine\nRUN true\n")
	loc := rules.NewRangeLocation(file, 2, 0, 2, len("RUN true"))
	violation := rules.NewViolation(loc, "test/emit-copy-heredoc", "emit COPY heredoc", rules.SeverityStyle).
		WithSuggestedFix(&rules.SuggestedFix{
			Description: "Emit COPY heredoc",
			Safety:      rules.FixSafe,
			Priority:    1,
			Edits: []rules.TextEdit{
				{
					Location: loc,
					NewText: "COPY <<EOF /etc/app/config.json\n" +
						"{\"b\":2,\"a\":1}\n" +
						"EOF",
				},
			},
			IsPreferred: true,
		})

	fixer := &Fixer{
		SafetyThreshold: rules.FixSafe,
		EnabledRules: map[string][]string{
			filepath.Clean(file): {rules.FormattedHeredocsRuleCode},
		},
	}
	result, err := fixer.Apply(context.Background(), []rules.Violation{violation}, map[string][]byte{file: src})
	if err != nil {
		t.Fatal(err)
	}

	change := result.Changes[filepath.Clean(file)]
	if change == nil {
		t.Fatal("missing file change")
	}
	got := string(change.ModifiedContent)
	want := "COPY <<EOF /etc/app/config.json\n{\n  \"b\": 2,\n  \"a\": 1\n}\nEOF"
	if !strings.Contains(got, want) {
		t.Fatalf("generated heredoc was not formatted\ngot:\n%s\nwant substring:\n%s", got, want)
	}
	if result.TotalApplied() != 2 {
		t.Fatalf("applied fixes = %d, want 2", result.TotalApplied())
	}
}

func TestFormattedHeredocsFinalizerFormatsRunHeredoc(t *testing.T) {
	t.Parallel()

	file := filepath.Join(t.TempDir(), "Dockerfile")
	src := []byte("FROM ubuntu:22.04\n" +
		"RUN <<EOF\n" +
		"apt-get install -y --no-install-recommends build-essential ca-certificates curl git jq openssl " +
		"pkg-config python3-dev unzip vim wget zlib1g-dev\n" +
		"EOF\n")
	fixer := &Fixer{
		SafetyThreshold: rules.FixSafe,
		EnabledRules: map[string][]string{
			filepath.Clean(file): {rules.FormattedHeredocsRuleCode},
		},
	}
	result, err := fixer.Apply(context.Background(), nil, map[string][]byte{file: src})
	if err != nil {
		t.Fatal(err)
	}

	change := result.Changes[filepath.Clean(file)]
	if change == nil {
		t.Fatal("missing file change")
	}
	got := string(change.ModifiedContent)
	want := "apt-get install -y --no-install-recommends build-essential ca-certificates curl git jq openssl \\\n" +
		"\tpkg-config python3-dev unzip vim wget zlib1g-dev"
	if !strings.Contains(got, want) {
		t.Fatalf("RUN heredoc was not formatted\ngot:\n%s\nwant substring:\n%s", got, want)
	}
	if result.TotalApplied() != 1 {
		t.Fatalf("applied fixes = %d, want 1", result.TotalApplied())
	}
}

func TestFormattedHeredocsFinalizerFormatsPowerShellRunHeredoc(t *testing.T) {
	t.Parallel()

	file := filepath.Join(t.TempDir(), "Dockerfile")
	src := []byte("FROM mcr.microsoft.com/powershell:lts-alpine-3.20\n" +
		"SHELL [\"pwsh\", \"-Command\"]\n" +
		"RUN <<EOF\n" +
		"if ($true) {\n" +
		"Write-Host hi\n" +
		"}\n" +
		"EOF\n")
	formatter := &fakePowerShellFormatter{
		formatted: "if ($true) {\n    Write-Host hi\n}\n",
	}
	finalizer := formattedHeredocsFinalizer{powerShellFormatter: formatter}
	edits, err := finalizer.Finalize(context.Background(), FinalizeContext{
		FilePath: file,
		Content:  src,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(edits) != 1 {
		t.Fatalf("got %d edits, want 1", len(edits))
	}
	if len(formatter.calls) != 1 {
		t.Fatalf("PowerShell formatter calls = %d, want 1", len(formatter.calls))
	}
	if want := "if ($true) {\n    Write-Host hi\n}\n"; edits[0].NewText != want {
		t.Fatalf("edit text mismatch\ngot:\n%s\nwant:\n%s", edits[0].NewText, want)
	}
}

func TestFormattedHeredocsFinalizerRequiresRuleEnabled(t *testing.T) {
	t.Parallel()

	file := filepath.Join(t.TempDir(), "Dockerfile")
	src := []byte("FROM alpine\nCOPY <<EOF /etc/app/config.json\n{\"a\":1}\nEOF\n")
	fixer := &Fixer{SafetyThreshold: rules.FixSafe}
	result, err := fixer.Apply(context.Background(), nil, map[string][]byte{file: src})
	if err != nil {
		t.Fatal(err)
	}
	change := result.Changes[filepath.Clean(file)]
	if change == nil {
		t.Fatal("missing file change")
	}
	if !bytes.Equal(change.ModifiedContent, src) {
		t.Fatalf("finalizer ran without enabled-rule context:\n%s", change.ModifiedContent)
	}
}
