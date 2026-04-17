package tally

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/command"
	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/wharflab/tally/internal/ai/autofixdata"
	"github.com/wharflab/tally/internal/config"
	"github.com/wharflab/tally/internal/dockerfile"
	patchutil "github.com/wharflab/tally/internal/patch"
)

func init() {
	autofixdata.RegisterObjective(&multiStageObjective{})
}

// multiStageObjective implements the Objective interface for the
// tally/prefer-multi-stage-build rule.
type multiStageObjective struct{}

func (o *multiStageObjective) Kind() autofixdata.ObjectiveKind {
	return autofixdata.ObjectiveMultiStage
}

func (o *multiStageObjective) BuildPrompt(ctx autofixdata.PromptContext) (string, error) {
	file := multiStageTargetFile(ctx.Request.File, ctx.FilePath)

	runtimeSummary, err := summarizeFinalStageRuntime(ctx.OrigParse, ctx.Source, ctx.Config)
	if err != nil {
		return "", err
	}

	normalized := autofixdata.NormalizeLF(string(ctx.Source))
	lines := autofixdata.CountLines(normalized)

	var b strings.Builder
	writeMultiStagePreamble(&b, runtimeSummary)
	autofixdata.WriteFileContext(&b, ctx.AbsPath, ctx.ContextDir)
	autofixdata.WriteRegistryContext(&b, ctx.Request.RegistryInsights)
	autofixdata.WriteSignals(&b, ctx.Request.Signals)
	autofixdata.WriteInputDockerfile(&b, file, lines, normalized)
	autofixdata.WriteOutputFormat(&b, file, ctx.Mode)
	return b.String(), nil
}

func (o *multiStageObjective) BuildRetryPrompt(ctx autofixdata.RetryPromptContext) (string, error) {
	issuesJSON, err := json.Marshal(ctx.BlockingIssues, jsontext.WithIndentPrefix(""), jsontext.WithIndent("  "))
	if err != nil {
		return "", fmt.Errorf("ai-autofix: marshal blocking issues: %w", err)
	}

	file := filepath.Base(ctx.FilePath)

	var b strings.Builder
	b.WriteString("You previously produced a Dockerfile refactor, but tally found blocking issues.\n")
	b.WriteString("Fix ONLY the issues listed below.\n")
	b.WriteString("- Do not make any other changes.\n")
	b.WriteString("- Preserve runtime settings in the final stage exactly: ENTRYPOINT, CMD, EXPOSE, USER, WORKDIR, ENV, LABEL, ")
	b.WriteString("HEALTHCHECK.\n\n")

	autofixdata.WriteFileContext(&b, ctx.AbsPath, ctx.ContextDir)

	b.WriteString("Blocking issues (JSON):\n")
	b.Write(issuesJSON)
	b.WriteString("\n\n")

	autofixdata.WriteProposedDockerfile(&b, ctx.Proposed, ctx.Mode)
	autofixdata.WriteRetryOutputFormat(&b, file, ctx.Mode)

	return b.String(), nil
}

func (o *multiStageObjective) BuildSimplifiedPrompt(ctx autofixdata.SimplifiedPromptContext) string {
	var b strings.Builder
	b.WriteString("Convert the Dockerfile below to a correct multi-stage build.\n")
	b.WriteString("Only do the multi-stage conversion; do not optimize or rewrite unrelated parts.\n")
	b.WriteString("If you cannot do so safely, output exactly: NO_CHANGE.\n\n")
	autofixdata.WriteFileContext(&b, ctx.AbsPath, ctx.ContextDir)
	autofixdata.WriteSimplifiedInput(&b, filepath.Base(ctx.FilePath), ctx.Source, ctx.Mode)
	return b.String()
}

