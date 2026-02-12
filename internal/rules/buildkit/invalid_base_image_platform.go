package buildkit

import (
	"errors"
	"fmt"
	"strings"

	"github.com/containerd/platforms"
	"github.com/moby/buildkit/frontend/dockerfile/linter"
	"github.com/moby/buildkit/frontend/dockerfile/parser"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"github.com/tinovyatkin/tally/internal/async"
	"github.com/tinovyatkin/tally/internal/registry"
	"github.com/tinovyatkin/tally/internal/rules"
	"github.com/tinovyatkin/tally/internal/semantic"
)

// InvalidBaseImagePlatformRule implements BuildKit's InvalidBaseImagePlatform check.
// This is an async-only rule: Check() returns nil, violations are produced by PlanAsync.
type InvalidBaseImagePlatformRule struct{}

func NewInvalidBaseImagePlatformRule() *InvalidBaseImagePlatformRule {
	return &InvalidBaseImagePlatformRule{}
}

func (r *InvalidBaseImagePlatformRule) Metadata() rules.RuleMetadata {
	const name = "InvalidBaseImagePlatform"
	return *GetMetadata(name)
}

// Check returns nil — this is an async-only rule.
func (r *InvalidBaseImagePlatformRule) Check(_ rules.LintInput) []rules.Violation {
	return nil
}

// PlanAsync creates check requests for each external base image.
func (r *InvalidBaseImagePlatformRule) PlanAsync(input rules.LintInput) []async.CheckRequest {
	return planExternalImageChecks(input, r.Metadata(),
		func(meta rules.RuleMetadata, info *semantic.StageInfo, file, platform string) async.ResultHandler {
			var loc []parser.Range
			if info.BaseImage != nil {
				loc = info.BaseImage.Location
			}
			return &platformCheckHandler{
				meta:     meta,
				file:     file,
				ref:      info.Stage.BaseName,
				expected: platform,
				location: loc,
				stageIdx: info.Index,
			}
		},
	)
}

// platformCheckHandler processes resolved image config for platform validation.
type platformCheckHandler struct {
	meta     rules.RuleMetadata
	file     string
	ref      string
	expected string
	location []parser.Range
	stageIdx int
}

func (h *platformCheckHandler) OnSuccess(resolved any) []any {
	switch v := resolved.(type) {
	case *registry.ImageConfig:
		if v == nil {
			return nil
		}
		return h.checkPlatform(v)
	case *registry.PlatformMismatchError:
		return h.handleMismatchError(v)
	default:
		return nil
	}
}

func (h *platformCheckHandler) handleMismatchError(platErr *registry.PlatformMismatchError) []any {
	actual := "unknown"
	if len(platErr.Available) > 0 {
		actual = fmt.Sprintf("[%s]", strings.Join(platErr.Available, ", "))
	}
	msg := linter.RuleInvalidBaseImagePlatform.Format(h.ref, h.expected, actual)
	loc := rules.NewLocationFromRanges(h.file, h.location)
	v := rules.NewViolation(loc, h.meta.Code, msg, h.meta.DefaultSeverity).WithDocURL(h.meta.DocURL)
	v.StageIndex = h.stageIdx
	return []any{v}
}

func (h *platformCheckHandler) checkPlatform(cfg *registry.ImageConfig) []any {
	actualPlatform := cfg.OS + "/" + cfg.Arch
	if cfg.Variant != "" {
		actualPlatform += "/" + cfg.Variant
	}

	// Use containerd's normalization to handle default variants correctly.
	// For example, arm64 normalizes to arm64/v8, so "linux/arm64" matches
	// "linux/arm64/v8". But amd64 has no default variant, so "linux/amd64"
	// does NOT match "linux/amd64/v3" (different microarchitecture level).
	expected, err := platforms.Parse(h.expected)
	if err != nil {
		return nil
	}
	expected = platforms.Normalize(expected)
	actual := platforms.Normalize(ocispec.Platform{
		OS:           cfg.OS,
		Architecture: cfg.Arch,
		Variant:      cfg.Variant,
	})

	if expected.OS != actual.OS ||
		expected.Architecture != actual.Architecture ||
		expected.Variant != actual.Variant {
		return h.emitViolation(actualPlatform)
	}

	// Non-nil empty slice signals "completed with 0 violations" to the runtime.
	return []any{}
}

func (h *platformCheckHandler) emitViolation(actualPlatform string) []any {
	msg := linter.RuleInvalidBaseImagePlatform.Format(h.ref, h.expected, actualPlatform)
	loc := rules.NewLocationFromRanges(h.file, h.location)
	v := rules.NewViolation(loc, h.meta.Code, msg, h.meta.DefaultSeverity).WithDocURL(h.meta.DocURL)
	v.StageIndex = h.stageIdx
	return []any{v}
}

// HandlePlatformMismatch converts a PlatformMismatchError from the resolver into
// a violation. Returns nil if the error is not a PlatformMismatchError.
func HandlePlatformMismatch(err error, meta rules.RuleMetadata, file, ref, expected string, loc rules.Location) []rules.Violation {
	var platErr *registry.PlatformMismatchError
	if !errors.As(err, &platErr) {
		return nil
	}

	actual := "unknown"
	if len(platErr.Available) > 0 {
		actual = fmt.Sprintf("[%s]", strings.Join(platErr.Available, ", "))
	}
	msg := linter.RuleInvalidBaseImagePlatform.Format(ref, expected, actual)
	return []rules.Violation{
		rules.NewViolation(loc, meta.Code, msg, meta.DefaultSeverity).WithDocURL(meta.DocURL),
	}
}

func init() {
	rules.Register(NewInvalidBaseImagePlatformRule())
}
