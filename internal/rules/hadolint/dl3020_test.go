package hadolint

import (
	"testing"

	"github.com/tinovyatkin/tally/internal/rules"
	"github.com/tinovyatkin/tally/internal/testutil"
)

func TestDL3020Rule_Metadata(t *testing.T) {
	r := NewDL3020Rule()
	meta := r.Metadata()

	if meta.Code != rules.HadolintRulePrefix+"DL3020" {
		t.Errorf("Code = %q, want %q", meta.Code, rules.HadolintRulePrefix+"DL3020")
	}
	if meta.DefaultSeverity != rules.SeverityError {
		t.Errorf("DefaultSeverity = %v, want %v", meta.DefaultSeverity, rules.SeverityError)
	}
	if meta.Category != "best-practice" {
		t.Errorf("Category = %q, want %q", meta.Category, "best-practice")
	}
}

func TestDL3020Rule_Check(t *testing.T) {
	tests := []struct {
		name       string
		dockerfile string
		wantCount  int
		wantCode   string
	}{
		{
			name: "ADD with local file",
			dockerfile: `FROM ubuntu:22.04
ADD file.txt /app/
`,
			wantCount: 1,
			wantCode:  rules.HadolintRulePrefix + "DL3020",
		},
		{
			name: "ADD with local directory",
			dockerfile: `FROM ubuntu:22.04
ADD src/ /app/src/
`,
			wantCount: 1,
		},
		{
			name: "COPY is fine",
			dockerfile: `FROM ubuntu:22.04
COPY file.txt /app/
`,
			wantCount: 0,
		},
		{
			name: "ADD with HTTP URL is allowed",
			dockerfile: `FROM ubuntu:22.04
ADD https://example.com/file.txt /app/
`,
			wantCount: 0,
		},
		{
			name: "ADD with HTTPS URL is allowed",
			dockerfile: `FROM ubuntu:22.04
ADD https://github.com/project/archive.tar.gz /tmp/
`,
			wantCount: 0,
		},
		{
			name: "ADD with tar.gz is allowed",
			dockerfile: `FROM ubuntu:22.04
ADD archive.tar.gz /app/
`,
			wantCount: 0,
		},
		{
			name: "ADD with .tar is allowed",
			dockerfile: `FROM ubuntu:22.04
ADD backup.tar /app/
`,
			wantCount: 0,
		},
		{
			name: "ADD with .tgz is allowed",
			dockerfile: `FROM ubuntu:22.04
ADD package.tgz /app/
`,
			wantCount: 0,
		},
		{
			name: "ADD with .tar.bz2 is allowed",
			dockerfile: `FROM ubuntu:22.04
ADD archive.tar.bz2 /app/
`,
			wantCount: 0,
		},
		{
			name: "ADD with .tar.xz is allowed",
			dockerfile: `FROM ubuntu:22.04
ADD archive.tar.xz /app/
`,
			wantCount: 0,
		},
		{
			name: "multiple ADD instructions",
			dockerfile: `FROM ubuntu:22.04
ADD file1.txt /app/
ADD file2.txt /app/
`,
			wantCount: 2,
		},
		{
			name: "mixed ADD usage",
			dockerfile: `FROM ubuntu:22.04
ADD https://example.com/download.tar.gz /tmp/
ADD localfile.txt /app/
`,
			wantCount: 1,
		},
		{
			name: "multi-stage with ADD",
			dockerfile: `FROM ubuntu:22.04 AS builder
ADD src/ /build/

FROM alpine:3.18
COPY --from=builder /build/bin /app/bin
`,
			wantCount: 1,
		},
		{
			name: "ADD with wildcard",
			dockerfile: `FROM ubuntu:22.04
ADD *.txt /app/
`,
			wantCount: 1,
		},
		{
			name: "ADD with git URL is allowed",
			dockerfile: `FROM ubuntu:22.04
ADD git://github.com/user/repo.git /app/
`,
			wantCount: 0,
		},
		{
			name: "ADD with multiple sources",
			dockerfile: `FROM ubuntu:22.04
ADD file1.txt file2.txt /app/
`,
			wantCount: 1, // Only one violation per ADD instruction
		},
		// Tests from hadolint/hadolint test/Hadolint/Rule/DL3020Spec.hs
		{
			name: "ADD for tgz with quotes",
			dockerfile: `FROM ubuntu:22.04
ADD "file.tgz" /usr/src/app/
`,
			wantCount: 0, // tar archives are fine
		},
		{
			name: "ADD for URL with quotes",
			dockerfile: `FROM ubuntu:22.04
ADD "http://file.com" /usr/src/app/
`,
			wantCount: 0, // URLs are fine
		},
		{
			name: "ADD for zip with quotes - should warn",
			dockerfile: `FROM ubuntu:22.04
ADD "file.zip" /usr/src/app/
`,
			wantCount: 1, // zip is not auto-extracted by ADD
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := testutil.MakeLintInput(t, "Dockerfile", tt.dockerfile)

			r := NewDL3020Rule()
			violations := r.Check(input)

			if len(violations) != tt.wantCount {
				t.Errorf("got %d violations, want %d", len(violations), tt.wantCount)
				for _, v := range violations {
					t.Logf("  - %s: %s", v.RuleCode, v.Message)
				}
			}

			if tt.wantCode != "" && len(violations) > 0 {
				if violations[0].RuleCode != tt.wantCode {
					t.Errorf("RuleCode = %q, want %q", violations[0].RuleCode, tt.wantCode)
				}
			}
		})
	}
}