func (o *multiStageObjective) ValidateProposal(
	orig, proposed *dockerfile.ParseResult,
) []autofixdata.BlockingIssue {
	var blocking []autofixdata.BlockingIssue

	if err := validateStageCount(orig, proposed); err != nil {
		blocking = append(blocking, autofixdata.BlockingIssue{Rule: "semantics", Message: err.Error()})
	}

	for _, err := range runtimeValidationErrors(orig, proposed) {
		blocking = append(blocking, autofixdata.BlockingIssue{Rule: "runtime", Message: err.Error()})
	}

	return blocking
}

// multiStageTargetFile returns a stable, basename-only file name for use in
// diff headers and prompt labels across all rounds (initial, retry, fallback).
func multiStageTargetFile(requestFile, filePath string) string {
	if f := strings.TrimSpace(requestFile); f != "" {
		return filepath.Base(f)
	}
	return filepath.Base(filePath)
}

// ValidatePatch returns nil — stage-count enforcement is handled by
// ValidateProposal after the patch is applied. A patch-level FROM check
// would break round-2 retries where the agent only fixes runtime issues
// on an already-converted multi-stage Dockerfile.
func (o *multiStageObjective) ValidatePatch(_ patchutil.Meta) []autofixdata.BlockingIssue {
	return nil
}

// --- Multi-stage-specific prompt helpers ---

func writeMultiStagePreamble(b *strings.Builder, runtimeSummary string) {
	b.WriteString(`You are a software engineer with deep knowledge of Dockerfile semantics.

Task: convert the Dockerfile below to a correct multi-stage build.
  - Use one or more builder/cache stages as needed.
  - Ensure there is a final runtime stage.
Goals:
- Reduce the final image size (primary).
- Improve build caching (secondary).

Rules (strict):
- Only do the multi-stage conversion. Do not optimize or rewrite unrelated parts unless required for the conversion.
- Keep all comments. If you move code lines, move any related comments with them (no orphaned comments).
- If you need to communicate an assumption, add a VERY concise comment inside the Dockerfile.
  - Do not output prose outside the required fenced code block.
- If clearly safe, you may choose a smaller runtime base image (e.g. scratch or distroless) to reduce final size.
  - If not clearly safe, keep the runtime base image unchanged.
- Final-stage runtime settings must remain identical (tally validates this):
`)
	b.WriteString(runtimeSummary)
	b.WriteString(`- If you cannot satisfy these rules safely, output exactly: NO_CHANGE.

`)
}

type finalStageRuntime struct {
	workdir     []string
	user        []string
	envKeys     []string
	envCount    int
	labelKeys   []string
	labelCount  int
	exposePorts []string
	exposeCount int
	healthcheck []string
	entrypoint  []string
	cmd         []string
}

