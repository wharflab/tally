package extract

import (
	"strings"
	"testing"

	"github.com/wharflab/tally/internal/dockerfile"
	"github.com/wharflab/tally/internal/sourcemap"
)

func TestExtractRunScript_PlainHeredocUsesBody(t *testing.T) {
	t.Parallel()

	mapping := extractRunScriptForTest(t, `FROM alpine
RUN <<EOF
echo hi
EOF
`)

	if !mapping.IsHeredoc {
		t.Fatal("expected plain RUN heredoc to use body mapping")
	}
	if mapping.Script != "echo hi" {
		t.Fatalf("expected body-only script, got %q", mapping.Script)
	}
	if mapping.OriginStartLine != 3 {
		t.Fatalf("expected body origin line 3, got %d", mapping.OriginStartLine)
	}
}

func TestExtractRunScript_FilePayloadKeepsHeredocAsData(t *testing.T) {
	t.Parallel()

	mapping := extractRunScriptForTest(t, `FROM alpine
RUN <<EOF cat > /etc/app.conf
enable-rpc=true
EOF
`)

	if mapping.IsHeredoc {
		t.Fatal("expected file payload heredoc to remain part of the shell command")
	}
	if strings.Contains(mapping.Script, ">>>") {
		t.Fatalf("unexpected marker content in script %q", mapping.Script)
	}
	if !strings.Contains(mapping.Script, "<<EOF cat > /etc/app.conf") {
		t.Fatalf("expected heredoc command to be preserved, got %q", mapping.Script)
	}
	if !strings.Contains(mapping.Script, "enable-rpc=true") {
		t.Fatalf("expected payload lines to remain in script, got %q", mapping.Script)
	}
	if mapping.OriginStartLine != 2 {
		t.Fatalf("expected command origin line 2, got %d", mapping.OriginStartLine)
	}
}

func TestExtractRunScript_ExplicitShellUsesBodyWithOverride(t *testing.T) {
	t.Parallel()

	mapping := extractRunScriptForTest(t, `FROM alpine
RUN <<EOF bash
echo hi
EOF
`)

	if !mapping.IsHeredoc {
		t.Fatal("expected explicit shell heredoc to use body mapping")
	}
	if mapping.ShellNameOverride != "bash" {
		t.Fatalf("expected shell override bash, got %q", mapping.ShellNameOverride)
	}
	if mapping.Script != "echo hi" {
		t.Fatalf("expected body-only script, got %q", mapping.Script)
	}
}

func extractRunScriptForTest(t *testing.T, content string) Mapping {
	t.Helper()

	result, err := dockerfile.Parse(strings.NewReader(content), nil)
	if err != nil {
		t.Fatalf("parse Dockerfile: %v", err)
	}
	if result.AST == nil || result.AST.AST == nil || len(result.AST.AST.Children) < 2 {
		t.Fatal("expected parsed AST with RUN node")
	}

	mapping, ok := ExtractRunScript(
		sourcemap.New([]byte(content)),
		result.AST.AST.Children[1],
		result.AST.EscapeToken,
	)
	if !ok {
		t.Fatal("expected ExtractRunScript to succeed")
	}
	return mapping
}
