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
			name: "most-frequent tool wins tiebreak",
			dockerfile: `FROM ubuntu:22.04
RUN wget https://example.com/file1
RUN curl https://example.com/file2
RUN curl https://example.com/file3
`,
			// Neither tool is installed explicitly; curl is used twice vs wget once,
			// so auto mode prefers curl and flags the single wget use.
			wantCount: 1,
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
		{
			// Install both, invoke only curl: wget is dead weight and still counts
			// as "in play" for DL4001 because the point is to avoid the install.
			name: "both installed, only curl invoked - flags wget install",
			dockerfile: `FROM ubuntu:22.04
RUN apt-get update && apt-get install -y --no-install-recommends wget curl
RUN curl https://example.com/file
`,
			wantCount: 1,
			wantCode:  rules.HadolintRulePrefix + "DL4001",
		},
		{
			// Both installed, neither invoked: the install alone is the offense.
			name: "both installed, neither invoked - flags install",
			dockerfile: `FROM ubuntu:22.04
RUN apt-get install -y curl wget
`,
			wantCount: 1,
			wantCode:  rules.HadolintRulePrefix + "DL4001",
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
			name: "curl installed, wget used without install - prefer wget (used-without-install wins)",
			dockerfile: `FROM ubuntu:22.04
RUN apt-get update && apt-get install -y curl
RUN curl https://example.com/file1
RUN wget https://example.com/file2
`,
			// wget is used without being installed, so auto mode prefers wget and
			// flags the curl usage. The message still reflects the install state.
			wantCount:       1,
			wantMsgContains: "curl is installed",
		},
		{
			name: "wget installed, curl used without install - prefer curl (used-without-install wins)",
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
			name: "apk add curl, wget used without install - prefer wget (used-without-install wins)",
			dockerfile: `FROM alpine:3.18
RUN apk add --no-cache curl
RUN curl https://example.com/file1
RUN wget https://example.com/file2
`,
			// wget is used without install; auto mode prefers wget and flags curl.
			// Message still reports the install state for user context.
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
			// curl is explicitly installed; wget is used without install, so auto mode
			// prefers wget (the used-without-install tool) and rewrites the installed curl.
			name: "rewrite curl to wget when wget is used without install",
			dockerfile: `FROM ubuntu:22.04
RUN apt-get update && apt-get install -y curl
RUN curl -fsSL https://example.com/bootstrap.tgz
RUN wget https://example.com/file.tgz
`,
			wantFix:          true,
			wantNeedsResolve: false,
			wantNewText:      "wget -nv -O- https://example.com/bootstrap.tgz",
			wantLoc: rules.NewRangeLocation(
				"Dockerfile",
				3,
				len("RUN "),
				3,
				len("RUN curl -fsSL https://example.com/bootstrap.tgz"),
			),
		},
		{
			// wget is explicitly installed; curl is used without install, so auto mode
			// prefers curl and rewrites the installed wget.
			name: "rewrite wget to curl when curl is used without install",
			dockerfile: `FROM ubuntu:22.04
RUN apt-get update && apt-get install -y wget
RUN wget https://example.com/bootstrap.tgz
RUN curl -fsSL https://example.com/app.tgz | tar -xz -C /opt
`,
			wantFix:          true,
			wantNeedsResolve: false,
			wantNewText:      "curl -fL -O https://example.com/bootstrap.tgz",
			wantLoc:          rules.NewRangeLocation("Dockerfile", 3, len("RUN "), 3, len("RUN wget https://example.com/bootstrap.tgz")),
		},
		{
			// Both tools used without install; curl has one invocation, wget has one — tie.
			// First-seen is wget (line 3) so preferred = wget, curl is flagged.
			// curl -fsS ... lacks -L and cannot be deterministically lowered to wget, so AI fallback.
			name: "use ai fallback for curl without redirect-following to wget",
			dockerfile: `FROM ubuntu:22.04
RUN wget -q https://example.com/bootstrap.tgz
RUN curl -fsS -o /tmp/file https://example.com/file
`,
			wantFix:          true,
			wantNeedsResolve: true,
		},
		{
			name: "use ai fallback for curl without fail-on-http-status to wget",
			dockerfile: `FROM ubuntu:22.04
RUN wget -q https://example.com/bootstrap.tgz
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

	// Both tools installed, so auto mode defaults to wget via the first-seen
	// tiebreak (wget appears first on line 3). Explicit curl/wget preferences
	// must override that tie-break regardless.
	const dockerfileBothInstalledWgetFirst = `FROM ubuntu:22.04
RUN apt-get update && apt-get install -y curl wget
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
			// curl is installed but wget is used-without-install, so auto mode prefers wget.
			name:        "auto prefers the used-without-install tool",
			dockerfile:  dockerfileCurlInstalled,
			config:      DL4001Config{FixPreference: DL4001FixPreferenceAuto},
			wantNewText: "wget -nv -O- https://example.com/bootstrap.tgz",
			wantLoc: rules.NewRangeLocation(
				"Dockerfile", 3, len("RUN "),
				3, len("RUN curl -fsSL https://example.com/bootstrap.tgz"),
			),
		},
		{
			// With both tools installed and wget seen first, auto would prefer wget.
			// Explicit "curl" flips the direction to rewrite wget to curl.
			name:        "explicit curl preference overrides auto tie-break",
			dockerfile:  dockerfileBothInstalledWgetFirst,
			config:      DL4001Config{FixPreference: DL4001FixPreferenceCurl},
			wantNewText: "curl -fL -O https://example.com/bootstrap.tgz",
			wantLoc: rules.NewRangeLocation(
				"Dockerfile", 3, len("RUN "),
				3, len("RUN wget https://example.com/bootstrap.tgz"),
			),
		},
		{
			// dockerfileCurlInstalled: auto would prefer wget (curl installed, wget UWI);
			// explicit "wget" confirms the override path still emits a deterministic lowering
			// pointing at the curl line.
			name:        "explicit wget preference rewrites installed curl",
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
			if len(v.SuggestedFix.Edits) < 1 {
				t.Fatalf("expected at least 1 edit, got %d", len(v.SuggestedFix.Edits))
			}
			// The rewrite edit is always the first; install-removal edits (if any) follow.
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
			// Both tools used-without-install in the same stage; first-seen is curl (line 3),
			// so auto prefers curl and rewrites wget.exe (line 4) to curl.exe.
			name: "pwsh on windows stage lowers to curl.exe",
			dockerfile: `FROM mcr.microsoft.com/windows/servercore:ltsc2022
SHELL ["pwsh", "-Command"]
RUN curl -fL -o /tmp/boot.tgz https://example.com/bootstrap.tgz
RUN wget.exe https://example.com/file.tgz
`,
			wantNewText: "curl.exe -fL -O https://example.com/file.tgz",
			wantLoc: rules.NewRangeLocation(
				"Dockerfile", 4, len("RUN "),
				4, len("RUN wget.exe https://example.com/file.tgz"),
			),
		},
		{
			// Both tools used-without-install in the same stage; first-seen is wget (line 3),
			// so auto prefers wget and rewrites curl.exe (line 4) to wget.exe.
			name: "cmd on windows stage lowers to wget.exe",
			dockerfile: `FROM mcr.microsoft.com/windows/servercore:ltsc2022
SHELL ["cmd", "/S", "/C"]
RUN wget.exe https://example.com/bootstrap.tgz
RUN curl.exe -fL -O https://example.com/file.tgz
`,
			wantNewText: "wget.exe https://example.com/file.tgz",
			wantLoc: rules.NewRangeLocation(
				"Dockerfile", 4, len("RUN "),
				4, len("RUN curl.exe -fL -O https://example.com/file.tgz"),
			),
		},
		{
			// curl is installed, wget is used-without-install, so auto prefers wget and
			// rewrites the installed curl usage. Linux stage keeps bare wget name.
			name: "linux stage keeps bare tool name",
			dockerfile: `FROM ubuntu:22.04
RUN apt-get install -y curl
RUN curl -fsSL https://example.com/bootstrap.tgz
RUN wget https://example.com/file.tgz
`,
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
	// Auto mode: curl is installed, wget is used-without-install → prefer wget and
	// rewrite the installed curl usage.
	if got := v.SuggestedFix.Edits[0].NewText; got != "wget -nv -O- https://example.com/bootstrap.tgz" {
		t.Fatalf("edit NewText = %q, want auto-inferred wget rewrite", got)
	}
}

