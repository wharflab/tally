package tally

import (
	"fmt"
	"strings"

	"github.com/containerd/platforms"
	"github.com/moby/buildkit/frontend/dockerfile/parser"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"github.com/wharflab/tally/internal/async"
	"github.com/wharflab/tally/internal/registry"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/semantic"
)

// PlatformMismatchRuleCode is the full rule code for the platform-mismatch rule.
const PlatformMismatchRuleCode = rules.TallyRulePrefix + "platform-mismatch"

// autoPlatformArgs are the automatic build platform ARGs provided by BuildKit.
// These are dynamic at build time, so we cannot validate them statically.
var autoPlatformArgs = []string{
	"BUILDPLATFORM", "BUILDOS", "BUILDARCH", "BUILDVARIANT",
	"TARGETPLATFORM", "TARGETOS", "TARGETARCH", "TARGETVARIANT",
}

// PlatformMismatchRule validates that an explicit --platform on FROM matches
// what the registry actually provides. Unlike buildkit/InvalidBaseImagePlatform,
// this rule only fires when --platform is explicitly set — it never compares
// against the host platform, so results are deterministic across machines.
type PlatformMismatchRule struct{}

func NewPlatformMismatchRule() *PlatformMismatchRule {
	return &PlatformMismatchRule{}
}

func (r *PlatformMismatchRule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            PlatformMismatchRuleCode,
		Name:            "Platform Mismatch",
		Description:     "Explicit --platform on FROM does not match what the registry provides",
		DocURL:          rules.TallyDocURL(PlatformMismatchRuleCode),
		DefaultSeverity: rules.SeverityError,
		Category:        "correctness",
	}
}

// Check returns nil — this is an async-only rule.
func (r *PlatformMismatchRule) Check(_ rules.LintInput) []rules.Violation {
	return nil
}

// PlanAsync creates check requests for each external base image that has an
// explicit --platform flag with a statically resolvable value.
func (r *PlatformMismatchRule) PlanAsync(input rules.LintInput) []async.CheckRequest {
	sem := input.Semantic

	meta := r.Metadata()
	var requests []async.CheckRequest

	for info := range sem.ExternalImageStages() {
		if info.Stage == nil {
			continue
		}

		// Only consider stages with explicit --platform.
		rawPlatform := info.Stage.Platform
		if rawPlatform == "" {
			continue
		}

		// Skip automatic build platform args — these are dynamic at build time.
		if referencesAutoPlatformArg(rawPlatform) {
			continue
		}

		// Resolve the platform expression (handles user-defined ARGs).
		resolved, unresolved := semantic.ExpectedPlatform(info, sem)
		if len(unresolved) > 0 || resolved == "" {
			continue
		}

		ref := info.Stage.BaseName
		key := ref + "|" + resolved

		var loc []parser.Range
		if info.BaseImage != nil {
			loc = info.BaseImage.Location
		}

		requests = append(requests, async.CheckRequest{
			RuleCode:   meta.Code,
			Category:   async.CategoryNetwork,
			Key:        key,
			ResolverID: registry.RegistryResolverID(),
			Data:       &registry.ResolveRequest{Ref: ref, Platform: resolved},
			File:       input.File,
			StageIndex: info.Index,
			Handler: &platformMismatchHandler{
				meta:      meta,
				file:      input.File,
				ref:       ref,
				requested: resolved,
				location:  loc,
				stageIdx:  info.Index,
			},
		})
	}

	return requests
}

// referencesAutoPlatformArg returns true if the expression references any of
// the automatic build platform ARGs (BUILDPLATFORM, TARGETPLATFORM, etc.)
// via $NAME or ${NAME} syntax.
func referencesAutoPlatformArg(expr string) bool {
	for _, name := range autoPlatformArgs {
		if strings.Contains(expr, "$"+name) || strings.Contains(expr, "${"+name) {
			return true
		}
	}
	return false
}

// platformMismatchHandler processes resolved image config for platform validation.
type platformMismatchHandler struct {
	meta      rules.RuleMetadata
	file      string
	ref       string
	requested string
	location  []parser.Range
	stageIdx  int
}

func (h *platformMismatchHandler) OnSuccess(resolved any) []any {
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

func (h *platformMismatchHandler) checkPlatform(cfg *registry.ImageConfig) []any {
	requested, err := platforms.Parse(h.requested)
	if err != nil {
		return nil
	}
	requested = platforms.Normalize(requested)

	actual := platforms.Normalize(ocispec.Platform{
		OS:           cfg.OS,
		Architecture: cfg.Arch,
		Variant:      cfg.Variant,
	})

	if requested.OS == actual.OS &&
		requested.Architecture == actual.Architecture &&
		requested.Variant == actual.Variant {
		return []any{} // match — completed with 0 violations
	}

	actualStr := cfg.OS + "/" + cfg.Arch
	if cfg.Variant != "" {
		actualStr += "/" + cfg.Variant
	}

	msg := fmt.Sprintf(
		"Base image %s has platform %q, does not match requested %q",
		h.ref, actualStr, h.requested,
	)
	return h.emit(msg)
}

func (h *platformMismatchHandler) handleMismatchError(platErr *registry.PlatformMismatchError) []any {
	avail := "unknown"
	if len(platErr.Available) > 0 {
		avail = strings.Join(platErr.Available, ", ")
	}
	msg := fmt.Sprintf(
		"Base image %s does not support requested platform %q; available: %s",
		h.ref, h.requested, avail,
	)
	return h.emit(msg)
}

func (h *platformMismatchHandler) emit(msg string) []any {
	loc := rules.NewLocationFromRanges(h.file, h.location)
	v := rules.NewViolation(loc, h.meta.Code, msg, h.meta.DefaultSeverity).
		WithDocURL(h.meta.DocURL)
	v.StageIndex = h.stageIdx
	return []any{v}
}

func init() {
	rules.Register(NewPlatformMismatchRule())
}
