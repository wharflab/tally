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

	"github.com/tinovyatkin/tally/internal/ai/acp"
	"github.com/tinovyatkin/tally/internal/ai/autofixdata"
	"github.com/tinovyatkin/tally/internal/config"
	"github.com/tinovyatkin/tally/internal/dockerfile"
	"github.com/tinovyatkin/tally/internal/fix"
	"github.com/tinovyatkin/tally/internal/linter"
	"github.com/tinovyatkin/tally/internal/processor"
	"github.com/tinovyatkin/tally/internal/rules"
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
	req, err := multiStageRequest(sf)
	if err != nil {
		return nil, err
	}

	cfg := req.Config
	if cfg == nil {
		return nil, errors.New("ai-autofix: missing config on resolver data")
	}
	timeout, err := validateAIConfig(cfg)
	if err != nil {
		return nil, err
	}

	origParse, err := parseDockerfile(resolveCtx.Content, cfg)
	if err != nil {
		return nil, fmt.Errorf("ai-autofix: parse original: %w", err)
	}

	proposed, err := r.proposeMultiStageDockerfile(ctx, resolveCtx.FilePath, resolveCtx.Content, req, cfg, timeout, origParse)
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

func multiStageRequest(sf *rules.SuggestedFix) (*autofixdata.MultiStageResolveData, error) {
	req, ok := sf.ResolverData.(*autofixdata.MultiStageResolveData)
	if !ok || req == nil {
		return nil, errors.New("ai-autofix: invalid resolver data")
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

func (r *resolver) proposeMultiStageDockerfile(
	ctx context.Context,
	filePath string,
	original []byte,
	req *autofixdata.MultiStageResolveData,
	cfg *config.Config,
	timeout time.Duration,
	origParse *dockerfile.ParseResult,
) ([]byte, error) {
	roundInput := original
	var proposed []byte
	var blocking []blockingIssue

	for round := 1; round <= maxAgentRounds; round++ {
		prompt, err := buildRoundPrompt(round, filePath, roundInput, proposed, blocking, req, cfg, origParse)
		if err != nil {
			return nil, err
		}

		parsed, noChange, err := r.runAndParseRound(ctx, filePath, cfg, timeout, prompt, roundInput)
		if err != nil {
			return nil, err
		}
		if noChange {
			return nil, nil
		}

		proposed = []byte(parsed)
		proposed, blocking, err = r.checkProposal(ctx, filePath, proposed, cfg, origParse, req.FixContext)
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

func buildRoundPrompt(
	round int,
	filePath string,
	roundInput []byte,
	proposed []byte,
	blocking []blockingIssue,
	req *autofixdata.MultiStageResolveData,
	cfg *config.Config,
	origParse *dockerfile.ParseResult,
) (string, error) {
	switch round {
	case 1:
		return buildRound1Prompt(filePath, roundInput, req, cfg, origParse)
	case 2:
		return buildRound2Prompt(filePath, proposed, blocking, cfg)
	default:
		return "", errors.New("ai-autofix: unexpected round")
	}
}

func (r *resolver) runAndParseRound(
	ctx context.Context,
	filePath string,
	cfg *config.Config,
	timeout time.Duration,
	prompt string,
	roundInput []byte,
) (string, bool, error) {
	respText, err := r.runAgent(ctx, filePath, cfg, timeout, prompt)
	if err != nil {
		return "", false, err
	}

	parsed, noChange, perr := parseAgentResponse(respText)
	if perr == nil {
		if noChange {
			return "", true, nil
		}
		out, err := normalizeAgentDockerfile(parsed)
		return out, noChange, err
	}

	lastErr := perr
	for range maxMalformedRetries {
		simplePrompt := buildSimplifiedPrompt(filePath, roundInput, cfg)
		respText, err = r.runAgent(ctx, filePath, cfg, timeout, simplePrompt)
		if err != nil {
			return "", false, err
		}
		parsed, noChange, perr = parseAgentResponse(respText)
		if perr != nil {
			lastErr = perr
			continue
		}
		if noChange {
			return "", true, nil
		}
		out, err := normalizeAgentDockerfile(parsed)
		return out, noChange, err
	}

	return "", false, fmt.Errorf("ai-autofix: malformed agent output: %w", lastErr)
}

func normalizeAgentDockerfile(parsed string) (string, error) {
	parsed = normalizeLF(parsed)
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
	fixCtx autofixdata.FixContext,
) ([]byte, []blockingIssue, error) {
	var blocking []blockingIssue

	propParse, parseErr := parseDockerfile(proposed, cfg)
	if parseErr != nil {
		blocking = []blockingIssue{{
			Rule:    "syntax",
			Message: "proposed Dockerfile failed to parse: " + parseErr.Error(),
		}}
	} else if err := validateStageCount(origParse, propParse); err != nil {
		blocking = []blockingIssue{{Rule: "semantics", Message: err.Error()}}
	} else if err := validateRuntimeSettings(origParse, propParse); err != nil {
		blocking = []blockingIssue{{Rule: "runtime", Message: err.Error()}}
	}

	if len(blocking) > 0 {
		return proposed, blocking, nil
	}

	return r.validateWithLint(ctx, filePath, proposed, cfg, fixCtx)
}

func (r *resolver) runAgent(
	ctx context.Context,
	filePath string,
	cfg *config.Config,
	timeout time.Duration,
	prompt string,
) (string, error) {
	if cfg.AI.MaxInputBytes > 0 && len(prompt) > cfg.AI.MaxInputBytes {
		return "", fmt.Errorf("ai-autofix: prompt too large (%d bytes > ai.max-input-bytes=%d)", len(prompt), cfg.AI.MaxInputBytes)
	}

	redacted := prompt
	if cfg.AI.RedactSecrets {
		det, err := r.gitleaksFactory()
		if err != nil {
			return "", fmt.Errorf("ai-autofix: redact-secrets enabled but detector init failed: %w", err)
		}
		var redactions int
		redacted, redactions = redactSecrets(det, redacted)
		_ = redactions // Intentionally not logged (avoid leakage via logs).
	}

	cwd := filepath.Dir(filePath)
	resp, err := r.runner.Run(ctx, acp.RunRequest{
		Command: cfg.AI.Command,
		Cwd:     cwd,
		Timeout: timeout,
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

func normalizeLF(s string) string {
	return strings.ReplaceAll(s, "\r\n", "\n")
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
) ([]byte, []blockingIssue, error) {
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
