package heredocfmt

import (
	"errors"
	"strings"
	"testing"

	editorconfig "github.com/editorconfig/editorconfig-core-go/v2"
)

func TestSupportedKindXMLAliases(t *testing.T) {
	t.Parallel()

	tests := []string{
		"/etc/app/config.xml",
		"/etc/app/Web.config",
		"/schema/app.xsd",
		"/schema/service.wsdl",
		"/transforms/app.xsl",
		"/transforms/app.xslt",
	}

	for _, filename := range tests {
		t.Run(filename, func(t *testing.T) {
			t.Parallel()
			got, ok := SupportedKind(filename)
			if !ok {
				t.Fatalf("SupportedKind(%q) ok = false, want true", filename)
			}
			if got != KindXML {
				t.Fatalf("SupportedKind(%q) = %q, want %q", filename, got, KindXML)
			}
		})
	}
}

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

func TestFormatYAMLUsesMaxLineLength(t *testing.T) {
	t.Parallel()

	content := "message: alpha beta gamma delta epsilon zeta eta theta iota kappa lambda mu\n"
	got, err := formatYAML(content, style{indent: "  ", indentWidth: 2, maxLineLength: 32})
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(got, "\n  ") {
		t.Fatalf("expected YAML line wrapping, got:\n%s", got)
	}
	for _, word := range []string{"alpha", "lambda", "mu"} {
		if !strings.Contains(got, word) {
			t.Fatalf("expected wrapped YAML to preserve scalar word %q, got:\n%s", word, got)
		}
	}
}

func TestStyleFromDefinitionReadsMaxLineLength(t *testing.T) {
	t.Parallel()

	st := styleFromDefinition(&editorconfig.Definition{
		Raw: map[string]string{
			"max_line_length": "40",
		},
	})
	if st.maxLineLength != 40 {
		t.Fatalf("maxLineLength = %d, want 40", st.maxLineLength)
	}

	st = styleFromDefinition(&editorconfig.Definition{
		Raw: map[string]string{
			"max_line_length": "off",
		},
	})
	if st.maxLineLength != 0 {
		t.Fatalf("maxLineLength = %d, want 0 for off", st.maxLineLength)
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

func TestFormatINIUsesPrettyFormat(t *testing.T) {
	t.Parallel()

	content := "zend_extension=opcache\n[opcache]\nopcache.enable=1\nopcache.memory_consumption=128\n"
	got, err := formatINI(content, style{indent: "  ", indentWidth: 2})
	if err != nil {
		t.Fatal(err)
	}

	want := "zend_extension = opcache\n\n[opcache]\n  opcache.enable             = 1\n  opcache.memory_consumption = 128\n"
	if got != want {
		t.Fatalf("formatted INI mismatch\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestFormatINIPreservesDuplicateKeys(t *testing.T) {
	t.Parallel()

	got, err := formatINI("extension=foo\nextension=bar\n", style{indent: "  ", indentWidth: 2})
	if err != nil {
		t.Fatal(err)
	}

	if strings.Count(got, "extension") != 2 {
		t.Fatalf("expected duplicate keys to be preserved, got:\n%s", got)
	}
}

func TestFormatINIPreservesComments(t *testing.T) {
	t.Parallel()

	got, err := formatINI("[opcache]\n; extension settings\nopcache.enable=1\n", style{indent: "  ", indentWidth: 2})
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(got, "; extension settings") {
		t.Fatalf("expected comment to be preserved, got:\n%s", got)
	}
}

func TestNormalizeLineEndings(t *testing.T) {
	t.Parallel()

	got := normalizeLineEndings("a\r\nb\rc\n")
	if strings.Contains(got, "\r") {
		t.Fatalf("expected LF-only output, got %q", got)
	}
	want := "a\nb\nc\n"
	if got != want {
		t.Fatalf("normalizeLineEndings() = %q, want %q", got, want)
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
