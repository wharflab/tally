package autofix

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/zricethezav/gitleaks/v8/detect"

	"github.com/wharflab/tally/internal/ai/acp"
	"github.com/wharflab/tally/internal/ai/autofixdata"
	"github.com/wharflab/tally/internal/config"
	"github.com/wharflab/tally/internal/dockerfile"
	"github.com/wharflab/tally/internal/fix"
	"github.com/wharflab/tally/internal/linter"
	patchutil "github.com/wharflab/tally/internal/patch"
	"github.com/wharflab/tally/internal/processor"
	"github.com/wharflab/tally/internal/rules"
)

const (
	maxAgentRounds       = 2
	maxMalformedRetries  = 1
	unreachableStagesKey = "tally/no-unreachable-stages"
)

type resolver struct {
	runner          agentRunner
	gitleaksFactory func() (*detect.Detector, error)
}

// agentConfig bundles the validated config and timeout for the agent loop,
// reducing parameter counts on internal functions.
type agentConfig struct {
	cfg     *config.Config
	timeout time.Duration
}

type agentRunner interface {
	Run(ctx context.Context, req acp.RunRequest) (acp.RunResponse, error)
}

func newResolver() *resolver {
	return &resolver{
		runner:          acp.NewRunner(),
		gitleaksFactory: detect.NewDetectorDefaultConfig,
	}
}

func (r *resolver) ID() string { return autofixdata.ResolverID }

func (r *resolver) Resolve(ctx context.Context, resolveCtx fix.ResolveContext, sf *rules.SuggestedFix) ([]rules.TextEdit, error) {
	req, err := objectiveRequest(sf)
	if err != nil {
		return nil, err
	}

	obj, ok := autofixdata.GetObjective(req.Kind)
	if !ok {
		return nil, fmt.Errorf("ai-autofix: unknown objective kind %q", req.Kind)
	}

	cfg := req.Config
	if cfg == nil {
		return nil, errors.New("ai-autofix: missing config on resolver data")
	}
	timeout, err := validateAIConfig(cfg)
	if err != nil {
		return nil, err
	}
	ac := agentConfig{cfg: cfg, timeout: timeout}

	origParse, err := parseDockerfile(resolveCtx.Content, cfg)
	if err != nil {
		return nil, fmt.Errorf("ai-autofix: parse original: %w", err)
	}

	proposed, err := r.proposeDockerfile(ctx, resolveCtx.FilePath, resolveCtx.Content, req, obj, ac, origParse)
	if err != nil || proposed == nil {
		return nil, err
	}

	newText := string(proposed)
	if bytes.HasSuffix(resolveCtx.Content, []byte("\n")) && !strings.HasSuffix(newText, "\n") {
		newText += "\n"
	}
	if bytes.Equal(resolveCtx.Content, []byte(newText)) {
		return nil, nil
	}
	return []rules.TextEdit{wholeFileReplacement(resolveCtx.FilePath, resolveCtx.Content, newText)}, nil
}

func objectiveRequest(sf *rules.SuggestedFix) (*autofixdata.ObjectiveRequest, error) {
	req, ok := sf.ResolverData.(*autofixdata.ObjectiveRequest)
	if !ok || req == nil {
		return nil, errors.New("ai-autofix: invalid resolver data (expected *ObjectiveRequest)")
	}
	return req, nil
}

func validateAIConfig(cfg *config.Config) (time.Duration, error) {
	if !cfg.AI.Enabled {
		return 0, errors.New("ai-autofix: ai disabled")
	}
	if len(cfg.AI.Command) == 0 {
		return 0, errors.New("ai-autofix: agent command is empty")
	}

	timeout, err := time.ParseDuration(cfg.AI.Timeout)
	if err != nil || timeout <= 0 {
		return 0, fmt.Errorf("ai-autofix: invalid ai.timeout %q", cfg.AI.Timeout)
	}
	return timeout, nil
}

