package hadolint

import (
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/testutil"
)

func TestDL3047Rule_Metadata(t *testing.T) {
	t.Parallel()
	snaps.MatchStandaloneJSON(t, NewDL3047Rule().Metadata())
}

func TestDL3047Rule_Check(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		dockerfile string
		wantCount  int
	}{
		// Original Hadolint test cases - ruleCatches (should trigger)
		{
			name: "wget without --progress option",
			dockerfile: `FROM node as foo
RUN wget my.xyz`,
			wantCount: 1,
		},

		// Original Hadolint test cases - ruleCatchesNot (should NOT trigger)
		{
			name: "wget with --progress option",
			dockerfile: `FROM node as foo
RUN wget --progress=dot:giga my.xyz`,
			wantCount: 0,
		},
		{
			name: "wget with -q (quiet short)",
			dockerfile: `FROM node as foo
RUN wget -q my.xyz`,
			wantCount: 0,
		},
		{
			name: "wget with --quiet (quiet long)",
			dockerfile: `FROM node as foo
RUN wget --quiet my.xyz`,
			wantCount: 0,
		},
		{
			name: "wget with -nv (no-verbose short)",
			dockerfile: `FROM node as foo
RUN wget -nv my.xyz`,
			wantCount: 0,
		},
		{
			name: "wget with --no-verbose (no-verbose long)",
			dockerfile: `FROM node as foo
RUN wget --no-verbose my.xyz`,
			wantCount: 0,
		},
		{
			name: "wget with --output-file long option",
			dockerfile: `FROM node as foo
RUN wget --output-file=/tmp/wget.log my.xyz`,
			wantCount: 0,
		},
		{
			name: "wget with -o short option",
			dockerfile: `FROM node as foo
RUN wget -o /tmp/wget.log my.xyz`,
			wantCount: 0,
		},
		{
			name: "wget with --append-output long option",
			dockerfile: `FROM node as foo
RUN wget --append-output=/tmp/wget.log my.xyz`,
			wantCount: 0,
		},
		{
			name: "wget with -a short option",
			dockerfile: `FROM node as foo
RUN wget -a /tmp/wget.log my.xyz`,
			wantCount: 0,
		},

		// Additional edge cases
		{
			name: "no wget command",
			dockerfile: `FROM ubuntu
RUN apt-get update`,
			wantCount: 0,
		},
		// The following pipeline/chain cases also overlap with tally/prefer-add-unpack
		// (which suggests replacing wget|tar with ADD) and hadolint/DL4001 (which
		// warns when both wget and curl are present). DL3047 fires independently.
		{
			name: "wget in pipeline",
			dockerfile: `FROM ubuntu
RUN wget http://example.com/file.tar.gz | tar xz`,
			wantCount: 1,
		},
		{
			name: "wget in pipeline with progress",
			dockerfile: `FROM ubuntu
RUN wget --progress=dot:giga http://example.com/file.tar.gz | tar xz`,
			wantCount: 0,
		},
		{
			name: "wget with && chain",
			dockerfile: `FROM ubuntu
RUN wget http://example.com/file.tar.gz && tar xf file.tar.gz`,
			wantCount: 1,
		},
		{
			name: "multiple wget commands without progress",
			dockerfile: `FROM ubuntu
RUN wget http://a.com/a.tar.gz && wget http://b.com/b.tar.gz`,
			wantCount: 2,
		},
		{
			name: "multiple wget one with progress one without",
			dockerfile: `FROM ubuntu
RUN wget --progress=dot:giga http://a.com/a.tar.gz && wget http://b.com/b.tar.gz`,
			wantCount: 1,
		},
		// Exec-form tests: upstream Hadolint skips exec-form (no shell AST),
		// but tally intentionally extends coverage since the bloated-log problem
		// applies equally to RUN ["wget", ...].
		{
			name: "exec form wget without progress (tally extension)",
			dockerfile: `FROM ubuntu
RUN ["wget", "http://example.com/file.tar.gz"]`,
			wantCount: 1,
		},
		{
			name: "exec form wget with progress",
			dockerfile: `FROM ubuntu
RUN ["wget", "--progress=dot:giga", "http://example.com/file.tar.gz"]`,
			wantCount: 0,
		},
		{
			name: "multi-stage with wget",
			dockerfile: `FROM ubuntu AS builder
RUN wget http://example.com/file.tar.gz

FROM alpine
RUN wget -q http://example.com/small.txt`,
			wantCount: 1,
		},
		{
			name: "multiline RUN with wget",
			dockerfile: `FROM ubuntu
RUN apt-get update && \
    wget http://example.com/file.tar.gz`,
			wantCount: 1,
		},
		{
			name: "wget via env wrapper",
			dockerfile: `FROM ubuntu
RUN env wget http://example.com/file.tar.gz`,
			wantCount: 1,
		},
		{
			name: "wget as argument not command",
			dockerfile: `FROM ubuntu
RUN echo wget`,
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			input := testutil.MakeLintInput(t, "Dockerfile", tt.dockerfile)

			r := NewDL3047Rule()
			violations := r.Check(input)

			if len(violations) != tt.wantCount {
				t.Errorf("got %d violations, want %d", len(violations), tt.wantCount)
				for i, v := range violations {
					t.Logf("violation %d: %s at %v", i+1, v.Message, v.Location)
				}
			}

			// Verify violation details for positive cases
			if tt.wantCount > 0 && len(violations) > 0 {
				v := violations[0]
				if v.RuleCode != rules.HadolintRulePrefix+"DL3047" {
					t.Errorf("got rule code %q, want %q", v.RuleCode, rules.HadolintRulePrefix+"DL3047")
				}
				if v.Message == "" {
					t.Error("violation message is empty")
				}
				if v.Detail == "" {
					t.Error("violation detail is empty")
				}
				if v.DocURL != "https://github.com/hadolint/hadolint/wiki/DL3047" {
					t.Errorf("got doc URL %q, want %q", v.DocURL, "https://github.com/hadolint/hadolint/wiki/DL3047")
				}
			}
		})
	}
}

