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

func TestFixer_Apply_ViolationWithoutFix(t *testing.T) {
	sources := map[string][]byte{
		"Dockerfile": []byte("FROM alpine"),
	}

	// Violation without SuggestedFix should be ignored
	violations := []rules.Violation{
		{
			Location: rules.NewLineLocation("Dockerfile", 1),
			RuleCode: "some-rule",
			Message:  "Some message",
			// No SuggestedFix
		},
	}

	fixer := &Fixer{SafetyThreshold: FixSafe}
	result, err := fixer.Apply(context.Background(), violations, sources)
	if err != nil {
		t.Fatalf("Apply error: %v", err)
	}

	if result.TotalApplied() != 0 {
		t.Errorf("TotalApplied() = %d, want 0", result.TotalApplied())
	}
	if result.TotalSkipped() != 0 {
		t.Errorf("TotalSkipped() = %d, want 0", result.TotalSkipped())
	}
}

func TestFixer_Apply_FixWithNoEdits(t *testing.T) {
	sources := map[string][]byte{
		"Dockerfile": []byte("FROM alpine"),
	}

	violations := []rules.Violation{
		{
			Location: rules.NewLineLocation("Dockerfile", 1),
			RuleCode: "some-rule",
			Message:  "Some message",
			SuggestedFix: &rules.SuggestedFix{
				Description: "Empty fix",
				Safety:      rules.FixSafe,
				Edits:       []rules.TextEdit{}, // No edits
			},
		},
	}

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
	if len(fc.FixesSkipped) != 1 || fc.FixesSkipped[0].Reason != SkipNoEdits {
		t.Errorf("Expected SkipNoEdits reason")
	}
}

func TestFixer_Apply_FixModeNever(t *testing.T) {
	sources := map[string][]byte{
		"Dockerfile": []byte("RUN apt install curl"),
	}

	violations := []rules.Violation{
		{
			Location: rules.NewLineLocation("Dockerfile", 1),
			RuleCode: "hadolint/DL3027",
			Message:  "Use apt-get",
			SuggestedFix: &rules.SuggestedFix{
				Description: "Replace apt with apt-get",
				Safety:      rules.FixSafe,
				Edits: []rules.TextEdit{
					{
						Location: rules.NewRangeLocation("Dockerfile", 1, 4, 1, 7),
						NewText:  "apt-get",
					},
				},
			},
		},
	}

	fixer := &Fixer{
		SafetyThreshold: FixSafe,
		FixModes: map[string]map[string]FixMode{
			"Dockerfile": {
				"hadolint/DL3027": FixModeNever,
			},
		},
	}
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
	if len(fc.FixesSkipped) != 1 || fc.FixesSkipped[0].Reason != SkipFixMode {
		t.Errorf("Expected SkipFixMode reason")
	}
}

func TestFixer_Apply_FixModeExplicit(t *testing.T) {
	sources := map[string][]byte{
		"Dockerfile": []byte("RUN apt install curl"),
	}

	violations := []rules.Violation{
		{
			Location: rules.NewLineLocation("Dockerfile", 1),
			RuleCode: "hadolint/DL3027",
			Message:  "Use apt-get",
			SuggestedFix: &rules.SuggestedFix{
				Description: "Replace apt with apt-get",
				Safety:      rules.FixSafe,
				Edits: []rules.TextEdit{
					{
						Location: rules.NewRangeLocation("Dockerfile", 1, 4, 1, 7),
						NewText:  "apt-get",
					},
				},
			},
		},
	}

	// FixModeExplicit without RuleFilter should skip
	fixer := &Fixer{
		SafetyThreshold: FixSafe,
		FixModes: map[string]map[string]FixMode{
			"Dockerfile": {
				"hadolint/DL3027": FixModeExplicit,
			},
		},
	}
	result, err := fixer.Apply(context.Background(), violations, sources)
	if err != nil {
		t.Fatalf("Apply error: %v", err)
	}

	if result.TotalApplied() != 0 {
		t.Errorf("TotalApplied() = %d, want 0 (explicit mode without rule filter)", result.TotalApplied())
	}

	// FixModeExplicit with matching RuleFilter should apply
	fixer.RuleFilter = []string{"hadolint/DL3027"}
	result, err = fixer.Apply(context.Background(), violations, sources)
	if err != nil {
		t.Fatalf("Apply error: %v", err)
	}

	if result.TotalApplied() != 1 {
		t.Errorf("TotalApplied() = %d, want 1 (explicit mode with matching rule filter)", result.TotalApplied())
	}
}

