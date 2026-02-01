package fix

import (
	"bytes"
	"context"
	"testing"

	"github.com/tinovyatkin/tally/internal/rules"
)

func TestApplyEdit_SingleLine(t *testing.T) {
	content := []byte("FROM alpine\nRUN apt install curl")

	// Replace "apt" with "apt-get" on line 2 (1-based), columns 4-7
	edit := rules.TextEdit{
		Location: rules.NewRangeLocation("Dockerfile", 2, 4, 2, 7),
		NewText:  "apt-get",
	}

	result := applyEdit(content, edit)
	expected := []byte("FROM alpine\nRUN apt-get install curl")

	if !bytes.Equal(result, expected) {
		t.Errorf("applyEdit() =\n%q\nwant:\n%q", result, expected)
	}
}

func TestApplyEdit_MultiLine(t *testing.T) {
	content := []byte("FROM alpine\nRUN apt install \\\n    curl")

	// Replace entire RUN command (lines 2-3, 1-based)
	edit := rules.TextEdit{
		Location: rules.NewRangeLocation("Dockerfile", 2, 0, 3, 8),
		NewText:  "RUN apt-get install curl",
	}

	result := applyEdit(content, edit)
	expected := []byte("FROM alpine\nRUN apt-get install curl")

	if !bytes.Equal(result, expected) {
		t.Errorf("applyEdit() =\n%q\nwant:\n%q", result, expected)
	}
}

