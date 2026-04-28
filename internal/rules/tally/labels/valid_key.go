package labels

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/wharflab/tally/internal/facts"
	"github.com/wharflab/tally/internal/rules"
)

// ValidKeyRuleCode is the full rule code.
const ValidKeyRuleCode = rules.TallyRulePrefix + "labels/valid-key"

// ValidKeyRule validates Dockerfile LABEL keys using Docker's documented guidance.
type ValidKeyRule struct{}

// NewValidKeyRule creates a new rule instance.
func NewValidKeyRule() *ValidKeyRule {
	return &ValidKeyRule{}
}

// Metadata returns the rule metadata.
func (r *ValidKeyRule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            ValidKeyRuleCode,
		Name:            "Valid label key",
		Description:     "Detects Dockerfile LABEL keys that violate Docker's documented key guidance",
		DocURL:          rules.TallyDocURL(ValidKeyRuleCode),
		DefaultSeverity: rules.SeverityWarning,
		Category:        "correctness",
		IsExperimental:  false,
	}
}

// Check runs the rule.
func (r *ValidKeyRule) Check(input rules.LintInput) []rules.Violation {
	if input.Facts == nil {
		return nil
	}

	meta := r.Metadata()
	labels := input.Facts.Labels()
	violations := make([]rules.Violation, 0, len(labels))
	for _, pair := range labels {
		if pair.NoDelim {
			continue // buildkit/LegacyKeyValueFormat owns old LABEL key value syntax.
		}
		if pair.KeyIsDynamic {
			violations = append(violations, r.dynamicKeyViolation(input.File, meta, pair))
			continue
		}
		if pair.ExpansionError != "" {
			continue
		}
		if reason := invalidLabelKeyReason(pair.Key); reason != "" {
			violations = append(violations, rules.NewViolation(
				rules.NewLocationFromRanges(input.File, pair.Location),
				meta.Code,
				fmt.Sprintf("label key %q %s", pair.Key, reason),
				meta.DefaultSeverity,
			).WithDocURL(meta.DocURL))
		}
	}
	return violations
}

func (r *ValidKeyRule) dynamicKeyViolation(file string, meta rules.RuleMetadata, pair facts.LabelPairFact) rules.Violation {
	return rules.NewViolation(
		rules.NewLocationFromRanges(file, pair.Location),
		meta.Code,
		fmt.Sprintf("label key %q uses variable expansion and cannot be validated statically", facts.Unquote(pair.RawKey)),
		rules.SeverityInfo,
	).WithDocURL(meta.DocURL).WithDetail(
		"Keep LABEL keys static so duplicate detection, key validation, and schema checks can reason about image metadata reliably.",
	)
}

func invalidLabelKeyReason(key string) string {
	if key == "" {
		return "is empty"
	}
	if hasWhitespace(key) {
		return "contains whitespace"
	}
	if hasControlCharacter(key) {
		return "contains a control character"
	}
	if hasUppercase(key) {
		return "uses uppercase characters; Docker recommends lower-case label keys"
	}
	if bad := firstUnsupportedLabelKeyRune(key); bad != 0 {
		return fmt.Sprintf("contains %q, which is outside Docker's documented label-key guidance", bad)
	}
	if startsOrEndsWithPunctuation(key) {
		return "must start and end with a lower-case letter or digit"
	}
	if hasRepeatedSeparator(key) {
		return "contains repeated separators"
	}
	if isReservedDockerNamespace(key) && !isAllowedDockerNamespaceKey(key) {
		return "uses a Docker-reserved namespace"
	}
	return ""
}

func startsOrEndsWithPunctuation(key string) bool {
	return !isLowerAlphaNum(rune(key[0])) || !isLowerAlphaNum(rune(key[len(key)-1]))
}

func hasWhitespace(key string) bool {
	for _, r := range key {
		if unicode.IsSpace(r) {
			return true
		}
	}
	return false
}

func hasControlCharacter(key string) bool {
	for _, r := range key {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}

func hasUppercase(key string) bool {
	for _, r := range key {
		if unicode.IsUpper(r) {
			return true
		}
	}
	return false
}

func firstUnsupportedLabelKeyRune(key string) rune {
	for _, r := range key {
		if isLowerAlphaNum(r) || r == '.' || r == '-' {
			continue
		}
		return r
	}
	return 0
}

func hasRepeatedSeparator(key string) bool {
	return strings.Contains(key, "..") ||
		strings.Contains(key, "--") ||
		strings.Contains(key, ".-") ||
		strings.Contains(key, "-.")
}

func isLowerAlphaNum(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
}

func isReservedDockerNamespace(key string) bool {
	return strings.HasPrefix(key, "com.docker.") ||
		strings.HasPrefix(key, "io.docker.") ||
		strings.HasPrefix(key, "org.dockerproject.")
}

func isAllowedDockerNamespaceKey(key string) bool {
	return key == "com.docker.image.source.entrypoint" ||
		strings.HasPrefix(key, "com.docker.extension.") ||
		strings.HasPrefix(key, "com.docker.desktop.extension.")
}

func init() {
	rules.Register(NewValidKeyRule())
}