func TestFixer_Apply_FixModeUnsafeOnly(t *testing.T) {
	sources := map[string][]byte{
		"Dockerfile": []byte("RUN apt install curl"),
	}

	violations := []rules.Violation{
		{
			Location: rules.NewLineLocation("Dockerfile", 1),
			RuleCode: "hadolint/DL3027",
			Message:  "Use apt-get",
			SuggestedFix: &rules.SuggestedFix{
				Description: "Replace apt with apt-get",
				Safety:      rules.FixSafe,
				Edits: []rules.TextEdit{
					{
						Location: rules.NewRangeLocation("Dockerfile", 1, 4, 1, 7),
						NewText:  "apt-get",
					},
				},
			},
		},
	}

	// FixModeUnsafeOnly with SafetyThreshold < FixUnsafe should skip
	fixer := &Fixer{
		SafetyThreshold: FixSafe,
		FixModes: map[string]map[string]FixMode{
			"Dockerfile": {
				"hadolint/DL3027": FixModeUnsafeOnly,
			},
		},
	}
	result, err := fixer.Apply(context.Background(), violations, sources)
	if err != nil {
		t.Fatalf("Apply error: %v", err)
	}

	if result.TotalApplied() != 0 {
		t.Errorf("TotalApplied() = %d, want 0 (unsafe-only mode with safe threshold)", result.TotalApplied())
	}

	// FixModeUnsafeOnly with SafetyThreshold >= FixUnsafe should apply
	fixer.SafetyThreshold = FixUnsafe
	result, err = fixer.Apply(context.Background(), violations, sources)
	if err != nil {
		t.Fatalf("Apply error: %v", err)
	}

	if result.TotalApplied() != 1 {
		t.Errorf("TotalApplied() = %d, want 1 (unsafe-only mode with unsafe threshold)", result.TotalApplied())
	}
}

func TestFixer_Apply_UnknownFixMode(t *testing.T) {
	sources := map[string][]byte{
		"Dockerfile": []byte("RUN apt install curl"),
	}

	violations := []rules.Violation{
		{
			Location: rules.NewLineLocation("Dockerfile", 1),
			RuleCode: "hadolint/DL3027",
			Message:  "Use apt-get",
			SuggestedFix: &rules.SuggestedFix{
				Description: "Replace apt with apt-get",
				Safety:      rules.FixSafe,
				Edits: []rules.TextEdit{
					{
						Location: rules.NewRangeLocation("Dockerfile", 1, 4, 1, 7),
						NewText:  "apt-get",
					},
				},
			},
		},
	}

	// Unknown fix mode should be treated as always
	fixer := &Fixer{
		SafetyThreshold: FixSafe,
		FixModes: map[string]map[string]FixMode{
			"Dockerfile": {
				"hadolint/DL3027": FixMode("unknown-mode"),
			},
		},
	}
	result, err := fixer.Apply(context.Background(), violations, sources)
	if err != nil {
		t.Fatalf("Apply error: %v", err)
	}

	if result.TotalApplied() != 1 {
		t.Errorf("TotalApplied() = %d, want 1 (unknown mode treated as always)", result.TotalApplied())
	}
}

