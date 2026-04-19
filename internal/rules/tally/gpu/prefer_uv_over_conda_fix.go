package gpu

import (
	"cmp"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/wharflab/tally/internal/ai/autofixdata"
	"github.com/wharflab/tally/internal/dockerfile"
	patchutil "github.com/wharflab/tally/internal/patch"
	"github.com/wharflab/tally/internal/shell"
)

func init() {
	autofixdata.RegisterObjective(&uvOverCondaObjective{})
}

// uvOverCondaObjective implements the Objective interface for
// tally/gpu/prefer-uv-over-conda.
type uvOverCondaObjective struct{}

// Kind returns the objective kind.
func (o *uvOverCondaObjective) Kind() autofixdata.ObjectiveKind {
	return autofixdata.ObjectiveUVOverConda
}

// BuildPrompt builds the initial (round 1) prompt.
func (o *uvOverCondaObjective) BuildPrompt(ctx autofixdata.PromptContext) (string, error) {
	file := uvOverCondaTargetFile(ctx.Request, ctx.FilePath)
	normalized := autofixdata.NormalizeLF(string(ctx.Source))
	lines := autofixdata.CountLines(normalized)

	var b strings.Builder
	writeUVOverCondaPreamble(&b)
	autofixdata.WriteFileContext(&b, ctx.AbsPath, ctx.ContextDir)
	autofixdata.WriteRegistryContext(&b, ctx.Request.RegistryInsights)
	writeCUDAVersionHint(&b, ctx.Request)
	autofixdata.WriteSignals(&b, ctx.Request.Signals)
	autofixdata.WriteInputDockerfile(&b, file, lines, normalized)
	autofixdata.WriteOutputFormat(&b, file, ctx.Mode)
	return b.String(), nil
}

// writeCUDAVersionHint emits the base image's parsed CUDA major.minor so the
// agent can align the uv PyTorch `--index-url` wheel suffix (cuXYZ) with the
// image's CUDA runtime. Silent when the rule did not capture a valid CUDA
// version (non-nvidia/cuda base, digest ref, ARG-based tag, missing/negative
// minor, etc.).
func writeCUDAVersionHint(b *strings.Builder, req *autofixdata.ObjectiveRequest) {
	if req == nil {
		return
	}
	major, ok := autofixdata.FactsInt(req.Facts, "cuda-major")
	if !ok || major <= 0 {
		return
	}
	minor, ok := autofixdata.FactsInt(req.Facts, "cuda-minor")
	if !ok || minor < 0 {
		return
	}
	fmt.Fprintf(b, "Base image CUDA version: %d.%d\n", major, minor)
	fmt.Fprintf(b, "  - Align the uv PyTorch --index-url wheel suffix with this version (e.g. cu%d%d).\n\n", major, minor)
}

// BuildRetryPrompt builds a retry (round 2) prompt that includes blocking
// issues from the previous round.
func (o *uvOverCondaObjective) BuildRetryPrompt(ctx autofixdata.RetryPromptContext) (string, error) {
	issuesJSON, err := json.Marshal(ctx.BlockingIssues, jsontext.WithIndentPrefix(""), jsontext.WithIndent("  "))
	if err != nil {
		return "", fmt.Errorf("ai-autofix: marshal blocking issues: %w", err)
	}

	file := filepath.Base(ctx.FilePath)

	var b strings.Builder
	b.WriteString("You previously produced a Dockerfile migrating conda to uv, but tally found blocking issues.\n")
	b.WriteString("Fix ONLY the issues listed below.\n")
	b.WriteString("- Do not make any other changes.\n")
	b.WriteString(
		"- Preserve runtime settings in the final stage exactly: ENTRYPOINT, CMD, EXPOSE, USER, WORKDIR, ENV, LABEL, HEALTHCHECK.\n",
	)
	b.WriteString("- Do not re-introduce `conda install` / `mamba install` / `micromamba install` of Python/ML packages.\n\n")

	autofixdata.WriteFileContext(&b, ctx.AbsPath, ctx.ContextDir)

	b.WriteString("Blocking issues (JSON):\n")
	b.Write(issuesJSON)
	b.WriteString("\n\n")

	autofixdata.WriteProposedDockerfile(&b, ctx.Proposed, ctx.Mode)
	autofixdata.WriteRetryOutputFormat(&b, file, ctx.Mode)

	return b.String(), nil
}

