package hadolint

import (
	"strings"
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/wharflab/tally/internal/ai/autofixdata"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/testutil"
)

func TestDL4001Rule_Metadata(t *testing.T) {
	t.Parallel()
	snaps.MatchStandaloneJSON(t, NewDL4001Rule().Metadata())
}

func TestDL4001Rule_Check(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		dockerfile string
		wantCount  int
		wantCode   string
	}{
		{
			name: "only wget is fine",
			dockerfile: `FROM ubuntu:22.04
RUN apt-get update && apt-get install -y wget
RUN wget https://example.com/file
`,
			wantCount: 0,
		},
		{
			name: "only curl is fine",
			dockerfile: `FROM ubuntu:22.04
RUN apt-get update && apt-get install -y curl
RUN curl -o file https://example.com/file
`,
			wantCount: 0,
		},
		{
			name: "both wget and curl",
			dockerfile: `FROM ubuntu:22.04
RUN apt-get update && apt-get install -y wget
RUN wget https://example.com/file1
RUN curl -o file2 https://example.com/file2
`,
			wantCount: 1, // One violation for curl
			wantCode:  rules.HadolintRulePrefix + "DL4001",
		},
		{
			name: "wget and curl in same RUN",
			dockerfile: `FROM ubuntu:22.04
RUN wget https://example.com/file1 && curl https://example.com/file2
`,
			wantCount: 1,
		},
		{
			name: "no wget or curl",
			dockerfile: `FROM ubuntu:22.04
RUN apt-get update && apt-get install -y vim
`,
			wantCount: 0,
		},
		{
			name: "wget-like package name",
			dockerfile: `FROM ubuntu:22.04
RUN apt-get install -y curl
RUN apt-get install -y wget-doc
`,
			wantCount: 0, // wget-doc is not wget
		},
		{
			name: "multi-stage both tools",
			dockerfile: `FROM ubuntu:22.04 AS builder
RUN wget https://example.com/source.tar.gz

FROM alpine:3.18
RUN curl -o /tmp/file https://example.com/file
`,
			wantCount: 1, // curl is flagged
		},
		{
			name: "wget with full path",
			dockerfile: `FROM ubuntu:22.04
RUN /usr/bin/wget https://example.com/file1
RUN curl https://example.com/file2
`,
			wantCount: 1,
		},
		{
			name: "multiple curl usages flagged",
			dockerfile: `FROM ubuntu:22.04
RUN wget https://example.com/file1
RUN curl https://example.com/file2
RUN curl https://example.com/file3
`,
			wantCount: 2, // Both curl usages are flagged
		},
		// Tests from hadolint/hadolint test/Hadolint/Rule/DL4001Spec.hs
		{
			name: "different tools in different stages - hadolint allows this",
			dockerfile: `FROM node as foo
RUN wget my.xyz

FROM scratch
RUN curl localhost
`,
			// Note: Hadolint says this should NOT warn (different stages)
			// Our implementation is stricter - we warn because it's still
			// inconsistent across the build. Uncomment wantCount: 0 to match hadolint.
			wantCount: 1, // We flag curl (stricter than hadolint)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			input := testutil.MakeLintInput(t, "Dockerfile", tt.dockerfile)

			r := NewDL4001Rule()
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

func TestDL4001Rule_Check_SmartMessages(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name            string
		dockerfile      string
		wantCount       int
		wantMsgContains string
	}{
		{
			name: "curl installed, wget used - recommend curl",
			dockerfile: `FROM ubuntu:22.04
RUN apt-get update && apt-get install -y curl
RUN curl https://example.com/file1
RUN wget https://example.com/file2
`,
			wantCount:       1,
			wantMsgContains: "curl is installed",
		},
		{
			name: "wget installed, curl used - recommend wget",
			dockerfile: `FROM ubuntu:22.04
RUN apt-get update && apt-get install -y wget
RUN wget https://example.com/file1
RUN curl https://example.com/file2
`,
			wantCount:       1,
			wantMsgContains: "wget is installed",
		},
		{
			name: "both installed - mention both",
			dockerfile: `FROM ubuntu:22.04
RUN apt-get update && apt-get install -y curl wget
RUN curl https://example.com/file1
RUN wget https://example.com/file2
`,
			wantCount:       1,
			wantMsgContains: "both wget and curl are installed",
		},
		{
			name: "neither installed - generic message",
			dockerfile: `FROM ubuntu:22.04
RUN curl https://example.com/file1
RUN wget https://example.com/file2
`,
			wantCount:       1,
			wantMsgContains: "both wget and curl are used",
		},
		{
			name: "apk add curl, wget used",
			dockerfile: `FROM alpine:3.18
RUN apk add --no-cache curl
RUN curl https://example.com/file1
RUN wget https://example.com/file2
`,
			wantCount:       1,
			wantMsgContains: "curl is installed",
		},
		// Test case from benchmark real-world Dockerfile pattern
		{
			name: "benchmark pattern: curl and wget in same install with flags",
			dockerfile: `FROM ubuntu:22.04
RUN apt-get update && apt-get install -y --no-install-recommends curl ca-certificates wget && apt-get clean
RUN curl -L -o /tmp/file.sh https://example.com/file.sh
RUN wget https://example.com/another-file
`,
			wantCount:       1,
			wantMsgContains: "both wget and curl are installed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			input := testutil.MakeLintInput(t, "Dockerfile", tt.dockerfile)

			r := NewDL4001Rule()
			violations := r.Check(input)

			if len(violations) != tt.wantCount {
				t.Errorf("got %d violations, want %d", len(violations), tt.wantCount)
				for _, v := range violations {
					t.Logf("  - %s: %s", v.RuleCode, v.Message)
				}
				return
			}

			if tt.wantMsgContains != "" && len(violations) > 0 {
				if !strings.Contains(violations[0].Message, tt.wantMsgContains) {
					t.Errorf("Message %q should contain %q", violations[0].Message, tt.wantMsgContains)
				}
			}
		})
	}
}

