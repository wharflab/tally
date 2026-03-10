package hadolint

import (
	"path"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"
	"github.com/moby/buildkit/frontend/dockerfile/parser"

	"github.com/wharflab/tally/internal/async"
	"github.com/wharflab/tally/internal/registry"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/rules/asyncutil"
	"github.com/wharflab/tally/internal/semantic"
)

// DL3057Rule implements the DL3057 linting rule.
//
// Fast path (static): If no stage in the Dockerfile contains a HEALTHCHECK CMD
// instruction, emits a single file-level violation (StageIndex = -1). This is
// conservative — it may be a false positive when a base image already defines
// HEALTHCHECK, since Docker inherits it at runtime.
//
// Async path (registry-backed): For each external base image, checks whether it
// defines a HEALTHCHECK. If so, emits CompletedCheck to suppress the fast-path
// violation. Additionally detects useless HEALTHCHECK NONE instructions when the
// base image has no healthcheck to disable.
//
// Cross-rule interactions:
//   - buildkit/MultipleInstructionsDisallowed: flags duplicate HEALTHCHECK
//     instructions in a single stage. DL3057 honours Docker semantics by
//     evaluating only the last HEALTHCHECK per stage, so both rules may
//     fire together when duplicates exist.
//   - ONBUILD HEALTHCHECK: BuildKit parses this as an OnbuildCommand wrapping
//     a HealthCheckCommand. DL3057 does not inspect ONBUILD triggers, so
//     ONBUILD HEALTHCHECK CMD does not satisfy the "has healthcheck" check.
//     This is intentional — ONBUILD triggers execute in child images, not
//     the current one.
type DL3057Rule struct{}

// NewDL3057Rule creates a new DL3057 rule instance.
func NewDL3057Rule() *DL3057Rule {
	return &DL3057Rule{}
}

// Metadata returns the rule metadata.
func (r *DL3057Rule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            rules.HadolintRulePrefix + "DL3057",
		Name:            "HEALTHCHECK instruction missing",
		Description:     "`HEALTHCHECK` instruction missing",
		DocURL:          rules.HadolintDocURL("DL3057"),
		DefaultSeverity: rules.SeverityInfo,
		Category:        "best-practice",
		IsExperimental:  false,
	}
}

// Check implements the fast path for DL3057.
//
// If any stage contains a HEALTHCHECK CMD (not NONE), no violation is reported.
// Otherwise, a single file-level violation with StageIndex=-1 is emitted. The
// async path may later suppress this if a base image provides HEALTHCHECK.
func (r *DL3057Rule) Check(input rules.LintInput) []rules.Violation {
	sem, ok := input.Semantic.(*semantic.Model)
	if !ok || sem == nil {
		return nil
	}

	if sem.StageCount() == 0 {
		return nil
	}

	// If any stage has an explicit HEALTHCHECK CMD, no violation needed.
	for i := range input.Stages {
		if stageHasHealthcheckCmd(&input.Stages[i]) {
			return nil
		}
	}

	// Suppress for containers where HEALTHCHECK is not beneficial
	// (serverless functions, interactive shells, etc.).
	if shouldSuppressHealthcheck(sem, input.Stages) {
		return nil
	}

	// No HEALTHCHECK CMD anywhere — emit a file-level violation.
	meta := r.Metadata()
	loc := rules.NewFileLocation(input.File)
	v := rules.NewViolation(
		loc,
		meta.Code,
		meta.Description,
		meta.DefaultSeverity,
	).WithDocURL(meta.DocURL).WithDetail(
		"Add a HEALTHCHECK instruction to enable container health monitoring. " +
			"Use HEALTHCHECK CMD to define a check command, or HEALTHCHECK NONE " +
			"to explicitly opt out. Note: HEALTHCHECK is inherited from base images " +
			"at runtime, so this may be a false positive if your base image already " +
			"defines one.",
	)
	v.StageIndex = -1
	return []rules.Violation{v}
}

// PlanAsync creates check requests for each external base image to resolve
// whether it defines a HEALTHCHECK.
func (r *DL3057Rule) PlanAsync(input rules.LintInput) []async.CheckRequest {
	sem, ok := input.Semantic.(*semantic.Model)
	if !ok || sem == nil {
		return nil
	}

	// If any stage already has HEALTHCHECK CMD, Check() returns nil so
	// async refinement is unnecessary.
	for i := range input.Stages {
		if stageHasHealthcheckCmd(&input.Stages[i]) {
			return nil
		}
	}

	// No async work needed when the Dockerfile is already suppressed.
	if shouldSuppressHealthcheck(sem, input.Stages) {
		return nil
	}

	meta := r.Metadata()
	return asyncutil.PlanExternalImageChecks(input, meta, func(
		m rules.RuleMetadata,
		info *semantic.StageInfo,
		file, _ string,
	) async.ResultHandler {
		return &healthcheckHandler{
			meta:     m,
			file:     file,
			stageIdx: info.Index,
			semantic: sem,
			stages:   input.Stages,
		}
	})
}

