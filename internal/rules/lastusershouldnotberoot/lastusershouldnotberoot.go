// Package lastusershouldnotberoot implements hadolint DL3002.
// This rule warns when the last USER instruction in a stage is root,
// which is a security best practice violation.
package lastusershouldnotberoot

import (
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/tinovyatkin/tally/internal/rules"
)

// Rule implements the DL3002 linting rule.
type Rule struct{}

// Metadata returns the rule metadata.
func (r *Rule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            rules.HadolintRulePrefix + "DL3002",
		Name:            "Last USER should not be root",
		Description:     "Last USER should not be root to follow security best practices",
		DocURL:          "https://github.com/hadolint/hadolint/wiki/DL3002",
		DefaultSeverity: rules.SeverityWarning,
		Category:        "security",
		IsExperimental:  false,
	}
}

// Check runs the DL3002 rule.
// It warns when the last USER instruction in a stage is root.
// Only checks the final stage (the one that will actually be run).
func (r *Rule) Check(input rules.LintInput) []rules.Violation {
	if len(input.Stages) == 0 {
		return nil
	}

	// Only check the final stage - that's what actually runs in production
	finalStage := input.Stages[len(input.Stages)-1]

	// Find the last USER instruction in the final stage
	var lastUser *instructions.UserCommand
	for _, cmd := range finalStage.Commands {
		if user, ok := cmd.(*instructions.UserCommand); ok {
			lastUser = user
		}
	}

	// If there's no USER instruction at all, we could warn (but hadolint doesn't by default).
	// DL3002 specifically checks if the last USER is root.
	if lastUser == nil {
		return nil
	}

	// Check if the user is root
	if isRootUser(lastUser.User) {
		loc := rules.NewLocationFromRanges(input.File, lastUser.Location())
		return []rules.Violation{
			rules.NewViolation(
				loc,
				r.Metadata().Code,
				"last USER should not be root; use a non-privileged user for better security",
				r.Metadata().DefaultSeverity,
			).WithDocURL(r.Metadata().DocURL).WithDetail(
				"Running containers as root increases the attack surface. " +
					"Create a non-privileged user and switch to it with USER instruction. " +
					"For example: RUN useradd -m appuser && USER appuser",
			),
		}
	}

	return nil
}

// isRootUser checks if a user specification refers to the root user.
// The USER instruction can specify: username, uid, username:group, or uid:gid.
func isRootUser(user string) bool {
	// Strip group if present (user:group format)
	if idx := strings.Index(user, ":"); idx != -1 {
		user = user[:idx]
	}

	user = strings.TrimSpace(strings.ToLower(user))

	// root by name or UID 0
	return user == "root" || user == "0"
}

// New creates a new DL3002 rule instance.
func New() *Rule {
	return &Rule{}
}

// init registers the rule with the default registry.
func init() {
	rules.Register(New())
}
