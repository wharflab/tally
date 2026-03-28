package rules

import (
	"encoding/json/v2"
	"testing"

	"github.com/moby/buildkit/frontend/dockerfile/parser"
)

func TestNewViolation(t *testing.T) {
	t.Parallel()
	loc := NewLineLocation("Dockerfile", 5)
	v := NewViolation(loc, "test-rule", "test message", SeverityWarning)

	if v.RuleCode != "test-rule" {
		t.Errorf("RuleCode = %q, want %q", v.RuleCode, "test-rule")
	}
	if v.Message != "test message" {
		t.Errorf("Message = %q, want %q", v.Message, "test message")
	}
	if v.Severity != SeverityWarning {
		t.Errorf("Severity = %v, want %v", v.Severity, SeverityWarning)
	}
	if v.File() != "Dockerfile" {
		t.Errorf("File() = %q, want %q", v.File(), "Dockerfile")
	}
	if v.Line() != 5 {
		t.Errorf("Line() = %d, want 5", v.Line())
	}
}

func TestViolation_WithMethods(t *testing.T) {
	t.Parallel()
	loc := NewLineLocation("Dockerfile", 1)
	v := NewViolation(loc, "rule", "msg", SeverityError).
		WithDetail("extra detail").
		WithDocURL("https://example.com/doc").
		WithSourceCode("FROM alpine")

	if v.Detail != "extra detail" {
		t.Errorf("Detail = %q, want %q", v.Detail, "extra detail")
	}
	if v.DocURL != "https://example.com/doc" {
		t.Errorf("DocURL = %q, want %q", v.DocURL, "https://example.com/doc")
	}
	if v.SourceCode != "FROM alpine" {
		t.Errorf("SourceCode = %q, want %q", v.SourceCode, "FROM alpine")
	}
}

func TestViolation_WithSuggestedFix(t *testing.T) {
	t.Parallel()
	loc := NewRangeLocation("Dockerfile", 1, 1, 1, 12)
	fix := &SuggestedFix{
		Description: "Use specific tag",
		Edits: []TextEdit{
			{
				Location: loc,
				NewText:  "FROM alpine:3.18",
			},
		},
	}

	v := NewViolation(loc, "DL3006", "Always specify tag", SeverityWarning).
		WithSuggestedFix(fix)

	if v.SuggestedFix == nil {
		t.Fatal("SuggestedFix is nil")
	}
	if v.SuggestedFix.Description != "Use specific tag" {
		t.Errorf("SuggestedFix.Description = %q", v.SuggestedFix.Description)
	}
	if len(v.SuggestedFix.Edits) != 1 {
		t.Fatalf("len(Edits) = %d, want 1", len(v.SuggestedFix.Edits))
	}
	if v.SuggestedFix.Edits[0].NewText != "FROM alpine:3.18" {
		t.Errorf("Edit.NewText = %q", v.SuggestedFix.Edits[0].NewText)
	}
}

func TestViolation_JSON(t *testing.T) {
	t.Parallel()
	loc := NewLineLocation("Dockerfile", 10)
	v := NewViolation(loc, "max-lines", "file too long", SeverityError).
		WithDocURL("https://example.com")

	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var parsed Violation
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if parsed.RuleCode != v.RuleCode {
		t.Errorf("RuleCode = %q, want %q", parsed.RuleCode, v.RuleCode)
	}
	if parsed.Message != v.Message {
		t.Errorf("Message = %q, want %q", parsed.Message, v.Message)
	}
	if parsed.Severity != v.Severity {
		t.Errorf("Severity = %v, want %v", parsed.Severity, v.Severity)
	}
	if parsed.Line() != v.Line() {
		t.Errorf("Line() = %d, want %d", parsed.Line(), v.Line())
	}
}

func TestViolation_JSON_WithFix(t *testing.T) {
	t.Parallel()
	loc := NewLineLocation("Dockerfile", 1)
	fix := &SuggestedFix{
		Description: "Fix the issue",
		Edits: []TextEdit{
			{Location: loc, NewText: "new text"},
		},
	}
	v := NewViolation(loc, "rule", "msg", SeverityWarning).WithSuggestedFix(fix)

	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var parsed Violation
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if parsed.SuggestedFix == nil {
		t.Fatal("SuggestedFix is nil after unmarshal")
	}
	if parsed.SuggestedFix.Description != "Fix the issue" {
		t.Errorf("SuggestedFix.Description = %q", parsed.SuggestedFix.Description)
	}
}