// BuildSimplifiedPrompt builds a minimal fallback prompt used when the agent
// produces malformed output.
func (o *uvOverCondaObjective) BuildSimplifiedPrompt(ctx autofixdata.SimplifiedPromptContext) string {
	var b strings.Builder
	b.WriteString("Migrate the Dockerfile below from conda/mamba/micromamba to uv for Python package installs.\n")
	b.WriteString("Only do the conda→uv migration; do not rewrite unrelated parts.\n")
	b.WriteString("Preserve CMD, ENTRYPOINT, USER, WORKDIR, ENV, LABEL, EXPOSE, HEALTHCHECK in the final stage.\n")
	b.WriteString("If the file uses `conda env create` or an `environment.yml`, output exactly: NO_CHANGE.\n")
	b.WriteString("If you cannot do so safely, output exactly: NO_CHANGE.\n\n")
	autofixdata.WriteFileContext(&b, ctx.AbsPath, ctx.ContextDir)
	autofixdata.WriteSimplifiedInput(&b, filepath.Base(ctx.FilePath), ctx.Source, ctx.Mode)
	return b.String()
}

// ValidateProposal validates that the proposed Dockerfile preserves final-stage
// runtime invariants and actually migrates (not merely deletes) the conda
// Python installs that were present in the original.
func (o *uvOverCondaObjective) ValidateProposal(orig, proposed *dockerfile.ParseResult) []autofixdata.BlockingIssue {
	runtimeErrs := uvOverCondaRuntimeValidationErrors(orig, proposed)
	migrationErrs := uvOverCondaMigrationErrors(orig, proposed)
	blocking := make([]autofixdata.BlockingIssue, 0, len(runtimeErrs)+len(migrationErrs))
	for _, err := range runtimeErrs {
		blocking = append(blocking, autofixdata.BlockingIssue{Rule: "runtime", Message: err.Error()})
	}
	for _, err := range migrationErrs {
		blocking = append(blocking, autofixdata.BlockingIssue{Rule: "migration", Message: err.Error()})
	}
	return blocking
}

// ValidatePatch defers to ValidateProposal.
func (o *uvOverCondaObjective) ValidatePatch(_ patchutil.Meta) []autofixdata.BlockingIssue {
	return nil
}

// uvOverCondaTargetFile returns a stable basename for diff headers/prompt labels.
func uvOverCondaTargetFile(req *autofixdata.ObjectiveRequest, filePath string) string {
	if req != nil {
		if f := strings.TrimSpace(req.File); f != "" {
			return filepath.Base(f)
		}
	}
	return filepath.Base(filePath)
}

// --- Prompt helpers ---

func writeUVOverCondaPreamble(b *strings.Builder) {
	const preamble = `You are a software engineer with deep knowledge of Python packaging, CUDA Docker images, and uv.

Task: migrate the Dockerfile below from conda/mamba/micromamba to uv for Python package installation.
Goals:
- Replace conda/mamba/micromamba Python/ML package installs (torch, numpy, transformers, xformers,
  flash-attn, etc.) with uv equivalents.
- Leave OS package installs (apt, apt-get, yum, dnf, apk) untouched.
- Install uv before its first use (via the official uv installer or pip install uv).
- When CUDA wheels are needed (torch/torchvision/torchaudio), pass the matching --index-url such as
  https://download.pytorch.org/whl/cuXYZ aligned with the base image's CUDA version.

Rules (strict):
- Only do the conda→uv migration. Do not reorganize stages, change the base image, or touch
  unrelated RUN steps unless required for the migration.
- Keep all comments. If you move code lines, move any related comments with them (no orphaned
  comments).
- If you need to communicate an assumption, add a VERY concise comment inside the Dockerfile.
  - Do not output prose outside the required fenced code block.
- Final-stage runtime settings MUST remain byte-identical (tally validates this): ENTRYPOINT, CMD,
  USER, WORKDIR, ENV, LABEL, EXPOSE, HEALTHCHECK.
- Do NOT re-introduce conda/mamba/micromamba install of Python/ML packages after the migration.
- If the Dockerfile uses ` + "`conda env create`" + ` or copies an ` + "`environment.yml`" + ` (or similar env file),
  output exactly: NO_CHANGE.
- If you cannot satisfy these rules safely, output exactly: NO_CHANGE.

`
	b.WriteString(preamble)
}

