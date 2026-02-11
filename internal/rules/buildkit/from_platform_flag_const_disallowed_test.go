package buildkit

import (
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/tinovyatkin/tally/internal/rules"
)

func TestFromPlatformFlagConstDisallowedRule_Metadata(t *testing.T) {
	t.Parallel()
	snaps.MatchStandaloneJSON(t, NewFromPlatformFlagConstDisallowedRule().Metadata())
}

// TestFromPlatformFlagConstDisallowed_ConstantPlatform tests that a constant
// platform value triggers a warning (matches BuildKit test case 1).
func TestFromPlatformFlagConstDisallowed_ConstantPlatform(t *testing.T) {
	t.Parallel()
	r := NewFromPlatformFlagConstDisallowedRule()

	input := rules.LintInput{
		File: "Dockerfile",
		Stages: []instructions.Stage{
			{
				BaseName: "scratch",
				Platform: "linux/amd64",
			},
		},
	}

	violations := r.Check(input)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}

	if violations[0].RuleCode != "buildkit/FromPlatformFlagConstDisallowed" {
		t.Errorf("expected code %q, got %q", "buildkit/FromPlatformFlagConstDisallowed", violations[0].RuleCode)
	}

	wantMsg := `FROM --platform flag should not use constant value "linux/amd64"`
	if violations[0].Message != wantMsg {
		t.Errorf("message = %q, want %q", violations[0].Message, wantMsg)
	}
}

// TestFromPlatformFlagConstDisallowed_StageNameContainsArch tests that no warning
// fires when the stage name references the architecture (matches BuildKit test case 2).
func TestFromPlatformFlagConstDisallowed_StageNameContainsArch(t *testing.T) {
	t.Parallel()
	r := NewFromPlatformFlagConstDisallowedRule()

	input := rules.LintInput{
		File: "Dockerfile",
		Stages: []instructions.Stage{
			{
				BaseName: "scratch",
				Platform: "linux/amd64",
				Name:     "my_amd64_stage",
			},
		},
	}

	violations := r.Check(input)
	if len(violations) != 0 {
		t.Errorf("expected no violations when stage name contains arch, got %d", len(violations))
	}
}

// TestFromPlatformFlagConstDisallowed_StageNameContainsOS tests that no warning
// fires when the stage name matches the OS name (matches BuildKit test case 3).
func TestFromPlatformFlagConstDisallowed_StageNameContainsOS(t *testing.T) {
	t.Parallel()
	r := NewFromPlatformFlagConstDisallowedRule()

	input := rules.LintInput{
		File: "Dockerfile",
		Stages: []instructions.Stage{
			{
				BaseName: "scratch",
				Platform: "linux/amd64",
				Name:     "linux",
			},
		},
	}

	violations := r.Check(input)
	if len(violations) != 0 {
		t.Errorf("expected no violations when stage name matches OS, got %d", len(violations))
	}
}

// TestFromPlatformFlagConstDisallowed_NoPlatform tests that no warning fires
// when there is no --platform flag.
func TestFromPlatformFlagConstDisallowed_NoPlatform(t *testing.T) {
	t.Parallel()
	r := NewFromPlatformFlagConstDisallowedRule()

	input := rules.LintInput{
		File: "Dockerfile",
		Stages: []instructions.Stage{
			{
				BaseName: "ubuntu:22.04",
				Platform: "",
			},
		},
	}

	violations := r.Check(input)
	if len(violations) != 0 {
		t.Errorf("expected no violations for no platform, got %d", len(violations))
	}
}

