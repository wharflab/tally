package sourcemap

import (
	"bytes"
	"testing"
)

func TestNew(t *testing.T) {
	source := []byte("FROM alpine\nRUN echo hello\nCMD [\"sh\"]")
	sm := New(source)

	if sm.LineCount() != 3 {
		t.Errorf("LineCount() = %d, want 3", sm.LineCount())
	}
}

func TestNew_EmptySource(t *testing.T) {
	sm := New([]byte{})
	if sm.LineCount() != 1 {
		// Empty source still has one empty "line"
		t.Errorf("LineCount() = %d, want 1", sm.LineCount())
	}
}

func TestNew_CRLF(t *testing.T) {
	source := []byte("FROM alpine\r\nRUN echo\r\n")
	sm := New(source)

	if sm.LineCount() != 3 {
		t.Errorf("LineCount() = %d, want 3", sm.LineCount())
	}
	// Lines should have \r stripped
	if sm.Line(0) != "FROM alpine" {
		t.Errorf("Line(0) = %q, want %q", sm.Line(0), "FROM alpine")
	}
}

func TestLines(t *testing.T) {
	source := []byte("FROM alpine\nRUN echo hello\nCMD [\"sh\"]")
	sm := New(source)

	lines := sm.Lines()
	expected := []string{"FROM alpine", "RUN echo hello", "CMD [\"sh\"]"}

	if len(lines) != len(expected) {
		t.Fatalf("len(Lines()) = %d, want %d", len(lines), len(expected))
	}

	for i, want := range expected {
		if lines[i] != want {
			t.Errorf("Lines()[%d] = %q, want %q", i, lines[i], want)
		}
	}
}

func TestLine(t *testing.T) {
	source := []byte("line0\nline1\nline2")
	sm := New(source)

	tests := []struct {
		line int
		want string
	}{
		{0, "line0"},
		{1, "line1"},
		{2, "line2"},
		{-1, ""},  // out of range
		{3, ""},   // out of range
		{100, ""}, // out of range
	}

	for _, tt := range tests {
		got := sm.Line(tt.line)
		if got != tt.want {
			t.Errorf("Line(%d) = %q, want %q", tt.line, got, tt.want)
		}
	}
}

func TestLineOffset(t *testing.T) {
	source := []byte("abc\ndefg\nhi")
	sm := New(source)
	// Line 0: "abc" at offset 0
	// Line 1: "defg" at offset 4 (after "abc\n")
	// Line 2: "hi" at offset 9 (after "abc\ndefg\n")

	tests := []struct {
		line int
		want int
	}{
		{0, 0},
		{1, 4},
		{2, 9},
		{-1, -1}, // out of range
		{3, -1},  // out of range
	}

	for _, tt := range tests {
		got := sm.LineOffset(tt.line)
		if got != tt.want {
			t.Errorf("LineOffset(%d) = %d, want %d", tt.line, got, tt.want)
		}
	}
}

func TestSnippet(t *testing.T) {
	source := []byte("line0\nline1\nline2\nline3\nline4")
	sm := New(source)

	tests := []struct {
		name      string
		startLine int
		endLine   int
		want      string
	}{
		{"single line", 2, 2, "line2"},
		{"multiple lines", 1, 3, "line1\nline2\nline3"},
		{"all lines", 0, 4, "line0\nline1\nline2\nline3\nline4"},
		{"clamped start", -5, 1, "line0\nline1"},
		{"clamped end", 3, 100, "line3\nline4"},
		{"inverted range", 3, 1, ""},
		{"start out of range", 10, 15, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sm.Snippet(tt.startLine, tt.endLine)
			if got != tt.want {
				t.Errorf("Snippet(%d, %d) = %q, want %q", tt.startLine, tt.endLine, got, tt.want)
			}
		})
	}
}

