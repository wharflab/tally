package hadolint

import (
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/wharflab/tally/internal/async"
	"github.com/wharflab/tally/internal/registry"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/testutil"
)

func TestDL3045Rule_Metadata(t *testing.T) {
	t.Parallel()
	snaps.MatchStandaloneJSON(t, NewDL3045Rule().Metadata())
}

func TestDL3045Rule_Check(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		dockerfile string
		wantCount  int
	}{
		// --- Cases from Hadolint spec: ruleCatchesNot (expect 0 violations) ---
		{
			name:       "ok: COPY with absolute destination and no WORKDIR set",
			dockerfile: "FROM scratch\nCOPY bla.sh /usr/local/bin/blubb.sh\n",
			wantCount:  0,
		},
		{
			name:       "ok: COPY with absolute destination and no WORKDIR set with quotes",
			dockerfile: "FROM scratch\nCOPY bla.sh \"/usr/local/bin/blubb.sh\"\n",
			wantCount:  0,
		},
		{
			name:       "ok: COPY with absolute destination and no WORKDIR set - windows",
			dockerfile: "FROM scratch\nCOPY bla.sh c:\\system32\\blubb.sh\n",
			wantCount:  0,
		},
		{
			name:       "ok: COPY with absolute destination and no WORKDIR set - windows with quotes",
			dockerfile: "FROM scratch\nCOPY bla.sh \"c:\\system32\\blubb.sh\"\n",
			wantCount:  0,
		},
		{
			name:       "ok: COPY with absolute destination and no WORKDIR set - windows with alternative paths",
			dockerfile: "FROM scratch\nCOPY bla.sh c:/system32/blubb.sh\n",
			wantCount:  0,
		},
		{
			name:       "ok: COPY with relative destination and WORKDIR set",
			dockerfile: "FROM scratch\nWORKDIR /usr\nCOPY bla.sh blubb.sh\n",
			wantCount:  0,
		},
		{
			name:       "ok: COPY with destination being env variable braces",
			dockerfile: "FROM scratch\nCOPY src.sh ${SRC_BASE_ENV}\n",
			wantCount:  0,
		},
		{
			name:       "ok: COPY with destination being env variable dollar",
			dockerfile: "FROM scratch\nCOPY src.sh $SRC_BASE_ENV\n",
			wantCount:  0,
		},
		{
			name:       "ok: COPY with destination being env variable braces in quotes",
			dockerfile: "FROM scratch\nCOPY src.sh \"${SRC_BASE_ENV}\"\n",
			wantCount:  0,
		},
		{
			name:       "ok: COPY with destination being env variable dollar in quotes",
			dockerfile: "FROM scratch\nCOPY src.sh \"$SRC_BASE_ENV\"\n",
			wantCount:  0,
		},

		// --- Cases from Hadolint spec: ruleCatches (expect violations) ---
		{
			name:       "not ok: COPY with relative destination and no WORKDIR set",
			dockerfile: "FROM scratch\nCOPY bla.sh blubb.sh\n",
			wantCount:  1,
		},
		{
			name:       "not ok: COPY with relative destination and no WORKDIR set with quotes",
			dockerfile: "FROM scratch\nCOPY bla.sh \"blubb.sh\"\n",
			wantCount:  1,
		},
		{
			name: "not ok: COPY to relative destination if WORKDIR is set in previous stage but not inherited",
			dockerfile: `FROM debian:buster as stage1
WORKDIR /usr
FROM debian:buster
COPY foo bar
`,
			wantCount: 1,
		},
		{
			name: "not ok: COPY to relative destination if WORKDIR is set in previous stage but not inherited - windows",
			dockerfile: `FROM microsoft/windowsservercore as stage1
WORKDIR c:\system32
FROM microsoft/windowsservercore
COPY foo bar
`,
			wantCount: 1,
		},

		// --- Inheritance cases from Hadolint spec ---
		{
			name: "ok: COPY to relative destination if WORKDIR has been set in base image",
			dockerfile: `FROM debian:buster as base
WORKDIR /usr
FROM debian:buster as stage-in-between
RUN foo
FROM base
COPY foo bar
`,
			wantCount: 0,
		},
		{
			name: "ok: COPY to relative destination if WORKDIR has been set in previous stage deep case",
			dockerfile: `FROM debian:buster as base1
WORKDIR /usr
FROM base1 as base2
RUN foo
FROM base2
COPY foo bar
`,
			wantCount: 0,
		},

		// --- ONBUILD cases from Hadolint spec ---
		{
			name: "ok: COPY to relative destination if WORKDIR has been set both within ONBUILD context",
			dockerfile: `FROM debian:buster
ONBUILD WORKDIR /usr/local/lib
ONBUILD COPY foo bar
`,
			wantCount: 0,
		},
		{
			name:       "not ok: ONBUILD COPY with relative destination and no WORKDIR",
			dockerfile: "FROM debian:buster\nONBUILD COPY foo bar\n",
			wantCount:  1,
		},

		// --- Regression from Hadolint spec ---
		{
			name:       "regression: don't crash with single character paths",
			dockerfile: "FROM scratch\nCOPY a b\n",
			wantCount:  1,
		},

		// --- Additional edge cases ---
		{
			name: "multiple COPY instructions some ok some not",
			dockerfile: `FROM scratch
COPY a /absolute/path
COPY b relative-path
`,
			wantCount: 1,
		},
		{
			name: "WORKDIR set then COPY is ok",
			dockerfile: `FROM ubuntu:22.04
WORKDIR /app
COPY . .
`,
			wantCount: 0,
		},
		{
			name: "COPY before WORKDIR triggers violation",
			dockerfile: `FROM ubuntu:22.04
COPY . .
WORKDIR /app
`,
			wantCount: 1,
		},
		{
			name: "multi-stage with mixed WORKDIR status",
			dockerfile: `FROM ubuntu:22.04 AS builder
WORKDIR /build
COPY . .

FROM alpine:3.18
COPY --from=builder /build/app .
`,
			// Second stage has no WORKDIR set but COPY destination "." is relative
			wantCount: 1,
		},
		{
			name: "COPY to dot in stage with WORKDIR is ok",
			dockerfile: `FROM alpine:3.18
WORKDIR /app
COPY . .
`,
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			input := testutil.MakeLintInput(t, "Dockerfile", tt.dockerfile)

			r := NewDL3045Rule()
			violations := r.Check(input)

			if len(violations) != tt.wantCount {
				t.Errorf("got %d violations, want %d", len(violations), tt.wantCount)
				for _, v := range violations {
					t.Logf("  - %s: %s (line %d)", v.RuleCode, v.Message, v.Location.Start.Line)
				}
			}

			for i, v := range violations {
				if v.RuleCode != rules.HadolintRulePrefix+"DL3045" {
					t.Errorf("violations[%d].RuleCode = %q, want %q", i, v.RuleCode, rules.HadolintRulePrefix+"DL3045")
				}
			}
		})
	}
}

