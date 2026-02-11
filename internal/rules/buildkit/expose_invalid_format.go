package buildkit

import (
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"
	"github.com/moby/buildkit/frontend/dockerfile/linter"

	"github.com/tinovyatkin/tally/internal/rules"
)

// ExposeInvalidFormatRule implements BuildKit's ExposeInvalidFormat check.
//
// EXPOSE should only contain container ports and protocols (e.g., "80/tcp").
// IP addresses and host-port mappings (e.g., "127.0.0.1:80:80", "5000:5000")
// are invalid in EXPOSE and will become errors in a future BuildKit release.
//
// Original source:
// https://github.com/moby/buildkit/blob/master/frontend/dockerfile/dockerfile2llb/convert_expose.go
type ExposeInvalidFormatRule struct{}

func NewExposeInvalidFormatRule() *ExposeInvalidFormatRule {
	return &ExposeInvalidFormatRule{}
}

func (r *ExposeInvalidFormatRule) Metadata() rules.RuleMetadata {
	const name = "ExposeInvalidFormat"
	return *GetMetadata(name)
}

// Check runs the ExposeInvalidFormat rule.
// It scans EXPOSE instructions for port specifications containing IP addresses
// or host-port mappings. One violation is reported per invalid port spec,
// matching BuildKit's behavior.
func (r *ExposeInvalidFormatRule) Check(input rules.LintInput) []rules.Violation {
	var violations []rules.Violation
	meta := r.Metadata()

	for _, stage := range input.Stages {
		for _, cmd := range stage.Commands {
			expose, ok := cmd.(*instructions.ExposeCommand)
			if !ok {
				continue
			}

			for _, port := range expose.Ports {
				ip, hostPort, _ := splitParts(port)
				if ip == "" && hostPort == "" {
					continue
				}

				loc := rules.NewLocationFromRanges(input.File, expose.Location())
				msg := linter.RuleExposeInvalidFormat.Format(port)
				violations = append(violations, rules.NewViolation(
					loc, meta.Code, msg, meta.DefaultSeverity,
				).WithDocURL(meta.DocURL))
			}
		}
	}

	return violations
}

// splitParts splits a port specification into IP, host port, and container port.
// This mirrors BuildKit's splitParts function from convert_expose.go.
//
// Format: [ip:]hostPort:containerPort or containerPort
// Examples:
//
//	"80"                  → ("", "", "80")
//	"5000:5000"           → ("", "5000", "5000")
//	"127.0.0.1:80:80"    → ("127.0.0.1", "80", "80")
//	"[::1]:8080:8080"    → ("[::1]", "8080", "8080")
func splitParts(rawport string) (string, string, string) {
	parts := strings.Split(rawport, ":")

	switch len(parts) {
	case 1:
		return "", "", parts[0]
	case 2:
		return "", parts[0], parts[1]
	case 3:
		return parts[0], parts[1], parts[2]
	default:
		n := len(parts)
		return strings.Join(parts[:n-2], ":"), parts[n-2], parts[n-1]
	}
}

func init() {
	rules.Register(NewExposeInvalidFormatRule())
}
