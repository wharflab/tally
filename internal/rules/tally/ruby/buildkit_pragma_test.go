package ruby

import (
	"testing"

	"github.com/wharflab/tally/internal/rules"
)

func TestHasBuildKitSyntaxPragma(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		src  string
		want bool
	}{
		{
			name: "canonical syntax pragma",
			src:  "# syntax=docker/dockerfile:1\nFROM ruby:3.3\n",
			want: true,
		},
		{
			name: "labs frontend",
			src:  "# syntax=docker/dockerfile:labs\nFROM ruby:3.3\n",
			want: true,
		},
		{
			name: "spaces around equals are tolerated",
			src:  "# syntax = docker/dockerfile:1\nFROM ruby:3.3\n",
			want: true,
		},
		{
			name: "absent",
			src:  "FROM ruby:3.3\n",
			want: false,
		},
		{
			name: "non-buildkit syntax (e.g. legacy frontend)",
			src:  "# syntax=docker/notbuildkit:1\nFROM ruby:3.3\n",
			want: false,
		},
		{
			name: "comment containing 'syntax=' word but no directive",
			src:  "# Note: we don't use syntax=... here\nFROM ruby:3.3\n",
			want: false,
		},
		{
			name: "bare # comment terminates directive block",
			src:  "#\n# syntax=docker/dockerfile:1\nFROM ruby:3.3\n",
			want: false,
		},
		{
			name: "directive block ends at first non-comment line",
			src:  "FROM ruby:3.3\n# syntax=docker/dockerfile:1\n",
			want: false,
		},
		{
			name: "syntax pragma with leading whitespace",
			src:  "  # syntax=docker/dockerfile:1\nFROM ruby:3.3\n",
			want: true,
		},
		{
			name: "preceded by a different directive",
			src:  "# escape=`\n# syntax=docker/dockerfile:1\nFROM ruby:3.3\n",
			want: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			input := rules.LintInput{Source: []byte(tc.src)}
			if got := hasBuildKitSyntaxPragma(input); got != tc.want {
				t.Errorf("hasBuildKitSyntaxPragma(%q) = %v, want %v", tc.src, got, tc.want)
			}
		})
	}
}
