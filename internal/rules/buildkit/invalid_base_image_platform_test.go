package buildkit

import (
	"strings"
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"

	"github.com/tinovyatkin/tally/internal/registry"
	"github.com/tinovyatkin/tally/internal/rules"
	"github.com/tinovyatkin/tally/internal/testutil"
)

func TestInvalidBaseImagePlatformRule_Metadata(t *testing.T) {
	t.Parallel()
	snaps.MatchStandaloneJSON(t, NewInvalidBaseImagePlatformRule().Metadata())
}

func TestInvalidBaseImagePlatformRule_Check_ReturnsNil(t *testing.T) {
	t.Parallel()
	r := NewInvalidBaseImagePlatformRule()
	input := testutil.MakeLintInputWithSemantic(t, "Dockerfile", "FROM alpine\nRUN echo hi\n")
	violations := r.Check(input)
	if len(violations) != 0 {
		t.Errorf("expected 0 violations (async-only rule), got %d", len(violations))
	}
}

func TestInvalidBaseImagePlatformRule_PlanAsync(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		content   string
		wantCount int
	}{
		{
			name:      "single external image",
			content:   "FROM alpine:3.19\nRUN echo hi\n",
			wantCount: 1,
		},
		{
			name:      "scratch is skipped",
			content:   "FROM scratch\nCOPY --from=0 /app /app\n",
			wantCount: 0,
		},
		{
			name: "stage reference is skipped",
			content: `FROM alpine:3.19 AS builder
RUN echo build

FROM builder
RUN echo run
`,
			wantCount: 1, // Only alpine, not builder
		},
		{
			name: "multiple external images",
			content: `FROM golang:1.22 AS builder
RUN go build

FROM alpine:3.19
COPY --from=builder /app /app
`,
			wantCount: 2, // golang and alpine
		},
		{
			name:      "with explicit platform",
			content:   "FROM --platform=linux/arm64 alpine:3.19\nRUN echo hi\n",
			wantCount: 1,
		},
		{
			name:      "digest-pinned ref",
			content:   "FROM alpine@sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890\nRUN echo hi\n",
			wantCount: 1,
		},
	}

	r := NewInvalidBaseImagePlatformRule()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			input := testutil.MakeLintInputWithSemantic(t, "Dockerfile", tc.content)
			plans := r.PlanAsync(input)
			if len(plans) != tc.wantCount {
				t.Errorf("expected %d plans, got %d", tc.wantCount, len(plans))
				for i, p := range plans {
					t.Logf("  plan[%d]: key=%q resolverID=%q", i, p.Key, p.ResolverID)
				}
			}
			for _, p := range plans {
				if p.ResolverID != registry.RegistryResolverID() {
					t.Errorf("expected resolverID %q, got %q", registry.RegistryResolverID(), p.ResolverID)
				}
				if p.Handler == nil {
					t.Error("handler should not be nil")
				}
			}
		})
	}
}

func TestPlatformCheckHandler_OnSuccess_Match(t *testing.T) {
	t.Parallel()
	meta := NewInvalidBaseImagePlatformRule().Metadata()
	h := &platformCheckHandler{
		meta:     meta,
		file:     "Dockerfile",
		ref:      "alpine:3.19",
		expected: "linux/amd64",
	}

	violations := h.OnSuccess(&registry.ImageConfig{
		OS:   "linux",
		Arch: "amd64",
	})

	if len(violations) != 0 {
		t.Errorf("expected 0 violations for matching platform, got %d", len(violations))
	}
}

func TestPlatformCheckHandler_OnSuccess_Mismatch(t *testing.T) {
	t.Parallel()
	meta := NewInvalidBaseImagePlatformRule().Metadata()
	h := &platformCheckHandler{
		meta:     meta,
		file:     "Dockerfile",
		ref:      "alpine:3.19",
		expected: "linux/amd64",
	}

	violations := h.OnSuccess(&registry.ImageConfig{
		OS:   "linux",
		Arch: "arm64",
	})

	if len(violations) != 1 {
		t.Fatalf("expected 1 violation for mismatched platform, got %d", len(violations))
	}

	v, ok := violations[0].(rules.Violation)
	if !ok {
		t.Fatalf("expected rules.Violation, got %T", violations[0])
	}
	if v.RuleCode != meta.Code {
		t.Errorf("expected code %q, got %q", meta.Code, v.RuleCode)
	}
}

func TestPlatformCheckHandler_OnSuccess_VariantMismatch(t *testing.T) {
	t.Parallel()
	meta := NewInvalidBaseImagePlatformRule().Metadata()
	h := &platformCheckHandler{
		meta:     meta,
		file:     "Dockerfile",
		ref:      "alpine:3.19",
		expected: "linux/arm/v7",
	}

	violations := h.OnSuccess(&registry.ImageConfig{
		OS:      "linux",
		Arch:    "arm",
		Variant: "v8",
	})

	if len(violations) != 1 {
		t.Fatalf("expected 1 violation for variant mismatch, got %d", len(violations))
	}
}

