package heredocfmt

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/wharflab/tally/internal/dockerfile"
	"github.com/wharflab/tally/internal/semantic"
)

type fakePowerShellFormatter struct {
	formatted string
	err       error
	calls     []string
}

func (f *fakePowerShellFormatter) FormatPowerShell(_ context.Context, script string) (string, error) {
	f.calls = append(f.calls, script)
	return f.formatted, f.err
}

func TestShellFromHeredocShebangIgnoresIndentedShebangComment(t *testing.T) {
	t.Parallel()

	if got, ok := shellFromHeredocShebang("  #!/usr/bin/env bash\necho hi\n"); ok {
		t.Fatalf("shellFromHeredocShebang() = %q, true; want no shebang", got)
	}
}

func TestIsPowerShellTarget(t *testing.T) {
	t.Parallel()

	tests := []struct {
		target string
		want   bool
	}{
		{"/opt/app/install.ps1", true},
		{"/opt/app/MyModule.psm1", true},
		{"/opt/app/MyModule.psd1", true},
		{"/opt/app/config.json", false},
		{"/opt/app/script.sh", false},
	}
	for _, tt := range tests {
		if got := IsPowerShellTarget(tt.target); got != tt.want {
			t.Fatalf("IsPowerShellTarget(%q) = %v, want %v", tt.target, got, tt.want)
		}
	}
}

func TestFormatDockerfileHeredocsFormatsAddPowerShellTarget(t *testing.T) {
	t.Parallel()

	src := `FROM alpine
ADD <<EOF /opt/app/install.ps1
if ($true) {
Write-Host hi
}
EOF
`
	result, err := dockerfile.Parse(strings.NewReader(src), nil)
	if err != nil {
		t.Fatal(err)
	}
	formatter := &fakePowerShellFormatter{
		formatted: "if ($true) {\n    Write-Host hi\n}\n",
	}

	edits, err := FormatDockerfileHeredocsWithPowerShell(
		context.Background(),
		"Dockerfile",
		result,
		semantic.NewModel(result, nil, "Dockerfile"),
		formatter,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(formatter.calls) != 1 {
		t.Fatalf("PowerShell formatter calls = %d, want 1", len(formatter.calls))
	}
	if want := "if ($true) {\nWrite-Host hi\n}\n"; formatter.calls[0] != want {
		t.Fatalf("formatter input mismatch\ngot:\n%s\nwant:\n%s", formatter.calls[0], want)
	}
	if len(edits) != 1 {
		t.Fatalf("edits = %d, want 1", len(edits))
	}
	if want := "if ($true) {\n    Write-Host hi\n}\n"; edits[0].NewText != want {
		t.Fatalf("edit text mismatch\ngot:\n%s\nwant:\n%s", edits[0].NewText, want)
	}
}

func TestFormatDockerfileHeredocsFormatsAddShellTarget(t *testing.T) {
	t.Parallel()

	src := `FROM alpine
ADD <<EOF /usr/local/bin/entrypoint.sh
if true; then
 echo hi
fi
EOF
`
	result, err := dockerfile.Parse(strings.NewReader(src), nil)
	if err != nil {
		t.Fatal(err)
	}

	edits, err := FormatDockerfileHeredocsWithPowerShell(
		context.Background(),
		"Dockerfile",
		result,
		semantic.NewModel(result, nil, "Dockerfile"),
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(edits) != 1 {
		t.Fatalf("edits = %d, want 1", len(edits))
	}
	if want := "if true; then\n\techo hi\nfi\n"; edits[0].NewText != want {
		t.Fatalf("edit text mismatch\ngot:\n%s\nwant:\n%s", edits[0].NewText, want)
	}
}

func TestFormatDockerfileHeredocsPropagatesPowerShellFormatterError(t *testing.T) {
	t.Parallel()

	src := `FROM mcr.microsoft.com/powershell:lts-alpine-3.20
SHELL ["pwsh", "-Command"]
RUN <<EOF
if ($true) {
Write-Host hi
}
EOF
`
	result, err := dockerfile.Parse(strings.NewReader(src), nil)
	if err != nil {
		t.Fatal(err)
	}
	wantErr := errors.New("PowerShell formatter protocol failed")
	formatter := &fakePowerShellFormatter{err: wantErr}

	edits, err := FormatDockerfileHeredocsWithPowerShell(
		context.Background(),
		"Dockerfile",
		result,
		semantic.NewModel(result, nil, "Dockerfile"),
		formatter,
	)
	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want %v", err, wantErr)
	}
	if len(edits) != 0 {
		t.Fatalf("edits = %d, want none", len(edits))
	}
}
