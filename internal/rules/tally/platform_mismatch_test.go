package tally

import (
	"strings"
	"testing"

	"github.com/wharflab/tally/internal/registry"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/testutil"
)

func TestPlatformMismatchRule_Metadata(t *testing.T) {
	t.Parallel()
	meta := NewPlatformMismatchRule().Metadata()
	if meta.Code != PlatformMismatchRuleCode {
		t.Errorf("code = %q, want %q", meta.Code, PlatformMismatchRuleCode)
	}
	if meta.DefaultSeverity != rules.SeverityError {
		t.Errorf("severity = %v, want Error", meta.DefaultSeverity)
	}
}

func TestPlatformMismatchRule_Check_ReturnsNil(t *testing.T) {
	t.Parallel()
	r := NewPlatformMismatchRule()
	input := testutil.MakeLintInput(t, "Dockerfile", "FROM alpine\nRUN echo hi\n")
	violations := r.Check(input)
	if len(violations) != 0 {
		t.Errorf("expected 0 violations (async-only rule), got %d", len(violations))
	}
}

func TestPlatformMismatchRule_PlanAsync(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		content   string
		wantCount int
	}{
		{
			name:      "explicit platform creates plan",
			content:   "FROM --platform=linux/amd64 alpine:3.19\nRUN echo hi\n",
			wantCount: 1,
		},
		{
			name:      "no platform skips",
			content:   "FROM alpine:3.19\nRUN echo hi\n",
			wantCount: 0,
		},
		{
			name:      "BUILDPLATFORM skips",
			content:   "FROM --platform=$BUILDPLATFORM alpine:3.19\nRUN echo hi\n",
			wantCount: 0,
		},
		{
			name:      "TARGETPLATFORM skips",
			content:   "FROM --platform=$TARGETPLATFORM alpine:3.19\nRUN echo hi\n",
			wantCount: 0,
		},
		{
			name:      "TARGETOS in expression skips",
			content:   "FROM --platform=$TARGETOS/amd64 alpine:3.19\nRUN echo hi\n",
			wantCount: 0,
		},
		{
			name:      "scratch skips",
			content:   "FROM --platform=linux/amd64 scratch\nCOPY /app /app\n",
			wantCount: 0,
		},
		{
			name: "stage reference skips",
			content: `FROM --platform=linux/amd64 alpine:3.19 AS builder
RUN echo build

FROM builder
RUN echo run
`,
			wantCount: 1, // Only alpine, not builder
		},
		{
			name: "multiple explicit platforms",
			content: `FROM --platform=linux/amd64 golang:1.22 AS builder
RUN go build

FROM --platform=linux/arm64 alpine:3.19
COPY --from=builder /app /app
`,
			wantCount: 2,
		},
		{
			name: "mixed explicit and no-platform",
			content: `FROM --platform=linux/amd64 golang:1.22 AS builder
RUN go build

FROM alpine:3.19
COPY --from=builder /app /app
`,
			wantCount: 1, // Only golang (alpine has no --platform)
		},
	}

	r := NewPlatformMismatchRule()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			input := testutil.MakeLintInput(t, "Dockerfile", tc.content)
			plans := r.PlanAsync(input)
			if len(plans) != tc.wantCount {
				t.Errorf("expected %d plans, got %d", tc.wantCount, len(plans))
				for i, p := range plans {
					t.Logf("  plan[%d]: key=%q resolverID=%q", i, p.Key, p.ResolverID)
				}
			}
			for _, p := range plans {
				if p.ResolverID != registry.RegistryResolverID() {
					t.Errorf("expected resolverID %q, got %q",
						registry.RegistryResolverID(), p.ResolverID)
				}
			}
		})
	}
}

func TestPlatformMismatchHandler_OnSuccess_Match(t *testing.T) {
	t.Parallel()
	h := &platformMismatchHandler{
		meta:      NewPlatformMismatchRule().Metadata(),
		file:      "Dockerfile",
		ref:       "alpine:3.19",
		requested: "linux/amd64",
	}

	results := h.OnSuccess(&registry.ImageConfig{
		OS:   "linux",
		Arch: "amd64",
	})

	if len(results) != 0 {
		t.Errorf("expected 0 violations for matching platform, got %d", len(results))
	}
}

