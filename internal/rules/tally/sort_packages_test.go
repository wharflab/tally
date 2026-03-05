package tally

import (
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"
	fixpkg "github.com/wharflab/tally/internal/fix"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/testutil"
)

func TestSortPackagesMetadata(t *testing.T) {
	t.Parallel()
	snaps.MatchStandaloneJSON(t, NewSortPackagesRule().Metadata())
}

func TestSortPackagesCheck(t *testing.T) {
	t.Parallel()

	testutil.RunRuleTests(t, NewSortPackagesRule(), []testutil.RuleTestCase{
		// === Clean cases ===
		{
			Name:           "already sorted - apt-get",
			Content:        "FROM alpine:3.20\nRUN apt-get install -y curl git wget\n",
			WantViolations: 0,
		},
		{
			Name:           "single package - no violation",
			Content:        "FROM alpine:3.20\nRUN apt-get install -y curl\n",
			WantViolations: 0,
		},
		{
			Name:           "no install command",
			Content:        "FROM alpine:3.20\nRUN echo hello\n",
			WantViolations: 0,
		},
		{
			Name:           "exec-form RUN skipped",
			Content:        "FROM alpine:3.20\nRUN [\"apt-get\", \"install\", \"wget\", \"curl\"]\n",
			WantViolations: 0,
		},
		{
			Name:           "pip install -r requirements skipped",
			Content:        "FROM python:3.12\nRUN pip install -r requirements.txt\n",
			WantViolations: 0,
		},
		{
			Name:           "pip install -e . skipped",
			Content:        "FROM python:3.12\nRUN pip install -e .\n",
			WantViolations: 0,
		},
		{
			Name:           "all variables - no violation",
			Content:        "FROM alpine:3.20\nRUN npm install $PKG1 ${PKG2}\n",
			WantViolations: 0,
		},
		{
			Name:           "empty file",
			Content:        "FROM scratch\n",
			WantViolations: 0,
		},
		{
			Name:           "apt-get update only",
			Content:        "FROM alpine:3.20\nRUN apt-get update\n",
			WantViolations: 0,
		},
		{
			Name:           "heredoc RUN sorted",
			Content:        "FROM alpine:3.20\nRUN <<EOF\napt-get install -y zoo foo\nEOF\n",
			WantViolations: 1,
			WantMessages:   []string{"packages in apt-get install are not sorted"},
		},
		{
			Name:           "heredoc RUN already sorted",
			Content:        "FROM alpine:3.20\nRUN <<EOF\napt-get install -y curl git wget\nEOF\n",
			WantViolations: 0,
		},

		// === Violation cases ===
		{
			Name:           "unsorted apt-get",
			Content:        "FROM alpine:3.20\nRUN apt-get install -y zoo foo bar\n",
			WantViolations: 1,
			WantMessages:   []string{"packages in apt-get install are not sorted"},
		},
		{
			Name:           "unsorted apk",
			Content:        "FROM alpine:3.20\nRUN apk add --no-cache wget curl\n",
			WantViolations: 1,
			WantMessages:   []string{"packages in apk add are not sorted"},
		},
		{
			Name:           "unsorted npm",
			Content:        "FROM node:20\nRUN npm install express axios\n",
			WantViolations: 1,
			WantMessages:   []string{"packages in npm install are not sorted"},
		},
		{
			Name:           "unsorted pip with versions",
			Content:        "FROM python:3.12\nRUN pip install flask==2.0 django==4.0\n",
			WantViolations: 1,
			WantMessages:   []string{"packages in pip install are not sorted"},
		},
		{
			Name:           "unsorted dnf",
			Content:        "FROM fedora:39\nRUN dnf install -y wget curl\n",
			WantViolations: 1,
			WantMessages:   []string{"packages in dnf install are not sorted"},
		},
		{
			Name:           "unsorted yarn",
			Content:        "FROM node:20\nRUN yarn add react axios\n",
			WantViolations: 1,
			WantMessages:   []string{"packages in yarn add are not sorted"},
		},
		{
			Name:           "multi-line unsorted",
			Content:        "FROM alpine:3.20\nRUN apt-get install -y \\\n    zoo \\\n    foo \\\n    bar\n",
			WantViolations: 1,
			WantMessages:   []string{"packages in apt-get install are not sorted"},
		},
		{
			Name:           "chained command - only install part checked",
			Content:        "FROM alpine:3.20\nRUN apt-get update && apt-get install -y zoo foo\n",
			WantViolations: 1,
			WantMessages:   []string{"packages in apt-get install are not sorted"},
		},
		{
			Name:           "multiple install commands in one RUN",
			Content:        "FROM alpine:3.20\nRUN apt-get install -y zoo foo && pip install flask django\n",
			WantViolations: 2,
		},
	})
}

