package copyignoredfile

import (
	"testing"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"
	"github.com/moby/buildkit/frontend/dockerfile/parser"

	"github.com/tinovyatkin/tally/internal/rules"
)

// mockBuildContext implements rules.BuildContext for testing.
type mockBuildContext struct {
	ignoredPaths map[string]bool
	heredocPaths map[string]bool
	hasIgnore    bool
}

func (m *mockBuildContext) IsIgnored(path string) (bool, error) {
	return m.ignoredPaths[path], nil
}

func (m *mockBuildContext) FileExists(_ string) bool {
	return true
}

func (m *mockBuildContext) IsHeredocFile(path string) bool {
	return m.heredocPaths[path]
}

func (m *mockBuildContext) HasIgnoreFile() bool {
	return m.hasIgnore
}

func TestMetadata(t *testing.T) {
	r := New()
	meta := r.Metadata()

	if meta.Code != "buildkit/CopyIgnoredFile" {
		t.Errorf("expected code %q, got %q", "buildkit/CopyIgnoredFile", meta.Code)
	}

	if meta.Category != "correctness" {
		t.Errorf("expected category %q, got %q", "correctness", meta.Category)
	}

	if !meta.EnabledByDefault {
		t.Error("expected rule to be enabled by default")
	}
}

func TestCheck_NoContext(t *testing.T) {
	r := New()

	input := rules.LintInput{
		File:    "Dockerfile",
		Context: nil,
	}

	violations := r.Check(input)
	if len(violations) != 0 {
		t.Errorf("expected no violations without context, got %d", len(violations))
	}
}

func TestCheck_NoIgnoreFile(t *testing.T) {
	r := New()

	ctx := &mockBuildContext{
		hasIgnore: false,
	}

	input := rules.LintInput{
		File:    "Dockerfile",
		Context: ctx,
		Stages: []instructions.Stage{
			{
				Commands: []instructions.Command{
					&instructions.CopyCommand{
						SourcesAndDest: instructions.SourcesAndDest{
							SourcePaths: []string{"app.go"},
						},
					},
				},
			},
		},
	}

	violations := r.Check(input)
	if len(violations) != 0 {
		t.Errorf("expected no violations without .dockerignore, got %d", len(violations))
	}
}

func TestCheck_IgnoredFile(t *testing.T) {
	r := New()

	ctx := &mockBuildContext{
		hasIgnore:    true,
		ignoredPaths: map[string]bool{"ignored.txt": true},
	}

	input := rules.LintInput{
		File:    "Dockerfile",
		Context: ctx,
		Stages: []instructions.Stage{
			{
				Commands: []instructions.Command{
					&instructions.CopyCommand{
						SourcesAndDest: instructions.SourcesAndDest{
							SourcePaths: []string{"ignored.txt"},
						},
					},
				},
			},
		},
	}

	violations := r.Check(input)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}

	if violations[0].RuleCode != "buildkit/CopyIgnoredFile" {
		t.Errorf("expected code %q, got %q", "buildkit/CopyIgnoredFile", violations[0].RuleCode)
	}
}

func TestCheck_NonIgnoredFile(t *testing.T) {
	r := New()

	ctx := &mockBuildContext{
		hasIgnore:    true,
		ignoredPaths: map[string]bool{"other.txt": true},
	}

	input := rules.LintInput{
		File:    "Dockerfile",
		Context: ctx,
		Stages: []instructions.Stage{
			{
				Commands: []instructions.Command{
					&instructions.CopyCommand{
						SourcesAndDest: instructions.SourcesAndDest{
							SourcePaths: []string{"app.go"},
						},
					},
				},
			},
		},
	}

	violations := r.Check(input)
	if len(violations) != 0 {
		t.Errorf("expected no violations, got %d", len(violations))
	}
}

func TestCheck_SkipsCopyFromStage(t *testing.T) {
	r := New()

	ctx := &mockBuildContext{
		hasIgnore:    true,
		ignoredPaths: map[string]bool{"app": true},
	}

	input := rules.LintInput{
		File:    "Dockerfile",
		Context: ctx,
		Stages: []instructions.Stage{
			{
				Commands: []instructions.Command{
					&instructions.CopyCommand{
						SourcesAndDest: instructions.SourcesAndDest{
							SourcePaths: []string{"app"},
						},
						From: "builder", // Copying from another stage
					},
				},
			},
		},
	}

	violations := r.Check(input)
	if len(violations) != 0 {
		t.Errorf("expected no violations for COPY --from, got %d", len(violations))
	}
}

func TestCheck_SkipsHeredocInSourceContents(t *testing.T) {
	r := New()

	ctx := &mockBuildContext{
		hasIgnore:    true,
		ignoredPaths: map[string]bool{"/app/script.sh": true},
	}

	input := rules.LintInput{
		File:    "Dockerfile",
		Context: ctx,
		Stages: []instructions.Stage{
			{
				Commands: []instructions.Command{
					&instructions.CopyCommand{
						SourcesAndDest: instructions.SourcesAndDest{
							SourcePaths: []string{"/app/script.sh"},
							SourceContents: []instructions.SourceContent{
								{Path: "/app/script.sh", Data: "#!/bin/bash\necho hello"},
							},
						},
					},
				},
			},
		},
	}

	violations := r.Check(input)
	if len(violations) != 0 {
		t.Errorf("expected no violations for heredoc sources, got %d", len(violations))
	}
}

