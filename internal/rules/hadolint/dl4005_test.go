package hadolint

import (
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/tinovyatkin/tally/internal/rules"
	"github.com/tinovyatkin/tally/internal/testutil"
)

func TestDL4005Rule_Metadata(t *testing.T) {
	t.Parallel()
	snaps.MatchStandaloneJSON(t, NewDL4005Rule().Metadata())
}

func TestDL4005Rule_Check(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		dockerfile string
		wantCount  int
		wantCode   string
	}{
		// --- Cases ported from Hadolint DL4005Spec.hs ---

		// ruleCatches: RUN ln that symlinks /bin/sh
		{
			name: "ln symlink to /bin/sh",
			dockerfile: `FROM ubuntu:22.04
RUN ln -sfv /bin/bash /bin/sh
`,
			wantCount: 1,
			wantCode:  rules.HadolintRulePrefix + "DL4005",
		},

		// ruleCatchesNot: ln with unrelated symlinks
		{
			name: "ln with unrelated target",
			dockerfile: `FROM ubuntu:22.04
RUN ln -sf /bin/true /sbin/initctl
`,
			wantCount: 0,
		},

		// ruleCatchesNot: ln + other commands with /bin/sh as argument to a different command
		{
			name: "ln with unrelated symlink and /bin/sh in different command",
			dockerfile: `FROM ubuntu:22.04
RUN ln -s foo bar && unrelated && something_with /bin/sh
`,
			wantCount: 0,
		},

		// --- Additional test cases ---

		{
			name: "no ln command",
			dockerfile: `FROM ubuntu:22.04
RUN echo hello
`,
			wantCount: 0,
		},

		{
			name: "ln without /bin/sh",
			dockerfile: `FROM ubuntu:22.04
RUN ln -s /usr/bin/python3 /usr/bin/python
`,
			wantCount: 0,
		},

		{
			name: "multi-stage with ln /bin/sh in first stage",
			dockerfile: `FROM ubuntu:22.04 AS builder
RUN ln -sf /bin/bash /bin/sh

FROM alpine:3.18
RUN echo hello
`,
			wantCount: 1,
		},

		{
			name: "multiple RUN instructions with one violation",
			dockerfile: `FROM ubuntu:22.04
RUN apt-get update
RUN ln -sfv /bin/bash /bin/sh
RUN echo done
`,
			wantCount: 1,
		},

		{
			name: "exec form with ln /bin/sh",
			dockerfile: `FROM ubuntu:22.04
RUN ["ln", "-sf", "/bin/bash", "/bin/sh"]
`,
			wantCount: 1,
		},

		{
			name: "ln in pipeline with /bin/sh",
			dockerfile: `FROM ubuntu:22.04
RUN apt-get update && ln -sf /bin/bash /bin/sh
`,
			wantCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			input := testutil.MakeLintInput(t, "Dockerfile", tt.dockerfile)

			r := NewDL4005Rule()
			violations := r.Check(input)

			if len(violations) != tt.wantCount {
				t.Errorf("got %d violations, want %d", len(violations), tt.wantCount)
				for _, v := range violations {
					t.Logf("  - %s: %s", v.RuleCode, v.Message)
				}
			}

			if tt.wantCode != "" && len(violations) > 0 {
				if violations[0].RuleCode != tt.wantCode {
					t.Errorf("RuleCode = %q, want %q", violations[0].RuleCode, tt.wantCode)
				}
			}
		})
	}
}

func TestDL4005Rule_Fix(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		dockerfile string
		wantFix    string
	}{
		{
			name: "standalone ln replaced with SHELL",
			dockerfile: `FROM ubuntu:22.04
RUN ln -sfv /bin/bash /bin/sh
`,
			wantFix: `SHELL ["/bin/bash", "-c"]`,
		},
		{
			name: "ln at end of chain",
			dockerfile: `FROM ubuntu:22.04
RUN apt-get update && ln -sf /bin/bash /bin/sh
`,
			wantFix: "RUN apt-get update\nSHELL [\"/bin/bash\", \"-c\"]",
		},
		{
			name: "ln at start of chain",
			dockerfile: `FROM ubuntu:22.04
RUN ln -sf /bin/bash /bin/sh && echo done
`,
			wantFix: "SHELL [\"/bin/bash\", \"-c\"]\nRUN echo done",
		},
		{
			name: "ln in middle of chain",
			dockerfile: `FROM ubuntu:22.04
RUN apt-get update && ln -sf /bin/bash /bin/sh && echo done
`,
			wantFix: "RUN apt-get update\nSHELL [\"/bin/bash\", \"-c\"]\nRUN echo done",
		},
		{
			name: "exec form has no fix",
			dockerfile: `FROM ubuntu:22.04
RUN ["ln", "-sf", "/bin/bash", "/bin/sh"]
`,
			wantFix: "",
		},
		{
			name: "semicolon separated has no fix",
			dockerfile: `FROM ubuntu:22.04
RUN ln -sf /bin/bash /bin/sh; echo done
`,
			wantFix: "",
		},
		{
			name: "semicolon with chain has no fix",
			dockerfile: `FROM ubuntu:22.04
RUN apt-get update && ln -sf /bin/bash /bin/sh; echo done
`,
			wantFix: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			input := testutil.MakeLintInput(t, "Dockerfile", tt.dockerfile)

			r := NewDL4005Rule()
			violations := r.Check(input)

			if len(violations) == 0 {
				t.Fatal("expected at least one violation")
			}

			v := violations[0]
			if tt.wantFix == "" {
				if v.SuggestedFix != nil {
					t.Errorf("expected no fix, got: %q", v.SuggestedFix.Edits[0].NewText)
				}
				return
			}

			if v.SuggestedFix == nil {
				t.Fatal("expected a suggested fix, got nil")
			}

			if len(v.SuggestedFix.Edits) != 1 {
				t.Fatalf("expected 1 edit, got %d", len(v.SuggestedFix.Edits))
			}

			got := v.SuggestedFix.Edits[0].NewText
			if got != tt.wantFix {
				t.Errorf("fix text mismatch\ngot:  %q\nwant: %q", got, tt.wantFix)
			}

			if v.SuggestedFix.Safety != rules.FixSuggestion {
				t.Errorf("fix safety = %v, want FixSuggestion", v.SuggestedFix.Safety)
			}
		})
	}
}