// TestFromPlatformFlagConstDisallowed_HadolintDL3029 covers all 8 test cases
// from the original Hadolint DL3029 spec (test/Hadolint/Rule/DL3029Spec.hs).
// DL3029 is mapped to buildkit/FromPlatformFlagConstDisallowed in tally.
//
// Note: BuildKit's parser strips quotes from --platform values, so quoted
// variants like "--platform=\"$BUILDPLATFORM\"" arrive as "$BUILDPLATFORM"
// in stage.Platform. The variable-expansion tests below use the parsed
// (unquoted) values that tally's rule actually sees.
func TestFromPlatformFlagConstDisallowed_HadolintDL3029(t *testing.T) {
	t.Parallel()
	r := NewFromPlatformFlagConstDisallowedRule()

	tests := []struct {
		name      string
		platform  string
		wantCount int
	}{
		// Hadolint case 1: explicit constant platform → violation
		{"explicit platform flag", "linux", 1},
		// Hadolint case 2: no platform flag → no violation
		{"no platform flag", "", 0},
		// Hadolint case 3: $BUILDPLATFORM → no violation
		{"allows $BUILDPLATFORM", "$BUILDPLATFORM", 0},
		// Hadolint case 4: quoted "$BUILDPLATFORM" (parser strips quotes)
		{"allows quoted $BUILDPLATFORM", "$BUILDPLATFORM", 0},
		// Hadolint case 5: ${BUILDPLATFORM} → no violation
		{"allows ${BUILDPLATFORM}", "${BUILDPLATFORM}", 0},
		// Hadolint case 6: ${BUILDPLATFORM:-} (default expansion) → no violation
		{"allows ${BUILDPLATFORM:-}", "${BUILDPLATFORM:-}", 0},
		// Hadolint case 7: quoted "${BUILDPLATFORM:-}" (parser strips quotes)
		{"allows quoted ${BUILDPLATFORM:-}", "${BUILDPLATFORM:-}", 0},
		// Hadolint case 8: $TARGETPLATFORM → no violation
		{"allows $TARGETPLATFORM", "$TARGETPLATFORM", 0},
		// Partial variable in platform string → no violation (contains $)
		{"allows partial variable linux/$ARCH", "linux/$ARCH", 0},
		{"allows partial variable $OS/amd64", "$OS/amd64", 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			input := rules.LintInput{
				File: "Dockerfile",
				Stages: []instructions.Stage{
					{
						BaseName: "debian:jessie",
						Platform: tc.platform,
					},
				},
			}
			violations := r.Check(input)
			if len(violations) != tc.wantCount {
				t.Errorf("platform=%q: got %d violations, want %d",
					tc.platform, len(violations), tc.wantCount)
			}
		})
	}
}

// TestFromPlatformFlagConstDisallowed_SingleComponentPlatform verifies that
// single-component platform values (e.g., "linux") are flagged.
// This matches Hadolint DL3029 case 1 and BuildKit's platforms.Parse() which
// accepts single-component values by normalizing with runtime defaults.
func TestFromPlatformFlagConstDisallowed_SingleComponentPlatform(t *testing.T) {
	t.Parallel()
	r := NewFromPlatformFlagConstDisallowedRule()

	input := rules.LintInput{
		File: "Dockerfile",
		Stages: []instructions.Stage{
			{
				BaseName: "debian:jessie",
				Platform: "linux",
			},
		},
	}

	violations := r.Check(input)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation for single-component platform, got %d", len(violations))
	}

	wantMsg := `FROM --platform flag should not use constant value "linux"`
	if violations[0].Message != wantMsg {
		t.Errorf("message = %q, want %q", violations[0].Message, wantMsg)
	}
}

// TestFromPlatformFlagConstDisallowed_SingleComponentWithStageName verifies the
// stage-name exception works for single-component platforms (e.g., "linux" with
// stage name containing "linux").
func TestFromPlatformFlagConstDisallowed_SingleComponentWithStageName(t *testing.T) {
	t.Parallel()
	r := NewFromPlatformFlagConstDisallowedRule()

	input := rules.LintInput{
		File: "Dockerfile",
		Stages: []instructions.Stage{
			{
				BaseName: "debian:jessie",
				Platform: "linux",
				Name:     "linux_build",
			},
		},
	}

	violations := r.Check(input)
	if len(violations) != 0 {
		t.Errorf(
			"expected no violations when stage name contains single-component platform, got %d",
			len(violations),
		)
	}
}

