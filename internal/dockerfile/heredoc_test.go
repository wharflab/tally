package dockerfile

import (
	"strings"
	"testing"
)

// syntaxDirective is required to enable heredoc support in BuildKit parser
const syntaxDirective = "# syntax=docker/dockerfile:1\n"

func TestExtractHeredocs_Empty(t *testing.T) {
	content := "FROM alpine\nRUN echo hello"
	result, err := Parse(strings.NewReader(content), nil)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	heredocs := ExtractHeredocs(result)
	if len(heredocs) != 0 {
		t.Errorf("ExtractHeredocs() = %d heredocs, want 0", len(heredocs))
	}
}

func TestExtractHeredocs_RUNHeredoc(t *testing.T) {
	content := syntaxDirective + `FROM alpine
RUN <<EOF
echo hello
echo world
EOF
`
	result, err := Parse(strings.NewReader(content), nil)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	heredocs := ExtractHeredocs(result)
	if len(heredocs) != 1 {
		t.Fatalf("ExtractHeredocs() = %d heredocs, want 1", len(heredocs))
	}

	hd := heredocs[0]
	if hd.Kind != HeredocKindScript {
		t.Errorf("Kind = %v, want HeredocKindScript", hd.Kind)
	}
	if hd.Instruction != "RUN" {
		t.Errorf("Instruction = %q, want %q", hd.Instruction, "RUN")
	}
	if hd.Name != "EOF" {
		t.Errorf("Name = %q, want %q", hd.Name, "EOF")
	}
	if !strings.Contains(hd.Content, "echo hello") {
		t.Errorf("Content does not contain 'echo hello': %q", hd.Content)
	}
	if !hd.IsScript() {
		t.Error("IsScript() = false, want true")
	}
	if hd.IsInlineSource() {
		t.Error("IsInlineSource() = true, want false")
	}
}

func TestExtractHeredocs_COPYHeredoc(t *testing.T) {
	content := syntaxDirective + `FROM alpine
COPY <<EOF /app/config.txt
key=value
other=data
EOF
`
	result, err := Parse(strings.NewReader(content), nil)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	heredocs := ExtractHeredocs(result)
	if len(heredocs) != 1 {
		t.Fatalf("ExtractHeredocs() = %d heredocs, want 1", len(heredocs))
	}

	hd := heredocs[0]
	if hd.Kind != HeredocKindInlineSource {
		t.Errorf("Kind = %v, want HeredocKindInlineSource", hd.Kind)
	}
	if hd.Instruction != "COPY" {
		t.Errorf("Instruction = %q, want %q", hd.Instruction, "COPY")
	}
	if !hd.IsInlineSource() {
		t.Error("IsInlineSource() = false, want true")
	}
	if hd.IsScript() {
		t.Error("IsScript() = true, want false")
	}
}

func TestExtractHeredocs_ADDHeredoc(t *testing.T) {
	content := syntaxDirective + `FROM alpine
ADD <<EOF /app/data.txt
some data
EOF
`
	result, err := Parse(strings.NewReader(content), nil)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	heredocs := ExtractHeredocs(result)
	if len(heredocs) != 1 {
		t.Fatalf("ExtractHeredocs() = %d heredocs, want 1", len(heredocs))
	}

	hd := heredocs[0]
	if hd.Kind != HeredocKindInlineSource {
		t.Errorf("Kind = %v, want HeredocKindInlineSource", hd.Kind)
	}
	if hd.Instruction != "ADD" {
		t.Errorf("Instruction = %q, want %q", hd.Instruction, "ADD")
	}
}

func TestExtractHeredocs_Multiple(t *testing.T) {
	content := syntaxDirective + `FROM alpine
RUN <<SCRIPT
#!/bin/bash
set -e
echo "Building..."
SCRIPT

COPY <<CONFIG /etc/app.conf
debug=false
port=8080
CONFIG

RUN <<'EOF2'
echo "Done"
EOF2
`
	result, err := Parse(strings.NewReader(content), nil)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	heredocs := ExtractHeredocs(result)
	if len(heredocs) != 3 {
		t.Fatalf("ExtractHeredocs() = %d heredocs, want 3", len(heredocs))
	}

	// First: RUN script
	if heredocs[0].Kind != HeredocKindScript {
		t.Errorf("heredocs[0].Kind = %v, want HeredocKindScript", heredocs[0].Kind)
	}
	if heredocs[0].Name != "SCRIPT" {
		t.Errorf("heredocs[0].Name = %q, want %q", heredocs[0].Name, "SCRIPT")
	}

	// Second: COPY inline source
	if heredocs[1].Kind != HeredocKindInlineSource {
		t.Errorf("heredocs[1].Kind = %v, want HeredocKindInlineSource", heredocs[1].Kind)
	}

	// Third: RUN script
	if heredocs[2].Kind != HeredocKindScript {
		t.Errorf("heredocs[2].Kind = %v, want HeredocKindScript", heredocs[2].Kind)
	}
	if heredocs[2].Name != "EOF2" {
		t.Errorf("heredocs[2].Name = %q, want %q", heredocs[2].Name, "EOF2")
	}
}