//nolint:gocognit,nestif // The table covers both sync and async fix contracts in one place.
func TestDL4001Rule_SuggestedFix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		dockerfile       string
		wantFix          bool
		wantNeedsResolve bool
		wantNewText      string
		wantLoc          rules.Location
	}{
		{
			name: "rewrite wget remote file to curl when curl is installed",
			dockerfile: `FROM ubuntu:22.04
RUN apt-get update && apt-get install -y curl
RUN curl -fsSL https://example.com/bootstrap.tgz
RUN wget https://example.com/file.tgz
`,
			wantFix:          true,
			wantNeedsResolve: false,
			wantNewText:      "curl -fL -O https://example.com/file.tgz",
			wantLoc:          rules.NewRangeLocation("Dockerfile", 4, len("RUN "), 4, len("RUN wget https://example.com/file.tgz")),
		},
		{
			name: "rewrite piped curl stdout to wget stdout when wget is installed",
			dockerfile: `FROM ubuntu:22.04
RUN apt-get update && apt-get install -y wget
RUN wget https://example.com/bootstrap.tgz
RUN curl -fsSL https://example.com/app.tgz | tar -xz -C /opt
`,
			wantFix:          true,
			wantNeedsResolve: false,
			wantNewText:      "wget -nv -O- https://example.com/app.tgz",
			wantLoc:          rules.NewRangeLocation("Dockerfile", 4, len("RUN "), 4, len("RUN curl -fsSL https://example.com/app.tgz")),
		},
		{
			name: "use ai fallback for curl without redirect-following to wget",
			dockerfile: `FROM ubuntu:22.04
RUN apt-get update && apt-get install -y wget
RUN wget https://example.com/bootstrap.tgz
RUN curl -fsS -o /tmp/file https://example.com/file
`,
			wantFix:          true,
			wantNeedsResolve: true,
		},
		{
			name: "use ai fallback for curl without fail-on-http-status to wget",
			dockerfile: `FROM ubuntu:22.04
RUN apt-get update && apt-get install -y wget
RUN wget https://example.com/bootstrap.tgz
RUN curl -sSL https://example.com/app.tgz | tar -xz -C /opt
`,
			wantFix:          true,
			wantNeedsResolve: true,
		},
		{
			name: "do not rewrite when the preferred tool already appears in the same run",
			dockerfile: `FROM ubuntu:22.04
RUN apt-get update && apt-get install -y wget
RUN (curl -Ls https://example.com/install.sh || wget -qO- https://example.com/install.sh) | sh
`,
			wantFix: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			input := testutil.MakeLintInput(t, "Dockerfile", tt.dockerfile)
			violations := NewDL4001Rule().Check(input)
			if len(violations) != 1 {
				t.Fatalf("got %d violations, want 1", len(violations))
			}

			v := violations[0]
			if tt.wantFix {
				if v.SuggestedFix == nil {
					t.Fatal("expected SuggestedFix")
				}
				if v.SuggestedFix.Safety != rules.FixUnsafe {
					t.Fatalf("fix safety = %v, want %v", v.SuggestedFix.Safety, rules.FixUnsafe)
				}
				if v.SuggestedFix.NeedsResolve != tt.wantNeedsResolve {
					t.Fatalf("NeedsResolve = %v, want %v", v.SuggestedFix.NeedsResolve, tt.wantNeedsResolve)
				}
				if tt.wantNeedsResolve {
					if v.SuggestedFix.ResolverID != autofixdata.ResolverID {
						t.Fatalf("ResolverID = %q, want %q", v.SuggestedFix.ResolverID, autofixdata.ResolverID)
					}
					if len(v.SuggestedFix.Edits) != 0 {
						t.Fatalf("expected no immediate edits for async fix, got %d", len(v.SuggestedFix.Edits))
					}
					req, ok := v.SuggestedFix.ResolverData.(*autofixdata.ObjectiveRequest)
					if !ok || req == nil {
						t.Fatalf("expected ObjectiveRequest resolver data, got %T", v.SuggestedFix.ResolverData)
					}
					if req.Kind != autofixdata.ObjectiveCommandFamilyNormalize {
						t.Fatalf("ObjectiveRequest.Kind = %q, want %q", req.Kind, autofixdata.ObjectiveCommandFamilyNormalize)
					}
					got, ok := req.Facts["preferred-tool"].(string)
					if !ok || got == "" {
						t.Fatal("expected preferred-tool fact for async fix")
					}
				} else {
					if len(v.SuggestedFix.Edits) != 1 {
						t.Fatalf("expected 1 edit, got %d", len(v.SuggestedFix.Edits))
					}
					edit := v.SuggestedFix.Edits[0]
					if got := edit.NewText; got != tt.wantNewText {
						t.Fatalf("edit NewText = %q, want %q", got, tt.wantNewText)
					}
					if edit.Location != tt.wantLoc {
						t.Fatalf("edit Location = %#v, want %#v", edit.Location, tt.wantLoc)
					}
				}
			} else if v.SuggestedFix != nil {
				t.Fatalf("expected no SuggestedFix, got %+v", v.SuggestedFix)
			}
		})
	}
}

