package hadolint

import (
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/tinovyatkin/tally/internal/async"
	"github.com/tinovyatkin/tally/internal/registry"
	"github.com/tinovyatkin/tally/internal/rules"
	"github.com/tinovyatkin/tally/internal/testutil"
)

func TestDL3057Rule_Metadata(t *testing.T) {
	t.Parallel()
	snaps.MatchStandaloneJSON(t, NewDL3057Rule().Metadata())
}

func TestDL3057Rule_Check(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		dockerfile string
		wantCount  int
		wantCode   string
	}{
		// === Fast-path tests (static, no registry) ===
		{
			name: "warn with no HEALTHCHECK instructions",
			dockerfile: `FROM scratch
`,
			wantCount: 1,
			wantCode:  rules.HadolintRulePrefix + "DL3057",
		},
		{
			name: "ok with one HEALTHCHECK CMD instruction",
			dockerfile: `FROM scratch
HEALTHCHECK CMD /bin/bla
`,
			wantCount: 0,
		},
		{
			name: "ok with inheriting HEALTHCHECK CMD instruction",
			dockerfile: `FROM scratch AS base
HEALTHCHECK CMD /bin/bla
FROM base
`,
			wantCount: 0,
		},
		{
			name: "single file-level violation for multi-stage without HEALTHCHECK CMD",
			dockerfile: `FROM alpine:3.18 AS builder
RUN echo "build"

FROM debian:bookworm
RUN echo "run"
`,
			wantCount: 1, // Single file-level violation, not per-stage
		},
		{
			name: "HEALTHCHECK NONE alone still triggers missing violation",
			dockerfile: `FROM scratch
HEALTHCHECK NONE
`,
			wantCount: 1, // NONE doesn't count as CMD; async resolves whether base has HC
		},
		{
			name: "HEALTHCHECK CMD in any stage suppresses all",
			dockerfile: `FROM alpine:3.18 AS builder
RUN echo "build"

FROM debian:bookworm
HEALTHCHECK CMD curl -f http://localhost/
`,
			wantCount: 0,
		},
		{
			name: "chain with HEALTHCHECK CMD in middle",
			dockerfile: `FROM scratch AS base
FROM base AS middle
HEALTHCHECK CMD /bin/check
FROM middle AS final
`,
			wantCount: 0,
		},
		{
			name: "parallel branches both with HEALTHCHECK CMD",
			dockerfile: `FROM scratch AS base
FROM base AS branch1
HEALTHCHECK CMD /bin/check1
FROM base AS branch2
HEALTHCHECK CMD /bin/check2
`,
			wantCount: 0,
		},
		{
			name: "HEALTHCHECK with interval options",
			dockerfile: `FROM alpine:3.18
HEALTHCHECK --interval=30s CMD curl -f http://localhost/ || exit 1
`,
			wantCount: 0,
		},
		{
			name: "HEALTHCHECK CMD followed by HEALTHCHECK NONE uses last instruction",
			dockerfile: `FROM scratch
HEALTHCHECK CMD /bin/check
HEALTHCHECK NONE
`,
			wantCount: 1, // Last HEALTHCHECK is NONE, so violation
		},
		{
			name: "HEALTHCHECK NONE followed by HEALTHCHECK CMD uses last instruction",
			dockerfile: `FROM scratch
HEALTHCHECK NONE
HEALTHCHECK CMD /bin/check
`,
			wantCount: 0, // Last HEALTHCHECK is CMD, so no violation
		},
		{
			name: "ONBUILD HEALTHCHECK does not satisfy healthcheck requirement",
			dockerfile: `FROM scratch
ONBUILD HEALTHCHECK CMD /bin/check
`,
			wantCount: 1, // ONBUILD triggers in child images, not this one
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			input := testutil.MakeLintInputWithSemantic(t, "Dockerfile", tt.dockerfile)

			r := NewDL3057Rule()
			violations := r.Check(input)

			if len(violations) != tt.wantCount {
				t.Errorf("got %d violations, want %d", len(violations), tt.wantCount)
				for _, v := range violations {
					t.Logf("  - %s at line %d: %s", v.RuleCode, v.Location.Start.Line, v.Message)
				}
			}

			if tt.wantCode != "" && len(violations) > 0 {
				if violations[0].RuleCode != tt.wantCode {
					t.Errorf("RuleCode = %q, want %q", violations[0].RuleCode, tt.wantCode)
				}
			}

			// Verify file-level violation uses StageIndex=-1
			if tt.wantCount > 0 && len(violations) > 0 {
				if violations[0].StageIndex != -1 {
					t.Errorf("StageIndex = %d, want -1 (file-level)", violations[0].StageIndex)
				}
				if !violations[0].Location.IsFileLevel() {
					t.Error("expected file-level location")
				}
			}
		})
	}
}

func TestDL3057Rule_RequiresSemantic(t *testing.T) {
	t.Parallel()
	// Without semantic model, the rule should return no violations
	input := testutil.MakeLintInput(t, "Dockerfile", "FROM scratch\n")
	r := NewDL3057Rule()
	violations := r.Check(input)
	if len(violations) != 0 {
		t.Errorf("expected 0 violations without semantic model, got %d", len(violations))
	}
}

