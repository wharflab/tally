package tally

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wharflab/tally/internal/fix"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/testutil"
)

func TestPreferFormattedHeredocsRule_Metadata(t *testing.T) {
	t.Parallel()
	meta := NewPreferFormattedHeredocsRule().Metadata()
	if meta.Code != rules.FormattedHeredocsRuleCode {
		t.Fatalf("Code = %q, want %q", meta.Code, rules.FormattedHeredocsRuleCode)
	}
	if meta.DefaultSeverity != rules.SeverityStyle {
		t.Fatalf("DefaultSeverity = %v, want style", meta.DefaultSeverity)
	}
	if meta.FixPriority != rules.FormattedHeredocsFixPriority {
		t.Fatalf("FixPriority = %d, want %d", meta.FixPriority, rules.FormattedHeredocsFixPriority)
	}
}

func TestPreferFormattedHeredocsRule_Check(t *testing.T) {
	t.Parallel()
	rule := NewPreferFormattedHeredocsRule()

	testutil.RunRuleTests(t, rule, []testutil.RuleTestCase{
		{
			Name: "compact JSON COPY heredoc",
			Content: `FROM alpine
COPY <<EOF /etc/app/config.json
{"b":2,"a":1}
EOF
`,
			WantViolations: 1,
			WantMessages:   []string{"COPY heredoc for /etc/app/config.json should be pretty-printed as JSON"},
		},
		{
			Name: "compact XML ADD heredoc",
			Content: `FROM alpine
ADD <<EOF /etc/app/config.xml
<root><child>1</child></root>
EOF
`,
			WantViolations: 1,
			WantMessages:   []string{"ADD heredoc for /etc/app/config.xml should be pretty-printed as XML"},
		},
		{
			Name: "compact XML config COPY heredoc",
			Content: `FROM alpine
COPY <<EOF /etc/app/Web.config
<configuration><system.web></system.web></configuration>
EOF
`,
			WantViolations: 1,
			WantMessages:   []string{"COPY heredoc for /etc/app/Web.config should be pretty-printed as XML"},
		},
		{
			Name: "compact YAML COPY heredoc",
			Content: `FROM alpine
COPY <<EOF /etc/app/config.yaml
{"b":2,"a":1}
EOF
`,
			WantViolations: 1,
			WantMessages:   []string{"COPY heredoc for /etc/app/config.yaml should be pretty-printed as YAML"},
		},
		{
			Name: "compact TOML COPY heredoc",
			Content: `FROM alpine
COPY <<EOF /etc/app/config.toml
a=1
EOF
`,
			WantViolations: 1,
			WantMessages:   []string{"COPY heredoc for /etc/app/config.toml should be pretty-printed as TOML"},
		},
		{
			Name: "compact INI COPY heredoc",
			Content: `FROM alpine
COPY <<EOF /etc/app/php.ini
zend_extension=opcache
[opcache]
opcache.enable=1
EOF
`,
			WantViolations: 1,
			WantMessages:   []string{"COPY heredoc for /etc/app/php.ini should be pretty-printed as INI"},
		},
		{
			Name: "already formatted JSON",
			Content: `FROM alpine
COPY <<EOF /etc/app/config.json
{
  "a": 1
}
EOF
`,
			WantViolations: 0,
		},
		{
			Name: "unsupported extension",
			Content: `FROM alpine
COPY <<EOF /etc/app/config.txt
{"b":2,"a":1}
EOF
`,
			WantViolations: 0,
		},
		{
			Name: "invalid JSON",
			Content: `FROM alpine
COPY <<EOF /etc/app/config.json
{"b":2,
EOF
`,
			WantViolations: 0,
		},
	})
}