func TestNewViolationFromBuildKitWarning(t *testing.T) {
	t.Parallel()
	// Test with location (0-based coordinates from BuildKit)
	location := []parser.Range{
		{
			Start: parser.Position{Line: 5, Character: 1},
			End:   parser.Position{Line: 5, Character: 20},
		},
	}

	v := NewViolationFromBuildKitWarning(
		"Dockerfile",
		"StageNameCasing",
		"Stage names should be lowercase",
		"https://docs.docker.com/go/dockerfile/rule/stage-name-casing/",
		"Stage name 'Builder' should be lowercase",
		location,
	)

	if v.RuleCode != "buildkit/StageNameCasing" {
		t.Errorf("RuleCode = %q, want %q", v.RuleCode, "buildkit/StageNameCasing")
	}
	if v.Message != "Stage name 'Builder' should be lowercase" {
		t.Errorf("Message = %q", v.Message)
	}
	if v.Detail != "Stage names should be lowercase" {
		t.Errorf("Detail = %q", v.Detail)
	}
	if v.DocURL != BuildKitDocURL("StageNameCasing") {
		t.Errorf("DocURL = %q, want %q", v.DocURL, BuildKitDocURL("StageNameCasing"))
	}
	if v.Severity != SeverityWarning {
		t.Errorf("Severity = %v, want %v", v.Severity, SeverityWarning)
	}
	// Direct mapping, 0-based coordinates preserved
	if v.Location.Start.Line != 5 {
		t.Errorf("Location.Start.Line = %d, want 5", v.Location.Start.Line)
	}
	if v.Location.Start.Column != 1 {
		t.Errorf("Location.Start.Column = %d, want 1", v.Location.Start.Column)
	}
}

func TestNewViolationFromBuildKitWarning_NoLocation(t *testing.T) {
	t.Parallel()
	// Test without location (file-level warning)
	v := NewViolationFromBuildKitWarning(
		"Dockerfile",
		"SomeRule",
		"Description",
		"https://example.com",
		"File-level warning",
		nil,
	)

	if v.Location.File != "Dockerfile" {
		t.Errorf("File = %q, want %q", v.Location.File, "Dockerfile")
	}
	if v.Location.Start.Line != -1 {
		t.Errorf("Start.Line = %d, want -1 (file-level sentinel)", v.Location.Start.Line)
	}
	if !v.Location.IsFileLevel() {
		t.Error("IsFileLevel() = false, want true")
	}
}

func TestViolation_WithSuggestedFixes(t *testing.T) {
	t.Parallel()
	loc := NewRangeLocation("Dockerfile", 5, 0, 5, 20)
	fixA := &SuggestedFix{
		Description: "Comment out the line",
		Safety:      FixSafe,
		IsPreferred: true,
		Edits:       []TextEdit{{Location: loc, NewText: "# commented: STOPSIGNAL SIGKILL"}},
	}
	fixB := &SuggestedFix{
		Description: "Delete the line",
		Safety:      FixSuggestion,
		Edits:       []TextEdit{{Location: loc, NewText: ""}},
	}

	v := NewViolation(loc, "tally/windows/no-stopsignal", "STOPSIGNAL not supported", SeverityWarning).
		WithSuggestedFixes([]*SuggestedFix{fixA, fixB})

	// SuggestedFixes stores both alternatives
	if len(v.SuggestedFixes) != 2 {
		t.Fatalf("SuggestedFixes len = %d, want 2", len(v.SuggestedFixes))
	}
	// SuggestedFix mirrors the preferred fix for backward compatibility
	if v.SuggestedFix != fixA {
		t.Error("SuggestedFix should point to the preferred fix (fixA)")
	}
}

