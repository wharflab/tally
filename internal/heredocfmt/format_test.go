package heredocfmt

import (
	"errors"
	"strings"
	"testing"
)

func TestFormatYAMLPreservesQuotedScalars(t *testing.T) {
	t.Parallel()

	got, err := formatYAML("enabled: \"true\"\nmode: '0644'\n", style{indent: "  ", indentWidth: 2})
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(got, `enabled: "true"`) {
		t.Fatalf("expected double-quoted scalar to stay quoted, got:\n%s", got)
	}
	if !strings.Contains(got, "mode: '0644'") {
		t.Fatalf("expected single-quoted scalar to stay quoted, got:\n%s", got)
	}
}

func TestTOMLHasCommentIgnoresHashesInMultilineStrings(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{
			name:    "multiline basic string",
			content: "message = \"\"\"value # not a comment\"\"\"\n",
			want:    false,
		},
		{
			name:    "multiline literal string",
			content: "message = '''value # not a comment'''\n",
			want:    false,
		},
		{
			name:    "actual comment after value",
			content: "message = \"value\" # comment\n",
			want:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tomlHasComment(tt.content); got != tt.want {
				t.Fatalf("tomlHasComment() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFormatTOMLAllowsHashesInMultilineStrings(t *testing.T) {
	t.Parallel()

	got, err := formatTOML("message = \"\"\"value # not a comment\"\"\"\n", style{indent: "  ", indentWidth: 2})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "# not a comment") {
		t.Fatalf("expected formatted TOML to keep string content, got:\n%s", got)
	}
}

func TestFormatXMLAllowsIndentationOnlyCharData(t *testing.T) {
	t.Parallel()

	got, err := formatXML("<root>\n  <child>1</child>\n</root>\n", style{indent: "  ", indentWidth: 2})
	if err != nil {
		t.Fatal(err)
	}

	want := "<root>\n  <child>1</child>\n</root>\n"
	if got != want {
		t.Fatalf("formatted XML mismatch\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestFormatXMLSkipsWhitespaceSignificantMixedContent(t *testing.T) {
	t.Parallel()

	_, err := formatXML("<root>\n  text\n</root>\n", style{indent: "  ", indentWidth: 2})
	if !errors.Is(err, errSkipFormat) {
		t.Fatalf("formatXML() error = %v, want errSkipFormat", err)
	}
}