func summarizeFinalStageRuntime(parsed *dockerfile.ParseResult, source []byte, cfg *config.Config) (string, error) {
	if parsed == nil {
		var err error
		parsed, err = dockerfile.Parse(bytes.NewReader(source), cfg)
		if err != nil {
			return "", fmt.Errorf("ai-autofix: parse input Dockerfile for prompt: %w", err)
		}
	}
	if parsed == nil || len(parsed.Stages) == 0 {
		return "", errors.New("ai-autofix: parsed Dockerfile has no stages")
	}

	stage := parsed.Stages[len(parsed.Stages)-1]
	rt := extractFinalStageRuntime(stage)

	lines := make([]string, 0, 10)
	present := map[string]bool{}

	addLine := func(key, label string, count int, detail string) {
		if count == 0 {
			return
		}
		present[key] = true
		var b strings.Builder
		b.WriteString("  - ")
		b.WriteString(label)
		if count > 1 {
			b.WriteString(" (")
			b.WriteString(strconv.Itoa(count))
			b.WriteString(")")
		}
		if detail != "" {
			b.WriteString(": ")
			b.WriteString(detail)
		}
		lines = append(lines, b.String())
	}

	upper := strings.ToUpper
	addLine(upper(command.Workdir), upper(command.Workdir), len(rt.workdir), strings.Join(rt.workdir, " | "))
	addLine(upper(command.User), upper(command.User), len(rt.user), strings.Join(rt.user, " | "))
	addLine(upper(command.Env), upper(command.Env), rt.envCount, "keys="+autofixdata.FormatList(rt.envKeys, 8))
	addLine(upper(command.Label), upper(command.Label), rt.labelCount, "keys="+autofixdata.FormatList(rt.labelKeys, 8))
	addLine(upper(command.Expose), upper(command.Expose), rt.exposeCount, "ports="+autofixdata.FormatList(rt.exposePorts, 12))
	addLine(upper(command.Healthcheck), upper(command.Healthcheck), len(rt.healthcheck), strings.Join(rt.healthcheck, " | "))
	addLine(upper(command.Entrypoint), upper(command.Entrypoint), len(rt.entrypoint), strings.Join(rt.entrypoint, " | "))
	addLine(upper(command.Cmd), upper(command.Cmd), len(rt.cmd), strings.Join(rt.cmd, " | "))

	orderedKeys := []string{
		upper(command.Workdir), upper(command.User), upper(command.Env), upper(command.Label),
		upper(command.Expose), upper(command.Healthcheck), upper(command.Entrypoint), upper(command.Cmd),
	}
	missing := make([]string, 0, len(orderedKeys))
	for _, k := range orderedKeys {
		if !present[k] {
			missing = append(missing, k)
		}
	}

	if len(lines) == 0 {
		lines = append(lines, "  (none)")
	}
	if len(missing) > 0 {
		lines = append(lines, "  - Absent in input (do not add): "+strings.Join(missing, ", "))
	}

	return strings.Join(lines, "\n") + "\n", nil
}

func extractFinalStageRuntime(stage instructions.Stage) finalStageRuntime {
	var rt finalStageRuntime
	for _, cmd := range stage.Commands {
		switch c := cmd.(type) {
		case *instructions.WorkdirCommand:
			rt.workdir = append(rt.workdir, c.String())
		case *instructions.UserCommand:
			rt.user = append(rt.user, c.String())
		case *instructions.EnvCommand:
			rt.envCount++
			for _, kv := range c.Env {
				rt.envKeys = append(rt.envKeys, kv.Key)
			}
		case *instructions.LabelCommand:
			rt.labelCount++
			for _, kv := range c.Labels {
				rt.labelKeys = append(rt.labelKeys, kv.Key)
			}
		case *instructions.ExposeCommand:
			rt.exposeCount++
			rt.exposePorts = append(rt.exposePorts, c.Ports...)
		case *instructions.HealthCheckCommand:
			rt.healthcheck = append(rt.healthcheck, c.String())
		case *instructions.EntrypointCommand:
			rt.entrypoint = append(rt.entrypoint, c.String())
		case *instructions.CmdCommand:
			rt.cmd = append(rt.cmd, c.String())
		}
	}
	return rt
}

// --- Multi-stage-specific validation ---

func countFromInstructions(pr *dockerfile.ParseResult) int {
	if pr == nil {
		return 0
	}

	count := 0
	for _, stage := range pr.Stages {
		if strings.TrimSpace(stage.SourceCode) == "" {
			continue
		}
		count++
	}
	return count
}

func validateStageCount(orig, proposed *dockerfile.ParseResult) error {
	if proposed == nil {
		return errors.New("proposed parse result is nil")
	}

	proposedFrom := countFromInstructions(proposed)
	if proposedFrom == 0 {
		return errors.New("proposed Dockerfile has no FROM instruction")
	}

	if orig != nil && countFromInstructions(orig) == 1 {
		if proposedFrom < 2 {
			return errors.New("proposed Dockerfile still has a single stage (expected 2+ stages)")
		}
	}
	return nil
}

// runtimeValidationErrors is a thin wrapper around the shared
// autofixdata.FinalStageRuntimeErrors, kept for external test callers.
func runtimeValidationErrors(orig, proposed *dockerfile.ParseResult) []error {
	return autofixdata.FinalStageRuntimeErrors(orig, proposed)
}