func TestPlatformMismatchHandler_OnSuccess_Mismatch(t *testing.T) {
	t.Parallel()
	h := &platformMismatchHandler{
		meta:      NewPlatformMismatchRule().Metadata(),
		file:      "Dockerfile",
		ref:       "alpine:3.19",
		requested: "linux/amd64",
	}

	results := h.OnSuccess(&registry.ImageConfig{
		OS:   "linux",
		Arch: "arm64",
	})

	if len(results) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(results))
	}

	v, ok := results[0].(rules.Violation)
	if !ok {
		t.Fatalf("expected rules.Violation, got %T", results[0])
	}
	if v.RuleCode != PlatformMismatchRuleCode {
		t.Errorf("code = %q, want %q", v.RuleCode, PlatformMismatchRuleCode)
	}
	if !strings.Contains(v.Message, "linux/arm64") {
		t.Errorf("expected message to mention actual platform, got %q", v.Message)
	}
	if !strings.Contains(v.Message, "linux/amd64") {
		t.Errorf("expected message to mention requested platform, got %q", v.Message)
	}
}

func TestPlatformMismatchHandler_OnSuccess_PlatformMismatchError(t *testing.T) {
	t.Parallel()
	h := &platformMismatchHandler{
		meta:      NewPlatformMismatchRule().Metadata(),
		file:      "Dockerfile",
		ref:       "myimage:latest",
		requested: "linux/s390x",
	}

	results := h.OnSuccess(&registry.PlatformMismatchError{
		Ref:       "myimage:latest",
		Requested: "linux/s390x",
		Available: []string{"linux/amd64", "linux/arm64"},
	})

	if len(results) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(results))
	}

	v, ok := results[0].(rules.Violation)
	if !ok {
		t.Fatalf("expected rules.Violation, got %T", results[0])
	}
	if v.RuleCode != PlatformMismatchRuleCode {
		t.Errorf("code = %q, want %q", v.RuleCode, PlatformMismatchRuleCode)
	}
	if !strings.Contains(v.Message, "linux/amd64") {
		t.Errorf("expected message to contain available platforms, got %q", v.Message)
	}
	if !strings.Contains(v.Message, "does not support") {
		t.Errorf("expected message format, got %q", v.Message)
	}
}

func TestPlatformMismatchHandler_OnSuccess_Arm64Variant(t *testing.T) {
	t.Parallel()
	h := &platformMismatchHandler{
		meta:      NewPlatformMismatchRule().Metadata(),
		file:      "Dockerfile",
		ref:       "alpine:3.19",
		requested: "linux/arm64",
	}

	// linux/arm64/v8 should match linux/arm64 (default variant).
	results := h.OnSuccess(&registry.ImageConfig{
		OS:      "linux",
		Arch:    "arm64",
		Variant: "v8",
	})

	if len(results) != 0 {
		t.Errorf("expected 0 violations (arm64 default variant matches), got %d", len(results))
	}
}

func TestPlatformMismatchHandler_OnSuccess_NilConfig(t *testing.T) {
	t.Parallel()
	h := &platformMismatchHandler{
		meta:      NewPlatformMismatchRule().Metadata(),
		file:      "Dockerfile",
		ref:       "alpine:3.19",
		requested: "linux/amd64",
	}

	results := h.OnSuccess(nil)
	if len(results) != 0 {
		t.Errorf("expected 0 violations for nil, got %d", len(results))
	}
}

func TestPlatformMismatchHandler_OnSuccess_WrongType(t *testing.T) {
	t.Parallel()
	h := &platformMismatchHandler{
		meta:      NewPlatformMismatchRule().Metadata(),
		file:      "Dockerfile",
		ref:       "alpine:3.19",
		requested: "linux/amd64",
	}

	results := h.OnSuccess("not an ImageConfig")
	if len(results) != 0 {
		t.Errorf("expected 0 violations for wrong type, got %d", len(results))
	}
}

func TestReferencesAutoPlatformArg(t *testing.T) {
	t.Parallel()
	tests := []struct {
		expr string
		want bool
	}{
		{"$BUILDPLATFORM", true},
		{"$TARGETPLATFORM", true},
		{"${BUILDPLATFORM}", true},
		{"${TARGETPLATFORM:-linux/amd64}", true},
		{"$TARGETOS/$TARGETARCH", true},
		{"linux/$BUILDARCH", true},
		{"$BUILDOS/amd64", true},
		{"$BUILDVARIANT", true},
		{"$TARGETVARIANT", true},
		{"linux/amd64", false},
		{"$MYPLATFORM", false},
		{"$PLATFORM", false},
		{"${MY_CUSTOM_ARG}", false},
		// User ARGs that happen to contain auto-arg names as substrings must NOT match.
		{"$MY_BUILDPLATFORM", false},
		{"${NOTBUILDPLATFORM}", false},
		{"$CUSTOM_TARGETPLATFORM_V2", false},
	}

	for _, tc := range tests {
		t.Run(tc.expr, func(t *testing.T) {
			t.Parallel()
			got := referencesAutoPlatformArg(tc.expr)
			if got != tc.want {
				t.Errorf("referencesAutoPlatformArg(%q) = %v, want %v",
					tc.expr, got, tc.want)
			}
		})
	}
}
