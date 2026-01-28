// Package secretsincode implements secret detection in Dockerfile content.
// This rule scans heredocs, RUN commands, ENV values, and ARG defaults for
// actual secrets like API keys, private keys, and credentials.
//
// This goes beyond BuildKit's SecretsUsedInArgOrEnv which only checks variable
// names - we detect actual secret values using gitleaks' curated pattern database.
package secretsincode

import (
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"
	"github.com/moby/buildkit/frontend/dockerfile/parser"
	"github.com/zricethezav/gitleaks/v8/detect"

	"github.com/tinovyatkin/tally/internal/rules"
)

// Rule implements secret detection in Dockerfile content.
type Rule struct {
	detector *detect.Detector
}

// Metadata returns the rule metadata.
func (r *Rule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:             rules.TallyRulePrefix + "secrets-in-code",
		Name:             "Secrets in Dockerfile Content",
		Description:      "Detects hardcoded secrets, API keys, and credentials in Dockerfile content",
		DocURL:           "https://github.com/tinovyatkin/tally#secrets-in-code",
		DefaultSeverity:  rules.SeverityError, // Secrets are serious
		Category:         "security",
		EnabledByDefault: true,
		IsExperimental:   true, // New rule, mark as experimental initially
	}
}

// Check scans the Dockerfile for hardcoded secrets.
func (r *Rule) Check(input rules.LintInput) []rules.Violation {
	// Lazy-initialize detector
	if r.detector == nil {
		d, err := detect.NewDetectorDefaultConfig()
		if err != nil {
			// If we can't create detector, skip this rule silently
			return nil
		}
		r.detector = d
	}

	var violations []rules.Violation

	// Check global ARG default values
	violations = append(violations, r.checkMetaArgs(input)...)

	// Check stage commands
	for _, stage := range input.Stages {
		violations = append(violations, r.checkStageCommands(input.File, stage.Commands)...)
	}

	return violations
}

// checkMetaArgs scans global ARG default values.
func (r *Rule) checkMetaArgs(input rules.LintInput) []rules.Violation {
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
func (r *Rule) checkStageCommands(file string, commands []instructions.Command) []rules.Violation {
	var violations []rules.Violation //nolint:prealloc // size unknown: depends on secret findings
	for _, cmd := range commands {
		violations = append(violations, r.checkCommand(file, cmd)...)
	}
	return violations
}

// checkCommand scans a single command for secrets.
func (r *Rule) checkCommand(file string, cmd instructions.Command) []rules.Violation {
	switch c := cmd.(type) {
	case *instructions.RunCommand:
		return r.checkRunCommand(file, c)
	case *instructions.CopyCommand:
		return r.checkCopyCommand(file, c)
	case *instructions.AddCommand:
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

// checkRunCommand scans RUN commands for secrets.
func (r *Rule) checkRunCommand(file string, c *instructions.RunCommand) []rules.Violation {
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
func (r *Rule) checkCopyCommand(file string, c *instructions.CopyCommand) []rules.Violation {
	var violations []rules.Violation //nolint:prealloc // size unknown: depends on secret findings
	for _, content := range c.SourceContents {
		violations = append(violations,
			r.scanContent(content.Data, file, c.Location(), "COPY heredoc")...)
	}
	return violations
}

// checkAddCommand scans ADD heredocs for secrets.
func (r *Rule) checkAddCommand(file string, c *instructions.AddCommand) []rules.Violation {
	var violations []rules.Violation //nolint:prealloc // size unknown: depends on secret findings
	for _, content := range c.SourceContents {
		violations = append(violations,
			r.scanContent(content.Data, file, c.Location(), "ADD heredoc")...)
	}
	return violations
}

// checkEnvCommand scans ENV values for secrets.
func (r *Rule) checkEnvCommand(file string, c *instructions.EnvCommand) []rules.Violation {
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
func (r *Rule) checkArgCommand(file string, c *instructions.ArgCommand) []rules.Violation {
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
func (r *Rule) checkLabelCommand(file string, c *instructions.LabelCommand) []rules.Violation {
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
func (r *Rule) scanContent(
	content, file string,
	location []parser.Range,
	context string,
) []rules.Violation {
	if content == "" {
		return nil
	}

	findings := r.detector.DetectString(content)
	if len(findings) == 0 {
		return nil
	}

	var violations []rules.Violation
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

// New creates a new SecretsInCode rule instance.
func New() *Rule {
	return &Rule{}
}

// init registers the rule with the default registry.
func init() {
	rules.Register(New())
}
