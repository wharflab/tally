package tally

import (
	"strings"
	"testing"

	"github.com/moby/buildkit/frontend/dockerfile/parser"

	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/testutil"
)

func TestInvalidOnbuildTriggerRule_Metadata(t *testing.T) {
	t.Parallel()
	r := NewInvalidOnbuildTriggerRule()
	meta := r.Metadata()

	if meta.Code != InvalidOnbuildTriggerRuleCode {
		t.Errorf("Code = %q, want %q", meta.Code, InvalidOnbuildTriggerRuleCode)
	}
	if meta.DefaultSeverity != rules.SeverityError {
		t.Errorf("DefaultSeverity = %v, want Error", meta.DefaultSeverity)
	}
	if meta.Category != "correctness" {
		t.Errorf("Category = %q, want correctness", meta.Category)
	}
}

func TestInvalidOnbuildTriggerRule_Check(t *testing.T) {
	t.Parallel()

	testutil.RunRuleTests(t, NewInvalidOnbuildTriggerRule(), []testutil.RuleTestCase{
		{
			Name:           "no ONBUILD — no violations",
			Content:        "FROM alpine:3.19\nRUN echo hello\n",
			WantViolations: 0,
		},
		{
			Name:           "valid ONBUILD COPY — no violation",
			Content:        "FROM alpine:3.19\nONBUILD COPY . /app\n",
			WantViolations: 0,
		},
		{
			Name:           "valid ONBUILD RUN — no violation",
			Content:        "FROM alpine:3.19\nONBUILD RUN echo hello\n",
			WantViolations: 0,
		},
		{
			Name:           "valid ONBUILD ENV — no violation",
			Content:        "FROM alpine:3.19\nONBUILD ENV FOO=bar\n",
			WantViolations: 0,
		},
		{
			Name:           "ONBUILD COPPY typo — one violation with suggestion",
			Content:        "FROM alpine:3.19\nONBUILD COPPY . /app\n",
			WantViolations: 1,
			WantMessages:   []string{`did you mean "COPY"`},
		},
		{
			Name:           "ONBUILD RUNN typo — one violation with suggestion",
			Content:        "FROM alpine:3.19\nONBUILD RUNN echo hello\n",
			WantViolations: 1,
			WantMessages:   []string{`did you mean "RUN"`},
		},
		{
			Name:           "ONBUILD WROKDIR typo — one violation with suggestion",
			Content:        "FROM alpine:3.19\nONBUILD WROKDIR /app\n",
			WantViolations: 1,
			WantMessages:   []string{`did you mean "WORKDIR"`},
		},
		{
			Name:           "ONBUILD FOOBAR — unknown without suggestion",
			Content:        "FROM alpine:3.19\nONBUILD FOOBAR /x\n",
			WantViolations: 1,
			WantMessages:   []string{`unknown instruction "FOOBAR"`},
		},
		{
			Name:           "ONBUILD FROM — skipped (covered by DL3043)",
			Content:        "FROM alpine:3.19\nONBUILD FROM debian\n",
			WantViolations: 0,
		},
		{
			Name:           "ONBUILD ONBUILD — skipped (covered by DL3043)",
			Content:        "FROM alpine:3.19\nONBUILD ONBUILD RUN echo\n",
			WantViolations: 0,
		},
		{
			Name:           "ONBUILD MAINTAINER — skipped (covered by DL3043)",
			Content:        "FROM alpine:3.19\nONBUILD MAINTAINER foo@bar.com\n",
			WantViolations: 0,
		},
		{
			Name: "multiple ONBUILD typos — all reported",
			Content: "FROM alpine:3.19\n" +
				"ONBUILD COPPY . /app\n" +
				"ONBUILD RUNN echo hello\n",
			WantViolations: 2,
		},
		{
			Name: "mixed valid and invalid ONBUILD",
			Content: "FROM alpine:3.19\n" +
				"ONBUILD COPY . /app\n" +
				"ONBUILD COPPY . /tmp\n",
			WantViolations: 1,
			WantMessages:   []string{`"COPPY"`},
		},
	})
}

func TestInvalidOnbuildTriggerRule_ViolationLocation(t *testing.T) {
	t.Parallel()

	content := "FROM alpine:3.19\nONBUILD COPPY . /app\n"
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	r := NewInvalidOnbuildTriggerRule()
	violations := r.Check(input)

	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}

	// ONBUILD is on line 2 (1-based)
	if violations[0].Location.Start.Line != 2 {
		t.Errorf("Start.Line = %d, want 2", violations[0].Location.Start.Line)
	}
}

