package buildkit

import (
	"strings"
	"testing"

	"github.com/wharflab/tally/internal/dockerfile"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/rules/buildkit/fixes"
)

func TestJSONArgsRecommendedRule_Check_AndFix(t *testing.T) {
	t.Parallel()
	df := `FROM alpine
CMD echo hello # trailing comment
ENTRYPOINT echo 'hello world'
CMD ["echo", "already-exec"]
`

	pr, err := dockerfile.Parse(strings.NewReader(df), nil)
	if err != nil {
		t.Fatalf("parse dockerfile: %v", err)
	}

	input := rules.LintInput{
		File:     "Dockerfile",
		Stages:   pr.Stages,
		MetaArgs: pr.MetaArgs,
		Source:   pr.Source,
	}

	r := NewJSONArgsRecommendedRule()
	violations := r.Check(input)
	if len(violations) != 2 {
		t.Fatalf("expected 2 violations, got %d", len(violations))
	}

	fixes.EnrichBuildKitFixes(violations, nil, pr.Source)
	for i, v := range violations {
		if v.RuleCode != "buildkit/JSONArgsRecommended" {
			t.Fatalf("violations[%d].RuleCode = %q, want %q", i, v.RuleCode, "buildkit/JSONArgsRecommended")
		}
		if v.SuggestedFix == nil {
			t.Fatalf("violations[%d] expected SuggestedFix, got nil", i)
		}
		if v.SuggestedFix.Safety != rules.FixSuggestion {
			t.Fatalf("violations[%d] fix safety = %v, want %v", i, v.SuggestedFix.Safety, rules.FixSuggestion)
		}
		if len(v.SuggestedFix.Edits) != 1 {
			t.Fatalf("violations[%d] fix edits = %d, want 1", i, len(v.SuggestedFix.Edits))
		}
	}

	if got := violations[0].SuggestedFix.Edits[0].NewText; got != "[\"echo\",\"hello\"] " {
		t.Fatalf("CMD fix NewText = %q, want %q", got, "[\"echo\",\"hello\"] ")
	}
	if got := violations[1].SuggestedFix.Edits[0].NewText; got != "[\"echo\",\"hello world\"]" {
		t.Fatalf("ENTRYPOINT fix NewText = %q, want %q", got, "[\"echo\",\"hello world\"]")
	}
}
