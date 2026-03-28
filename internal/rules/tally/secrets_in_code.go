package tally

import (
	"strings"
	"sync"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"
	"github.com/moby/buildkit/frontend/dockerfile/parser"
	"github.com/zricethezav/gitleaks/v8/detect"

	"github.com/wharflab/tally/internal/facts"
	"github.com/wharflab/tally/internal/rules"
)

var (
	gitleaksOnce     sync.Once
	gitleaksDetector *detect.Detector
)

// SecretsInCodeRuleCode is the full rule code for the secrets-in-code rule.
const SecretsInCodeRuleCode = rules.TallyRulePrefix + "secrets-in-code"

// SecretsInCodeRule implements secret detection in Dockerfile content.
type SecretsInCodeRule struct{}

// NewSecretsInCodeRule creates a new SecretsInCode rule instance.
func NewSecretsInCodeRule() *SecretsInCodeRule {
	return &SecretsInCodeRule{}
}

// Metadata returns the rule metadata.
func (r *SecretsInCodeRule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            SecretsInCodeRuleCode,
		Name:            "Secrets in Dockerfile Content",
		Description:     "Detects hardcoded secrets, API keys, and credentials in Dockerfile content",
		DocURL:          rules.TallyDocURL(SecretsInCodeRuleCode),
		DefaultSeverity: rules.SeverityError, // Secrets are serious
		Category:        "security",
		IsExperimental:  true, // New rule, mark as experimental initially
	}
}

// Check scans the Dockerfile for hardcoded secrets.
func (r *SecretsInCodeRule) Check(input rules.LintInput) []rules.Violation {
	gitleaksOnce.Do(func() {
		d, err := detect.NewDetectorDefaultConfig()
		if err != nil {
			return
		}
		gitleaksDetector = d
	})
	if gitleaksDetector == nil {
		return nil
	}

	var violations []rules.Violation

	// Check global ARG default values
	violations = append(violations, r.checkMetaArgs(input)...)

	fileFacts, _ := input.Facts.(*facts.FileFacts) //nolint:errcheck // nil-safe assertion

	// Check stage commands
	for stageIdx, stage := range input.Stages {
		if fileFacts != nil {
			if stageFacts := fileFacts.Stage(stageIdx); stageFacts != nil {
				violations = append(violations, r.checkObservableFiles(input.File, stageFacts)...)
			}
		}
		violations = append(violations, r.checkStageCommands(input.File, stage.Commands, fileFacts != nil)...)
	}

	return violations
}

// checkMetaArgs scans global ARG default values.
func (r *SecretsInCodeRule) checkMetaArgs(input rules.LintInput) []rules.Violation {
	var violations []rules.Violation
	for _, arg := range input.MetaArgs {
		for _, kv := range arg.Args {
			if kv.Value != nil && *kv.Value != "" {
				violations = append(violations,
					r.scanContent(*kv.Value, input.File, arg.Location(), "ARG default value")...)
			}
		}
	}
	return violations
}

// checkStageCommands scans commands within a stage.
func (r *SecretsInCodeRule) checkStageCommands(file string, commands []instructions.Command, skipCopyAdd bool) []rules.Violation {
	violations := make([]rules.Violation, 0, len(commands))
	for _, cmd := range commands {
		violations = append(violations, r.checkCommand(file, cmd, skipCopyAdd)...)
	}
	return violations
}

// checkCommand scans a single command for secrets.
func (r *SecretsInCodeRule) checkCommand(file string, cmd instructions.Command, skipCopyAdd bool) []rules.Violation {
	switch c := cmd.(type) {
	case *instructions.RunCommand:
		return r.checkRunCommand(file, c)
	case *instructions.CopyCommand:
		if skipCopyAdd {
			return nil
		}
		return r.checkCopyCommand(file, c)
	case *instructions.AddCommand:
		if skipCopyAdd {
			return nil
		}
		return r.checkAddCommand(file, c)
	case *instructions.EnvCommand:
		return r.checkEnvCommand(file, c)
	case *instructions.ArgCommand:
		return r.checkArgCommand(file, c)
	case *instructions.LabelCommand:
		return r.checkLabelCommand(file, c)
	}
	return nil
}

func (r *SecretsInCodeRule) checkObservableFiles(file string, stageFacts *facts.StageFacts) []rules.Violation {
	if stageFacts == nil {
		return nil
	}

	var violations []rules.Violation
	for _, observed := range stageFacts.ObservableFiles {
		if observed == nil {
			continue
		}

		content, ok := observed.Content()
		if !ok {
			continue
		}

		var context string
		switch observed.Source {
		case facts.ObservableFileSourceAddHeredoc:
			context = "ADD heredoc"
		case facts.ObservableFileSourceAddContext:
			context = "ADD context file"
		case facts.ObservableFileSourceCopyHeredoc:
			context = "COPY heredoc"
		case facts.ObservableFileSourceCopyContext:
			context = "COPY context file"
		case facts.ObservableFileSourceCopyStage:
			context = "COPY --from stage file"
		case facts.ObservableFileSourceRun:
			continue
		}

		violations = append(
			violations,
			r.scanContent(content, file, []parser.Range{{
				Start: parser.Position{Line: observed.Line},
				End:   parser.Position{Line: observed.Line},
			}}, context)...,
		)
	}
	return violations
}