func TestFixer_Apply_PerFileFixModes(t *testing.T) {
	// Two files with the same violation type but different fix modes
	sources := map[string][]byte{
		"file1.Dockerfile": []byte("RUN apt install curl"),
		"file2.Dockerfile": []byte("RUN apt install wget"),
	}
	violations := []rules.Violation{
		{
			RuleCode: "hadolint/DL3027",
			Message:  "Do not use apt",
			Location: rules.NewRangeLocation("file1.Dockerfile", 1, 4, 1, 7),
			SuggestedFix: &rules.SuggestedFix{
				Description: "Replace apt with apt-get",
				Safety:      rules.FixSafe,
				Edits: []rules.TextEdit{
					{
						Location: rules.NewRangeLocation("file1.Dockerfile", 1, 4, 1, 7),
						NewText:  "apt-get",
					},
				},
			},
		},
		{
			RuleCode: "hadolint/DL3027",
			Message:  "Do not use apt",
			Location: rules.NewRangeLocation("file2.Dockerfile", 1, 4, 1, 7),
			SuggestedFix: &rules.SuggestedFix{
				Description: "Replace apt with apt-get",
				Safety:      rules.FixSafe,
				Edits: []rules.TextEdit{
					{
						Location: rules.NewRangeLocation("file2.Dockerfile", 1, 4, 1, 7),
						NewText:  "apt-get",
					},
				},
			},
		},
	}

	// file1 allows fixes, file2 has fix=never
	fixer := &Fixer{
		SafetyThreshold: FixSafe,
		FixModes: map[string]map[string]FixMode{
			"file1.Dockerfile": {
				"hadolint/DL3027": FixModeAlways,
			},
			"file2.Dockerfile": {
				"hadolint/DL3027": FixModeNever,
			},
		},
	}
	result, err := fixer.Apply(context.Background(), violations, sources)
	if err != nil {
		t.Fatalf("Apply error: %v", err)
	}

	// Only file1 should have the fix applied
	if result.TotalApplied() != 1 {
		t.Errorf("TotalApplied() = %d, want 1 (only file1 should be fixed)", result.TotalApplied())
	}
	if result.TotalSkipped() != 1 {
		t.Errorf("TotalSkipped() = %d, want 1 (file2 should be skipped)", result.TotalSkipped())
	}

	// Verify file1 was modified
	fc1 := result.Changes["file1.Dockerfile"]
	if fc1 == nil || !fc1.HasChanges() {
		t.Error("file1.Dockerfile should have changes")
	}

	// Verify file2 was NOT modified
	fc2 := result.Changes["file2.Dockerfile"]
	if fc2 == nil {
		t.Fatal("file2.Dockerfile should exist in changes")
	}
	if fc2.HasChanges() {
		t.Error("file2.Dockerfile should NOT have changes (fix mode is never)")
	}
	if len(fc2.FixesSkipped) != 1 {
		t.Errorf("file2.Dockerfile FixesSkipped = %d, want 1", len(fc2.FixesSkipped))
	}
}

func TestApplyEdit_CRLF(t *testing.T) {
	// Test with Windows-style line endings
	content := []byte("FROM alpine\r\nRUN apt install curl\r\n")

	edit := rules.TextEdit{
		Location: rules.NewRangeLocation("Dockerfile", 2, 4, 2, 7),
		NewText:  "apt-get",
	}

	result := applyEdit(content, edit)
	expected := []byte("FROM alpine\r\nRUN apt-get install curl\r\n")

	if !bytes.Equal(result, expected) {
		t.Errorf("applyEdit() with CRLF =\n%q\nwant:\n%q", result, expected)
	}
}

func TestApplyEdit_CRLF_ReplacementWithNewlines(t *testing.T) {
	// Test that replacement text with embedded LF newlines is normalized to CRLF
	// when the file uses CRLF line endings. This prevents mixed line endings.
	content := []byte("FROM alpine\r\nRUN cd /app && make\r\n")

	// Replacement text uses LF (Unix-style) but file uses CRLF (Windows-style)
	// The fix should normalize the replacement to use CRLF
	edit := rules.TextEdit{
		Location: rules.NewRangeLocation("Dockerfile", 2, 0, 2, 19),
		NewText:  "WORKDIR /app\nRUN make", // LF-only newlines
	}

	result := applyEdit(content, edit)
	// Expected: replacement newlines should be converted to CRLF
	expected := []byte("FROM alpine\r\nWORKDIR /app\r\nRUN make\r\n")

	if !bytes.Equal(result, expected) {
		t.Errorf("applyEdit() should normalize replacement newlines to CRLF\ngot:\n%q\nwant:\n%q", result, expected)
	}
}