// healthcheckHandler processes resolved image config for HEALTHCHECK detection.
type healthcheckHandler struct {
	meta     rules.RuleMetadata
	file     string
	stageIdx int
	semantic *semantic.Model
	stages   []instructions.Stage
}

func (h *healthcheckHandler) OnSuccess(resolved any) []any {
	cfg, ok := resolved.(*registry.ImageConfig)
	if !ok || cfg == nil {
		return nil
	}

	out := make([]any, 0)

	if cfg.HasHealthcheck {
		// Base image has HEALTHCHECK — suppress the file-level fast violation.
		out = append(out, async.CompletedCheck{
			RuleCode:   h.meta.Code,
			File:       h.file,
			StageIndex: -1, // Matches the fast-path violation's StageIndex
		})
		return out
	}

	// Base image has no HEALTHCHECK. Check this stage and descendants for
	// useless HEALTHCHECK NONE instructions.
	descendants := h.semantic.FromDescendants(h.stageIdx, nil)
	allStages := append([]int{h.stageIdx}, descendants...)

	for _, idx := range allStages {
		if idx < 0 || idx >= len(h.stages) {
			continue
		}
		if loc := healthcheckNoneLocation(&h.stages[idx]); loc != nil {
			// HEALTHCHECK NONE with no inherited HC to disable — useless.
			v := rules.NewViolation(
				rules.NewLocationFromRanges(h.file, loc),
				h.meta.Code,
				"`HEALTHCHECK NONE` has no effect: base image has no health check to disable",
				h.meta.DefaultSeverity,
			).WithDocURL(h.meta.DocURL)
			v.StageIndex = idx

			// Suppress the generic "missing" violation since we have a specific one.
			out = append(out,
				async.CompletedCheck{
					RuleCode:   h.meta.Code,
					File:       h.file,
					StageIndex: -1,
				},
				v,
			)
		}
	}

	// Don't emit CompletedCheck(-1) when base has no HC and no HEALTHCHECK NONE:
	// the fast-path "missing" violation should remain.
	return out
}

// stageHasHealthcheckCmd reports whether the last HEALTHCHECK instruction in a
// stage is a CMD (not NONE). Docker only honours the final HEALTHCHECK, so we
// must scan all commands rather than returning on the first match.
func stageHasHealthcheckCmd(stage *instructions.Stage) bool {
	last := ""
	for _, cmd := range stage.Commands {
		if hc, ok := cmd.(*instructions.HealthCheckCommand); ok {
			if hc.Health != nil && hc.Health.Test != nil &&
				len(hc.Health.Test) > 0 {
				last = hc.Health.Test[0]
			}
		}
	}
	return last != "" && last != "NONE"
}

// healthcheckNoneLocation returns the location of the last HEALTHCHECK
// instruction in a stage if it is NONE, or nil otherwise. Docker only honours
// the final HEALTHCHECK, so earlier instructions are irrelevant.
func healthcheckNoneLocation(stage *instructions.Stage) []parser.Range {
	var lastLoc []parser.Range
	for _, cmd := range stage.Commands {
		if hc, ok := cmd.(*instructions.HealthCheckCommand); ok {
			if hc.Health != nil && hc.Health.Test != nil &&
				len(hc.Health.Test) > 0 {
				if hc.Health.Test[0] == "NONE" {
					lastLoc = hc.Location()
				} else {
					lastLoc = nil // CMD overrides earlier NONE
				}
			}
		}
	}
	return lastLoc
}

// shouldSuppressHealthcheck returns true when the Dockerfile shows strong
// signals that the container will not benefit from a HEALTHCHECK instruction.
//
// Suppressed cases:
//   - Serverless / FaaS base images (AWS Lambda, Azure Functions, OpenFaaS
//     watchdog). These platforms manage function lifecycle externally; a
//     container-level HEALTHCHECK is ignored.
//   - Serverless framework entrypoints where the final stage's CMD or
//     ENTRYPOINT invokes a known FaaS wrapper (e.g. functions-framework for
//     Google Cloud Functions).
//   - Interactive / shell-only containers where the final stage's CMD or
//     ENTRYPOINT is a bare shell (sh, bash, etc.). These are not long-running
//     services and have no endpoint to health-check.
func shouldSuppressHealthcheck(sem *semantic.Model, stages []instructions.Stage) bool {
	if len(stages) == 0 {
		return false
	}

	// Any external base image from a serverless platform → suppress.
	for info := range sem.ExternalImageStages() {
		if isServerlessImage(info.Stage.BaseName) {
			return true
		}
	}

	lastStage := &stages[len(stages)-1]
	cmdLine, shell := lastEntrypointArgs(lastStage)

	// Final stage runs a known serverless framework → suppress.
	if isServerlessEntrypoint(cmdLine, shell) {
		return true
	}

	// Final stage's CMD/ENTRYPOINT is just a shell → suppress.
	if isShellOnlyArgs(cmdLine, shell) {
		return true
	}

	return false
}