func TestDL4001Rule_FixPreference(t *testing.T) {
	t.Parallel()

	const dockerfileCurlInstalled = `FROM ubuntu:22.04
RUN apt-get update && apt-get install -y curl
RUN curl -fsSL https://example.com/bootstrap.tgz
RUN wget https://example.com/file.tgz
`

	const dockerfileWgetInstalled = `FROM ubuntu:22.04
RUN apt-get update && apt-get install -y wget
RUN wget https://example.com/bootstrap.tgz
RUN curl -fsSL https://example.com/app.tgz
`

	tests := []struct {
		name        string
		dockerfile  string
		config      any
		wantNewText string
		wantLoc     rules.Location
	}{
		{
			name:        "auto infers curl when curl is installed",
			dockerfile:  dockerfileCurlInstalled,
			config:      DL4001Config{FixPreference: DL4001FixPreferenceAuto},
			wantNewText: "curl -fL -O https://example.com/file.tgz",
			wantLoc: rules.NewRangeLocation(
				"Dockerfile", 4, len("RUN "),
				4, len("RUN wget https://example.com/file.tgz"),
			),
		},
		{
			name:        "explicit curl preference overrides wget install signal",
			dockerfile:  dockerfileWgetInstalled,
			config:      DL4001Config{FixPreference: DL4001FixPreferenceCurl},
			wantNewText: "curl -fL -O https://example.com/bootstrap.tgz",
			wantLoc: rules.NewRangeLocation(
				"Dockerfile", 3, len("RUN "),
				3, len("RUN wget https://example.com/bootstrap.tgz"),
			),
		},
		{
			name:        "explicit wget preference overrides curl install signal",
			dockerfile:  dockerfileCurlInstalled,
			config:      DL4001Config{FixPreference: DL4001FixPreferenceWget},
			wantNewText: "wget -nv -O- https://example.com/bootstrap.tgz",
			wantLoc: rules.NewRangeLocation(
				"Dockerfile", 3, len("RUN "),
				3, len("RUN curl -fsSL https://example.com/bootstrap.tgz"),
			),
		},
		{
			name:        "map-form config routed through schema coercion",
			dockerfile:  dockerfileCurlInstalled,
			config:      map[string]any{"fix-preference": "wget"},
			wantNewText: "wget -nv -O- https://example.com/bootstrap.tgz",
			wantLoc: rules.NewRangeLocation(
				"Dockerfile", 3, len("RUN "),
				3, len("RUN curl -fsSL https://example.com/bootstrap.tgz"),
			),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			input := testutil.MakeLintInputWithConfig(t, "Dockerfile", tt.dockerfile, tt.config)

			violations := NewDL4001Rule().Check(input)
			if len(violations) != 1 {
				t.Fatalf("got %d violations, want 1", len(violations))
			}
			v := violations[0]
			if v.SuggestedFix == nil {
				t.Fatal("expected SuggestedFix")
			}
			if v.SuggestedFix.NeedsResolve {
				t.Fatalf("NeedsResolve = true, want deterministic fix")
			}
			if len(v.SuggestedFix.Edits) != 1 {
				t.Fatalf("expected 1 edit, got %d", len(v.SuggestedFix.Edits))
			}
			edit := v.SuggestedFix.Edits[0]
			if edit.NewText != tt.wantNewText {
				t.Fatalf("edit NewText = %q, want %q", edit.NewText, tt.wantNewText)
			}
			if edit.Location != tt.wantLoc {
				t.Fatalf("edit Location = %#v, want %#v", edit.Location, tt.wantLoc)
			}
		})
	}
}