func TestPreferFormattedHeredocsRule_FixJSON(t *testing.T) {
	t.Parallel()
	content := `FROM alpine
COPY <<EOF /etc/app/config.json
{"b":2,"a":1}
EOF
`
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	violations := NewPreferFormattedHeredocsRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("got %d violations, want 1", len(violations))
	}

	got := string(fix.ApplyFix([]byte(content), violations[0].PreferredFix()))
	want := `FROM alpine
COPY <<EOF /etc/app/config.json
{
  "b": 2,
  "a": 1
}
EOF
`
	if got != want {
		t.Fatalf("fixed content mismatch\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestPreferFormattedHeredocsRule_FixYAML(t *testing.T) {
	t.Parallel()
	content := `FROM alpine
COPY <<EOF /etc/app/config.yaml
{"b":2,"a":1}
EOF
`
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	violations := NewPreferFormattedHeredocsRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("got %d violations, want 1", len(violations))
	}

	got := string(fix.ApplyFix([]byte(content), violations[0].PreferredFix()))
	want := `FROM alpine
COPY <<EOF /etc/app/config.yaml
"b": 2
"a": 1
EOF
`
	if got != want {
		t.Fatalf("fixed content mismatch\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestPreferFormattedHeredocsRule_FixTOML(t *testing.T) {
	t.Parallel()
	content := `FROM alpine
COPY <<EOF /etc/app/config.toml
a=1
EOF
`
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	violations := NewPreferFormattedHeredocsRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("got %d violations, want 1", len(violations))
	}

	got := string(fix.ApplyFix([]byte(content), violations[0].PreferredFix()))
	want := `FROM alpine
COPY <<EOF /etc/app/config.toml
a = 1
EOF
`
	if got != want {
		t.Fatalf("fixed content mismatch\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestPreferFormattedHeredocsRule_FixINI(t *testing.T) {
	t.Parallel()
	content := `FROM alpine
COPY <<EOF /etc/app/php.ini
zend_extension=opcache
[opcache]
opcache.enable=1
opcache.memory_consumption=128
EOF
`
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	violations := NewPreferFormattedHeredocsRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("got %d violations, want 1", len(violations))
	}

	got := string(fix.ApplyFix([]byte(content), violations[0].PreferredFix()))
	want := `FROM alpine
COPY <<EOF /etc/app/php.ini
zend_extension = opcache

[opcache]
  opcache.enable             = 1
  opcache.memory_consumption = 128
EOF
`
	if got != want {
		t.Fatalf("fixed content mismatch\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestPreferFormattedHeredocsRule_FixXML(t *testing.T) {
	t.Parallel()
	content := `FROM alpine
COPY <<EOF /etc/app/config.xml
<root><child>1</child></root>
EOF
`
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	violations := NewPreferFormattedHeredocsRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("got %d violations, want 1", len(violations))
	}

	got := string(fix.ApplyFix([]byte(content), violations[0].PreferredFix()))
	want := `FROM alpine
COPY <<EOF /etc/app/config.xml
<root>
  <child>1</child>
</root>
EOF
`
	if got != want {
		t.Fatalf("fixed content mismatch\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestPreferFormattedHeredocsRule_EditorConfig(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := writeTestFile(filepath.Join(dir, ".editorconfig"), `root = true

[*.json]
indent_style = space
indent_size = 4
`); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "Dockerfile")
	content := `FROM alpine
COPY <<EOF /etc/app/config.json
{"a":1}
EOF
`
	input := testutil.MakeLintInput(t, file, content)
	violations := NewPreferFormattedHeredocsRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("got %d violations, want 1", len(violations))
	}

	got := string(fix.ApplyFix([]byte(content), violations[0].PreferredFix()))
	if !strings.Contains(got, "    \"a\": 1") {
		t.Fatalf("expected 4-space JSON indent, got:\n%s", got)
	}
}

func TestPreferFormattedHeredocsRule_ChompedHeredocKeepsTabPrefix(t *testing.T) {
	t.Parallel()
	content := "FROM alpine AS build\n\tCOPY <<-EOF /etc/app/config.json\n\t{\"a\":1}\n\tEOF\n"
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	violations := NewPreferFormattedHeredocsRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("got %d violations, want 1", len(violations))
	}

	got := string(fix.ApplyFix([]byte(content), violations[0].PreferredFix()))
	want := "FROM alpine AS build\n\tCOPY <<-EOF /etc/app/config.json\n\t{\n\t  \"a\": 1\n\t}\n\tEOF\n"
	if got != want {
		t.Fatalf("fixed content mismatch\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func writeTestFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}