func TestSortPackagesFix(t *testing.T) {
	t.Parallel()

	r := NewSortPackagesRule()

	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "simple swap",
			content: "FROM alpine:3.20\nRUN apt-get install -y wget curl\n",
			want:    "FROM alpine:3.20\nRUN apt-get install -y curl wget\n",
		},
		{
			name:    "three packages reverse order",
			content: "FROM alpine:3.20\nRUN apt-get install -y zoo foo bar\n",
			want:    "FROM alpine:3.20\nRUN apt-get install -y bar foo zoo\n",
		},
		{
			name:    "multi-line",
			content: "FROM alpine:3.20\nRUN apt-get install -y \\\n    zoo \\\n    foo\n",
			want:    "FROM alpine:3.20\nRUN apt-get install -y \\\n    foo \\\n    zoo\n",
		},
		{
			name:    "mixed literals and variables - vars at tail",
			content: "FROM alpine:3.20\nRUN npm install zoo foo $NPM_PKG ${OTHER}\n",
			want:    "FROM alpine:3.20\nRUN npm install foo zoo $NPM_PKG ${OTHER}\n",
		},
		{
			name:    "interleaved vars - literals sorted, vars at tail",
			content: "FROM python:3.12\nRUN uv pip install $CDK_DEPS otel aws-otel $RUNTIME_DEPS polars==1.2.3\n",
			want:    "FROM python:3.12\nRUN uv pip install aws-otel otel polars==1.2.3 $CDK_DEPS $RUNTIME_DEPS\n",
		},
		{
			name: "multi-line mixed - literals sorted, vars stay in place",
			content: "FROM python:3.12\nRUN pip install \\\n" +
				"  foo zoo \\\n" +
				"  boo abbr $TADA oops \\\n" +
				"  $END \\\n" +
				"  almost there\n",
			want: "FROM python:3.12\nRUN pip install \\\n" +
				"  abbr almost \\\n" +
				"  boo foo $TADA oops \\\n" +
				"  $END \\\n" +
				"  there zoo\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			input := testutil.MakeLintInputWithConfig(t, "Dockerfile", tt.content, nil)
			violations := r.Check(input)

			if len(violations) != 1 {
				t.Fatalf("got %d violations, want 1", len(violations))
			}

			fix := violations[0].SuggestedFix
			if fix == nil {
				t.Fatal("violation has no SuggestedFix")
			}
			if fix.Safety != rules.FixSafe {
				t.Errorf("fix safety = %v, want FixSafe", fix.Safety)
			}

			// Apply edits back-to-front using the production fix engine.
			got := []byte(tt.content)
			for i := len(fix.Edits) - 1; i >= 0; i-- {
				got = fixpkg.ApplyEdit(got, fix.Edits[i])
			}
			if string(got) != tt.want {
				t.Errorf("after fix:\ngot:  %q\nwant: %q", got, tt.want)
			}
		})
	}
}

func TestSortKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  string
	}{
		{"curl", "curl"},
		{"flask==2.0", "flask"},
		{"curl=7.88.1-10+deb12u5", "curl"},
		{"@eslint/js", "@eslint/js"},
		{"@eslint/js@8.0.0", "@eslint/js"},
		{"react@18.2.0", "react"},
		{"lodash@4", "lodash"},
		{"CamelCase", "camelcase"},
		{"Zlib", "zlib"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			got := sortKey(tt.input)
			if got != tt.want {
				t.Errorf("sortKey(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