func TestDL3045Rule_Check_FixSuggestion(t *testing.T) {
	t.Parallel()

	input := testutil.MakeLintInput(t, "Dockerfile", "FROM scratch\nCOPY bla.sh blubb.sh\n")
	r := NewDL3045Rule()
	violations := r.Check(input)

	if len(violations) != 1 {
		t.Fatalf("got %d violations, want 1", len(violations))
	}

	fix := violations[0].SuggestedFix
	if fix == nil {
		t.Fatal("expected SuggestedFix, got nil")
	}
	if fix.Safety != rules.FixSuggestion {
		t.Errorf("Safety = %v, want FixSuggestion", fix.Safety)
	}
	if len(fix.Edits) != 1 {
		t.Fatalf("got %d edits, want 1", len(fix.Edits))
	}
	edit := fix.Edits[0]
	if edit.NewText != "WORKDIR /app\n" {
		t.Errorf("NewText = %q, want %q", edit.NewText, "WORKDIR /app\n")
	}
	// The edit should insert at the COPY line (line 2).
	if edit.Location.Start.Line != 2 {
		t.Errorf("edit start line = %d, want 2", edit.Location.Start.Line)
	}
}

func TestDL3045Rule_PlanAsync(t *testing.T) {
	t.Parallel()

	t.Run("plans checks for external images with violations", func(t *testing.T) {
		t.Parallel()
		input := testutil.MakeLintInput(t, "Dockerfile", "FROM alpine:3.18\nCOPY foo bar\n")
		r := NewDL3045Rule()
		requests := r.PlanAsync(input)
		if len(requests) == 0 {
			t.Fatal("expected at least one async request")
		}
		if requests[0].RuleCode != rules.HadolintRulePrefix+"DL3045" {
			t.Errorf("RuleCode = %q, want hadolint/DL3045", requests[0].RuleCode)
		}
	})

	t.Run("no plans when no violations", func(t *testing.T) {
		t.Parallel()
		input := testutil.MakeLintInput(t, "Dockerfile", "FROM alpine:3.18\nWORKDIR /app\nCOPY foo bar\n")
		r := NewDL3045Rule()
		requests := r.PlanAsync(input)
		if len(requests) != 0 {
			t.Errorf("expected no async requests when no violations, got %d", len(requests))
		}
	})

	t.Run("no plans when all destinations absolute", func(t *testing.T) {
		t.Parallel()
		input := testutil.MakeLintInput(t, "Dockerfile", "FROM alpine:3.18\nCOPY foo /bar\n")
		r := NewDL3045Rule()
		requests := r.PlanAsync(input)
		if len(requests) != 0 {
			t.Errorf("expected no async requests when all dests absolute, got %d", len(requests))
		}
	})
}