func TestCheck_SkipsHeredocInContext(t *testing.T) {
	r := New()

	ctx := &mockBuildContext{
		hasIgnore:    true,
		ignoredPaths: map[string]bool{"script.sh": true},
		heredocPaths: map[string]bool{"script.sh": true},
	}

	input := rules.LintInput{
		File:    "Dockerfile",
		Context: ctx,
		Stages: []instructions.Stage{
			{
				Commands: []instructions.Command{
					&instructions.CopyCommand{
						SourcesAndDest: instructions.SourcesAndDest{
							SourcePaths: []string{"script.sh"},
						},
					},
				},
			},
		},
	}

	violations := r.Check(input)
	if len(violations) != 0 {
		t.Errorf("expected no violations for heredoc in context, got %d", len(violations))
	}
}

func TestCheck_SkipsURLs(t *testing.T) {
	r := New()

	ctx := &mockBuildContext{
		hasIgnore:    true,
		ignoredPaths: map[string]bool{"http://example.com/file.txt": true},
	}

	input := rules.LintInput{
		File:    "Dockerfile",
		Context: ctx,
		Stages: []instructions.Stage{
			{
				Commands: []instructions.Command{
					&instructions.AddCommand{
						SourcesAndDest: instructions.SourcesAndDest{
							SourcePaths: []string{"http://example.com/file.txt"},
						},
					},
				},
			},
		},
	}

	violations := r.Check(input)
	if len(violations) != 0 {
		t.Errorf("expected no violations for URLs, got %d", len(violations))
	}
}

func TestCheck_NormalizesLeadingDotSlash(t *testing.T) {
	r := New()

	ctx := &mockBuildContext{
		hasIgnore:    true,
		ignoredPaths: map[string]bool{"app.go": true},
	}

	input := rules.LintInput{
		File:    "Dockerfile",
		Context: ctx,
		Stages: []instructions.Stage{
			{
				Commands: []instructions.Command{
					&instructions.CopyCommand{
						SourcesAndDest: instructions.SourcesAndDest{
							SourcePaths: []string{"./app.go"},
						},
					},
				},
			},
		},
	}

	violations := r.Check(input)
	if len(violations) != 1 {
		t.Errorf("expected 1 violation for ./app.go, got %d", len(violations))
	}
}

func TestCheck_ADD(t *testing.T) {
	r := New()

	ctx := &mockBuildContext{
		hasIgnore:    true,
		ignoredPaths: map[string]bool{"ignored.tar.gz": true},
	}

	input := rules.LintInput{
		File:    "Dockerfile",
		Context: ctx,
		Stages: []instructions.Stage{
			{
				Commands: []instructions.Command{
					&instructions.AddCommand{
						SourcesAndDest: instructions.SourcesAndDest{
							SourcePaths: []string{"ignored.tar.gz"},
						},
					},
				},
			},
		},
	}

	violations := r.Check(input)
	if len(violations) != 1 {
		t.Errorf("expected 1 violation for ADD, got %d", len(violations))
	}
}

func TestCheck_MultipleStages(t *testing.T) {
	r := New()

	ctx := &mockBuildContext{
		hasIgnore: true,
		ignoredPaths: map[string]bool{
			"ignored1.txt": true,
			"ignored2.txt": true,
		},
	}

	input := rules.LintInput{
		File:    "Dockerfile",
		Context: ctx,
		Stages: []instructions.Stage{
			{
				Commands: []instructions.Command{
					&instructions.CopyCommand{
						SourcesAndDest: instructions.SourcesAndDest{
							SourcePaths: []string{"ignored1.txt"},
						},
					},
				},
			},
			{
				Commands: []instructions.Command{
					&instructions.CopyCommand{
						SourcesAndDest: instructions.SourcesAndDest{
							SourcePaths: []string{"ignored2.txt"},
						},
					},
				},
			},
		},
	}

	violations := r.Check(input)
	if len(violations) != 2 {
		t.Errorf("expected 2 violations from 2 stages, got %d", len(violations))
	}
}

func TestIsURL(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"http://example.com/file", true},
		{"https://example.com/file", true},
		{"ftp://example.com/file", true},
		{"git://github.com/user/repo.git", true},
		{"file.txt", false},
		{"./file.txt", false},
		{"/abs/path", false},
	}

	for _, tc := range tests {
		got := isURL(tc.path)
		if got != tc.want {
			t.Errorf("isURL(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

func TestNormalizePath(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"./file.txt", "file.txt"},
		{"file.txt", "file.txt"},
		{"./dir/file.txt", "dir/file.txt"},
		{"/abs/path", "/abs/path"},
	}

	for _, tc := range tests {
		got := normalizePath(tc.path)
		if got != tc.want {
			t.Errorf("normalizePath(%q) = %q, want %q", tc.path, got, tc.want)
		}
	}
}

func TestLocationFromRanges(t *testing.T) {
	r := New()

	ctx := &mockBuildContext{
		hasIgnore:    true,
		ignoredPaths: map[string]bool{"ignored.txt": true},
	}

	loc := []parser.Range{
		{
			Start: parser.Position{Line: 5, Character: 0},
			End:   parser.Position{Line: 5, Character: 20},
		},
	}

	input := rules.LintInput{
		File:    "Dockerfile",
		Context: ctx,
		Stages: []instructions.Stage{
			{
				Commands: []instructions.Command{
					&instructions.CopyCommand{
						SourcesAndDest: instructions.SourcesAndDest{
							SourcePaths: []string{"ignored.txt"},
						},
					},
				},
			},
		},
	}

	// The real CopyCommand would have Location() returning the ranges
	// but in tests we're using a minimal mock. The actual location test
	// is done via integration tests.
	violations := r.Check(input)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}

	// Location should be file-level when no ranges provided
	_ = loc // unused in this minimal test
}