func TestDL4001Rule_WindowsLoweringAddsExeSuffix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		dockerfile  string
		wantNewText string
		wantLoc     rules.Location
	}{
		{
			name: "pwsh on windows stage lowers to wget.exe",
			dockerfile: `FROM mcr.microsoft.com/windows/servercore:ltsc2022
SHELL ["pwsh", "-Command"]
RUN curl -fL -o /tmp/boot.tgz https://example.com/bootstrap.tgz
RUN wget.exe https://example.com/file.tgz
`,
			wantNewText: "wget.exe -O /tmp/boot.tgz https://example.com/bootstrap.tgz",
			wantLoc: rules.NewRangeLocation(
				"Dockerfile", 3, len("RUN "),
				3, len("RUN curl -fL -o /tmp/boot.tgz https://example.com/bootstrap.tgz"),
			),
		},
		{
			name: "cmd on windows stage lowers to curl.exe",
			dockerfile: `FROM mcr.microsoft.com/windows/servercore:ltsc2022
SHELL ["cmd", "/S", "/C"]
RUN wget.exe https://example.com/bootstrap.tgz
RUN curl.exe -fL -O https://example.com/file.tgz
`,
			// With explicit cmd shell and neither tool declared installed, the rule prefers wget
			// across stages — but here we test the same-stage path: the offending tool is curl.
			wantNewText: "wget.exe https://example.com/file.tgz",
			wantLoc: rules.NewRangeLocation(
				"Dockerfile", 4, len("RUN "),
				4, len("RUN curl.exe -fL -O https://example.com/file.tgz"),
			),
		},
		{
			name: "linux stage keeps bare tool name",
			dockerfile: `FROM ubuntu:22.04
RUN apt-get install -y curl
RUN curl -fsSL https://example.com/bootstrap.tgz
RUN wget https://example.com/file.tgz
`,
			wantNewText: "curl -fL -O https://example.com/file.tgz",
			wantLoc: rules.NewRangeLocation(
				"Dockerfile", 4, len("RUN "),
				4, len("RUN wget https://example.com/file.tgz"),
			),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			input := testutil.MakeLintInput(t, "Dockerfile", tt.dockerfile)

			violations := NewDL4001Rule().Check(input)
			if len(violations) != 1 {
				t.Fatalf("got %d violations, want 1", len(violations))
			}
			v := violations[0]
			if v.SuggestedFix == nil {
				t.Fatal("expected SuggestedFix")
			}
			if v.SuggestedFix.NeedsResolve {
				t.Fatalf("NeedsResolve = true, want deterministic fix")
			}
			if len(v.SuggestedFix.Edits) != 1 {
				t.Fatalf("expected 1 edit, got %d", len(v.SuggestedFix.Edits))
			}
			edit := v.SuggestedFix.Edits[0]
			if edit.NewText != tt.wantNewText {
				t.Fatalf("edit NewText = %q, want %q", edit.NewText, tt.wantNewText)
			}
			if edit.Location != tt.wantLoc {
				t.Fatalf("edit Location = %#v, want %#v", edit.Location, tt.wantLoc)
			}
		})
	}
}

func TestDL4001Rule_FixPreferenceInvalidFallsBackToAuto(t *testing.T) {
	t.Parallel()

	dockerfile := `FROM ubuntu:22.04
RUN apt-get update && apt-get install -y curl
RUN curl -fsSL https://example.com/bootstrap.tgz
RUN wget https://example.com/file.tgz
`
	input := testutil.MakeLintInputWithConfig(t, "Dockerfile", dockerfile, map[string]any{
		"fix-preference": "nope",
	})

	violations := NewDL4001Rule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("got %d violations, want 1", len(violations))
	}
	v := violations[0]
	if v.SuggestedFix == nil || v.SuggestedFix.NeedsResolve {
		t.Fatalf("expected deterministic SuggestedFix, got %+v", v.SuggestedFix)
	}
	if len(v.SuggestedFix.Edits) != 1 {
		t.Fatalf("expected 1 edit, got %d", len(v.SuggestedFix.Edits))
	}
	if got := v.SuggestedFix.Edits[0].NewText; got != "curl -fL -O https://example.com/file.tgz" {
		t.Fatalf("edit NewText = %q, want auto-inferred curl rewrite", got)
	}
}
