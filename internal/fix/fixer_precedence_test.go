package fix

import (
	"bytes"
	"context"
	"testing"

	"github.com/wharflab/tally/internal/rules"
	_ "github.com/wharflab/tally/internal/rules/all"
)

func TestFixer_Apply_ConflictPrefersPerformanceOverStyle(t *testing.T) {
	t.Parallel()

	styleRule := rules.DefaultRegistry().Get("tally/consistent-indentation")
	if styleRule == nil {
		t.Fatal("expected tally/consistent-indentation to be registered")
	}
	perfRule := rules.DefaultRegistry().Get("tally/prefer-package-cache-mounts")
	if perfRule == nil {
		t.Fatal("expected tally/prefer-package-cache-mounts to be registered")
	}

	original := "RUN apt-get update && apt-get install -y curl"
	sources := map[string][]byte{
		"Dockerfile": []byte(original),
	}

	perfRewrite := "RUN --mount=type=cache,target=/var/cache/apt apt-get update && apt-get install -y curl"
	styleRewrite := "RUN apt-get update \\\n\t&& apt-get install -y curl"

	violations := []rules.Violation{
		{
			Location: rules.NewLineLocation("Dockerfile", 1),
			RuleCode: "tally/consistent-indentation",
			Message:  "style rewrite",
			SuggestedFix: &rules.SuggestedFix{
				Description: "Style rewrite",
				Safety:      rules.FixSafe,
				Priority:    styleRule.Metadata().FixPriority,
				Edits: []rules.TextEdit{
					{
						Location: rules.NewRangeLocation("Dockerfile", 1, 0, 1, len(original)),
						NewText:  styleRewrite,
					},
				},
			},
		},
		{
			Location: rules.NewLineLocation("Dockerfile", 1),
			RuleCode: "tally/prefer-package-cache-mounts",
			Message:  "performance rewrite",
			SuggestedFix: &rules.SuggestedFix{
				Description: "Performance rewrite",
				Safety:      rules.FixSafe,
				Priority:    perfRule.Metadata().FixPriority,
				Edits: []rules.TextEdit{
					{
						Location: rules.NewRangeLocation("Dockerfile", 1, 0, 1, len(original)),
						NewText:  perfRewrite,
					},
				},
			},
		},
	}

	fixer := &Fixer{SafetyThreshold: FixSafe}
	result, err := fixer.Apply(context.Background(), violations, sources)
	if err != nil {
		t.Fatalf("Apply error: %v", err)
	}

	if result.TotalApplied() != 1 {
		t.Errorf("TotalApplied() = %d, want 1", result.TotalApplied())
	}
	if result.TotalSkipped() != 1 {
		t.Errorf("TotalSkipped() = %d, want 1", result.TotalSkipped())
	}

	fc := result.Changes["Dockerfile"]
	if fc == nil {
		t.Fatal("FileChange for Dockerfile is nil")
	}

	if !bytes.Equal(fc.ModifiedContent, []byte(perfRewrite)) {
		t.Errorf("ModifiedContent =\n%q\nwant:\n%q", fc.ModifiedContent, perfRewrite)
	}

	if len(fc.FixesApplied) != 1 || fc.FixesApplied[0].RuleCode != "tally/prefer-package-cache-mounts" {
		t.Fatalf("FixesApplied = %#v, want only prefer-package-cache-mounts", fc.FixesApplied)
	}

	foundStyleConflict := false
	for _, skip := range fc.FixesSkipped {
		if skip.RuleCode == "tally/consistent-indentation" && skip.Reason == SkipConflict {
			foundStyleConflict = true
			break
		}
	}
	if !foundStyleConflict {
		t.Fatalf("expected tally/consistent-indentation to be skipped with SkipConflict, got %#v", fc.FixesSkipped)
	}
}