func TestInvalidOnbuildTriggerRule_SuggestedFix(t *testing.T) {
	t.Parallel()

	content := "FROM alpine:3.19\nONBUILD COPPY . /app\n"
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	r := NewInvalidOnbuildTriggerRule()
	violations := r.Check(input)

	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}

	v := violations[0]
	if v.SuggestedFix == nil {
		t.Fatal("expected a SuggestedFix for a typo within distance 2")
	}
	if v.SuggestedFix.Safety != rules.FixSuggestion {
		t.Errorf("Safety = %v, want FixSuggestion", v.SuggestedFix.Safety)
	}
	if len(v.SuggestedFix.Edits) != 1 {
		t.Fatalf("expected 1 edit, got %d", len(v.SuggestedFix.Edits))
	}
	edit := v.SuggestedFix.Edits[0]
	if edit.NewText != "COPY" {
		t.Errorf("NewText = %q, want %q", edit.NewText, "COPY")
	}

	// Edit should target line 2, within the "COPPY" token range
	if edit.Location.Start.Line != 2 {
		t.Errorf("edit Start.Line = %d, want 2", edit.Location.Start.Line)
	}
}

func TestInvalidOnbuildTriggerRule_NoFixForUnknownTrigger(t *testing.T) {
	t.Parallel()

	content := "FROM alpine:3.19\nONBUILD FOOBAR /x\n"
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	r := NewInvalidOnbuildTriggerRule()
	violations := r.Check(input)

	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}
	if violations[0].SuggestedFix != nil {
		t.Error("expected no SuggestedFix for a trigger with no close match")
	}
	if strings.Contains(violations[0].Message, "did you mean") {
		t.Error("message should not contain 'did you mean' when no suggestion exists")
	}
}

func TestTriggerColumnRange(t *testing.T) {
	t.Parallel()

	tests := []struct {
		line      string
		trigger   string
		wantStart int
		wantEnd   int
	}{
		{"ONBUILD COPPY . /app", "COPPY", 8, 13},
		{"ONBUILD  RUNN echo", "RUNN", 9, 13},       // double space
		{"onbuild coppy . /app", "coppy", 8, 13},    // lowercase
		{"  ONBUILD COPPY . /app", "COPPY", 10, 15}, // indented instruction
		{"WORKDIR /app", "WORKDIR", -1, -1},         // not an ONBUILD line
	}

	for _, tt := range tests {
		t.Run(tt.line, func(t *testing.T) {
			t.Parallel()
			start, end := triggerColumnRange(tt.line, tt.trigger)
			if start != tt.wantStart || end != tt.wantEnd {
				t.Errorf("triggerColumnRange(%q, %q) = (%d, %d), want (%d, %d)",
					tt.line, tt.trigger, start, end, tt.wantStart, tt.wantEnd)
			}
		})
	}
}

func TestOnbuildTrigger_NilGuards(t *testing.T) {
	t.Parallel()

	// Empty node.Next
	node := &parser.Node{Value: "onbuild"}
	if got := onbuildTrigger(node); got != "" {
		t.Errorf("onbuildTrigger(no Next) = %q, want empty", got)
	}

	// node.Next with no children
	node.Next = &parser.Node{}
	if got := onbuildTrigger(node); got != "" {
		t.Errorf("onbuildTrigger(empty Children) = %q, want empty", got)
	}

	// node.Next.Children[0] == nil
	node.Next.Children = []*parser.Node{nil}
	if got := onbuildTrigger(node); got != "" {
		t.Errorf("onbuildTrigger(nil Children[0]) = %q, want empty", got)
	}
}

func TestBuildTriggerFix_LineOutOfBounds(t *testing.T) {
	t.Parallel()

	// StartLine = 99 but source has only 2 lines → buildTriggerFix returns nil
	node := &parser.Node{Value: "onbuild", StartLine: 99}
	source := []byte("FROM alpine:3.19\nONBUILD COPPY . /app\n")
	fix := buildTriggerFix("Dockerfile", source, node, "COPPY", "COPY")
	if fix != nil {
		t.Errorf("expected nil fix for out-of-bounds line, got %v", fix)
	}
}
