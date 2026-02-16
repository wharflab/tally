package fix

import (
	"testing"

	"github.com/wharflab/tally/internal/rules"
)

func TestSkipReason_String(t *testing.T) {
	t.Parallel()
	tests := []struct {
		reason SkipReason
		want   string
	}{
		{SkipConflict, "conflicts with another fix"},
		{SkipSafety, "below safety threshold"},
		{SkipRuleFilter, "rule not in fix-rule list"},
		{SkipResolveError, "resolver failed"},
		{SkipNoEdits, "no edits in fix"},
		{SkipReason(99), "unknown reason"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			t.Parallel()
			if got := tt.reason.String(); got != tt.want {
				t.Errorf("SkipReason(%d).String() = %q, want %q", tt.reason, got, tt.want)
			}
		})
	}
}

func TestFileChange_HasChanges(t *testing.T) {
	t.Parallel()
	t.Run("no changes", func(t *testing.T) {
		t.Parallel()
		fc := &FileChange{
			Path:            "Dockerfile",
			OriginalContent: []byte("FROM alpine"),
			ModifiedContent: []byte("FROM alpine"),
		}
		if fc.HasChanges() {
			t.Error("HasChanges() = true, want false")
		}
	})

	t.Run("with changes", func(t *testing.T) {
		t.Parallel()
		fc := &FileChange{
			Path:            "Dockerfile",
			OriginalContent: []byte("RUN apt install curl"),
			ModifiedContent: []byte("RUN apt-get install curl"),
			FixesApplied: []AppliedFix{
				{
					RuleCode:    "hadolint/DL3027",
					Description: "Replace apt with apt-get",
					Location:    rules.NewLineLocation("Dockerfile", 1),
				},
			},
		}
		if !fc.HasChanges() {
			t.Error("HasChanges() = false, want true")
		}
	})
}

func TestFixSafety_ReExport(t *testing.T) {
	t.Parallel()
	// Verify re-exports match rules package
	if FixSafe != rules.FixSafe {
		t.Errorf("FixSafe = %v, want %v", FixSafe, rules.FixSafe)
	}
	if FixSuggestion != rules.FixSuggestion {
		t.Errorf("FixSuggestion = %v, want %v", FixSuggestion, rules.FixSuggestion)
	}
	if FixUnsafe != rules.FixUnsafe {
		t.Errorf("FixUnsafe = %v, want %v", FixUnsafe, rules.FixUnsafe)
	}
}