func TestDL3045Rule_Handler_BaseHasWorkingDir(t *testing.T) {
	t.Parallel()

	h := makeDL3045Handler(t, "FROM alpine:3.18\nCOPY foo bar\n")
	result := h.OnSuccess(&registry.ImageConfig{WorkingDir: "/usr/src/app"})
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	// Should have CompletedCheck (to replace fast-path) + refined violation.
	var hasCompleted, hasViolation bool
	for _, r := range result {
		switch v := r.(type) {
		case async.CompletedCheck:
			if v.StageIndex == 0 {
				hasCompleted = true
			}
		case rules.Violation:
			hasViolation = true
			if v.SuggestedFix == nil {
				t.Fatal("refined violation has no SuggestedFix")
			}
			if v.SuggestedFix.Safety != rules.FixSafe {
				t.Errorf("Safety = %v, want FixSafe (registry-confirmed)", v.SuggestedFix.Safety)
			}
			if len(v.SuggestedFix.Edits) != 1 {
				t.Fatalf("got %d edits, want 1", len(v.SuggestedFix.Edits))
			}
			if want := "WORKDIR /usr/src/app\n"; v.SuggestedFix.Edits[0].NewText != want {
				t.Errorf("NewText = %q, want %q", v.SuggestedFix.Edits[0].NewText, want)
			}
		}
	}
	if !hasCompleted {
		t.Error("expected CompletedCheck for stage 0")
	}
	if !hasViolation {
		t.Error("expected refined violation to be re-emitted")
	}
}

func TestDL3045Rule_Handler_BaseNoWorkingDir(t *testing.T) {
	t.Parallel()

	h := makeDL3045Handler(t, "FROM alpine:3.18\nCOPY foo bar\n")
	result := h.OnSuccess(&registry.ImageConfig{WorkingDir: ""})
	if result != nil {
		t.Errorf("expected nil result when base has no WorkingDir, got %v", result)
	}
}