// --- Migration validation ---

// uvPipManagers enumerates managers that can satisfy a Python/ML package
// requirement in the proposed Dockerfile. pip and pip3 cover direct pip
// installs; uv covers both `uv add` and `uv pip install` (shell.installManagers
// normalizes the latter to "uv").
var uvPipManagers = map[string]bool{
	"pip":  true,
	"pip3": true,
	"uv":   true,
}

// uvOverCondaMigrationErrors walks the original and proposed Dockerfiles and
// reports migration-level validation failures:
//
//  1. Any conda/mamba/micromamba install of a Python/ML package remains.
//  2. A `conda env create` invocation was introduced in the proposal.
//  3. A Python/ML package that the original installed via conda is neither
//     reinstalled via uv/pip nor present on any conda install (i.e., it was
//     deleted rather than migrated).
func uvOverCondaMigrationErrors(orig, proposed *dockerfile.ParseResult) []error {
	if proposed == nil {
		return nil
	}
	remaining := findRemainingCondaMLInstalls(proposed)
	introduced := findIntroducedCondaEnvCreate(orig, proposed)
	deleted := findDeletedMLPackages(orig, proposed)
	errs := make([]error, 0, len(remaining)+len(introduced)+len(deleted))
	errs = append(errs, remaining...)
	errs = append(errs, introduced...)
	errs = append(errs, deleted...)
	return errs
}

// findRemainingCondaMLInstalls reports every conda-family install of a known
// Python/ML package that survived the migration.
func findRemainingCondaMLInstalls(proposed *dockerfile.ParseResult) []error {
	var errs []error
	forEachRunInstallCommand(proposed, func(ic shell.InstallCommand) bool {
		if !condaManagers[ic.Manager] {
			return true
		}
		if offending := firstCondaMLPackageName(ic); offending != "" {
			errs = append(errs, fmt.Errorf(
				"proposed Dockerfile still installs Python/ML package %q via %s; migrate it to uv",
				offending, ic.Manager,
			))
			return false // one blocking issue per RUN is enough
		}
		return true
	})
	return errs
}

// findIntroducedCondaEnvCreate flags a `conda env create` invocation that is
// present in the proposal but was not in the original. A proposal that
// "migrates" by switching to an env-create workflow bypasses the rule intent.
func findIntroducedCondaEnvCreate(orig, proposed *dockerfile.ParseResult) []error {
	if origHasCondaEnvCreate(orig) {
		return nil // original already used env create; not a regression
	}
	for _, stage := range proposed.Stages {
		for _, cmd := range stage.Commands {
			run, ok := cmd.(*instructions.RunCommand)
			if !ok {
				continue
			}
			if scriptHasCondaEnvCreate(runScriptText(run), shell.VariantBash) {
				return []error{errors.New(
					"proposed Dockerfile introduces `conda env create`; the migration must use uv/pip, not env workflows",
				)}
			}
		}
	}
	return nil
}

// findDeletedMLPackages verifies every Python/ML package installed via conda
// in the original Dockerfile reappears either in a uv/pip install or in a
// (still-surviving) conda install in the proposal. A package that vanishes
// entirely indicates the agent deleted dependencies rather than migrating.
func findDeletedMLPackages(orig, proposed *dockerfile.ParseResult) []error {
	targets := collectCondaMLPackages(orig)
	if len(targets) == 0 {
		return nil
	}

	covered := collectInstalledPackages(proposed)
	var errs []error
	for name := range targets {
		if !covered[name] {
			errs = append(errs, fmt.Errorf(
				"proposed Dockerfile dropped Python/ML package %q without reinstalling it via uv or pip",
				name,
			))
		}
	}
	// Sort errors by message so the output is deterministic across runs.
	sortErrorsByMessage(errs)
	return errs
}

