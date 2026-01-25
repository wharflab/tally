package lint

import (
	"testing"

	"github.com/tinovyatkin/tally/internal/config"
	"github.com/tinovyatkin/tally/internal/dockerfile"
)

func TestCheckMaxLines(t *testing.T) {
	tests := []struct {
		name      string
		result    *dockerfile.ParseResult
		rule      config.MaxLinesRule
		wantIssue bool
		wantMsg   string
	}{
		{
			name: "disabled when max is 0",
			result: &dockerfile.ParseResult{
				TotalLines: 1000,
			},
			rule:      config.MaxLinesRule{Max: 0},
			wantIssue: false,
		},
		{
			name: "under limit",
			result: &dockerfile.ParseResult{
				TotalLines: 50,
			},
			rule:      config.MaxLinesRule{Max: 100},
			wantIssue: false,
		},
		{
			name: "at limit",
			result: &dockerfile.ParseResult{
				TotalLines: 100,
			},
			rule:      config.MaxLinesRule{Max: 100},
			wantIssue: false,
		},
		{
			name: "over limit",
			result: &dockerfile.ParseResult{
				TotalLines: 150,
			},
			rule:      config.MaxLinesRule{Max: 100},
			wantIssue: true,
			wantMsg:   "file has 150 lines, maximum allowed is 100",
		},
		{
			name: "just over limit",
			result: &dockerfile.ParseResult{
				TotalLines: 101,
			},
			rule:      config.MaxLinesRule{Max: 100},
			wantIssue: true,
			wantMsg:   "file has 101 lines, maximum allowed is 100",
		},
		{
			name: "skip blank lines - passes when blanks excluded",
			result: &dockerfile.ParseResult{
				TotalLines: 120,
				BlankLines: 30,
			},
			rule:      config.MaxLinesRule{Max: 100, SkipBlankLines: true},
			wantIssue: false,
		},
		{
			name: "skip blank lines - fails when blanks excluded but still over",
			result: &dockerfile.ParseResult{
				TotalLines: 150,
				BlankLines: 30,
			},
			rule:      config.MaxLinesRule{Max: 100, SkipBlankLines: true},
			wantIssue: true,
			wantMsg:   "file has 120 lines (excluding 30 skipped), maximum allowed is 100",
		},
		{
			name: "skip comments - passes when comments excluded",
			result: &dockerfile.ParseResult{
				TotalLines:   110,
				CommentLines: 20,
			},
			rule:      config.MaxLinesRule{Max: 100, SkipComments: true},
			wantIssue: false,
		},
		{
			name: "skip comments - fails when comments excluded but still over",
			result: &dockerfile.ParseResult{
				TotalLines:   150,
				CommentLines: 20,
			},
			rule:      config.MaxLinesRule{Max: 100, SkipComments: true},
			wantIssue: true,
			wantMsg:   "file has 130 lines (excluding 20 skipped), maximum allowed is 100",
		},
		{
			name: "skip both blanks and comments",
			result: &dockerfile.ParseResult{
				TotalLines:   150,
				BlankLines:   30,
				CommentLines: 20,
			},
			rule:      config.MaxLinesRule{Max: 100, SkipBlankLines: true, SkipComments: true},
			wantIssue: false,
		},
		{
			name: "skip both - still over limit",
			result: &dockerfile.ParseResult{
				TotalLines:   200,
				BlankLines:   30,
				CommentLines: 20,
			},
			rule:      config.MaxLinesRule{Max: 100, SkipBlankLines: true, SkipComments: true},
			wantIssue: true,
			wantMsg:   "file has 150 lines (excluding 50 skipped), maximum allowed is 100",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			issue := CheckMaxLines(tt.result, tt.rule)

			if tt.wantIssue && issue == nil {
				t.Error("expected issue but got nil")
			}
			if !tt.wantIssue && issue != nil {
				t.Errorf("expected no issue but got: %v", issue)
			}
			if issue != nil {
				if issue.Rule != "max-lines" {
					t.Errorf("issue.Rule = %q, want %q", issue.Rule, "max-lines")
				}
				if issue.Severity != "error" {
					t.Errorf("issue.Severity = %q, want %q", issue.Severity, "error")
				}
				if tt.wantMsg != "" && issue.Message != tt.wantMsg {
					t.Errorf("issue.Message = %q, want %q", issue.Message, tt.wantMsg)
				}
			}
		})
	}
}

func TestMaxLinesRuleEnabled(t *testing.T) {
	tests := []struct {
		max  int
		want bool
	}{
		{0, false},
		{1, true},
		{100, true},
		{-1, false},
	}

	for _, tt := range tests {
		rule := config.MaxLinesRule{Max: tt.max}
		if got := rule.Enabled(); got != tt.want {
			t.Errorf("MaxLinesRule{Max: %d}.Enabled() = %v, want %v", tt.max, got, tt.want)
		}
	}
}
