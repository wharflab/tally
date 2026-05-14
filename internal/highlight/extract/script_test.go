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

func TestExtractRunScript_BridgesDockerfileCommentsInContinuedRun(t *testing.T) {
	t.Parallel()

	mapping := extractRunScriptForTest(t, `FROM alpine
RUN echo one \
    # Dockerfile comment
    && echo two
`)

	want := "    echo one \\\n    \\\n    && echo two"
	if mapping.Script != want {
		t.Fatalf("script = %q, want %q", mapping.Script, want)
	}
	if strings.Contains(mapping.Script, "Dockerfile comment") {
		t.Fatalf("expected Dockerfile comment to be elided from shell script, got %q", mapping.Script)
	}
	if mapping.OriginStartLine != 2 {
		t.Fatalf("expected origin line 2, got %d", mapping.OriginStartLine)
	}
}

func TestExtractRunScript_KeepsHeredocBodyComments(t *testing.T) {
	t.Parallel()

	mapping := extractRunScriptForTest(t, `FROM alpine
RUN <<EOF
echo one
# shell comment
echo two
EOF
`)

	if mapping.Script != "echo one\n# shell comment\necho two" {
		t.Fatalf("expected heredoc body comments to be preserved, got %q", mapping.Script)
	}
}

func TestExtractRunScript_BridgesDockerfileCommentsInHeredocHeader(t *testing.T) {
	t.Parallel()

	mapping := extractRunScriptForTest(t, `FROM alpine
RUN --mount=type=cache,target=/root/.cache \
    # Dockerfile comment
    <<EOF
echo one
# shell comment
echo two
EOF
`)

	if !mapping.IsHeredoc {
		t.Fatalf("expected header comment to preserve heredoc body mapping, got Script=%q", mapping.Script)
	}
	want := "echo one\n# shell comment\necho two"
	if mapping.Script != want {
		t.Fatalf("expected body-only script, got %q", mapping.Script)
	}
	if mapping.OriginStartLine != 5 {
		t.Fatalf("expected body origin line 5, got %d", mapping.OriginStartLine)
	}
}

func TestExtractRunScript_BridgesHeaderCommentsForHeredocPayload(t *testing.T) {
	t.Parallel()

	mapping := extractRunScriptForTest(t, `FROM alpine
RUN --mount=type=cache,target=/root/.cache \
    # Dockerfile comment
    <<EOF cat > /etc/app.conf
# payload comment
enable-rpc=true
EOF
`)

	if mapping.IsHeredoc {
		t.Fatalf("expected file payload heredoc to remain part of full shell command, got Script=%q", mapping.Script)
	}
	if strings.Contains(mapping.Script, "Dockerfile comment") {
		t.Fatalf("expected Dockerfile header comment to be elided from shell script, got %q", mapping.Script)
	}
	if !strings.Contains(mapping.Script, "\n    \\\n") {
		t.Fatalf("expected header comment line to be bridged as a continuation line, got %q", mapping.Script)
	}
	if !strings.Contains(mapping.Script, "# payload comment") {
		t.Fatalf("expected heredoc payload comment to be preserved, got %q", mapping.Script)
	}
}

func TestExtractRunScript_BridgesCommentsBetweenHeredocPayloadOpeners(t *testing.T) {
	t.Parallel()

	mapping := extractRunScriptForTest(t, `FROM alpine
RUN <<FILE1 cat > /tmp/one && \
    # Dockerfile header comment
    <<FILE2 cat > /tmp/two
one \
# payload comment
FILE1
two
FILE2
`)

	if mapping.IsHeredoc {
		t.Fatalf("expected file-payload heredocs to remain part of full shell command, got Script=%q", mapping.Script)
	}
	if strings.Contains(mapping.Script, "Dockerfile header comment") {
		t.Fatalf("expected Dockerfile header comment between heredoc openers to be elided, got %q", mapping.Script)
	}
	if !strings.Contains(mapping.Script, "# payload comment") {
		t.Fatalf("expected heredoc payload comment to be preserved, got %q", mapping.Script)
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

func TestExtractRunScript_FlagsAcrossContinuationBeforeHeredoc(t *testing.T) {
	t.Parallel()

	mapping := extractRunScriptForTest(t, `FROM alpine
RUN --mount=type=cache,target=/root/.cache,id=cache \
	--mount=type=bind,source=payload,target=/payload,readonly \
	<<EOF
set -e
echo hi
EOF
`)

	if !mapping.IsHeredoc {
		t.Fatalf("expected heredoc body mapping when flags span continuation lines, got Script=%q", mapping.Script)
	}
	if mapping.Script != "set -e\necho hi" {
		t.Fatalf("expected body-only script, got %q", mapping.Script)
	}
	// Opener appears on line 4 of the Dockerfile, so the body starts on line 5.
	if mapping.OriginStartLine != 5 {
		t.Fatalf("expected body origin line 5, got %d", mapping.OriginStartLine)
	}
}

func TestExtractRunScript_FlagsAcrossContinuationWithShellOverride(t *testing.T) {
	t.Parallel()

	mapping := extractRunScriptForTest(t, `FROM alpine
RUN --mount=type=cache,target=/root/.cache,id=cache \
	<<EOF bash
echo hi
EOF
`)

	if !mapping.IsHeredoc {
		t.Fatalf("expected heredoc body mapping, got Script=%q", mapping.Script)
	}
	if mapping.ShellNameOverride != "bash" {
		t.Fatalf("expected shell override bash, got %q", mapping.ShellNameOverride)
	}
	if mapping.Script != "echo hi" {
		t.Fatalf("expected body-only script, got %q", mapping.Script)
	}
	if mapping.OriginStartLine != 4 {
		t.Fatalf("expected body origin line 4, got %d", mapping.OriginStartLine)
	}
}

func TestExtractRunScript_FilePayloadWithFlagsAcrossContinuation(t *testing.T) {
	t.Parallel()

	mapping := extractRunScriptForTest(t, `FROM alpine
RUN --mount=type=cache,target=/root/.cache,id=cache \
	--mount=type=bind,source=etc,target=/etc,readonly \
	<<EOF cat > /etc/app.conf
enable-rpc=true
EOF
`)

	if mapping.IsHeredoc {
		t.Fatalf(
			"expected file-payload heredoc to remain non-heredoc data even when flags span continuation lines, got Script=%q",
			mapping.Script,
		)
	}
	if strings.Contains(mapping.Script, ">>>") {
		t.Fatalf("unexpected marker content in script %q", mapping.Script)
	}
	if !strings.Contains(mapping.Script, "<<EOF cat > /etc/app.conf") {
		t.Fatalf("expected heredoc command to be preserved in script, got %q", mapping.Script)
	}
	if !strings.Contains(mapping.Script, "enable-rpc=true") {
		t.Fatalf("expected payload lines to remain in script, got %q", mapping.Script)
	}
	if mapping.OriginStartLine != 2 {
		t.Fatalf("expected command origin line 2, got %d", mapping.OriginStartLine)
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
