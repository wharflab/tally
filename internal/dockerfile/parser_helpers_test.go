package dockerfile

import (
	"strings"
	"testing"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/wharflab/tally/internal/sourcemap"
)

func TestExtractRuleNameFromURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		url  string
		want string
	}{
		{
			name: "valid",
			url:  "https://docs.docker.com/go/dockerfile/rule/no-empty-continuation/",
			want: "NoEmptyContinuation",
		},
		{
			name: "valid-no-trailing-slash",
			url:  "https://docs.docker.com/go/dockerfile/rule/no-empty-continuation",
			want: "NoEmptyContinuation",
		},
		{
			name: "wrong-prefix",
			url:  "https://example.com/go/dockerfile/rule/no-empty-continuation/",
			want: "",
		},
		{
			name: "empty-suffix",
			url:  "https://docs.docker.com/go/dockerfile/rule/",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := extractRuleNameFromURL(tt.url); got != tt.want {
				t.Fatalf("extractRuleNameFromURL(%q) = %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}

func TestKebabToPascalCase(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   string
		want string
	}{
		{"no-empty-continuation", "NoEmptyContinuation"},
		{"json-args-recommended", "JsonArgsRecommended"},
		{"", ""},
		{"--", ""},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()
			if got := kebabToPascalCase(tt.in); got != tt.want {
				t.Fatalf("kebabToPascalCase(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestExtractHeredocFiles(t *testing.T) {
	t.Parallel()
	content := syntaxDirective + `FROM alpine
RUN echo hi
COPY <<CONFIG /app/config.txt
key=value
CONFIG
ADD <<DATA /app/data.txt
data
DATA
`

	result, err := Parse(strings.NewReader(content), nil)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	files := ExtractHeredocFiles(result.Stages)
	if !files["CONFIG"] {
		t.Fatalf("expected CONFIG to be detected as heredoc file name")
	}
	if !files["DATA"] {
		t.Fatalf("expected DATA to be detected as heredoc file name")
	}
	if len(files) != 2 {
		t.Fatalf("expected exactly 2 heredoc file names, got %d (%v)", len(files), files)
	}
}

func TestBlankRunFlags(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			"no flags",
			"    curl -fsSL https://example.com",
			"    curl -fsSL https://example.com",
		},
		{
			"single mount",
			"    --mount=type=cache,target=/var/cache/apt curl -fsSL https://example.com",
			"                                             curl -fsSL https://example.com",
		},
		{
			"multiple mounts",
			"    --mount=type=cache,target=/var/cache/apt --mount=type=bind,source=go.sum,target=go.sum apt-get update",
			"                                                                                           apt-get update",
		},
		{
			"network flag",
			"    --network=none useradd app",
			"                   useradd app",
		},
		{
			"security flag",
			"    --security=insecure make build",
			"                        make build",
		},
		{
			"unknown flag not blanked",
			"    --custom=value curl -fsSL",
			"    --custom=value curl -fsSL",
		},
		{
			"shell flag not blanked",
			"    -c echo hello",
			"    -c echo hello",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := blankRunFlags(tt.in)
			if got != tt.want {
				t.Errorf("blankRunFlags(%q)\ngot:  %q\nwant: %q", tt.in, got, tt.want)
			}
			if len(got) != len(tt.in) {
				t.Errorf("length changed: got %d, want %d (must preserve column positions)", len(got), len(tt.in))
			}
		})
	}
}

func TestResolveRunSource(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		dockerfile string
		wantScript string
		wantPrefix string
	}{
		{
			name: "preserves buildkit run flags",
			dockerfile: `FROM alpine
RUN --mount=type=cache,target=/var/cache/apk --network=none echo hello
`,
			wantScript: "echo hello",
			wantPrefix: "RUN --mount=type=cache,target=/var/cache/apk --network=none ",
		},
		{
			name: "uses heredoc content",
			dockerfile: `FROM alpine
RUN <<EOF
echo hello
EOF
`,
			wantScript: "echo hello\n",
			wantPrefix: "RUN <<EOF\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result, err := Parse(strings.NewReader(tt.dockerfile), nil)
			if err != nil {
				t.Fatalf("Parse failed: %v", err)
			}
			sm := sourcemap.New(result.Source)

			run, ok := result.Stages[0].Commands[0].(*instructions.RunCommand)
			if !ok {
				t.Fatal("expected RUN command")
			}

			got, ok := ResolveRunSource(run, sm)
			if !ok {
				t.Fatal("ResolveRunSource() returned !ok")
			}
			if got.Script != tt.wantScript {
				t.Fatalf("ResolveRunSource().Script = %q, want %q", got.Script, tt.wantScript)
			}
			if !strings.HasPrefix(got.Source, tt.wantPrefix) {
				t.Fatalf("ResolveRunSource().Source = %q, want prefix %q", got.Source, tt.wantPrefix)
			}
			if got.ScriptIndex < len(tt.wantPrefix) {
				t.Fatalf("ResolveRunSource().ScriptIndex = %d, want >= %d", got.ScriptIndex, len(tt.wantPrefix))
			}
			if got.Source[got.ScriptIndex:got.ScriptIndex+len(got.Script)] != got.Script {
				t.Fatalf("script span mismatch in source at index %d", got.ScriptIndex)
			}
		})
	}
}