func TestDL3057Rule_PlanAsync(t *testing.T) {
	t.Parallel()

	t.Run("plans checks for external images", func(t *testing.T) {
		t.Parallel()
		input := testutil.MakeLintInputWithSemantic(t, "Dockerfile", "FROM alpine:3.18\nRUN echo hello\n")
		r := NewDL3057Rule()
		requests := r.PlanAsync(input)
		if len(requests) == 0 {
			t.Fatal("expected at least one async request")
		}
		if requests[0].RuleCode != rules.HadolintRulePrefix+"DL3057" {
			t.Errorf("RuleCode = %q, want hadolint/DL3057", requests[0].RuleCode)
		}
	})

	t.Run("no plans when HEALTHCHECK CMD present", func(t *testing.T) {
		t.Parallel()
		input := testutil.MakeLintInputWithSemantic(t, "Dockerfile", "FROM alpine:3.18\nHEALTHCHECK CMD curl -f http://localhost/\n")
		r := NewDL3057Rule()
		requests := r.PlanAsync(input)
		if len(requests) != 0 {
			t.Errorf("expected no async requests when HEALTHCHECK CMD present, got %d", len(requests))
		}
	})
}

func makeHandler(t *testing.T, dockerfile string) *healthcheckHandler {
	t.Helper()
	r := NewDL3057Rule()
	input := testutil.MakeLintInputWithSemantic(t, "Dockerfile", dockerfile)
	return &healthcheckHandler{
		meta:     r.Metadata(),
		file:     input.File,
		stageIdx: 0,
		semantic: testutil.GetSemantic(t, input),
		stages:   input.Stages,
	}
}

func TestDL3057Rule_Handler_BaseHasHealthcheck(t *testing.T) {
	t.Parallel()
	h := makeHandler(t, "FROM alpine:3.18\nRUN echo hello\n")
	result := h.OnSuccess(&registry.ImageConfig{HasHealthcheck: true})
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	// Should contain a CompletedCheck with StageIndex=-1
	if !hasCompletedCheckAtStage(result, -1) {
		t.Error("expected CompletedCheck with StageIndex=-1")
	}
	// Should have no violations
	if hasAnyViolation(result) {
		t.Error("expected no violations when base has healthcheck")
	}
}

func TestDL3057Rule_Handler_BaseNoHealthcheck(t *testing.T) {
	t.Parallel()
	h := makeHandler(t, "FROM alpine:3.18\nRUN echo hello\n")
	result := h.OnSuccess(&registry.ImageConfig{HasHealthcheck: false})
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	// Should NOT contain a CompletedCheck with StageIndex=-1 (fast violation stays)
	if hasCompletedCheckAtStage(result, -1) {
		t.Error("should not suppress fast violation when base has no healthcheck")
	}
}

func TestDL3057Rule_Handler_UselessHealthcheckNone(t *testing.T) {
	t.Parallel()
	h := makeHandler(t, "FROM alpine:3.18\nHEALTHCHECK NONE\n")
	result := h.OnSuccess(&registry.ImageConfig{HasHealthcheck: false})
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if !hasAnyViolation(result) {
		t.Error("expected violation about useless HEALTHCHECK NONE")
	}
	if !hasCompletedCheckAtStage(result, -1) {
		t.Error("expected CompletedCheck(-1) to replace generic missing with specific NONE violation")
	}
	for _, item := range result {
		if v, ok := item.(rules.Violation); ok {
			if v.Message != "`HEALTHCHECK NONE` has no effect: base image has no health check to disable" {
				t.Errorf("unexpected message: %s", v.Message)
			}
		}
	}
}

func TestDL3057Rule_Handler_NilConfig(t *testing.T) {
	t.Parallel()
	h := makeHandler(t, "FROM alpine:3.18\n")
	if result := h.OnSuccess((*registry.ImageConfig)(nil)); result != nil {
		t.Errorf("expected nil for nil config, got %v", result)
	}
}

func TestDL3057Rule_Handler_WrongType(t *testing.T) {
	t.Parallel()
	h := makeHandler(t, "FROM alpine:3.18\n")
	if result := h.OnSuccess("not an ImageConfig"); result != nil {
		t.Errorf("expected nil for wrong type, got %v", result)
	}
}

// hasCompletedCheckAtStage checks if any item in the result is a CompletedCheck
// with the given StageIndex.
func hasCompletedCheckAtStage(result []any, stageIdx int) bool {
	for _, item := range result {
		if cc, ok := item.(async.CompletedCheck); ok && cc.StageIndex == stageIdx {
			return true
		}
	}
	return false
}

// hasAnyViolation checks if any item in the result is a Violation.
func hasAnyViolation(result []any) bool {
	for _, item := range result {
		if _, ok := item.(rules.Violation); ok {
			return true
		}
	}
	return false
}