// checkRunCommand scans RUN commands for secrets.
func (r *SecretsInCodeRule) checkRunCommand(file string, c *instructions.RunCommand) []rules.Violation {
	var violations []rules.Violation

	// Scan heredoc content in RUN commands
	for _, f := range c.Files {
		violations = append(violations,
			r.scanContent(f.Data, file, c.Location(), "RUN heredoc")...)
	}

	// Scan the command line itself for inline secrets
	if len(c.CmdLine) > 0 {
		cmdStr := strings.Join(c.CmdLine, " ")
		violations = append(violations,
			r.scanContent(cmdStr, file, c.Location(), "RUN command")...)
	}

	return violations
}

// checkCopyCommand scans COPY heredocs for secrets.
func (r *SecretsInCodeRule) checkCopyCommand(file string, c *instructions.CopyCommand) []rules.Violation {
	violations := make([]rules.Violation, 0, len(c.SourceContents))
	for _, content := range c.SourceContents {
		violations = append(violations,
			r.scanContent(content.Data, file, c.Location(), "COPY heredoc")...)
	}
	return violations
}

// checkAddCommand scans ADD heredocs for secrets.
func (r *SecretsInCodeRule) checkAddCommand(file string, c *instructions.AddCommand) []rules.Violation {
	violations := make([]rules.Violation, 0, len(c.SourceContents))
	for _, content := range c.SourceContents {
		violations = append(violations,
			r.scanContent(content.Data, file, c.Location(), "ADD heredoc")...)
	}
	return violations
}

// checkEnvCommand scans ENV values for secrets.
func (r *SecretsInCodeRule) checkEnvCommand(file string, c *instructions.EnvCommand) []rules.Violation {
	var violations []rules.Violation
	for _, kv := range c.Env {
		if kv.Value != "" {
			violations = append(violations,
				r.scanContent(kv.Value, file, c.Location(), "ENV value")...)
		}
	}
	return violations
}

// checkArgCommand scans ARG default values for secrets.
func (r *SecretsInCodeRule) checkArgCommand(file string, c *instructions.ArgCommand) []rules.Violation {
	var violations []rules.Violation
	for _, kv := range c.Args {
		if kv.Value != nil && *kv.Value != "" {
			violations = append(violations,
				r.scanContent(*kv.Value, file, c.Location(), "ARG default value")...)
		}
	}
	return violations
}

// checkLabelCommand scans LABEL values for secrets.
func (r *SecretsInCodeRule) checkLabelCommand(file string, c *instructions.LabelCommand) []rules.Violation {
	var violations []rules.Violation
	for _, kv := range c.Labels {
		if kv.Value != "" {
			violations = append(violations,
				r.scanContent(kv.Value, file, c.Location(), "LABEL value")...)
		}
	}
	return violations
}

// scanContent scans a string for secrets and returns violations.
func (r *SecretsInCodeRule) scanContent(
	content, file string,
	location []parser.Range,
	context string,
) []rules.Violation {
	if content == "" {
		return nil
	}

	findings := gitleaksDetector.DetectString(content)
	if len(findings) == 0 {
		return nil
	}

	violations := make([]rules.Violation, 0, len(findings))
	for _, finding := range findings {
		loc := rules.NewLocationFromRanges(file, location)

		// Redact the actual secret in the message
		redactedSecret := redact(finding.Secret)

		msg := finding.Description
		if msg == "" {
			msg = "Potential secret detected"
		}

		v := rules.NewViolation(
			loc,
			r.Metadata().Code,
			msg+" in "+context,
			r.Metadata().DefaultSeverity,
		).WithDetail(
			"Found: " + redactedSecret + " (rule: " + finding.RuleID + "). " +
				"Secrets in Dockerfiles are visible in image history and layers. " +
				"Use --mount=type=secret for build-time secrets or environment variables at runtime.",
		)

		violations = append(violations, v)
	}

	return violations
}

// redact redacts a secret for safe display.
func redact(secret string) string {
	if len(secret) <= 8 {
		return "***"
	}
	// Show first 4 and last 4 characters
	return secret[:4] + "..." + secret[len(secret)-4:]
}

// init registers the rule with the default registry.
func init() {
	rules.Register(NewSecretsInCodeRule())
}
