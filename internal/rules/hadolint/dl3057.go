package hadolint

import (
	"path"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/wharflab/tally/internal/async"
	"github.com/wharflab/tally/internal/registry"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/rules/asyncutil"
	"github.com/wharflab/tally/internal/semantic"
	"github.com/wharflab/tally/internal/shell"
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

	// If any stage has an explicit HEALTHCHECK (CMD or NONE), no violation needed.
	// HEALTHCHECK NONE is a deliberate opt-out and satisfies DL3057.
	for i := range input.Stages {
		if stageHasExplicitHealthcheck(&input.Stages[i]) {
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

	// If any stage already has an explicit HEALTHCHECK (CMD or NONE),
	// Check() returns nil so async refinement is unnecessary.
	for i := range input.Stages {
		if stageHasExplicitHealthcheck(&input.Stages[i]) {
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
		_ *semantic.StageInfo,
		file, _ string,
	) async.ResultHandler {
		return &healthcheckHandler{
			meta: m,
			file: file,
		}
	})
}

// healthcheckHandler processes resolved image config for HEALTHCHECK detection.
type healthcheckHandler struct {
	meta rules.RuleMetadata
	file string
}

func (h *healthcheckHandler) OnSuccess(resolved any) []any {
	cfg, ok := resolved.(*registry.ImageConfig)
	if !ok || cfg == nil {
		return nil
	}

	if cfg.HasHealthcheck {
		// Base image has HEALTHCHECK — suppress the file-level fast violation.
		return []any{async.CompletedCheck{
			RuleCode:   h.meta.Code,
			File:       h.file,
			StageIndex: -1, // Matches the fast-path violation's StageIndex
		}}
	}

	// Base has no HEALTHCHECK and no explicit opt-out exists (PlanAsync
	// already short-circuits when HEALTHCHECK NONE is present), so the
	// fast-path "missing" violation should remain.
	return nil
}

// stageHasExplicitHealthcheck reports whether a stage contains any HEALTHCHECK
// instruction (CMD or NONE). HEALTHCHECK NONE is a deliberate opt-out and
// counts as "addressed" for DL3057 purposes.
func stageHasExplicitHealthcheck(stage *instructions.Stage) bool {
	for _, cmd := range stage.Commands {
		if _, ok := cmd.(*instructions.HealthCheckCommand); ok {
			return true
		}
	}
	return false
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
		if info.Stage == nil {
			continue
		}
		if isServerlessImage(info.Stage.BaseName) {
			return true
		}
	}

	lastIdx := len(stages) - 1
	lastStage := &stages[lastIdx]
	cmdLine, prependShell := lastEntrypointArgs(lastStage)

	// Resolve the shell variant for proper parsing of shell-form commands.
	variant := shell.VariantBash // Docker default
	if info := sem.StageInfo(lastIdx); info != nil {
		variant = info.ShellSetting.Variant
	}

	// Final stage runs a known serverless framework → suppress.
	if isServerlessEntrypoint(cmdLine, prependShell, variant) {
		return true
	}

	// Final stage's CMD/ENTRYPOINT is just a shell → suppress.
	if isShellOnlyArgs(cmdLine, prependShell, variant) {
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
// framework. For shell form, uses the project's shell parser which properly
// handles exec/env/command wrappers (e.g. "exec functions-framework ...").
func isServerlessEntrypoint(cmdLine []string, prependShell bool, variant shell.Variant) bool {
	for _, name := range entrypointCommandNames(cmdLine, prependShell, variant) {
		if serverlessEntrypoints[name] {
			return true
		}
	}
	return false
}

// shellBinaries is the set of shell executable names that indicate an
// interactive container when used as the sole CMD or ENTRYPOINT.
var shellBinaries = map[string]bool{
	"sh": true, "bash": true, "zsh": true, "ash": true,
	"dash": true, "fish": true, "csh": true, "tcsh": true, "ksh": true,
}

// isShellOnlyArgs reports whether cmdLine represents a bare shell invocation.
//
// For shell form, uses the project's shell parser which understands wrappers
// (exec, env, …) and shell delegation (bash -c …). The last extracted command
// name is checked: "exec bash -l" → names ["exec","bash"] → last is "bash" ✓,
// while "bash -c my-app" → names ["bash","my-app"] → last is "my-app" ✗.
//
// For exec form, checks the argv directly: the executable must be a shell
// binary and no -c/-e flag may be present.
func isShellOnlyArgs(cmdLine []string, prependShell bool, variant shell.Variant) bool {
	if len(cmdLine) == 0 {
		return false
	}

	if prependShell {
		names := shell.CommandNamesWithVariant(cmdLine[0], variant)
		return len(names) > 0 && shellBinaries[names[len(names)-1]]
	}

	// Exec form: first element is the executable.
	name := path.Base(cmdLine[0])
	if !shellBinaries[name] {
		return false
	}
	// -c / -e pass a command string to the shell — not interactive.
	for _, arg := range cmdLine[1:] {
		if arg == "-c" || arg == "-e" {
			return false
		}
	}
	return true
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

// entrypointCommandNames extracts command names from a CMD/ENTRYPOINT.
//
// For shell form, delegates to shell.CommandNamesWithVariant which uses the
// mvdan.cc/sh parser and properly handles wrappers (exec, env, command, …)
// and shell delegation (bash -c …).
//
// For exec form, returns the base name of the first element.
func entrypointCommandNames(cmdLine []string, prependShell bool, variant shell.Variant) []string {
	if len(cmdLine) == 0 {
		return nil
	}
	if prependShell {
		return shell.CommandNamesWithVariant(cmdLine[0], variant)
	}
	name := path.Base(cmdLine[0])
	if name == "" || name == "." {
		return nil
	}
	return []string{name}
}

// init registers the rule with the default registry.
func init() {
	rules.Register(NewDL3057Rule())
}