func (r *resolver) proposeDockerfile(
	ctx context.Context,
	filePath string,
	original []byte,
	req *autofixdata.ObjectiveRequest,
	obj autofixdata.Objective,
	ac agentConfig,
	origParse *dockerfile.ParseResult,
) ([]byte, error) {
	roundInput := []byte(autofixdata.NormalizeLF(string(original)))
	var proposed []byte
	var blocking []autofixdata.BlockingIssue
	mode := autofixdata.OutputPatch

	rp := roundPromptParams{
		filePath:   filePath,
		absPath:    resolveAbsPath(filePath),
		contextDir: req.ContextDir,
		obj:        obj,
		req:        req,
		cfg:        ac.cfg,
		origParse:  origParse,
	}
	for round := 1; round <= maxAgentRounds; round++ {
	retryCurrentRound:
		rp.input = roundInput
		rp.proposed = proposed
		rp.blocking = blocking
		prompt, err := buildRoundPrompt(round, rp, mode)
		if err != nil {
			return nil, err
		}

		result, err := r.runRound(ctx, filePath, ac, prompt, roundInput, obj, mode)
		var fallbackErr *patchFallbackError
		if errors.As(err, &fallbackErr) {
			mode = autofixdata.OutputDockerfile
			goto retryCurrentRound
		}
		if err != nil {
			return nil, err
		}
		if result.noChange {
			return nil, nil
		}

		proposed = result.proposed
		if mode == autofixdata.OutputPatch {
			blocking = obj.ValidatePatch(result.patchMeta)
			if len(blocking) > 0 {
				roundInput = proposed
				continue
			}
		}

		proposed, blocking, err = r.checkProposal(ctx, filePath, proposed, ac.cfg, origParse, obj, req.FixContext)
		if err != nil {
			return nil, err
		}
		if len(blocking) == 0 {
			return proposed, nil
		}

		roundInput = proposed
	}

	return nil, errors.New("ai-autofix: blocking issues remain after max rounds")
}

// roundPromptParams bundles the inputs for buildRoundPrompt across rounds.
type roundPromptParams struct {
	filePath   string
	absPath    string
	contextDir string
	input      []byte // round 1: original content; round 2+: previous proposal
	proposed   []byte // round 2+: proposed content for retry
	blocking   []autofixdata.BlockingIssue
	obj        autofixdata.Objective
	req        *autofixdata.ObjectiveRequest
	cfg        *config.Config
	origParse  *dockerfile.ParseResult
}

func buildRoundPrompt(round int, p roundPromptParams, mode autofixdata.OutputMode) (string, error) {
	switch round {
	case 1:
		return p.obj.BuildPrompt(autofixdata.PromptContext{
			FilePath:   p.filePath,
			Source:     p.input,
			Request:    p.req,
			Config:     p.cfg,
			AbsPath:    p.absPath,
			ContextDir: p.contextDir,
			OrigParse:  p.origParse,
			Mode:       mode,
		})
	case 2:
		return p.obj.BuildRetryPrompt(autofixdata.RetryPromptContext{
			FilePath:       p.filePath,
			Proposed:       p.proposed,
			BlockingIssues: p.blocking,
			Config:         p.cfg,
			Mode:           mode,
		})
	default:
		return "", errors.New("ai-autofix: unexpected round")
	}
}

type roundResult struct {
	proposed  []byte
	noChange  bool
	patchMeta patchutil.Meta
}

type patchFallbackError struct {
	err error
}

func (e *patchFallbackError) Error() string {
	return "ai-autofix: falling back to Dockerfile output after patch-mode mechanical failures: " + e.err.Error()
}

func (e *patchFallbackError) Unwrap() error { return e.err }

func (r *resolver) runRound(
	ctx context.Context,
	filePath string,
	ac agentConfig,
	prompt string,
	roundInput []byte,
	obj autofixdata.Objective,
	mode autofixdata.OutputMode,
) (roundResult, error) {
	respText, err := r.runAgent(ctx, filePath, ac, prompt, roundInput, mode)
	if err != nil {
		return roundResult{}, err
	}

	result, perr := parseRoundOutput(respText, roundInput, mode)
	if perr == nil {
		return result, nil
	}

	lastErr := perr
	for range maxMalformedRetries {
		simplePrompt := obj.BuildSimplifiedPrompt(autofixdata.SimplifiedPromptContext{
			FilePath: filePath,
			Source:   roundInput,
			Mode:     mode,
		})
		respText, err = r.runAgent(ctx, filePath, ac, simplePrompt, roundInput, mode)
		if err != nil {
			return roundResult{}, err
		}
		result, perr = parseRoundOutput(respText, roundInput, mode)
		if perr != nil {
			lastErr = perr
			continue
		}
		return result, nil
	}

	if mode == autofixdata.OutputPatch {
		return roundResult{}, &patchFallbackError{err: lastErr}
	}

	return roundResult{}, fmt.Errorf("ai-autofix: malformed agent output: %w", lastErr)
}