func TestDL3045Rule_Handler_Descendants(t *testing.T) {
	t.Parallel()

	// Stage 0 (alpine) has violation, stage 1 (FROM base) inherits and has violation too.
	dockerfile := "FROM alpine:3.18 AS base\nCOPY foo bar\nFROM base\nCOPY baz qux\n"
	input := testutil.MakeLintInput(t, "Dockerfile", dockerfile)
	r := NewDL3045Rule()
	meta := r.Metadata()

	// Build hasWorkdir + violations map.
	hasWorkdir := make([]bool, len(input.Stages))
	sem := testutil.GetSemantic(t, input)
	stagesWithViolations := make(map[int]bool)
	for stageIdx, stage := range input.Stages {
		hasWorkdir[stageIdx] = inheritedWorkdir(sem, stageIdx, hasWorkdir)
		for _, cmd := range stage.Commands {
			if v := checkCopyDest(cmd, hasWorkdir[stageIdx], stageIdx, input.File, meta); v != nil {
				stagesWithViolations[stageIdx] = true
			}
		}
	}

	h := &dl3045Handler{
		meta:                 meta,
		file:                 input.File,
		stageIdx:             0,
		semantic:             sem,
		stages:               input.Stages,
		stagesWithViolations: stagesWithViolations,
	}

	result := h.OnSuccess(&registry.ImageConfig{WorkingDir: "/opt"})
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	// Should have CompletedCheck + re-emitted violations for both stages.
	completedStages := make(map[int]bool)
	violationStages := make(map[int]bool)
	for _, r := range result {
		switch v := r.(type) {
		case async.CompletedCheck:
			completedStages[v.StageIndex] = true
		case rules.Violation:
			violationStages[v.StageIndex] = true
			// All refined violations should use the resolved path.
			if v.SuggestedFix == nil || len(v.SuggestedFix.Edits) == 0 {
				t.Errorf("stage %d: missing fix edits", v.StageIndex)
				continue
			}
			if want := "WORKDIR /opt\n"; v.SuggestedFix.Edits[0].NewText != want {
				t.Errorf("stage %d: NewText = %q, want %q", v.StageIndex, v.SuggestedFix.Edits[0].NewText, want)
			}
		}
	}
	if !completedStages[0] {
		t.Error("expected CompletedCheck for stage 0")
	}
	if !completedStages[1] {
		t.Error("expected CompletedCheck for stage 1 (descendant)")
	}
	if !violationStages[0] {
		t.Error("expected refined violation for stage 0")
	}
	if !violationStages[1] {
		t.Error("expected refined violation for stage 1 (descendant)")
	}
}

func makeDL3045Handler(t *testing.T, dockerfile string) *dl3045Handler {
	t.Helper()
	r := NewDL3045Rule()
	input := testutil.MakeLintInput(t, "Dockerfile", dockerfile)
	meta := r.Metadata()
	sem := testutil.GetSemantic(t, input)

	hasWorkdir := make([]bool, len(input.Stages))
	stagesWithViolations := make(map[int]bool)
	for stageIdx, stage := range input.Stages {
		hasWorkdir[stageIdx] = inheritedWorkdir(sem, stageIdx, hasWorkdir)
		for _, cmd := range stage.Commands {
			if v := checkCopyDest(cmd, hasWorkdir[stageIdx], stageIdx, input.File, meta); v != nil {
				stagesWithViolations[stageIdx] = true
			}
		}
	}

	return &dl3045Handler{
		meta:                 meta,
		file:                 input.File,
		stageIdx:             0,
		semantic:             sem,
		stages:               input.Stages,
		stagesWithViolations: stagesWithViolations,
	}
}

func TestIsAbsoluteOrVariableDest(t *testing.T) {
	t.Parallel()
	tests := []struct {
		dest string
		want bool
	}{
		// Absolute Unix paths
		{"/usr/local/bin/foo", true},
		{"/foo", true},

		// Quoted absolute paths
		{"\"/usr/local/bin/foo\"", true},

		// Windows absolute paths
		{"c:\\system32\\foo", true},
		{"D:\\mypath\\foo", true},
		{"c:/system32/foo", true},

		// Quoted Windows paths
		{"\"c:\\system32\\foo\"", true},

		// Environment variables
		{"$SRC_BASE_ENV", true},
		{"${SRC_BASE_ENV}", true},
		{"\"$SRC_BASE_ENV\"", true},
		{"\"${SRC_BASE_ENV}\"", true},

		// Relative paths (should return false)
		{"foo", false},
		{".", false},
		{"./foo", false},
		{"\"foo\"", false},
		{"bar/baz", false},

		// Edge cases
		{"b", false},
		{"a", false},
	}

	for _, tt := range tests {
		t.Run(tt.dest, func(t *testing.T) {
			t.Parallel()
			got := isAbsoluteOrVariableDest(tt.dest)
			if got != tt.want {
				t.Errorf("isAbsoluteOrVariableDest(%q) = %v, want %v", tt.dest, got, tt.want)
			}
		})
	}
}
