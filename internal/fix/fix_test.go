package fix

import (
	"bytes"
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

func TestApplyEdits(t *testing.T) {
	t.Parallel()

	t.Run("empty edits returns src", func(t *testing.T) {
		t.Parallel()
		src := []byte("FROM alpine\n")
		got := ApplyEdits(src, nil)
		if !bytes.Equal(got, src) {
			t.Errorf("ApplyEdits(nil) = %q, want %q", got, src)
		}
	})

	t.Run("ascending-order edits applied correctly", func(t *testing.T) {
		t.Parallel()
		// Two same-line edits given in ascending order: ApplyEdits must
		// reverse them so the first edit's position is not invalidated.
		src := []byte("RUN apt install curl wget\n")
		edits := []rules.TextEdit{
			{Location: rules.NewRangeLocation("Dockerfile", 1, 4, 1, 7), NewText: "apt-get"},
			{Location: rules.NewRangeLocation("Dockerfile", 1, 21, 1, 25), NewText: "htop"},
		}
		want := "RUN apt-get install curl htop\n"
		if got := ApplyEdits(src, edits); string(got) != want {
			t.Errorf("ApplyEdits() = %q, want %q", got, want)
		}
	})

	t.Run("multi-line descending order", func(t *testing.T) {
		t.Parallel()
		src := []byte("FROM alpine\nRUN foo\nRUN bar\n")
		edits := []rules.TextEdit{
			{Location: rules.NewRangeLocation("Dockerfile", 2, 4, 2, 7), NewText: "FOO"},
			{Location: rules.NewRangeLocation("Dockerfile", 3, 4, 3, 7), NewText: "BAR"},
		}
		want := "FROM alpine\nRUN FOO\nRUN BAR\n"
		if got := ApplyEdits(src, edits); string(got) != want {
			t.Errorf("ApplyEdits() = %q, want %q", got, want)
		}
	})

	t.Run("does not mutate input slice", func(t *testing.T) {
		t.Parallel()
		edits := []rules.TextEdit{
			{Location: rules.NewRangeLocation("Dockerfile", 1, 0, 1, 4), NewText: "ENV"},
			{Location: rules.NewRangeLocation("Dockerfile", 2, 0, 2, 4), NewText: "ENV"},
		}
		first := edits[0]
		_ = ApplyEdits([]byte("FROM alpine\nFROM alpine\n"), edits)
		if edits[0] != first {
			t.Errorf("ApplyEdits mutated input slice: edits[0] = %+v, want %+v", edits[0], first)
		}
	})
}

func TestApplyFix(t *testing.T) {
	t.Parallel()

	t.Run("nil fix returns src", func(t *testing.T) {
		t.Parallel()
		src := []byte("FROM alpine\n")
		if got := ApplyFix(src, nil); !bytes.Equal(got, src) {
			t.Errorf("ApplyFix(nil) = %q, want %q", got, src)
		}
	})

	t.Run("applies edits from fix", func(t *testing.T) {
		t.Parallel()
		src := []byte("RUN apt install curl\n")
		fix := &rules.SuggestedFix{
			Edits: []rules.TextEdit{
				{Location: rules.NewRangeLocation("Dockerfile", 1, 4, 1, 7), NewText: "apt-get"},
			},
		}
		want := "RUN apt-get install curl\n"
		if got := ApplyFix(src, fix); string(got) != want {
			t.Errorf("ApplyFix() = %q, want %q", got, want)
		}
	})
}