// origHasCondaEnvCreate reports whether the original Dockerfile already
// declared a `conda env create` step, so the validator can distinguish
// "introduced" from "preserved".
func origHasCondaEnvCreate(orig *dockerfile.ParseResult) bool {
	if orig == nil {
		return false
	}
	for _, stage := range orig.Stages {
		for _, cmd := range stage.Commands {
			run, ok := cmd.(*instructions.RunCommand)
			if !ok {
				continue
			}
			if scriptHasCondaEnvCreate(runScriptText(run), shell.VariantBash) {
				return true
			}
		}
	}
	return false
}

// collectCondaMLPackages returns the set of known Python/ML packages that the
// Dockerfile installed via a conda-family manager.
func collectCondaMLPackages(parsed *dockerfile.ParseResult) map[string]bool {
	targets := map[string]bool{}
	forEachRunInstallCommand(parsed, func(ic shell.InstallCommand) bool {
		if !condaManagers[ic.Manager] {
			return true
		}
		for _, pkg := range ic.Packages {
			name := normalizeCondaPackageName(pkg.Normalized)
			if condaPythonMLPackages[name] {
				targets[name] = true
			}
		}
		return true
	})
	return targets
}

// collectInstalledPackages returns the set of package names installed in the
// Dockerfile via any package manager recognized by shell.installManagers
// (conda/mamba/micromamba/pip/pip3/uv, etc.). Used to check that migrated
// packages reappear somewhere under uv/pip/conda in the proposal.
func collectInstalledPackages(parsed *dockerfile.ParseResult) map[string]bool {
	installed := map[string]bool{}
	forEachRunInstallCommand(parsed, func(ic shell.InstallCommand) bool {
		if !uvPipManagers[ic.Manager] && !condaManagers[ic.Manager] {
			return true
		}
		for _, pkg := range ic.Packages {
			name := normalizeCondaPackageName(pkg.Normalized)
			if name != "" {
				installed[name] = true
			}
		}
		return true
	})
	return installed
}

// forEachRunInstallCommand invokes visit for every install command parsed
// from every RUN in every stage. visit returns false to skip remaining
// install commands in the same RUN (matches the "one blocking issue per
// RUN" behavior the remaining-conda check wants).
func forEachRunInstallCommand(parsed *dockerfile.ParseResult, visit func(shell.InstallCommand) bool) {
	if parsed == nil {
		return
	}
	for _, stage := range parsed.Stages {
		for _, cmd := range stage.Commands {
			run, ok := cmd.(*instructions.RunCommand)
			if !ok {
				continue
			}
			script := runScriptText(run)
			if script == "" {
				continue
			}
			for _, ic := range shell.FindInstallPackages(script, shell.VariantBash) {
				if !visit(ic) {
					break
				}
			}
		}
	}
}

// sortErrorsByMessage sorts errs in-place by the Error() string so the
// validator's output is deterministic regardless of map iteration order.
func sortErrorsByMessage(errs []error) {
	slices.SortFunc(errs, func(a, b error) int {
		return cmp.Compare(a.Error(), b.Error())
	})
}

// firstCondaMLPackageName returns the first Python/ML package in ic, or "".
func firstCondaMLPackageName(ic shell.InstallCommand) string {
	for _, pkg := range ic.Packages {
		name := normalizeCondaPackageName(pkg.Normalized)
		if condaPythonMLPackages[name] {
			return name
		}
	}
	return ""
}

// runScriptText returns the shell script text of a RUN command for parsing.
// All heredoc bodies are concatenated (RUN can declare multiple heredocs,
// e.g. RUN <<EOF1 ... EOF1 <<EOF2 ... EOF2) so a conda install hidden inside
// a later heredoc still gets scanned. Shell-form RUN args fall back to
// joining CmdLine with spaces so the shell parser can handle them uniformly.
func runScriptText(run *instructions.RunCommand) string {
	if run == nil {
		return ""
	}
	if len(run.Files) > 0 {
		parts := make([]string, 0, len(run.Files))
		for _, f := range run.Files {
			parts = append(parts, f.Data)
		}
		return strings.Join(parts, "\n")
	}
	return strings.Join(run.CmdLine, " ")
}

// uvOverCondaRuntimeValidationErrors is a thin wrapper around the shared
// autofixdata.FinalStageRuntimeErrors, kept so the call site reads clearly.
func uvOverCondaRuntimeValidationErrors(orig, proposed *dockerfile.ParseResult) []error {
	return autofixdata.FinalStageRuntimeErrors(orig, proposed)
}