func TestIsURLDL3020(t *testing.T) {
	tests := []struct {
		src  string
		want bool
	}{
		{"http://example.com/file", true},
		{"https://example.com/file", true},
		{"ftp://example.com/file", true},
		{"git://github.com/user/repo.git", true},
		{"HTTP://EXAMPLE.COM/FILE", true},
		{"file.txt", false},
		{"/absolute/path", false},
		{"./relative/path", false},
		{"httpfile.txt", false}, // Not a URL, just starts with http
	}

	for _, tt := range tests {
		t.Run(tt.src, func(t *testing.T) {
			got := isURLDL3020(tt.src)
			if got != tt.want {
				t.Errorf("isURLDL3020(%q) = %v, want %v", tt.src, got, tt.want)
			}
		})
	}
}

func TestStripQuotes(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{`"file.txt"`, "file.txt"},
		{`'file.txt'`, "file.txt"},
		{`file.txt`, "file.txt"},
		{`"http://example.com"`, "http://example.com"},
		{`""`, ""},
		{`"a"`, "a"},
		{``, ""},
		{`"`, `"`},
		{`'`, `'`},
		{`"mismatched'`, `"mismatched'`},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := stripQuotes(tt.input)
			if got != tt.want {
				t.Errorf("stripQuotes(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestIsTarArchiveDL3020(t *testing.T) {
	tests := []struct {
		src  string
		want bool
	}{
		{"archive.tar", true},
		{"archive.tar.gz", true},
		{"archive.tgz", true},
		{"archive.tar.bz2", true},
		{"archive.tbz", true},
		{"archive.tbz2", true},
		{"archive.tar.xz", true},
		{"archive.txz", true},
		{"archive.tar.zst", true},
		{"archive.tzst", true},
		{"archive.tar.lz4", true},
		{"ARCHIVE.TAR.GZ", true}, // Case insensitive
		{"file.txt", false},
		{"tarfile", false},
		{"file.tar.txt", false},
		{"file.zip", false},
	}

	for _, tt := range tests {
		t.Run(tt.src, func(t *testing.T) {
			got := isTarArchiveDL3020(tt.src)
			if got != tt.want {
				t.Errorf("isTarArchiveDL3020(%q) = %v, want %v", tt.src, got, tt.want)
			}
		})
	}
}