// TestDL3047_AutoFix verifies that DL3047 provides auto-fix suggestions.
func TestDL3047_AutoFix(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		dockerfile    string
		wantFix       bool
		wantInsertCol int
		wantNewText   string
	}{
		{
			name: "simple wget",
			dockerfile: `FROM ubuntu
RUN wget http://example.com/file.tar.gz`,
			wantFix:       true,
			wantInsertCol: 8, // After "wget" (RUN + space = 4 cols, wget = 4 chars)
			wantNewText:   " --progress=dot:giga",
		},
		{
			name: "exec form wget (detected but no auto-fix)",
			dockerfile: `FROM ubuntu
RUN ["wget", "http://example.com/file.tar.gz"]`,
			wantFix: false, // Exec form: positions don't map to source
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			input := testutil.MakeLintInput(t, "Dockerfile", tt.dockerfile)
			r := NewDL3047Rule()
			violations := r.Check(input)

			if len(violations) == 0 {
				t.Fatal("expected at least one violation")
			}

			v := violations[0]
			if tt.wantFix {
				if v.SuggestedFix == nil {
					t.Fatal("expected SuggestedFix")
				}
				if len(v.SuggestedFix.Edits) == 0 {
					t.Fatal("expected at least one edit")
				}
				edit := v.SuggestedFix.Edits[0]
				if edit.Location.Start.Column != tt.wantInsertCol {
					t.Errorf("insert column = %d, want %d", edit.Location.Start.Column, tt.wantInsertCol)
				}
				if edit.NewText != tt.wantNewText {
					t.Errorf("NewText = %q, want %q", edit.NewText, tt.wantNewText)
				}
				if v.SuggestedFix.Safety != rules.FixSafe {
					t.Errorf("Safety = %v, want FixSafe", v.SuggestedFix.Safety)
				}
			} else if v.SuggestedFix != nil {
				t.Error("expected no SuggestedFix for exec form")
			}
		})
	}
}