// serverlessImagePatterns contains substrings that, when found in a base image
// reference (case-insensitive), identify serverless / FaaS platforms where a
// container-level HEALTHCHECK provides no benefit.
var serverlessImagePatterns = []string{
	// AWS Lambda runtime images
	// e.g. public.ecr.aws/lambda/python:3.12, gallery.ecr.aws/lambda/nodejs:18
	"ecr.aws/lambda/",
	// AWS Lambda images on Docker Hub
	// e.g. amazon/aws-lambda-python:3.12
	"/aws-lambda-",
	// Azure Functions base images
	// e.g. mcr.microsoft.com/azure-functions/dotnet:4
	"/azure-functions/",
	// OpenFaaS function watchdog (entrypoint for serverless functions)
	// e.g. ghcr.io/openfaas/of-watchdog:latest, openfaas/classic-watchdog
	"openfaas/of-watchdog",
	"openfaas/classic-watchdog",
}

// isServerlessImage reports whether baseName matches a known serverless / FaaS
// base image pattern.
func isServerlessImage(baseName string) bool {
	lower := strings.ToLower(baseName)
	for _, pat := range serverlessImagePatterns {
		if strings.Contains(lower, pat) {
			return true
		}
	}
	return false
}

// serverlessEntrypoints is the set of executable names whose presence as
// CMD or ENTRYPOINT indicates a serverless function wrapper. The container's
// lifecycle is managed by the framework, not by Docker health checks.
var serverlessEntrypoints = map[string]bool{
	// Google Cloud Functions framework
	// e.g. CMD ["functions-framework", "--target=hello"]
	"functions-framework": true,
}

// isServerlessEntrypoint reports whether cmdLine invokes a known serverless
// framework (e.g. functions-framework for Google Cloud Functions).
func isServerlessEntrypoint(cmdLine []string, prependShell bool) bool {
	exe := entrypointExe(cmdLine, prependShell)
	return exe != "" && serverlessEntrypoints[exe]
}

// shellBinaries is the set of shell executable names that indicate an
// interactive container when used as the sole CMD or ENTRYPOINT.
var shellBinaries = map[string]bool{
	"sh": true, "bash": true, "zsh": true, "ash": true,
	"dash": true, "fish": true, "csh": true, "tcsh": true, "ksh": true,
}

// lastEntrypointArgs returns the effective CMD/ENTRYPOINT for a stage.
// If an ENTRYPOINT is present it takes precedence over CMD, matching Docker
// runtime semantics.
func lastEntrypointArgs(stage *instructions.Stage) ([]string, bool) {
	var (
		lastCmdLine    []string
		lastCmdShell   bool
		lastEntryLine  []string
		lastEntryShell bool
		hasEntry       bool
	)
	for _, cmd := range stage.Commands {
		switch c := cmd.(type) {
		case *instructions.CmdCommand:
			lastCmdLine = c.CmdLine
			lastCmdShell = c.PrependShell
		case *instructions.EntrypointCommand:
			lastEntryLine = c.CmdLine
			lastEntryShell = c.PrependShell
			hasEntry = true
		}
	}
	if hasEntry {
		return lastEntryLine, lastEntryShell
	}
	return lastCmdLine, lastCmdShell
}

// entrypointExe extracts the base executable name from a CMD/ENTRYPOINT,
// stripping any directory prefix. Returns "" if cmdLine is empty.
//
// In shell form, handles a leading "exec" prefix which is common Docker
// practice (e.g. CMD exec functions-framework --target=hello).
func entrypointExe(cmdLine []string, prependShell bool) string {
	if len(cmdLine) == 0 {
		return ""
	}
	exe := cmdLine[0]
	if prependShell {
		// Shell form: entire command is a single string.
		// Strip leading "exec " — a common pattern to replace the shell
		// with the target process (e.g. "exec functions-framework ...").
		exe = strings.TrimPrefix(exe, "exec ")
		exe, _, _ = strings.Cut(exe, " ")
	}
	if exe == "" {
		return ""
	}
	return path.Base(exe)
}

// isShellOnlyArgs reports whether cmdLine represents a bare shell invocation.
// A bare shell is a known shell binary (possibly with flags like -l, --login)
// but without -c or -e which would indicate command execution.
func isShellOnlyArgs(cmdLine []string, prependShell bool) bool {
	if len(cmdLine) == 0 {
		return false
	}

	var parts []string
	if prependShell {
		// Shell form: entire command is a single string element ("bash -l").
		parts = strings.Fields(cmdLine[0])
	} else {
		parts = cmdLine
	}
	if len(parts) == 0 {
		return false
	}

	// Check executable name (strip directory prefix).
	name := path.Base(parts[0])
	if !shellBinaries[name] {
		return false
	}

	// -c / -e pass a command string to the shell — not interactive.
	for _, arg := range parts[1:] {
		if arg == "-c" || arg == "-e" {
			return false
		}
	}
	return true
}

// init registers the rule with the default registry.
func init() {
	rules.Register(NewDL3057Rule())
}
