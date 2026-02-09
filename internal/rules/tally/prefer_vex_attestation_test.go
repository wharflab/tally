package tally

import (
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/tinovyatkin/tally/internal/rules"
	"github.com/tinovyatkin/tally/internal/testutil"
)

func TestPreferVEXAttestationRule_Metadata(t *testing.T) {
	t.Parallel()
	snaps.MatchStandaloneJSON(t, NewPreferVEXAttestationRule().Metadata())
}

func TestPreferVEXAttestationRule_Check(t *testing.T) {
	t.Parallel()
	testutil.RunRuleTests(t, NewPreferVEXAttestationRule(), []testutil.RuleTestCase{
		{
			Name:           "no COPY instruction",
			Content:        "FROM alpine\nRUN echo hello",
			WantViolations: 0,
		},
		{
			Name:           "COPY glob vex json triggers",
			Content:        "FROM alpine\nCOPY *.vex.json /usr/share/vex/",
			WantViolations: 1,
			WantCodes:      []string{"tally/prefer-vex-attestation"},
			WantMessages:   []string{"prefer attaching VEX as an OCI attestation"},
		},
		{
			Name:           "COPY concrete vex json triggers",
			Content:        "FROM alpine\nCOPY app.vex.json /usr/share/vex/app.vex.json",
			WantViolations: 1,
			WantCodes:      []string{"tally/prefer-vex-attestation"},
			WantMessages:   []string{"app.vex.json"},
		},
		{
			Name:           "COPY multiple vex json sources emits one per source",
			Content:        "FROM alpine\nCOPY a.vex.json b.vex.json /usr/share/vex/",
			WantViolations: 2,
			WantCodes:      []string{"tally/prefer-vex-attestation", "tally/prefer-vex-attestation"},
		},
		{
			Name:           "COPY non-vex json is ignored",
			Content:        "FROM alpine\nCOPY vex.json /usr/share/vex/",
			WantViolations: 0,
		},
		{
			Name:           "COPY vex json match is case-insensitive",
			Content:        "FROM alpine\nCOPY APP.VEX.JSON /usr/share/vex/app.vex.json",
			WantViolations: 1,
			WantCodes:      []string{"tally/prefer-vex-attestation"},
		},
	})
}

func TestPreferVEXAttestationRule_Interfaces(t *testing.T) {
	t.Parallel()
	r := NewPreferVEXAttestationRule()

	// Verify Rule interface
	var _ rules.Rule = r
}
