package hadolint

import (
	"strconv"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/wharflab/tally/internal/rules"
)

// DL3011Rule implements the DL3011 linting rule.
// It checks that EXPOSE instruction ports are valid UNIX ports (0-65535).
type DL3011Rule struct{}

// NewDL3011Rule creates a new DL3011 rule instance.
func NewDL3011Rule() *DL3011Rule {
	return &DL3011Rule{}
}

// Metadata returns the rule metadata.
func (r *DL3011Rule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            rules.HadolintRulePrefix + "DL3011",
		Name:            "Valid UNIX ports range from 0 to 65535",
		Description:     "EXPOSE instruction specifies a port outside the valid UNIX range (0-65535)",
		DocURL:          rules.HadolintDocURL("DL3011"),
		DefaultSeverity: rules.SeverityError,
		Category:        "correctness",
		IsExperimental:  false,
	}
}

// Check runs the DL3011 rule.
// It validates that all ports in EXPOSE instructions are within the valid UNIX port range.
func (r *DL3011Rule) Check(input rules.LintInput) []rules.Violation {
	var violations []rules.Violation
	meta := r.Metadata()

	for _, stage := range input.Stages {
		for _, cmd := range stage.Commands {
			expose, ok := cmd.(*instructions.ExposeCommand)
			if !ok {
				continue
			}

			for _, portSpec := range expose.Ports {
				invalidPorts := validatePortSpec(portSpec)
				for _, invalid := range invalidPorts {
					loc := rules.NewLocationFromRanges(input.File, expose.Location())
					violations = append(violations, rules.NewViolation(
						loc,
						meta.Code,
						"valid UNIX ports range from 0 to 65535; "+invalid+" is out of range",
						meta.DefaultSeverity,
					).WithDocURL(meta.DocURL))
				}
			}
		}
	}

	return violations
}

// validatePortSpec validates a port specification and returns any invalid port numbers.
// Port specs can be:
//   - Single port: "80", "80/tcp", "80/udp"
//   - Port range: "80-90", "80-90/tcp"
//   - Variable: "${PORT}", "40000-${END}"
//
// Returns empty slice if the port spec is valid or contains variables.
func validatePortSpec(portSpec string) []string {
	// Skip if it contains a variable reference
	if strings.Contains(portSpec, "$") {
		return nil
	}

	// Strip protocol suffix if present (e.g., "80/tcp" -> "80")
	portPart, _, _ := strings.Cut(portSpec, "/")

	// Check if it's a range (e.g., "80-90" or "-1-80")
	// Find the range separator: a "-" that's not at position 0 (which would be a negative sign)
	rangeIdx := strings.Index(portPart[1:], "-") // Skip first char to ignore leading negative sign
	if rangeIdx >= 0 {
		rangeIdx++ // Adjust for the skipped character
		startPort := portPart[:rangeIdx]
		endPort := portPart[rangeIdx+1:]
		return validatePortRange(startPort, endPort)
	}

	// Single port (including negative numbers like "-1")
	return validateSinglePort(portPart)
}

// validatePortRange validates start and end ports of a range.
func validatePortRange(startPort, endPort string) []string {
	var invalidPorts []string
	if invalid := checkPortValue(startPort); invalid != "" {
		invalidPorts = append(invalidPorts, invalid)
	}
	if invalid := checkPortValue(endPort); invalid != "" {
		invalidPorts = append(invalidPorts, invalid)
	}
	return invalidPorts
}

// validateSinglePort validates a single port value.
func validateSinglePort(portStr string) []string {
	if invalid := checkPortValue(portStr); invalid != "" {
		return []string{invalid}
	}
	return nil
}

// checkPortValue parses and validates a port number.
// Returns the port string if invalid, or empty string if valid.
func checkPortValue(portStr string) string {
	// Skip variable references
	if strings.HasPrefix(portStr, "$") {
		return ""
	}
	port, err := strconv.ParseInt(portStr, 10, 64)
	if err != nil {
		return "" // Non-numeric values are handled elsewhere
	}
	if port < 0 || port > 65535 {
		return portStr
	}
	return ""
}

// init registers the rule with the default registry.
func init() {
	rules.Register(NewDL3011Rule())
}