func TestPlatformCheckHandler_OnSuccess_VariantMismatch_Amd64(t *testing.T) {
	t.Parallel()
	meta := NewInvalidBaseImagePlatformRule().Metadata()
	h := &platformCheckHandler{
		meta:     meta,
		file:     "Dockerfile",
		ref:      "myimage:latest",
		expected: "linux/amd64",
	}

	// linux/amd64 (no variant) vs linux/amd64/v3 — different microarchitecture level.
	violations := h.OnSuccess(&registry.ImageConfig{
		OS:      "linux",
		Arch:    "amd64",
		Variant: "v3",
	})

	if len(violations) != 1 {
		t.Fatalf("expected 1 violation for amd64 variant mismatch, got %d", len(violations))
	}
}

func TestPlatformCheckHandler_OnSuccess_Arm64DefaultVariant(t *testing.T) {
	t.Parallel()
	meta := NewInvalidBaseImagePlatformRule().Metadata()
	h := &platformCheckHandler{
		meta:     meta,
		file:     "Dockerfile",
		ref:      "alpine:3.19",
		expected: "linux/arm64",
	}

	// linux/arm64 normalizes to linux/arm64/v8 — should match linux/arm64/v8.
	violations := h.OnSuccess(&registry.ImageConfig{
		OS:      "linux",
		Arch:    "arm64",
		Variant: "v8",
	})

	if len(violations) != 0 {
		t.Errorf("expected 0 violations (arm64 default variant v8 matches), got %d", len(violations))
	}
}

func TestPlatformCheckHandler_OnSuccess_PlatformMismatchError(t *testing.T) {
	t.Parallel()
	meta := NewInvalidBaseImagePlatformRule().Metadata()
	h := &platformCheckHandler{
		meta:     meta,
		file:     "Dockerfile",
		ref:      "myimage:latest",
		expected: "linux/amd64",
	}

	// When the resolver returns PlatformMismatchError (no matching manifest),
	// the handler should emit a violation listing available platforms.
	violations := h.OnSuccess(&registry.PlatformMismatchError{
		Ref:       "myimage:latest",
		Requested: "linux/amd64",
		Available: []string{"linux/arm64", "linux/arm/v7"},
	})

	if len(violations) != 1 {
		t.Fatalf("expected 1 violation for platform mismatch error, got %d", len(violations))
	}

	v, ok := violations[0].(rules.Violation)
	if !ok {
		t.Fatalf("expected rules.Violation, got %T", violations[0])
	}
	if v.RuleCode != meta.Code {
		t.Errorf("expected code %q, got %q", meta.Code, v.RuleCode)
	}
	// The message should include the available platforms list.
	if !strings.Contains(v.Message, "linux/arm64") {
		t.Errorf("expected message to contain available platforms, got %q", v.Message)
	}
}

func TestPlatformCheckHandler_OnSuccess_NilConfig(t *testing.T) {
	t.Parallel()
	meta := NewInvalidBaseImagePlatformRule().Metadata()
	h := &platformCheckHandler{
		meta:     meta,
		file:     "Dockerfile",
		ref:      "alpine:3.19",
		expected: "linux/amd64",
	}

	violations := h.OnSuccess(nil)
	if len(violations) != 0 {
		t.Errorf("expected 0 violations for nil config, got %d", len(violations))
	}
}

func TestPlatformCheckHandler_OnSuccess_WrongType(t *testing.T) {
	t.Parallel()
	meta := NewInvalidBaseImagePlatformRule().Metadata()
	h := &platformCheckHandler{
		meta:     meta,
		file:     "Dockerfile",
		ref:      "alpine:3.19",
		expected: "linux/amd64",
	}

	violations := h.OnSuccess("not an ImageConfig")
	if len(violations) != 0 {
		t.Errorf("expected 0 violations for wrong type, got %d", len(violations))
	}
}

func TestHandlePlatformMismatch(t *testing.T) {
	t.Parallel()
	meta := NewInvalidBaseImagePlatformRule().Metadata()
	loc := rules.NewRangeLocation("Dockerfile", 1, 0, 1, 0)

	t.Run("non-PlatformMismatchError returns nil", func(t *testing.T) {
		t.Parallel()
		violations := HandlePlatformMismatch(
			&registry.NetworkError{Err: nil},
			meta, "Dockerfile", "alpine:3.19", "linux/amd64", loc,
		)
		if len(violations) != 0 {
			t.Errorf("expected nil, got %d violations", len(violations))
		}
	})

	t.Run("PlatformMismatchError returns violation", func(t *testing.T) {
		t.Parallel()
		err := &registry.PlatformMismatchError{
			Ref:       "alpine:3.19",
			Requested: "linux/amd64",
			Available: []string{"linux/arm64", "linux/arm/v7"},
		}
		violations := HandlePlatformMismatch(err, meta, "Dockerfile", "alpine:3.19", "linux/amd64", loc)
		if len(violations) != 1 {
			t.Fatalf("expected 1 violation, got %d", len(violations))
		}
		if violations[0].RuleCode != meta.Code {
			t.Errorf("expected code %q, got %q", meta.Code, violations[0].RuleCode)
		}
	})
}