func TestHasHeredocs(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{
			name:    "no heredocs",
			content: "FROM alpine\nRUN echo hello",
			want:    false,
		},
		{
			name: "with RUN heredoc",
			content: syntaxDirective + `FROM alpine
RUN <<EOF
echo test
EOF
`,
			want: true,
		},
		{
			name: "with COPY heredoc",
			content: syntaxDirective + `FROM alpine
COPY <<EOF /app/file
content
EOF
`,
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := Parse(strings.NewReader(tt.content), nil)
			if err != nil {
				t.Fatalf("Parse failed: %v", err)
			}

			got := HasHeredocs(result)
			if got != tt.want {
				t.Errorf("HasHeredocs() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHeredocKind_String(t *testing.T) {
	tests := []struct {
		kind HeredocKind
		want string
	}{
		{HeredocKindUnknown, "unknown"},
		{HeredocKindScript, "script"},
		{HeredocKindInlineSource, "inline-source"},
		{HeredocKind(99), "unknown"},
	}

	for _, tt := range tests {
		got := tt.kind.String()
		if got != tt.want {
			t.Errorf("HeredocKind(%d).String() = %q, want %q", tt.kind, got, tt.want)
		}
	}
}

func TestExtractHeredocs_NilInput(t *testing.T) {
	// Should not panic
	heredocs := ExtractHeredocs(nil)
	if len(heredocs) != 0 {
		t.Errorf("ExtractHeredocs(nil) = %d heredocs, want 0", len(heredocs))
	}

	// Empty result
	heredocs = ExtractHeredocs(&ParseResult{})
	if len(heredocs) != 0 {
		t.Errorf("ExtractHeredocs(empty) = %d heredocs, want 0", len(heredocs))
	}
}

func TestHeredocLine(t *testing.T) {
	// Note: syntax directive is on line 0, so RUN is on line 3 (0-based)
	content := syntaxDirective + `FROM alpine
# comment
RUN <<EOF
echo test
EOF
`
	result, err := Parse(strings.NewReader(content), nil)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	heredocs := ExtractHeredocs(result)
	if len(heredocs) != 1 {
		t.Fatalf("ExtractHeredocs() = %d heredocs, want 1", len(heredocs))
	}

	// RUN is on line 3 (0-based): syntax=0, FROM=1, comment=2, RUN=3
	if heredocs[0].Line != 3 {
		t.Errorf("Line = %d, want 3", heredocs[0].Line)
	}
}

func TestHeredocExpand(t *testing.T) {
	// Test heredoc with variable expansion (no quotes around delimiter)
	content := syntaxDirective + `FROM alpine
ARG NAME=world
RUN <<EOF
echo "Hello $NAME"
EOF
`
	result, err := Parse(strings.NewReader(content), nil)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	heredocs := ExtractHeredocs(result)
	if len(heredocs) != 1 {
		t.Fatalf("ExtractHeredocs() = %d heredocs, want 1", len(heredocs))
	}

	// Without quotes, Expand should be true
	if !heredocs[0].Expand {
		t.Error("Expand = false, want true (no quotes around delimiter)")
	}
}

func TestHeredocNoExpand(t *testing.T) {
	// Test heredoc without variable expansion (quoted delimiter)
	content := syntaxDirective + `FROM alpine
ARG NAME=world
RUN <<'EOF'
echo "Hello $NAME"
EOF
`
	result, err := Parse(strings.NewReader(content), nil)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	heredocs := ExtractHeredocs(result)
	if len(heredocs) != 1 {
		t.Fatalf("ExtractHeredocs() = %d heredocs, want 1", len(heredocs))
	}

	// With quotes, Expand should be false
	if heredocs[0].Expand {
		t.Error("Expand = true, want false (quoted delimiter)")
	}
}