func TestApplyEdit_CRLF_ReplacementWithMixedNewlines(t *testing.T) {
	// Test that replacement text with mixed CRLF/LF newlines is fully normalized
	content := []byte("FROM alpine\r\nRUN echo hello\r\n")

	// Replacement text has mixed CRLF and LF
	edit := rules.TextEdit{
		Location: rules.NewRangeLocation("Dockerfile", 2, 0, 2, 14),
		NewText:  "RUN echo one\r\nRUN echo two\nRUN echo three", // Mixed line endings
	}

	result := applyEdit(content, edit)
	// Expected: all newlines should be normalized to CRLF
	expected := []byte("FROM alpine\r\nRUN echo one\r\nRUN echo two\r\nRUN echo three\r\n")

	if !bytes.Equal(result, expected) {
		t.Errorf("applyEdit() should normalize mixed newlines to CRLF\ngot:\n%q\nwant:\n%q", result, expected)
	}
}

func TestApplyEdit_InvalidStartLine(t *testing.T) {
	content := []byte("FROM alpine\nRUN echo hello")

	// Line 0 is invalid (1-based, so line 0 becomes -1 after conversion)
	edit := rules.TextEdit{
		Location: rules.NewRangeLocation("Dockerfile", 0, 0, 0, 5),
		NewText:  "replaced",
	}

	result := applyEdit(content, edit)
	// Should return original content unchanged
	if !bytes.Equal(result, content) {
		t.Errorf("applyEdit() with invalid start line should return original content")
	}
}

func TestApplyEdit_InvalidEndLine(t *testing.T) {
	content := []byte("FROM alpine")

	// End line 100 is beyond file
	edit := rules.TextEdit{
		Location: rules.NewRangeLocation("Dockerfile", 1, 0, 100, 5),
		NewText:  "replaced",
	}

	result := applyEdit(content, edit)
	// Should return original content unchanged
	if !bytes.Equal(result, content) {
		t.Errorf("applyEdit() with invalid end line should return original content")
	}
}

func TestApplyEdit_NegativeColumn(t *testing.T) {
	content := []byte("FROM alpine")

	// Negative column should be clamped to 0
	edit := rules.TextEdit{
		Location: rules.Location{
			File: "Dockerfile",
			Start: rules.Position{Line: 1, Column: -5},
			End:   rules.Position{Line: 1, Column: 4},
		},
		NewText: "COPY",
	}

	result := applyEdit(content, edit)
	expected := []byte("COPY alpine")

	if !bytes.Equal(result, expected) {
		t.Errorf("applyEdit() with negative column =\n%q\nwant:\n%q", result, expected)
	}
}

func TestApplyEdit_ColumnBeyondLineLength(t *testing.T) {
	content := []byte("FROM alpine")

	// Column 100 is beyond line length, should be clamped
	edit := rules.TextEdit{
		Location: rules.NewRangeLocation("Dockerfile", 1, 5, 1, 100),
		NewText:  "ubuntu",
	}

	result := applyEdit(content, edit)
	expected := []byte("FROM ubuntu")

	if !bytes.Equal(result, expected) {
		t.Errorf("applyEdit() with column beyond line =\n%q\nwant:\n%q", result, expected)
	}
}

func TestApplyEdit_NegativeEndColumn(t *testing.T) {
	content := []byte("FROM alpine")

	// Negative end column should be clamped to 0
	edit := rules.TextEdit{
		Location: rules.Location{
			File:  "Dockerfile",
			Start: rules.Position{Line: 1, Column: 0},
			End:   rules.Position{Line: 1, Column: -5},
		},
		NewText: "COPY",
	}

	result := applyEdit(content, edit)
	// Start=0, End clamped to 0, so nothing is replaced, just "COPY" inserted at position 0
	expected := []byte("COPYFROM alpine")

	if !bytes.Equal(result, expected) {
		t.Errorf("applyEdit() with negative end column =\n%q\nwant:\n%q", result, expected)
	}
}

// testResolver is a flexible test resolver for fixer tests.
type testResolver struct {
	id          string
	resolveFunc func(ctx context.Context, fix *rules.SuggestedFix) ([]rules.TextEdit, error)
}

