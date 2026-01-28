package redundanttargetplatform

import (
	"testing"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/tinovyatkin/tally/internal/rules"
)

func TestMetadata(t *testing.T) {
	r := New()
	meta := r.Metadata()

	if meta.Code != "buildkit/RedundantTargetPlatform" {
		t.Errorf("expected code %q, got %q", "buildkit/RedundantTargetPlatform", meta.Code)
	}

	if meta.Category != "best-practices" {
		t.Errorf("expected category %q, got %q", "best-practices", meta.Category)
	}

	if !meta.EnabledByDefault {
		t.Error("expected rule to be enabled by default")
	}
}

func TestCheck_RedundantTargetPlatform(t *testing.T) {
	r := New()

	input := rules.LintInput{
		File: "Dockerfile",
		Stages: []instructions.Stage{
			{
				BaseName: "ubuntu:22.04",
				Platform: "$TARGETPLATFORM",
			},
		},
	}

	violations := r.Check(input)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}

	if violations[0].RuleCode != "buildkit/RedundantTargetPlatform" {
		t.Errorf("expected code %q, got %q", "buildkit/RedundantTargetPlatform", violations[0].RuleCode)
	}
}

func TestCheck_RedundantTargetPlatformBraces(t *testing.T) {
	r := New()

	input := rules.LintInput{
		File: "Dockerfile",
		Stages: []instructions.Stage{
			{
				BaseName: "ubuntu:22.04",
				Platform: "${TARGETPLATFORM}",
			},
		},
	}

	violations := r.Check(input)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation for ${TARGETPLATFORM}, got %d", len(violations))
	}
}

func TestCheck_NoPlatform(t *testing.T) {
	r := New()

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

func TestCheck_ExplicitPlatform(t *testing.T) {
	r := New()

	input := rules.LintInput{
		File: "Dockerfile",
		Stages: []instructions.Stage{
			{
				BaseName: "ubuntu:22.04",
				Platform: "linux/amd64",
			},
		},
	}

	violations := r.Check(input)
	if len(violations) != 0 {
		t.Errorf("expected no violations for explicit platform, got %d", len(violations))
	}
}

func TestCheck_OtherVariable(t *testing.T) {
	r := New()

	input := rules.LintInput{
		File: "Dockerfile",
		Stages: []instructions.Stage{
			{
				BaseName: "ubuntu:22.04",
				Platform: "$BUILDPLATFORM",
			},
		},
	}

	violations := r.Check(input)
	if len(violations) != 0 {
		t.Errorf("expected no violations for $BUILDPLATFORM, got %d", len(violations))
	}
}

func TestCheck_MultipleStages(t *testing.T) {
	r := New()

	input := rules.LintInput{
		File: "Dockerfile",
		Stages: []instructions.Stage{
			{
				BaseName: "ubuntu:22.04",
				Platform: "$TARGETPLATFORM",
			},
			{
				BaseName: "builder",
				Platform: "",
			},
			{
				BaseName: "alpine:3.18",
				Platform: "${TARGETPLATFORM}",
			},
		},
	}

	violations := r.Check(input)
	if len(violations) != 2 {
		t.Errorf("expected 2 violations, got %d", len(violations))
	}
}

func TestIsRedundantPlatform(t *testing.T) {
	tests := []struct {
		platform string
		want     bool
	}{
		{"$TARGETPLATFORM", true},
		{"${TARGETPLATFORM}", true},
		{" $TARGETPLATFORM ", true}, // Whitespace trimmed
		{"$BUILDPLATFORM", false},
		{"linux/amd64", false},
		{"${TARGETPLATFORM:-linux/amd64}", false}, // Has default value
		{"$TARGETPLATFORM/$VARIANT", false},       // Combined with other
		{"", false},
	}

	for _, tc := range tests {
		got := isRedundantPlatform(tc.platform)
		if got != tc.want {
			t.Errorf("isRedundantPlatform(%q) = %v, want %v", tc.platform, got, tc.want)
		}
	}
}
