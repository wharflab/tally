package buildkit

import (
	"strings"
	"testing"

	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/testutil"
)

const multipleInstructionsCode = rules.BuildKitRulePrefix + "MultipleInstructionsDisallowed"

func TestMultipleInstructionsDisallowedCMD(t *testing.T) {
	t.Parallel()
	testMultipleInstructionsDisallowed(t, "CMD", []ruleCase{
		{
			name:       "many cmds",
			dockerfile: "FROM debian\nCMD bash\nRUN foo\nCMD another\n",
			wantLines:  []int{2},
		},
		{
			name:       "single cmds in different stages",
			dockerfile: "FROM debian AS distro1\nCMD bash\nRUN foo\nFROM debian AS distro2\nCMD another\n",
		},
		{
			name:       "three cmds in same stage",
			dockerfile: "FROM debian\nCMD first\nCMD second\nCMD third\n",
			wantLines:  []int{2, 3},
		},
	})
}

func TestMultipleInstructionsDisallowedHealthcheck(t *testing.T) {
	t.Parallel()
	testMultipleInstructionsDisallowed(t, "HEALTHCHECK", []ruleCase{
		{
			name:       "two HEALTHCHECK in same stage",
			dockerfile: "FROM scratch\nHEALTHCHECK CMD /bin/bla1\nHEALTHCHECK CMD /bin/bla2\n",
			wantLines:  []int{2},
		},
		{
			name:       "three HEALTHCHECK in same stage",
			dockerfile: "FROM scratch\nHEALTHCHECK CMD /bin/check1\nHEALTHCHECK CMD /bin/check2\nHEALTHCHECK CMD /bin/check3\n",
			wantLines:  []int{2, 3},
		},
		{
			name:       "HEALTHCHECK in different stages",
			dockerfile: "FROM scratch\nHEALTHCHECK CMD /bin/bla1\nFROM scratch\nHEALTHCHECK CMD /bin/bla2\n",
		},
	})
}

func TestMultipleInstructionsDisallowedEntrypoint(t *testing.T) {
	t.Parallel()
	testMultipleInstructionsDisallowed(t, "ENTRYPOINT", []ruleCase{
		{
			name:       "many entrypoints",
			dockerfile: "FROM debian\nENTRYPOINT bash\nRUN foo\nENTRYPOINT another\n",
			wantLines:  []int{2},
		},
		{
			name:       "many entrypoints in different stages",
			dockerfile: "FROM debian AS distro1\nENTRYPOINT bash\nRUN foo\nENTRYPOINT another\nFROM debian AS distro2\nENTRYPOINT another\n",
			wantLines:  []int{2},
		},
		{
			name:       "single entrypoint",
			dockerfile: "FROM scratch\nENTRYPOINT /bin/true\n",
		},
	})
}

type ruleCase struct {
	name       string
	dockerfile string
	wantLines  []int
}

func testMultipleInstructionsDisallowed(t *testing.T, instruction string, cases []ruleCase) {
	t.Helper()

	rule := NewMultipleInstructionsDisallowedRule()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			violations := rule.Check(testutil.MakeLintInput(t, "Dockerfile", tc.dockerfile))
			var found []rules.Violation
			for _, violation := range violations {
				if violation.RuleCode == multipleInstructionsCode && strings.Contains(violation.Message, instruction) {
					found = append(found, violation)
				}
			}

			if len(found) != len(tc.wantLines) {
				t.Fatalf("got %d %s violations, want %d", len(found), instruction, len(tc.wantLines))
			}
			for i, line := range tc.wantLines {
				if found[i].Line() != line {
					t.Fatalf("violation %d line = %d, want %d", i, found[i].Line(), line)
				}
				if found[i].Severity != rules.SeverityWarning {
					t.Fatalf("violation %d severity = %v, want %v", i, found[i].Severity, rules.SeverityWarning)
				}
			}
		})
	}
}
