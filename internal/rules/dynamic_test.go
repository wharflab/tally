package rules

import "testing"

func TestIsDynamicRuleCode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		ruleCode string
		want     bool
	}{
		{name: "shellcheck diagnostic", ruleCode: "shellcheck/SC2086", want: true},
		{name: "shellcheck engine", ruleCode: "shellcheck/ShellCheck", want: false},
		{name: "shellcheck too short", ruleCode: "shellcheck/SC208", want: false},
		{name: "shellcheck nonnumeric", ruleCode: "shellcheck/SC20AB", want: false},
		{name: "powershell pssa rule", ruleCode: "powershell/PSAvoidUsingWriteHost", want: true},
		{name: "powershell parser diagnostic", ruleCode: "powershell/InvalidLeftHandSide", want: true},
		{name: "powershell engine", ruleCode: "powershell/PowerShell", want: false},
		{name: "powershell internal error", ruleCode: "powershell/PowerShellInternalError", want: false},
		{name: "powershell empty name", ruleCode: "powershell/", want: false},
		{name: "powershell punctuation", ruleCode: "powershell/Invalid-LeftHandSide", want: false},
		{name: "other namespace", ruleCode: "tally/max-lines", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := IsDynamicRuleCode(tt.ruleCode); got != tt.want {
				t.Fatalf("IsDynamicRuleCode(%q) = %v, want %v", tt.ruleCode, got, tt.want)
			}
		})
	}
}