func TestDL4001Rule_InstallRemoval(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		dockerfile  string
		wantApplied string
	}{
		{
			// Multi-package install: the non-preferred tool is dropped from the install list
			// and every invocation is rewritten to the preferred tool. Both tools must be
			// invoked somewhere for DL4001 to fire.
			name: "multi-package install drops non-preferred tool",
			dockerfile: `FROM ubuntu:22.04
RUN apt-get update && apt-get install -y curl wget
RUN curl https://example.com/bootstrap.tgz
RUN wget https://example.com/file.tgz
`,
			wantApplied: `FROM ubuntu:22.04
RUN apt-get update && apt-get install -y curl
RUN curl https://example.com/bootstrap.tgz
RUN curl -fL -O https://example.com/file.tgz
`,
		},
		{
			// Version-pinned package: strip =version before matching so "wget=1.21" still
			// gets identified as the wget package.
			name: "version-pinned package is recognized for removal",
			dockerfile: `FROM ubuntu:22.04
RUN apt-get update && apt-get install -y curl=7.88.1-10 wget=1.21.3-1ubuntu1
RUN curl https://example.com/bootstrap.tgz
RUN wget https://example.com/file.tgz
`,
			wantApplied: `FROM ubuntu:22.04
RUN apt-get update && apt-get install -y curl=7.88.1-10
RUN curl https://example.com/bootstrap.tgz
RUN curl -fL -O https://example.com/file.tgz
`,
		},
		{
			// Single-package install: MVP skips removal to avoid producing a syntactically
			// broken "apt-get install -y" line. Only the invocation is rewritten.
			name: "single-package install is left alone",
			dockerfile: `FROM ubuntu:22.04
RUN apt-get install -y wget
RUN curl https://example.com/bootstrap.tgz
RUN wget https://example.com/file.tgz
`,
			wantApplied: `FROM ubuntu:22.04
RUN apt-get install -y wget
RUN curl https://example.com/bootstrap.tgz
RUN curl -fL -O https://example.com/file.tgz
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			input := testutil.MakeLintInput(t, "Dockerfile", tt.dockerfile)
			violations := NewDL4001Rule().Check(input)
			if len(violations) == 0 {
				t.Fatal("expected at least one violation")
			}

			var edits []rules.TextEdit
			for _, v := range violations {
				if v.SuggestedFix == nil {
					continue
				}
				edits = append(edits, v.SuggestedFix.Edits...)
			}
			got := applyEditsInReverse(tt.dockerfile, edits)
			if got != tt.wantApplied {
				t.Fatalf("applied diff:\n--- want\n%s\n--- got\n%s", tt.wantApplied, got)
			}
		})
	}
}

// applyEditsInReverse applies a slice of TextEdits to src by replaying them in
// descending source order so earlier offsets are not invalidated by later deletes.
// This is a test helper only; the production fix pipeline has its own applier.
func applyEditsInReverse(src string, edits []rules.TextEdit) string {
	lines := strings.Split(src, "\n")
	sorted := make([]rules.TextEdit, len(edits))
	copy(sorted, edits)
	for i := range sorted {
		for j := i + 1; j < len(sorted); j++ {
			a, b := sorted[i].Location, sorted[j].Location
			if b.Start.Line > a.Start.Line ||
				(b.Start.Line == a.Start.Line && b.Start.Column > a.Start.Column) {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}
	for _, e := range sorted {
		startLine := e.Location.Start.Line - 1
		endLine := e.Location.End.Line - 1
		startCol := e.Location.Start.Column
		endCol := e.Location.End.Column
		if startLine < 0 || startLine >= len(lines) {
			continue
		}
		if startLine == endLine {
			line := lines[startLine]
			if endCol > len(line) {
				endCol = len(line)
			}
			lines[startLine] = line[:startCol] + e.NewText + line[endCol:]
		} else {
			before := lines[startLine][:startCol]
			after := lines[endLine][endCol:]
			merged := before + e.NewText + after
			lines = append(lines[:startLine], append([]string{merged}, lines[endLine+1:]...)...)
		}
	}
	return strings.Join(lines, "\n")
}

func TestDL4001Rule_InstallRemovalHintsACPFallback(t *testing.T) {
	t.Parallel()

	// curl -fsS omits -L, so the deterministic path can't lower it to wget and
	// the rule emits an ACP fallback. The fallback should still carry a hint to
	// drop the source tool from the install so the agent can rewrite holistically.
	dockerfile := `FROM ubuntu:22.04
RUN apt-get install -y curl wget
RUN wget -q https://example.com/bootstrap.tgz
RUN curl -fsS -o /tmp/file https://example.com/file
`
	input := testutil.MakeLintInput(t, "Dockerfile", dockerfile)
	violations := NewDL4001Rule().Check(input)
	if len(violations) == 0 {
		t.Fatal("expected a violation")
	}

	foundHint := false
	for _, v := range violations {
		fix := v.SuggestedFix
		if fix == nil || !fix.NeedsResolve {
			continue
		}
		req, ok := fix.ResolverData.(*autofixdata.ObjectiveRequest)
		if !ok || req == nil {
			continue
		}
		if hint, ok := req.Facts["remove-source-tool-install"].(string); ok && hint != "" {
			foundHint = true
			break
		}
	}
	if !foundHint {
		t.Fatal("expected at least one ACP fix to carry remove-source-tool-install hint")
	}
}

func TestDL4001Rule_AutoTieBreaks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		dockerfile  string
		wantNewText string
		wantLoc     rules.Location
	}{
		{
			// Both tools used-without-install; curl is used twice vs wget once, so
			// invocation-count tie-break prefers curl. The single wget is rewritten.
			name: "invocation count breaks used-without-install tie",
			dockerfile: `FROM ubuntu:22.04
RUN wget https://example.com/file1
RUN curl https://example.com/file2
RUN curl https://example.com/file3
`,
			wantNewText: "curl -fL -O https://example.com/file1",
			wantLoc: rules.NewRangeLocation(
				"Dockerfile", 2, len("RUN "),
				2, len("RUN wget https://example.com/file1"),
			),
		},
		{
			// Both tools used-without-install with equal invocation count; first-seen is
			// curl (line 2) so auto prefers curl and rewrites the wget usage.
			name: "first-seen breaks invocation-count tie",
			dockerfile: `FROM ubuntu:22.04
RUN curl https://example.com/file1
RUN wget https://example.com/file2
`,
			wantNewText: "curl -fL -O https://example.com/file2",
			wantLoc: rules.NewRangeLocation(
				"Dockerfile", 3, len("RUN "),
				3, len("RUN wget https://example.com/file2"),
			),
		},
		{
			// curl is used without install; wget is installed and used — auto prefers
			// curl regardless of invocation count, because the used-without-install
			// signal is stronger than the count tie-break.
			name: "used-without-install beats invocation count",
			dockerfile: `FROM ubuntu:22.04
RUN apt-get install -y wget
RUN wget https://example.com/file1
RUN wget https://example.com/file2
RUN curl https://example.com/file3
`,
			wantNewText: "curl -fL -O https://example.com/file1",
			wantLoc: rules.NewRangeLocation(
				"Dockerfile", 3, len("RUN "),
				3, len("RUN wget https://example.com/file1"),
			),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			input := testutil.MakeLintInput(t, "Dockerfile", tt.dockerfile)

			violations := NewDL4001Rule().Check(input)
			if len(violations) == 0 {
				t.Fatal("expected at least one violation")
			}
			v := violations[0]
			if v.SuggestedFix == nil {
				t.Fatal("expected SuggestedFix")
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