func (r *testResolver) ID() string { return r.id }

func (r *testResolver) Resolve(ctx context.Context, fix *rules.SuggestedFix) ([]rules.TextEdit, error) {
	if r.resolveFunc != nil {
		return r.resolveFunc(ctx, fix)
	}
	return nil, nil
}

func TestFixer_Apply_AsyncFix_WithResolver(t *testing.T) {
	ClearResolvers()
	defer ClearResolvers()

	sources := map[string][]byte{
		"Dockerfile": []byte("FROM alpine"),
	}

	// Register a test resolver
	testResolverID := "test-resolver"
	RegisterResolver(&testResolver{
		id: testResolverID,
		resolveFunc: func(ctx context.Context, fix *rules.SuggestedFix) ([]rules.TextEdit, error) {
			return []rules.TextEdit{
				{
					Location: rules.NewRangeLocation("Dockerfile", 1, 5, 1, 11),
					NewText:  "ubuntu",
				},
			}, nil
		},
	})

	violations := []rules.Violation{
		{
			Location: rules.NewLineLocation("Dockerfile", 1),
			RuleCode: "some-rule",
			Message:  "Use ubuntu",
			SuggestedFix: &rules.SuggestedFix{
				Description:  "Change to ubuntu",
				Safety:       rules.FixSafe,
				NeedsResolve: true,
				ResolverID:   testResolverID,
			},
		},
	}

	fixer := &Fixer{SafetyThreshold: FixSafe, Concurrency: 2}
	result, err := fixer.Apply(context.Background(), violations, sources)
	if err != nil {
		t.Fatalf("Apply error: %v", err)
	}

	if result.TotalApplied() != 1 {
		t.Errorf("TotalApplied() = %d, want 1", result.TotalApplied())
	}

	fc := result.Changes["Dockerfile"]
	expected := []byte("FROM ubuntu")
	if !bytes.Equal(fc.ModifiedContent, expected) {
		t.Errorf("ModifiedContent =\n%q\nwant:\n%q", fc.ModifiedContent, expected)
	}
}

func TestFixer_Apply_AsyncFix_ResolverError(t *testing.T) {
	ClearResolvers()
	defer ClearResolvers()

	sources := map[string][]byte{
		"Dockerfile": []byte("FROM alpine"),
	}

	// Register a resolver that returns an error
	testResolverID := "test-error-resolver"
	RegisterResolver(&testResolver{
		id: testResolverID,
		resolveFunc: func(ctx context.Context, fix *rules.SuggestedFix) ([]rules.TextEdit, error) {
			return nil, context.DeadlineExceeded
		},
	})

	violations := []rules.Violation{
		{
			Location: rules.NewLineLocation("Dockerfile", 1),
			RuleCode: "some-rule",
			Message:  "Use ubuntu",
			SuggestedFix: &rules.SuggestedFix{
				Description:  "Change to ubuntu",
				Safety:       rules.FixSafe,
				NeedsResolve: true,
				ResolverID:   testResolverID,
			},
		},
	}

	fixer := &Fixer{SafetyThreshold: FixSafe}
	result, err := fixer.Apply(context.Background(), violations, sources)
	if err != nil {
		t.Fatalf("Apply error: %v", err)
	}

	// Fix should be skipped due to resolver error
	if result.TotalApplied() != 0 {
		t.Errorf("TotalApplied() = %d, want 0", result.TotalApplied())
	}
	if result.TotalSkipped() != 1 {
		t.Errorf("TotalSkipped() = %d, want 1", result.TotalSkipped())
	}

	fc := result.Changes["Dockerfile"]
	if len(fc.FixesSkipped) != 1 || fc.FixesSkipped[0].Reason != SkipResolveError {
		t.Errorf("Expected SkipResolveError reason")
	}
}