func TestSnippetAround(t *testing.T) {
	source := []byte("line0\nline1\nline2\nline3\nline4")
	sm := New(source)

	tests := []struct {
		name   string
		line   int
		before int
		after  int
		want   string
	}{
		{"center with context", 2, 1, 1, "line1\nline2\nline3"},
		{"at start", 0, 2, 1, "line0\nline1"},
		{"at end", 4, 1, 2, "line3\nline4"},
		{"no context", 2, 0, 0, "line2"},
		{"large context", 2, 10, 10, "line0\nline1\nline2\nline3\nline4"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sm.SnippetAround(tt.line, tt.before, tt.after)
			if got != tt.want {
				t.Errorf("SnippetAround(%d, %d, %d) = %q, want %q", tt.line, tt.before, tt.after, got, tt.want)
			}
		})
	}
}

func TestComments(t *testing.T) {
	source := []byte(`# This is a comment
FROM alpine
# Another comment
# tally ignore=DL3000
RUN echo hello
`)
	sm := New(source)

	comments := sm.Comments()

	if len(comments) != 3 {
		t.Fatalf("len(Comments()) = %d, want 3", len(comments))
	}

	// Check first comment
	if comments[0].Line != 0 {
		t.Errorf("comments[0].Line = %d, want 0", comments[0].Line)
	}
	if comments[0].Text != "# This is a comment" {
		t.Errorf("comments[0].Text = %q, want %q", comments[0].Text, "# This is a comment")
	}
	if comments[0].IsDirective {
		t.Error("comments[0].IsDirective = true, want false")
	}

	// Check directive comment
	if comments[2].Line != 3 {
		t.Errorf("comments[2].Line = %d, want 3", comments[2].Line)
	}
	if !comments[2].IsDirective {
		t.Error("comments[2].IsDirective = false, want true")
	}
}

func TestComments_Directives(t *testing.T) {
	tests := []struct {
		text        string
		isDirective bool
	}{
		{"# tally ignore=DL3000", true},
		{"# tally global ignore=DL3000", true},
		{"# hadolint ignore=DL3000", true},
		{"# hadolint global ignore=DL3000", true},
		{"# check=skip=StageNameCasing", true},
		{"# syntax=docker/dockerfile:1", true},
		{"# escape=`", true},
		{"# TALLY IGNORE=DL3000", true}, // case insensitive
		{"# regular comment", false},
		{"# tallyho!", false},                 // not a directive, just starts with "tally"
		{"#syntax=docker/dockerfile:1", true}, // no space after #
	}

	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			sm := New([]byte(tt.text))
			comments := sm.Comments()

			if len(comments) != 1 {
				t.Fatalf("len(Comments()) = %d, want 1", len(comments))
			}

			if comments[0].IsDirective != tt.isDirective {
				t.Errorf("IsDirective = %v, want %v", comments[0].IsDirective, tt.isDirective)
			}
		})
	}
}

func TestCommentsForLine(t *testing.T) {
	source := []byte(`FROM alpine

# Comment for RUN
# Another comment for RUN
RUN echo hello

# Standalone comment

CMD ["sh"]
`)
	sm := New(source)

	// Comments for line 4 (RUN) should be lines 2-3
	comments := sm.CommentsForLine(4)
	if len(comments) != 2 {
		t.Fatalf("CommentsForLine(4) = %d comments, want 2", len(comments))
	}
	if comments[0].Line != 2 {
		t.Errorf("comments[0].Line = %d, want 2", comments[0].Line)
	}
	if comments[1].Line != 3 {
		t.Errorf("comments[1].Line = %d, want 3", comments[1].Line)
	}

	// Comments for line 9 (CMD) - empty line breaks the block
	comments = sm.CommentsForLine(9)
	if len(comments) != 0 {
		t.Errorf("CommentsForLine(9) = %d comments, want 0 (empty line breaks block)", len(comments))
	}

	// Comments for line 0 (FROM) - nothing before it
	comments = sm.CommentsForLine(0)
	if len(comments) != 0 {
		t.Errorf("CommentsForLine(0) = %d comments, want 0", len(comments))
	}
}

func TestSource(t *testing.T) {
	source := []byte("FROM alpine\nRUN echo")
	sm := New(source)

	got := sm.Source()
	if !bytes.Equal(got, source) {
		t.Errorf("Source() = %q, want %q", string(got), string(source))
	}
}
