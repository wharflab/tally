package rules

import "strings"

// IsDynamicRuleCode reports whether a rule code belongs to an analyzer-backed
// namespace whose concrete findings are discovered at runtime.
func IsDynamicRuleCode(ruleCode string) bool {
	if after, ok := strings.CutPrefix(ruleCode, ShellcheckRulePrefix+"SC"); ok {
		code := after
		if len(code) != 4 {
			return false
		}
		for _, r := range code {
			if r < '0' || r > '9' {
				return false
			}
		}
		return true
	}

	if strings.HasPrefix(ruleCode, PowerShellRulePrefix+"PS") {
		name := strings.TrimPrefix(ruleCode, PowerShellRulePrefix)
		if len(name) <= 2 {
			return false
		}
		for _, r := range name {
			if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
				continue
			}
			return false
		}
		return true
	}

	return false
}