func TestFixer_Apply_AsyncFix_UnknownResolver(t *testing.T) {
	ClearResolvers()
	defer ClearResolvers()

	sources := map[string][]byte{
		"Dockerfile": []byte("FROM alpine"),
	}

	violations := []rules.Violation{
		{
			Location: rules.NewLineLocation("Dockerfile", 1),
			RuleCode: "some-rule",
			Message:  "Use ubuntu",
			SuggestedFix: &rules.SuggestedFix{
				Description:  "Change to ubuntu",
				Safety:       rules.FixSafe,
				NeedsResolve: true,
				ResolverID:   "non-existent-resolver",
			},
		},
	}

	fixer := &Fixer{SafetyThreshold: FixSafe}
	result, err := fixer.Apply(context.Background(), violations, sources)
	if err != nil {
		t.Fatalf("Apply error: %v", err)
	}

	// Fix should be skipped due to unknown resolver
	if result.TotalApplied() != 0 {
		t.Errorf("TotalApplied() = %d, want 0", result.TotalApplied())
	}
	if result.TotalSkipped() != 1 {
		t.Errorf("TotalSkipped() = %d, want 1", result.TotalSkipped())
	}
}

func TestFixer_Apply_ViolationForUnknownFile(t *testing.T) {
	sources := map[string][]byte{
		"Dockerfile": []byte("FROM alpine"),
	}

	// Violation for a file not in sources.
	// The fixer intentionally ignores violations for files not provided in sources:
	// - Only files in the sources map get FileChange entries in result.Changes
	// - Fixes targeting unknown files are silently skipped (not recorded as skipped)
	// - This is by design: the fixer only operates on files the caller provides
	violations := []rules.Violation{
		{
			Location: rules.NewLineLocation("other.Dockerfile", 1),
			RuleCode: "some-rule",
			Message:  "Some message",
			SuggestedFix: &rules.SuggestedFix{
				Description: "Some fix",
				Safety:      rules.FixSafe,
				Edits: []rules.TextEdit{
					{
						Location: rules.NewRangeLocation("other.Dockerfile", 1, 0, 1, 4),
						NewText:  "COPY",
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

	// Fix should not be applied (file not in sources)
	if result.TotalApplied() != 0 {
		t.Errorf("TotalApplied() = %d, want 0", result.TotalApplied())
	}

	// Unknown file violations are silently ignored (not tracked as skipped)
	// because there's no FileChange entry for unknown files
	if result.TotalSkipped() != 0 {
		t.Errorf("TotalSkipped() = %d, want 0 (unknown files are silently ignored)", result.TotalSkipped())
	}

	// Original Dockerfile should be unchanged
	fc := result.Changes["Dockerfile"]
	if !bytes.Equal(fc.ModifiedContent, fc.OriginalContent) {
		t.Error("Dockerfile should be unchanged")
	}

	// Verify that unknown file has no FileChange entry
	if _, exists := result.Changes["other.Dockerfile"]; exists {
		t.Error("Unknown file should not have a FileChange entry")
	}
}

func TestFixer_Apply_DefaultConcurrency(t *testing.T) {
	ClearResolvers()
	defer ClearResolvers()

	sources := map[string][]byte{
		"Dockerfile": []byte("FROM alpine"),
	}

	testResolverID := "test-concurrency-resolver"
	RegisterResolver(&testResolver{
		id: testResolverID,
		resolveFunc: func(ctx context.Context, fix *rules.SuggestedFix) ([]rules.TextEdit, error) {
			return []rules.TextEdit{
				{
					Location: rules.NewRangeLocation("Dockerfile", 1, 5, 1, 11),
					NewText:  "ubuntu",
				},
			}, nil
		},
	})

	violations := []rules.Violation{
		{
			Location: rules.NewLineLocation("Dockerfile", 1),
			RuleCode: "some-rule",
			Message:  "Use ubuntu",
			SuggestedFix: &rules.SuggestedFix{
				Description:  "Change to ubuntu",
				Safety:       rules.FixSafe,
				NeedsResolve: true,
				ResolverID:   testResolverID,
			},
		},
	}

	// Test with Concurrency=0 (should default to 4)
	fixer := &Fixer{SafetyThreshold: FixSafe, Concurrency: 0}
	result, err := fixer.Apply(context.Background(), violations, sources)
	if err != nil {
		t.Fatalf("Apply error: %v", err)
	}

	if result.TotalApplied() != 1 {
		t.Errorf("TotalApplied() = %d, want 1", result.TotalApplied())
	}
}