func parseRoundOutput(text string, roundInput []byte, mode autofixdata.OutputMode) (roundResult, error) {
	switch mode {
	case autofixdata.OutputPatch:
		parsed, noChange, err := parseAgentPatchResponse(text)
		if err != nil {
			return roundResult{}, err
		}
		if noChange {
			return roundResult{noChange: true}, nil
		}
		proposed, meta, err := patchutil.ParseAndApply(roundInput, parsed)
		if err != nil {
			return roundResult{}, err
		}
		return roundResult{proposed: proposed, patchMeta: meta}, nil
	case autofixdata.OutputDockerfile:
		parsed, noChange, err := parseAgentDockerfileResponse(text)
		if err != nil {
			return roundResult{}, err
		}
		if noChange {
			return roundResult{noChange: true}, nil
		}
		out, err := normalizeAgentDockerfile(parsed)
		if err != nil {
			return roundResult{}, err
		}
		return roundResult{proposed: []byte(out)}, nil
	default:
		return roundResult{}, errors.New("ai-autofix: unsupported agent output mode")
	}
}

func normalizeAgentDockerfile(parsed string) (string, error) {
	parsed = autofixdata.NormalizeLF(parsed)
	if parsed == "" {
		return "", errors.New("ai-autofix: empty Dockerfile output")
	}
	return parsed, nil
}

func (r *resolver) checkProposal(
	ctx context.Context,
	filePath string,
	proposed []byte,
	cfg *config.Config,
	origParse *dockerfile.ParseResult,
	obj autofixdata.Objective,
	fixCtx autofixdata.FixContext,
) ([]byte, []autofixdata.BlockingIssue, error) {
	var blocking []autofixdata.BlockingIssue

	propParse, parseErr := parseDockerfile(proposed, cfg)
	if parseErr != nil {
		blocking = []autofixdata.BlockingIssue{{
			Rule:    "syntax",
			Message: "proposed Dockerfile failed to parse: " + parseErr.Error(),
		}}
	} else {
		blocking = obj.ValidateProposal(origParse, propParse)
	}

	if len(blocking) > 0 {
		return proposed, blocking, nil
	}

	return r.validateWithLint(ctx, filePath, proposed, cfg, fixCtx)
}

func (r *resolver) runAgent(
	ctx context.Context,
	filePath string,
	ac agentConfig,
	prompt string,
	roundInput []byte,
	mode autofixdata.OutputMode,
) (string, error) {
	cfg := ac.cfg
	if cfg.AI.MaxInputBytes > 0 && len(prompt) > cfg.AI.MaxInputBytes {
		return "", fmt.Errorf(
			"ai-autofix: prompt too large (%d bytes > ai.max-input-bytes=%d)",
			len(prompt), cfg.AI.MaxInputBytes,
		)
	}

	redacted := prompt
	if cfg.AI.RedactSecrets {
		det, err := r.gitleaksFactory()
		if err != nil {
			return "", fmt.Errorf("ai-autofix: redact-secrets enabled but detector init failed: %w", err)
		}
		if mode == autofixdata.OutputPatch && countSecrets(det, string(roundInput)) > 0 {
			return "", &patchFallbackError{err: errors.New(
				"ai-autofix: ai.redact-secrets=true and secrets were detected in the Dockerfile payload",
			)}
		}
		var redactions int
		redacted, redactions = redactSecrets(det, redacted)
		_ = redactions // Intentionally not logged (avoid leakage via logs).
	}

	cwd := filepath.Dir(filePath)
	resp, err := r.runner.Run(ctx, acp.RunRequest{
		Command: cfg.AI.Command,
		Cwd:     cwd,
		Timeout: ac.timeout,
		Prompt:  redacted,
	})
	if err != nil {
		return "", err
	}
	return resp.Text, nil
}