func TestFixer_Apply_SingleFix(t *testing.T) {
	sources := map[string][]byte{
		"Dockerfile": []byte("FROM alpine\nRUN apt install curl"),
	}

	violations := []rules.Violation{
		{
			Location: rules.NewLineLocation("Dockerfile", 2), // 1-based line numbers
			RuleCode: "hadolint/DL3027",
			Message:  "Use apt-get",
			SuggestedFix: &rules.SuggestedFix{
				Description: "Replace apt with apt-get",
				Safety:      rules.FixSafe,
				Edits: []rules.TextEdit{
					{
						Location: rules.NewRangeLocation("Dockerfile", 2, 4, 2, 7), // 1-based
						NewText:  "apt-get",
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

	fc := result.Changes["Dockerfile"]
	if fc == nil {
		t.Fatal("FileChange for Dockerfile is nil")
	}

	expected := []byte("FROM alpine\nRUN apt-get install curl")
	if !bytes.Equal(fc.ModifiedContent, expected) {
		t.Errorf("ModifiedContent =\n%q\nwant:\n%q", fc.ModifiedContent, expected)
	}
}

func TestFixer_Apply_SafetyFilter(t *testing.T) {
	sources := map[string][]byte{
		"Dockerfile": []byte("RUN apt search foo"),
	}

	violations := []rules.Violation{
		{
			Location: rules.NewLineLocation("Dockerfile", 1), // 1-based line numbers
			RuleCode: "hadolint/DL3027",
			Message:  "Use apt-cache",
			SuggestedFix: &rules.SuggestedFix{
				Description: "Replace apt with apt-cache",
				Safety:      rules.FixSuggestion, // Not safe
				Edits: []rules.TextEdit{
					{
						Location: rules.NewRangeLocation("Dockerfile", 1, 4, 1, 7), // 1-based
						NewText:  "apt-cache",
					},
				},
			},
		},
	}

	// Only allow safe fixes
	fixer := &Fixer{SafetyThreshold: FixSafe}
	result, err := fixer.Apply(context.Background(), violations, sources)
	if err != nil {
		t.Fatalf("Apply error: %v", err)
	}

	if result.TotalApplied() != 0 {
		t.Errorf("TotalApplied() = %d, want 0", result.TotalApplied())
	}
	if result.TotalSkipped() != 1 {
		t.Errorf("TotalSkipped() = %d, want 1", result.TotalSkipped())
	}

	fc := result.Changes["Dockerfile"]
	if len(fc.FixesSkipped) != 1 {
		t.Fatalf("len(FixesSkipped) = %d, want 1", len(fc.FixesSkipped))
	}
	if fc.FixesSkipped[0].Reason != SkipSafety {
		t.Errorf("SkipReason = %v, want SkipSafety", fc.FixesSkipped[0].Reason)
	}
}

func TestFixer_Apply_RuleFilter(t *testing.T) {
	sources := map[string][]byte{
		"Dockerfile": []byte("RUN apt install curl"),
	}

	violations := []rules.Violation{
		{
			Location: rules.NewLineLocation("Dockerfile", 1), // 1-based line numbers
			RuleCode: "hadolint/DL3027",
			Message:  "Use apt-get",
			SuggestedFix: &rules.SuggestedFix{
				Description: "Replace apt with apt-get",
				Safety:      rules.FixSafe,
				Edits: []rules.TextEdit{
					{
						Location: rules.NewRangeLocation("Dockerfile", 1, 4, 1, 7), // 1-based
						NewText:  "apt-get",
					},
				},
			},
		},
	}

	// Filter to a different rule
	fixer := &Fixer{
		SafetyThreshold: FixSafe,
		RuleFilter:      []string{"hadolint/DL3004"},
	}
	result, err := fixer.Apply(context.Background(), violations, sources)
	if err != nil {
		t.Fatalf("Apply error: %v", err)
	}

	if result.TotalApplied() != 0 {
		t.Errorf("TotalApplied() = %d, want 0", result.TotalApplied())
	}

	fc := result.Changes["Dockerfile"]
	if len(fc.FixesSkipped) != 1 {
		t.Fatalf("len(FixesSkipped) = %d, want 1", len(fc.FixesSkipped))
	}
	if fc.FixesSkipped[0].Reason != SkipRuleFilter {
		t.Errorf("SkipReason = %v, want SkipRuleFilter", fc.FixesSkipped[0].Reason)
	}
}

func TestFixer_Apply_ConflictingFixes(t *testing.T) {
	sources := map[string][]byte{
		"Dockerfile": []byte("RUN apt install curl"),
	}

	// Two fixes that overlap
	violations := []rules.Violation{
		{
			Location: rules.NewLineLocation("Dockerfile", 1), // 1-based line numbers
			RuleCode: "rule1",
			Message:  "Fix 1",
			SuggestedFix: &rules.SuggestedFix{
				Description: "Fix 1",
				Safety:      rules.FixSafe,
				Edits: []rules.TextEdit{
					{
						Location: rules.NewRangeLocation("Dockerfile", 1, 4, 1, 15), // 1-based
						NewText:  "apt-get install",
					},
				},
			},
		},
		{
			Location: rules.NewLineLocation("Dockerfile", 1), // 1-based line numbers
			RuleCode: "rule2",
			Message:  "Fix 2",
			SuggestedFix: &rules.SuggestedFix{
				Description: "Fix 2",
				Safety:      rules.FixSafe,
				Edits: []rules.TextEdit{
					{
						// Overlaps with fix 1
						Location: rules.NewRangeLocation("Dockerfile", 1, 4, 1, 7), // 1-based
						NewText:  "apt-get",
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

	// One should be applied, one skipped
	if result.TotalApplied() != 1 {
		t.Errorf("TotalApplied() = %d, want 1", result.TotalApplied())
	}
	if result.TotalSkipped() != 1 {
		t.Errorf("TotalSkipped() = %d, want 1", result.TotalSkipped())
	}

	fc := result.Changes["Dockerfile"]
	foundConflict := false
	for _, skip := range fc.FixesSkipped {
		if skip.Reason == SkipConflict {
			foundConflict = true
			break
		}
	}
	if !foundConflict {
		t.Error("Expected SkipConflict reason")
	}
}

func TestFixer_Apply_MultipleFixes(t *testing.T) {
	sources := map[string][]byte{
		"Dockerfile": []byte("FROM alpine\nRUN apt install curl\nRUN apt update"),
	}

	violations := []rules.Violation{
		{
			Location: rules.NewLineLocation("Dockerfile", 2), // 1-based: line 2 is "RUN apt install curl"
			RuleCode: "hadolint/DL3027",
			Message:  "Use apt-get",
			SuggestedFix: &rules.SuggestedFix{
				Description: "Replace apt with apt-get",
				Safety:      rules.FixSafe,
				Edits: []rules.TextEdit{
					{
						Location: rules.NewRangeLocation("Dockerfile", 2, 4, 2, 7), // 1-based
						NewText:  "apt-get",
					},
				},
			},
		},
		{
			Location: rules.NewLineLocation("Dockerfile", 3), // 1-based: line 3 is "RUN apt update"
			RuleCode: "hadolint/DL3027",
			Message:  "Use apt-get",
			SuggestedFix: &rules.SuggestedFix{
				Description: "Replace apt with apt-get",
				Safety:      rules.FixSafe,
				Edits: []rules.TextEdit{
					{
						Location: rules.NewRangeLocation("Dockerfile", 3, 4, 3, 7), // 1-based
						NewText:  "apt-get",
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

	if result.TotalApplied() != 2 {
		t.Errorf("TotalApplied() = %d, want 2", result.TotalApplied())
	}

	fc := result.Changes["Dockerfile"]
	expected := []byte("FROM alpine\nRUN apt-get install curl\nRUN apt-get update")
	if !bytes.Equal(fc.ModifiedContent, expected) {
		t.Errorf("ModifiedContent =\n%q\nwant:\n%q", fc.ModifiedContent, expected)
	}
}

func TestResult_Methods(t *testing.T) {
	result := &Result{
		Changes: map[string]*FileChange{
			"a.txt": {
				Path:            "a.txt",
				OriginalContent: []byte("old"),
				ModifiedContent: []byte("new"),
				FixesApplied:    []AppliedFix{{RuleCode: "r1"}},
				FixesSkipped:    []SkippedFix{{RuleCode: "r2", Reason: SkipSafety}},
			},
			"b.txt": {
				Path:            "b.txt",
				OriginalContent: []byte("same"),
				ModifiedContent: []byte("same"),
			},
		},
	}

	if result.TotalApplied() != 1 {
		t.Errorf("TotalApplied() = %d, want 1", result.TotalApplied())
	}
	if result.TotalSkipped() != 1 {
		t.Errorf("TotalSkipped() = %d, want 1", result.TotalSkipped())
	}
	if result.FilesModified() != 1 {
		t.Errorf("FilesModified() = %d, want 1", result.FilesModified())
	}
}
