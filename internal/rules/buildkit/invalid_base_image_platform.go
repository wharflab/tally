package buildkit

import (
	"errors"
	"fmt"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/linter"
	"github.com/moby/buildkit/frontend/dockerfile/parser"

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
	sem, ok := input.Semantic.(*semantic.Model)
	if !ok || sem == nil {
		return nil
	}

	meta := r.Metadata()
	var requests []async.CheckRequest

	for info := range sem.ExternalImageStages() {
		if info.Stage == nil {
			continue
		}

		expectedPlatform, unresolved := semantic.ExpectedPlatform(info, sem)
		if len(unresolved) > 0 || expectedPlatform == "" {
			continue // skip when platform has unresolved ARGs or is empty
		}

		ref := info.Stage.BaseName
		key := ref + "|" + expectedPlatform

		var loc []parser.Range
		if info.BaseImage != nil {
			loc = info.BaseImage.Location
		}

		requests = append(requests, async.CheckRequest{
			RuleCode:   meta.Code,
			Category:   async.CategoryNetwork,
			Key:        key,
			ResolverID: registry.RegistryResolverID(),
			Data:       &registry.ResolveRequest{Ref: ref, Platform: expectedPlatform},
			File:       input.File,
			StageIndex: info.Index,
			Handler: &platformCheckHandler{
				meta:     meta,
				file:     input.File,
				ref:      ref,
				expected: expectedPlatform,
				location: loc,
				stageIdx: info.Index,
			},
		})
	}

	return requests
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
	cfg, ok := resolved.(*registry.ImageConfig)
	if !ok || cfg == nil {
		return nil
	}
	return h.checkPlatform(cfg)
}

func (h *platformCheckHandler) checkPlatform(cfg *registry.ImageConfig) []any {
	expectedOS, expectedArch, expectedVariant := semantic.ParsePlatform(h.expected)
	actualPlatform := cfg.OS + "/" + cfg.Arch
	if cfg.Variant != "" {
		actualPlatform += "/" + cfg.Variant
	}

	if !strings.EqualFold(cfg.OS, expectedOS) || !strings.EqualFold(cfg.Arch, expectedArch) {
		return h.emitViolation(actualPlatform)
	}
	if expectedVariant != "" && !strings.EqualFold(cfg.Variant, expectedVariant) {
		return h.emitViolation(actualPlatform)
	}

	return nil
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
