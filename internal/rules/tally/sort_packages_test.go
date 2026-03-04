package tally

import (
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"
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
			Name:           "heredoc RUN skipped",
			Content:        "FROM alpine:3.20\nRUN <<EOF\napt-get install -y zoo foo\nEOF\n",
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

func TestSortPackagesMixedLiteralsAndVariables(t *testing.T) {
	t.Parallel()

	r := NewSortPackagesRule()
	input := testutil.MakeLintInputWithConfig(t, "Dockerfile",
		"FROM alpine:3.20\nRUN npm install zoo foo $NPM_PKG ${OTHER}\n", nil)
	violations := r.Check(input)

	if len(violations) != 1 {
		t.Fatalf("got %d violations, want 1", len(violations))
	}

	// Should sort literals only: foo, zoo (variables stay at tail in original order)
	fix := violations[0].SuggestedFix
	if fix == nil {
		t.Fatal("violation has no SuggestedFix")
	}

	// There should be 2 edits (swapping zoo and foo)
	if len(fix.Edits) != 2 {
		t.Fatalf("got %d edits, want 2", len(fix.Edits))
	}
}

func TestSortPackagesFixEdits(t *testing.T) {
	t.Parallel()

	r := NewSortPackagesRule()

	tests := []struct {
		name         string
		content      string
		wantEdits    int
		wantNewTexts []string
	}{
		{
			name:         "simple swap - two packages",
			content:      "FROM alpine:3.20\nRUN apt-get install -y wget curl\n",
			wantEdits:    2,
			wantNewTexts: []string{"curl", "wget"},
		},
		{
			name:         "three packages reverse order",
			content:      "FROM alpine:3.20\nRUN apt-get install -y zoo foo bar\n",
			wantEdits:    2, // bar and zoo swap, foo stays
			wantNewTexts: []string{"bar", "zoo"},
		},
		{
			name:         "multi-line swap",
			content:      "FROM alpine:3.20\nRUN apt-get install -y \\\n    zoo \\\n    foo\n",
			wantEdits:    2,
			wantNewTexts: []string{"foo", "zoo"},
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

			if fix.Priority != 15 {
				t.Errorf("fix priority = %d, want 15", fix.Priority)
			}

			if len(fix.Edits) != tt.wantEdits {
				t.Fatalf("got %d edits, want %d", len(fix.Edits), tt.wantEdits)
			}

			for i, edit := range fix.Edits {
				if i < len(tt.wantNewTexts) && edit.NewText != tt.wantNewTexts[i] {
					t.Errorf("edit[%d].NewText = %q, want %q", i, edit.NewText, tt.wantNewTexts[i])
				}
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
