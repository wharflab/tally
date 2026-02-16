package hadolint

import (
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/testutil"
)

func TestDL3011Rule_Metadata(t *testing.T) {
	t.Parallel()
	snaps.MatchStandaloneJSON(t, NewDL3011Rule().Metadata())
}

func TestDL3011Rule_Check(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		dockerfile string
		wantCount  int
		wantCode   string
	}{
		// Test cases from original Hadolint spec
		{
			name: "invalid single port",
			dockerfile: `FROM alpine:3.18
EXPOSE 80000
`,
			wantCount: 1,
			wantCode:  rules.HadolintRulePrefix + "DL3011",
		},
		{
			name: "valid single port",
			dockerfile: `FROM alpine:3.18
EXPOSE 60000
`,
			wantCount: 0,
		},
		{
			name: "valid port variable",
			dockerfile: `FROM alpine:3.18
EXPOSE ${FOOBAR}
`,
			wantCount: 0,
		},
		{
			name: "invalid port in range",
			dockerfile: `FROM alpine:3.18
EXPOSE 40000-80000/tcp
`,
			wantCount: 1,
		},
		{
			name: "valid port range",
			dockerfile: `FROM alpine:3.18
EXPOSE 40000-60000/tcp
`,
			wantCount: 0,
		},
		{
			name: "valid port range with variable",
			dockerfile: `FROM alpine:3.18
EXPOSE 40000-${FOOBAR}
`,
			wantCount: 0,
		},
		// Additional edge cases
		{
			name: "port at boundary 65535",
			dockerfile: `FROM alpine:3.18
EXPOSE 65535
`,
			wantCount: 0,
		},
		{
			name: "port just over boundary 65536",
			dockerfile: `FROM alpine:3.18
EXPOSE 65536
`,
			wantCount: 1,
		},
		{
			name: "port 0 is valid",
			dockerfile: `FROM alpine:3.18
EXPOSE 0
`,
			wantCount: 0,
		},
		{
			name: "multiple valid ports",
			dockerfile: `FROM alpine:3.18
EXPOSE 80 443 8080
`,
			wantCount: 0,
		},
		{
			name: "multiple invalid ports",
			dockerfile: `FROM alpine:3.18
EXPOSE 80000 90000
`,
			wantCount: 2,
		},
		{
			name: "mixed valid and invalid ports",
			dockerfile: `FROM alpine:3.18
EXPOSE 80 80000 443
`,
			wantCount: 1,
		},
		{
			name: "port with tcp protocol valid",
			dockerfile: `FROM alpine:3.18
EXPOSE 8080/tcp
`,
			wantCount: 0,
		},
		{
			name: "port with udp protocol valid",
			dockerfile: `FROM alpine:3.18
EXPOSE 53/udp
`,
			wantCount: 0,
		},
		{
			name: "invalid port with protocol",
			dockerfile: `FROM alpine:3.18
EXPOSE 70000/tcp
`,
			wantCount: 1,
		},
		{
			name: "port range both ends invalid",
			dockerfile: `FROM alpine:3.18
EXPOSE 70000-80000/tcp
`,
			wantCount: 2,
		},
		{
			name: "port range start invalid",
			dockerfile: `FROM alpine:3.18
EXPOSE 70000-60000/tcp
`,
			wantCount: 1,
		},
		{
			name: "multi-stage dockerfile",
			dockerfile: `FROM alpine:3.18 AS builder
EXPOSE 80000

FROM alpine:3.18
EXPOSE 443
`,
			wantCount: 1,
		},
		{
			name: "variable at start of range",
			dockerfile: `FROM alpine:3.18
EXPOSE ${START}-60000
`,
			wantCount: 0,
		},
		// Cross-rule interaction: DL3011 fires correctly even with uppercase protocol
		// (ExposeProtoCasing would also flag this line, but at warning level)
		{
			name: "invalid port with uppercase protocol (overlap with ExposeProtoCasing)",
			dockerfile: `FROM alpine:3.18
EXPOSE 70000/TCP
`,
			wantCount: 1,
			wantCode:  rules.HadolintRulePrefix + "DL3011",
		},
		// Negative port validation (lower bound)
		{
			name: "negative port -1",
			dockerfile: `FROM alpine:3.18
EXPOSE -1
`,
			wantCount: 1,
			wantCode:  rules.HadolintRulePrefix + "DL3011",
		},
		{
			name: "negative port -100",
			dockerfile: `FROM alpine:3.18
EXPOSE -100
`,
			wantCount: 1,
		},
		{
			name: "negative port in range start (-1-80)",
			dockerfile: `FROM alpine:3.18
EXPOSE -1-80
`,
			wantCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			input := testutil.MakeLintInput(t, "Dockerfile", tt.dockerfile)

			r := NewDL3011Rule()
			violations := r.Check(input)

			if len(violations) != tt.wantCount {
				t.Errorf("got %d violations, want %d", len(violations), tt.wantCount)
				for _, v := range violations {
					t.Logf("  - %s: %s", v.RuleCode, v.Message)
				}
			}

			if tt.wantCode != "" && len(violations) > 0 {
				if violations[0].RuleCode != tt.wantCode {
					t.Errorf("RuleCode = %q, want %q", violations[0].RuleCode, tt.wantCode)
				}
			}
		})
	}
}

// TestValidatePortSpec verifies the port validation helper function.
func TestValidatePortSpec(t *testing.T) {
	t.Parallel()
	tests := []struct {
		portSpec string
		want     []string
	}{
		// Valid ports
		{"80", nil},
		{"443", nil},
		{"8080", nil},
		{"65535", nil},
		{"0", nil},

		// Invalid ports - above max
		{"65536", []string{"65536"}},
		{"70000", []string{"70000"}},
		{"80000", []string{"80000"}},

		// Invalid ports - negative (below min)
		{"-1", []string{"-1"}},
		{"-100", []string{"-100"}},
		{"-1/tcp", []string{"-1"}},

		// With protocol
		{"80/tcp", nil},
		{"80/udp", nil},
		{"70000/tcp", []string{"70000"}},

		// Ranges - valid
		{"80-90", nil},
		{"1000-2000", nil},
		{"40000-60000", nil},
		{"40000-60000/tcp", nil},

		// Ranges - invalid end
		{"40000-80000", []string{"80000"}},
		{"40000-80000/tcp", []string{"80000"}},

		// Ranges - invalid start
		{"70000-60000", []string{"70000"}},

		// Ranges - both invalid
		{"70000-80000", []string{"70000", "80000"}},

		// Variables - all should pass
		{"${PORT}", nil},
		{"$PORT", nil},
		{"${START}-${END}", nil},
		{"40000-${END}", nil},
		{"${START}-60000", nil},
	}

	for _, tt := range tests {
		t.Run(tt.portSpec, func(t *testing.T) {
			t.Parallel()
			got := validatePortSpec(tt.portSpec)
			if len(got) != len(tt.want) {
				t.Errorf("validatePortSpec(%q) = %v, want %v", tt.portSpec, got, tt.want)
				return
			}
			for i, v := range got {
				if v != tt.want[i] {
					t.Errorf("validatePortSpec(%q)[%d] = %q, want %q", tt.portSpec, i, v, tt.want[i])
				}
			}
		})
	}
}