func TestViolation_PreferredFix(t *testing.T) {
	t.Parallel()
	loc := NewLineLocation("Dockerfile", 1)

	t.Run("from SuggestedFixes with IsPreferred", func(t *testing.T) {
		t.Parallel()
		fixA := &SuggestedFix{Description: "A", Safety: FixSafe}
		fixB := &SuggestedFix{Description: "B", Safety: FixSuggestion, IsPreferred: true}
		v := NewViolation(loc, "r", "m", SeverityWarning).WithSuggestedFixes([]*SuggestedFix{fixA, fixB})

		pf := v.PreferredFix()
		if pf != fixB {
			t.Errorf("PreferredFix() = %q, want %q", pf.Description, fixB.Description)
		}
	})

	t.Run("from SuggestedFixes without IsPreferred defaults to first", func(t *testing.T) {
		t.Parallel()
		fixA := &SuggestedFix{Description: "A"}
		fixB := &SuggestedFix{Description: "B"}
		v := NewViolation(loc, "r", "m", SeverityWarning).WithSuggestedFixes([]*SuggestedFix{fixA, fixB})

		if pf := v.PreferredFix(); pf != fixA {
			t.Errorf("PreferredFix() = %q, want %q (first element)", pf.Description, fixA.Description)
		}
	})

	t.Run("falls back to SuggestedFix when SuggestedFixes is empty", func(t *testing.T) {
		t.Parallel()
		fix := &SuggestedFix{Description: "single fix"}
		v := NewViolation(loc, "r", "m", SeverityWarning).WithSuggestedFix(fix)

		if pf := v.PreferredFix(); pf != fix {
			t.Errorf("PreferredFix() = %q, want %q", pf.Description, fix.Description)
		}
	})

	t.Run("returns nil when no fix", func(t *testing.T) {
		t.Parallel()
		v := NewViolation(loc, "r", "m", SeverityWarning)
		if pf := v.PreferredFix(); pf != nil {
			t.Errorf("PreferredFix() = %v, want nil", pf)
		}
	})
}

func TestViolation_AllFixes(t *testing.T) {
	t.Parallel()
	loc := NewLineLocation("Dockerfile", 1)

	t.Run("returns SuggestedFixes when populated", func(t *testing.T) {
		t.Parallel()
		fixA := &SuggestedFix{Description: "A"}
		fixB := &SuggestedFix{Description: "B"}
		v := NewViolation(loc, "r", "m", SeverityWarning).WithSuggestedFixes([]*SuggestedFix{fixA, fixB})

		all := v.AllFixes()
		if len(all) != 2 {
			t.Fatalf("AllFixes() len = %d, want 2", len(all))
		}
	})

	t.Run("wraps single SuggestedFix", func(t *testing.T) {
		t.Parallel()
		fix := &SuggestedFix{Description: "single"}
		v := NewViolation(loc, "r", "m", SeverityWarning).WithSuggestedFix(fix)

		all := v.AllFixes()
		if len(all) != 1 {
			t.Fatalf("AllFixes() len = %d, want 1", len(all))
		}
		if all[0] != fix {
			t.Error("AllFixes()[0] should be the single SuggestedFix")
		}
	})

	t.Run("returns nil when no fix", func(t *testing.T) {
		t.Parallel()
		v := NewViolation(loc, "r", "m", SeverityWarning)
		if all := v.AllFixes(); all != nil {
			t.Errorf("AllFixes() = %v, want nil", all)
		}
	})
}

func TestViolation_JSON_WithSuggestedFixes(t *testing.T) {
	t.Parallel()
	loc := NewLineLocation("Dockerfile", 5)
	fixA := &SuggestedFix{
		Description: "Comment out",
		Safety:      FixSafe,
		IsPreferred: true,
		Edits:       []TextEdit{{Location: loc, NewText: "# commented"}},
	}
	fixB := &SuggestedFix{
		Description: "Delete line",
		Safety:      FixSuggestion,
		Edits:       []TextEdit{{Location: loc, NewText: ""}},
	}
	v := NewViolation(loc, "test-rule", "msg", SeverityWarning).
		WithSuggestedFixes([]*SuggestedFix{fixA, fixB})

	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var parsed Violation
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	// SuggestedFix preserved (backward compat)
	if parsed.SuggestedFix == nil {
		t.Fatal("SuggestedFix is nil after unmarshal")
	}
	if parsed.SuggestedFix.Description != "Comment out" {
		t.Errorf("SuggestedFix.Description = %q, want %q", parsed.SuggestedFix.Description, "Comment out")
	}

	// SuggestedFixes preserved
	if len(parsed.SuggestedFixes) != 2 {
		t.Fatalf("SuggestedFixes len = %d, want 2", len(parsed.SuggestedFixes))
	}
	if parsed.SuggestedFixes[0].Description != "Comment out" {
		t.Errorf("SuggestedFixes[0].Description = %q", parsed.SuggestedFixes[0].Description)
	}
	if parsed.SuggestedFixes[1].Description != "Delete line" {
		t.Errorf("SuggestedFixes[1].Description = %q", parsed.SuggestedFixes[1].Description)
	}
	if parsed.SuggestedFixes[1].Safety != FixSuggestion {
		t.Errorf("SuggestedFixes[1].Safety = %v, want FixSuggestion", parsed.SuggestedFixes[1].Safety)
	}
}