// TestFromPlatformFlagConstDisallowed_MultipleStages tests multiple stages with
// different platform configurations.
func TestFromPlatformFlagConstDisallowed_MultipleStages(t *testing.T) {
	t.Parallel()
	r := NewFromPlatformFlagConstDisallowedRule()

	input := rules.LintInput{
		File: "Dockerfile",
		Stages: []instructions.Stage{
			{
				BaseName: "ubuntu:22.04",
				Platform: "linux/amd64", // Constant → violation
			},
			{
				BaseName: "alpine:3.21",
				Platform: "$BUILDPLATFORM", // Variable → no violation
			},
			{
				BaseName: "debian:bookworm",
				Platform: "linux/arm64", // Constant → violation
			},
			{
				BaseName: "scratch",
				Platform: "linux/amd64",
				Name:     "build_amd64", // Stage name contains arch → no violation
			},
		},
	}

	violations := r.Check(input)
	if len(violations) != 2 {
		t.Errorf("expected 2 violations, got %d", len(violations))
	}
}

// TestFromPlatformFlagConstDisallowed_WindowsPlatform tests that the rule also
// fires for non-Linux platforms.
func TestFromPlatformFlagConstDisallowed_WindowsPlatform(t *testing.T) {
	t.Parallel()
	r := NewFromPlatformFlagConstDisallowedRule()

	input := rules.LintInput{
		File: "Dockerfile",
		Stages: []instructions.Stage{
			{
				BaseName: "mcr.microsoft.com/windows/nanoserver:ltsc2022",
				Platform: "windows/amd64",
			},
		},
	}

	violations := r.Check(input)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation for windows/amd64, got %d", len(violations))
	}
}

// TestFromPlatformFlagConstDisallowed_PlatformWithVariant tests platform strings
// with a variant component (e.g., linux/arm/v7).
func TestFromPlatformFlagConstDisallowed_PlatformWithVariant(t *testing.T) {
	t.Parallel()
	r := NewFromPlatformFlagConstDisallowedRule()

	input := rules.LintInput{
		File: "Dockerfile",
		Stages: []instructions.Stage{
			{
				BaseName: "alpine:3.21",
				Platform: "linux/arm/v7",
			},
		},
	}

	violations := r.Check(input)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation for linux/arm/v7, got %d", len(violations))
	}
}

func TestParsePlatformParts(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		platform string
		wantOS   string
		wantArch string
		wantOK   bool
	}{
		{"standard linux/amd64", "linux/amd64", "linux", "amd64", true},
		{"standard linux/arm64", "linux/arm64", "linux", "arm64", true},
		{"with variant linux/arm/v7", "linux/arm/v7", "linux", "arm", true},
		{"windows platform", "windows/amd64", "windows", "amd64", true},
		{"darwin platform", "darwin/arm64", "darwin", "arm64", true},
		{"single component OS", "linux", "linux", "", true},
		{"single component arch", "amd64", "amd64", "", true},
		{"partial variable in arch", "linux/$ARCH", "linux", "$ARCH", true},
		{"partial variable in OS", "$OS/amd64", "$OS", "amd64", true},
		{"empty string", "", "", "", false},
		{"empty OS", "/amd64", "", "", false},
		{"empty arch", "linux/", "", "", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			os, arch, ok := parsePlatformParts(tc.platform)
			if os != tc.wantOS || arch != tc.wantArch || ok != tc.wantOK {
				t.Errorf("parsePlatformParts(%q) = (%q, %q, %v), want (%q, %q, %v)",
					tc.platform, os, arch, ok, tc.wantOS, tc.wantArch, tc.wantOK)
			}
		})
	}
}
