package syntax

import (
	"bytes"
	"strings"
	"testing"

	"github.com/moby/buildkit/frontend/dockerfile/parser"
)

func mustParse(t *testing.T, dockerfile string) *parser.Result {
	t.Helper()
	res, err := parser.Parse(bytes.NewReader([]byte(dockerfile)))
	if err != nil {
		t.Fatalf("parser.Parse: %v", err)
	}
	return res
}

func TestCheckUnknownInstructions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		dockerfile string
		wantCount  int
		wantSubstr string // substring expected in the first error message
	}{
		{
			name:       "FORM typo",
			dockerfile: "FORM alpine\nRUN echo hello\n",
			wantCount:  1,
			wantSubstr: `did you mean "FROM"`,
		},
		{
			name:       "COPPY typo",
			dockerfile: "FROM alpine\nCOPPY . /app\n",
			wantCount:  1,
			wantSubstr: `did you mean "COPY"`,
		},
		{
			name:       "WROKDIR typo",
			dockerfile: "FROM alpine\nWROKDIR /app\n",
			wantCount:  1,
			wantSubstr: `did you mean "WORKDIR"`,
		},
		{
			name:       "RUNN typo",
			dockerfile: "FROM alpine\nRUNN echo hello\n",
			wantCount:  1,
			wantSubstr: `did you mean "RUN"`,
		},
		{
			name:       "FOOBAR no suggestion",
			dockerfile: "FROM alpine\nFOOBAR something\n",
			wantCount:  1,
			wantSubstr: `unknown instruction "FOOBAR"`,
		},
		{
			name:       "multiple typos",
			dockerfile: "FORM alpine\nCOPPY . /app\nRUNN echo hello\n",
			wantCount:  3,
		},
		{
			name:       "valid dockerfile",
			dockerfile: "FROM alpine\nRUN echo hello\nCOPY . /app\n",
			wantCount:  0,
		},
		{
			name:       "all valid instructions",
			dockerfile: "FROM alpine\nLABEL key=val\nRUN echo hello\n",
			wantCount:  0,
		},
		{
			name:       "case insensitive valid",
			dockerfile: "from alpine\nrun echo hello\n",
			wantCount:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ast := mustParse(t, tt.dockerfile)
			errs := checkUnknownInstructions("Dockerfile", ast)
			if len(errs) != tt.wantCount {
				t.Errorf("got %d errors, want %d: %v", len(errs), tt.wantCount, errs)
			}
			if tt.wantSubstr != "" && len(errs) > 0 {
				if !strings.Contains(errs[0].Message, tt.wantSubstr) {
					t.Errorf("error message %q does not contain %q", errs[0].Message, tt.wantSubstr)
				}
			}
			// Verify all errors have the correct rule code.
			for _, e := range errs {
				if e.RuleCode != "tally/unknown-instruction" {
					t.Errorf("expected rule code tally/unknown-instruction, got %q", e.RuleCode)
				}
			}
		})
	}
}

func TestCheckSyntaxDirective(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		source     string
		wantCount  int
		wantSubstr string
	}{
		{
			name:       "misspelled docker/dokcerfile",
			source:     "# syntax=docker/dokcerfile:1.7\nFROM alpine\n",
			wantCount:  1,
			wantSubstr: `did you mean "docker/dockerfile:1.7"`,
		},
		{
			name:       "misspelled docker/dockefile",
			source:     "# syntax=docker/dockefile\nFROM alpine\n",
			wantCount:  1,
			wantSubstr: `did you mean "docker/dockerfile"`,
		},
		{
			name:       "misspelled docker.io prefix",
			source:     "# syntax=docker.io/docker/dockefile:1\nFROM alpine\n",
			wantCount:  1,
			wantSubstr: `did you mean "docker.io/docker/dockerfile:1"`,
		},
		{
			name:      "valid docker/dockerfile",
			source:    "# syntax=docker/dockerfile:1\nFROM alpine\n",
			wantCount: 0,
		},
		{
			name:      "valid docker.io/docker/dockerfile",
			source:    "# syntax=docker.io/docker/dockerfile:1.7\nFROM alpine\n",
			wantCount: 0,
		},
		{
			name:      "no syntax directive",
			source:    "FROM alpine\nRUN echo hello\n",
			wantCount: 0,
		},
		{
			name:      "custom frontend no match",
			source:    "# syntax=mycompany/custom-frontend:latest\nFROM alpine\n",
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			errs := checkSyntaxDirective("Dockerfile", []byte(tt.source))
			if len(errs) != tt.wantCount {
				t.Errorf("got %d errors, want %d: %v", len(errs), tt.wantCount, errs)
			}
			if tt.wantSubstr != "" && len(errs) > 0 {
				if !strings.Contains(errs[0].Message, tt.wantSubstr) {
					t.Errorf("error message %q does not contain %q", errs[0].Message, tt.wantSubstr)
				}
			}
			for _, e := range errs {
				if e.RuleCode != "tally/syntax-directive-typo" {
					t.Errorf("expected rule code tally/syntax-directive-typo, got %q", e.RuleCode)
				}
			}
		})
	}
}

func TestCheck(t *testing.T) {
	t.Parallel()

	t.Run("both checks fire", func(t *testing.T) {
		t.Parallel()
		source := "# syntax=docker/dokcerfile:1\nFORM alpine\n"
		ast := mustParse(t, source)
		errs := Check("Dockerfile", ast, []byte(source))
		if len(errs) != 2 {
			t.Errorf("expected 2 errors, got %d: %v", len(errs), errs)
		}
	})

	t.Run("clean file", func(t *testing.T) {
		t.Parallel()
		source := "# syntax=docker/dockerfile:1\nFROM alpine\nRUN echo hello\n"
		ast := mustParse(t, source)
		errs := Check("Dockerfile", ast, []byte(source))
		if len(errs) != 0 {
			t.Errorf("expected 0 errors, got %d: %v", len(errs), errs)
		}
	})
}

func TestCheckError(t *testing.T) {
	t.Parallel()

	t.Run("single error", func(t *testing.T) {
		t.Parallel()
		e := &CheckError{Errors: []Error{{File: "f", Message: "m", Line: 1}}}
		if e.Error() != "1 syntax error found" {
			t.Errorf("unexpected: %q", e.Error())
		}
	})

	t.Run("multiple errors", func(t *testing.T) {
		t.Parallel()
		e := &CheckError{Errors: []Error{{}, {}}}
		if e.Error() != "2 syntax errors found" {
			t.Errorf("unexpected: %q", e.Error())
		}
	})
}

func TestErrorString(t *testing.T) {
	t.Parallel()

	e := &Error{File: "path/to/Dockerfile", Message: `unknown instruction "FORM"`, Line: 3}
	want := `path/to/Dockerfile:3: unknown instruction "FORM"`
	if got := e.Error(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestLevenshteinDistance(t *testing.T) {
	t.Parallel()

	tests := []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"abc", "", 3},
		{"", "abc", 3},
		{"abc", "abc", 0},
		{"from", "form", 2},
		{"run", "runn", 1},
		{"copy", "coppy", 1},
		{"workdir", "wrokdir", 2},
	}

	for _, tt := range tests {
		t.Run(tt.a+"_"+tt.b, func(t *testing.T) {
			t.Parallel()
			got := levenshteinDistance(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("levenshteinDistance(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
			}
		})
	}
}
