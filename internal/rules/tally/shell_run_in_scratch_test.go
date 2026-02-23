package tally

import (
	"strings"
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/testutil"
)

func TestShellRunInScratchRule_Metadata(t *testing.T) {
	t.Parallel()
	snaps.MatchStandaloneJSON(t, NewShellRunInScratchRule().Metadata())
}

func TestShellRunInScratchRule_NoSemantic(t *testing.T) {
	t.Parallel()
	input := testutil.MakeLintInput(t, "Dockerfile", `FROM scratch
RUN echo "hello"
`)
	r := NewShellRunInScratchRule()
	violations := r.Check(input)

	if len(violations) != 0 {
		t.Errorf("expected 0 violations without semantic model, got %d", len(violations))
	}
}

func TestShellRunInScratchRule_ShellFormRUN(t *testing.T) {
	t.Parallel()
	content := `FROM scratch
RUN echo "hello"
`
	input := testutil.MakeLintInputWithSemantic(t, "Dockerfile", content)
	r := NewShellRunInScratchRule()
	violations := r.Check(input)

	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}

	v := violations[0]

	t.Run("detection", func(t *testing.T) {
		t.Parallel()
		if v.RuleCode != ShellRunInScratchRuleCode {
			t.Errorf("expected rule code %q, got %q", ShellRunInScratchRuleCode, v.RuleCode)
		}
		if v.Severity != rules.SeverityWarning {
			t.Errorf("expected warning severity, got %v", v.Severity)
		}
		if !strings.Contains(v.Message, "shell-form RUN") {
			t.Errorf("message should mention shell-form RUN, got %q", v.Message)
		}
		if !strings.Contains(v.Message, "/bin/sh") {
			t.Errorf("message should mention /bin/sh, got %q", v.Message)
		}
	})

	t.Run("location", func(t *testing.T) {
		t.Parallel()
		// Location should point to the RUN instruction (line 2).
		if v.Location.Start.Line != 2 {
			t.Errorf("expected location line 2, got %d", v.Location.Start.Line)
		}
	})

	t.Run("metadata", func(t *testing.T) {
		t.Parallel()
		if v.Detail == "" {
			t.Error("expected violation to have detail")
		}
		if v.DocURL == "" {
			t.Error("expected violation to have DocURL")
		}
	})
}

func TestShellRunInScratchRule_ExecFormRUN(t *testing.T) {
	t.Parallel()
	content := `FROM scratch
RUN ["echo", "hello"]
`
	input := testutil.MakeLintInputWithSemantic(t, "Dockerfile", content)
	r := NewShellRunInScratchRule()
	violations := r.Check(input)

	if len(violations) != 0 {
		t.Errorf("expected 0 violations for exec-form RUN in scratch, got %d", len(violations))
	}
}

func TestShellRunInScratchRule_NonScratchStage(t *testing.T) {
	t.Parallel()
	content := `FROM alpine:3.19
RUN echo "hello"
`
	input := testutil.MakeLintInputWithSemantic(t, "Dockerfile", content)
	r := NewShellRunInScratchRule()
	violations := r.Check(input)

	if len(violations) != 0 {
		t.Errorf("expected 0 violations for non-scratch stage, got %d", len(violations))
	}
}

func TestShellRunInScratchRule_MultipleScratchStages(t *testing.T) {
	t.Parallel()
	content := `FROM scratch AS first
RUN echo "first"

FROM scratch AS second
RUN echo "second"

FROM alpine:3.19
RUN echo "ok"
`
	input := testutil.MakeLintInputWithSemantic(t, "Dockerfile", content)
	r := NewShellRunInScratchRule()
	violations := r.Check(input)

	if len(violations) != 2 {
		t.Fatalf("expected 2 violations for two scratch stages with shell-form RUN, got %d", len(violations))
	}
}

func TestShellRunInScratchRule_MixedRunForms(t *testing.T) {
	t.Parallel()
	content := `FROM scratch
RUN echo "shell form"
RUN ["echo", "exec form"]
`
	input := testutil.MakeLintInputWithSemantic(t, "Dockerfile", content)
	r := NewShellRunInScratchRule()
	violations := r.Check(input)

	if len(violations) != 1 {
		t.Fatalf("expected 1 violation for mixed run forms, got %d", len(violations))
	}

	// Should be the shell-form RUN on line 2.
	if violations[0].Location.Start.Line != 2 {
		t.Errorf("expected violation at line 2, got %d", violations[0].Location.Start.Line)
	}
}

func TestShellRunInScratchRule_WithExplicitSHELL(t *testing.T) {
	t.Parallel()
	content := `FROM scratch
SHELL ["/busybox", "sh", "-c"]
RUN echo "hello"
`
	input := testutil.MakeLintInputWithSemantic(t, "Dockerfile", content)
	r := NewShellRunInScratchRule()
	violations := r.Check(input)

	if len(violations) != 0 {
		t.Errorf("expected 0 violations when SHELL is explicitly set before RUN, got %d", len(violations))
	}
}

func TestShellRunInScratchRule_ShellAfterRUN(t *testing.T) {
	t.Parallel()
	// RUN before SHELL should be flagged; RUN after SHELL should not.
	content := `FROM scratch
RUN echo "fail before shell"
SHELL ["/busybox", "sh", "-c"]
RUN echo "ok after shell"
`
	input := testutil.MakeLintInputWithSemantic(t, "Dockerfile", content)
	r := NewShellRunInScratchRule()
	violations := r.Check(input)

	if len(violations) != 1 {
		t.Fatalf("expected 1 violation for RUN before SHELL in scratch, got %d", len(violations))
	}
	if violations[0].Location.Start.Line != 2 {
		t.Errorf("expected violation at line 2, got %d", violations[0].Location.Start.Line)
	}
}

func TestShellRunInScratchRule_NamedScratchStage(t *testing.T) {
	t.Parallel()
	content := `FROM scratch AS minimal
RUN echo "fail"
`
	input := testutil.MakeLintInputWithSemantic(t, "Dockerfile", content)
	r := NewShellRunInScratchRule()
	violations := r.Check(input)

	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}

	if !strings.Contains(violations[0].Message, "minimal") {
		t.Errorf("message should mention stage name, got %q", violations[0].Message)
	}
}

func TestShellRunInScratch_CrossRule_WithCopyFrom(t *testing.T) {
	t.Parallel()
	// When a scratch stage has a shell-form RUN and is referenced by COPY --from,
	// shell-run-in-scratch should fire (no shell) but copy-from-empty-scratch-stage
	// should NOT fire (RUN counts as file-producing).
	content := `FROM scratch AS builder
RUN echo "will fail"

FROM alpine:3.19
COPY --from=builder /out /out
`
	input := testutil.MakeLintInputWithSemantic(t, "Dockerfile", content)

	shellViolations := NewShellRunInScratchRule().Check(input)
	copyViolations := NewCopyFromEmptyScratchStageRule().Check(input)

	if len(shellViolations) != 1 {
		t.Errorf("expected 1 shell-run-in-scratch violation, got %d", len(shellViolations))
	}
	if len(copyViolations) != 0 {
		t.Errorf("expected 0 copy-from-empty-scratch-stage violations, got %d", len(copyViolations))
	}
}
