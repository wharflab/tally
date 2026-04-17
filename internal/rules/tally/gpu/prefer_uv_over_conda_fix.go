package gpu

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/wharflab/tally/internal/ai/autofixdata"
	"github.com/wharflab/tally/internal/dockerfile"
	patchutil "github.com/wharflab/tally/internal/patch"
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
	autofixdata.WriteSignals(&b, ctx.Request.Signals)
	autofixdata.WriteInputDockerfile(&b, file, lines, normalized)
	autofixdata.WriteOutputFormat(&b, file, ctx.Mode)
	return b.String(), nil
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
// runtime invariants and actually removes conda Python installs.
func (o *uvOverCondaObjective) ValidateProposal(orig, proposed *dockerfile.ParseResult) []autofixdata.BlockingIssue {
	var blocking []autofixdata.BlockingIssue

	for _, err := range uvOverCondaRuntimeValidationErrors(orig, proposed) {
		blocking = append(blocking, autofixdata.BlockingIssue{Rule: "runtime", Message: err.Error()})
	}

	if proposed != nil {
		for _, err := range uvOverCondaMigrationErrors(proposed) {
			blocking = append(blocking, autofixdata.BlockingIssue{Rule: "migration", Message: err.Error()})
		}
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

// condaPythonMLInstallRe matches conda/mamba/micromamba install commands.
// Used as a first filter; callers then check for Python/ML package names
// inside the match range.
var condaPythonMLInstallRe = regexp.MustCompile(`(?m)\b(?:conda|mamba|micromamba)\s+install\b`)

// uvOverCondaMigrationErrors reports migration-level validation failures on
// the proposed Dockerfile:
//   - remaining conda/mamba/micromamba installs of Python/ML packages.
func uvOverCondaMigrationErrors(proposed *dockerfile.ParseResult) []error {
	if proposed == nil {
		return nil
	}
	var errs []error
	for _, stage := range proposed.Stages {
		for _, cmd := range stage.Commands {
			run, ok := cmd.(*instructions.RunCommand)
			if !ok {
				continue
			}
			script := runScriptText(run)
			if script == "" {
				continue
			}
			lower := strings.ToLower(script)
			if !condaPythonMLInstallRe.MatchString(lower) {
				continue
			}
			if offending := firstCondaPythonMLPackage(lower); offending != "" {
				errs = append(errs, fmt.Errorf(
					"proposed Dockerfile still installs Python/ML package %q via conda/mamba/micromamba; migrate it to uv",
					offending,
				))
			}
		}
	}
	return errs
}

// firstCondaPythonMLPackage returns the first Python/ML package name from the
// package list that appears in a conda-family install command, or "" if the
// install only touches non-ML packages (e.g., gcc, cmake).
func firstCondaPythonMLPackage(lowerScript string) string {
	for name := range condaPythonMLPackages {
		if !containsAsToken(lowerScript, name) {
			continue
		}
		return name
	}
	return ""
}

// containsAsToken returns true if name appears in s with non-alphanumeric
// boundaries so "torch" doesn't match "pytorch-cuda".
func containsAsToken(s, name string) bool {
	idx := 0
	for {
		rel := strings.Index(s[idx:], name)
		if rel < 0 {
			return false
		}
		pos := idx + rel
		before := byte(' ')
		if pos > 0 {
			before = s[pos-1]
		}
		after := byte(' ')
		if pos+len(name) < len(s) {
			after = s[pos+len(name)]
		}
		if !isPkgNameByte(before) && !isPkgNameByte(after) {
			return true
		}
		idx = pos + 1
		if idx >= len(s) {
			return false
		}
	}
}

// isPkgNameByte reports whether a byte can appear inside a Python package name.
// Matches the set used for typical PyPI/conda package identifiers.
func isPkgNameByte(b byte) bool {
	switch {
	case b >= 'a' && b <= 'z':
		return true
	case b >= '0' && b <= '9':
		return true
	case b == '_':
		return true
	case b == '.':
		return true
	}
	return false
}

// runScriptText returns the shell script text of a RUN command for string matching.
func runScriptText(run *instructions.RunCommand) string {
	if run == nil {
		return ""
	}
	if len(run.Files) > 0 {
		return run.Files[0].Data
	}
	return strings.Join(run.CmdLine, " ")
}

// uvOverCondaRuntimeValidationErrors is a thin wrapper around the shared
// autofixdata.FinalStageRuntimeErrors, kept so the call site reads clearly.
func uvOverCondaRuntimeValidationErrors(orig, proposed *dockerfile.ParseResult) []error {
	return autofixdata.FinalStageRuntimeErrors(orig, proposed)
}