func parseDockerfile(content []byte, cfg *config.Config) (*dockerfile.ParseResult, error) {
	return dockerfile.Parse(bytes.NewReader(content), cfg)
}

// resolveAbsPath returns the absolute path for a real file, or empty for
// synthetic paths (stdin, LSP virtual files).
func resolveAbsPath(filePath string) string {
	if filePath == "" || strings.HasPrefix(filePath, "<") {
		return ""
	}
	abs, err := filepath.Abs(filePath)
	if err != nil {
		return ""
	}
	return abs
}

func countSecrets(det *detect.Detector, input string) int {
	findings := det.DetectString(input)
	if len(findings) == 0 {
		return 0
	}
	seen := map[string]struct{}{}
	for _, f := range findings {
		if f.Secret == "" {
			continue
		}
		seen[f.Secret] = struct{}{}
	}
	return len(seen)
}

func redactSecrets(det *detect.Detector, input string) (string, int) {
	findings := det.DetectString(input)
	if len(findings) == 0 {
		return input, 0
	}
	out := input
	redactions := 0
	for _, f := range findings {
		if f.Secret == "" {
			continue
		}
		if strings.Contains(out, f.Secret) {
			out = strings.ReplaceAll(out, f.Secret, "REDACTED")
			redactions++
		}
	}
	return out, redactions
}

func (r *resolver) validateWithLint(
	ctx context.Context,
	filePath string,
	proposed []byte,
	cfg *config.Config,
	fixCtx autofixdata.FixContext,
) ([]byte, []autofixdata.BlockingIssue, error) {
	lintCfg := *cfg
	lintCfg.AI = config.AIConfig{Enabled: false}

	violations, err := lintAndProcess(filePath, proposed, &lintCfg)
	if err != nil {
		return nil, nil, err
	}

	if len(fixCtx.RuleFilter) == 0 {
		normalized, err := applySafeSyncFixes(ctx, filePath, proposed, violations, fixCtx.FixModes)
		if err != nil {
			return nil, nil, err
		}
		if !bytes.Equal(normalized, proposed) {
			proposed = normalized
			violations, err = lintAndProcess(filePath, proposed, &lintCfg)
			if err != nil {
				return nil, nil, err
			}
		}
	}

	blocking := collectBlockingIssues(violations)
	return proposed, blocking, nil
}

func lintAndProcess(filePath string, content []byte, cfg *config.Config) ([]rules.Violation, error) {
	res, err := linter.LintFile(linter.Input{
		FilePath: filePath,
		Content:  content,
		Config:   cfg,
	})
	if err != nil {
		return nil, err
	}

	chain, _ := linter.CLIProcessors()
	procCtx := processor.NewContext(map[string]*config.Config{filePath: cfg}, cfg, map[string][]byte{filePath: content})
	return chain.Process(res.Violations, procCtx), nil
}

func applySafeSyncFixes(
	ctx context.Context,
	filePath string,
	content []byte,
	violations []rules.Violation,
	fixModes map[string]map[string]config.FixMode,
) ([]byte, error) {
	// Filter to deterministic safe sync fixes only.
	filtered := make([]rules.Violation, 0, len(violations))
	for _, v := range violations {
		if v.SuggestedFix == nil {
			continue
		}
		if v.SuggestedFix.NeedsResolve {
			continue
		}
		if v.SuggestedFix.Safety != rules.FixSafe {
			continue
		}
		filtered = append(filtered, v)
	}
	if len(filtered) == 0 {
		return content, nil
	}

	fixer := &fix.Fixer{
		SafetyThreshold: fix.FixSafe,
		FixModes:        fixModes,
	}
	result, err := fixer.Apply(ctx, filtered, map[string][]byte{filePath: content})
	if err != nil {
		return nil, err
	}
	fc := result.Changes[filepath.Clean(filePath)]
	if fc == nil {
		return content, nil
	}
	return fc.ModifiedContent, nil
}