func TestViolation_JSON_SingleFix_NoSuggestedFixes(t *testing.T) {
	t.Parallel()
	loc := NewLineLocation("Dockerfile", 1)
	v := NewViolation(loc, "r", "m", SeverityWarning).
		WithSuggestedFix(&SuggestedFix{Description: "single fix"})

	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	// suggestedFixes key should not appear for a single fix
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal raw error: %v", err)
	}
	if _, ok := raw["suggestedFixes"]; ok {
		t.Error("suggestedFixes should be omitted when only SuggestedFix is set")
	}
}

func TestFixSafety_String(t *testing.T) {
	t.Parallel()
	tests := []struct {
		safety FixSafety
		want   string
	}{
		{FixSafe, "safe"},
		{FixSuggestion, "suggestion"},
		{FixUnsafe, "unsafe"},
		{FixSafety(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			t.Parallel()
			if got := tt.safety.String(); got != tt.want {
				t.Errorf("FixSafety(%d).String() = %q, want %q", tt.safety, got, tt.want)
			}
		})
	}
}

func TestSuggestedFix_WithSafety(t *testing.T) {
	t.Parallel()
	loc := NewLineLocation("Dockerfile", 1)
	fix := &SuggestedFix{
		Description: "Replace apt with apt-get",
		Safety:      FixSafe,
		Edits: []TextEdit{
			{Location: loc, NewText: "apt-get"},
		},
	}

	if fix.Description == "" {
		t.Error("Description should not be empty")
	}
	if fix.Safety != FixSafe {
		t.Errorf("Safety = %v, want %v", fix.Safety, FixSafe)
	}
	if len(fix.Edits) != 1 {
		t.Errorf("Edits count = %d, want 1", len(fix.Edits))
	}
	if fix.NeedsResolve {
		t.Error("NeedsResolve should be false for sync fix")
	}
}

func TestSuggestedFix_AsyncFix(t *testing.T) {
	t.Parallel()
	loc := NewLineLocation("Dockerfile", 1)
	fix := &SuggestedFix{
		Description:  "Add image digest",
		Safety:       FixSafe,
		NeedsResolve: true,
		ResolverID:   "image-digest",
		ResolverData: map[string]string{"image": "alpine", "tag": "3.18"},
	}

	if fix.Description == "" {
		t.Error("Description should not be empty")
	}
	if fix.Safety != FixSafe {
		t.Errorf("Safety = %v, want %v", fix.Safety, FixSafe)
	}
	if !fix.NeedsResolve {
		t.Error("NeedsResolve should be true for async fix")
	}
	if fix.ResolverID != "image-digest" {
		t.Errorf("ResolverID = %q, want %q", fix.ResolverID, "image-digest")
	}
	if fix.ResolverData == nil {
		t.Error("ResolverData should not be nil")
	}
	if len(fix.Edits) != 0 {
		t.Error("Edits should be empty for async fix before resolution")
	}

	// Simulate resolution
	fix.Edits = []TextEdit{
		{Location: loc, NewText: "alpine:3.18@sha256:abc123"},
	}
	fix.NeedsResolve = false

	if fix.NeedsResolve {
		t.Error("NeedsResolve should be false after resolution")
	}
	if len(fix.Edits) != 1 {
		t.Error("Edits should be populated after resolution")
	}
}

func TestSuggestedFix_JSON_WithSafety(t *testing.T) {
	t.Parallel()
	loc := NewLineLocation("Dockerfile", 1)
	fix := &SuggestedFix{
		Description: "Replace apt with apt-get",
		Safety:      FixSuggestion,
		IsPreferred: true,
		Edits: []TextEdit{
			{Location: loc, NewText: "apt-get"},
		},
	}

	data, err := json.Marshal(fix)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var parsed SuggestedFix
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if parsed.Safety != FixSuggestion {
		t.Errorf("Safety = %v, want %v", parsed.Safety, FixSuggestion)
	}
	if !parsed.IsPreferred {
		t.Error("IsPreferred should be true")
	}
}

func TestSuggestedFix_JSON_AsyncFix(t *testing.T) {
	t.Parallel()
	fix := &SuggestedFix{
		Description:  "Add image digest",
		NeedsResolve: true,
		ResolverID:   "image-digest",
		ResolverData: map[string]string{"image": "alpine"}, // Not serialized
	}

	data, err := json.Marshal(fix)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var parsed SuggestedFix
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if !parsed.NeedsResolve {
		t.Error("NeedsResolve should be true")
	}
	if parsed.ResolverID != "image-digest" {
		t.Errorf("ResolverID = %q, want %q", parsed.ResolverID, "image-digest")
	}
	if parsed.ResolverData != nil {
		t.Error("ResolverData should be nil (not serialized)")
	}
}
