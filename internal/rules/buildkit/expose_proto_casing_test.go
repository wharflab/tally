package buildkit

import (
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tinovyatkin/tally/internal/testutil"
)

func TestExposeProtoCasingRule_Metadata(t *testing.T) {
	t.Parallel()
	snaps.MatchStandaloneJSON(t, NewExposeProtoCasingRule().Metadata())
}

// Test cases ported from BuildKit's original test:
// https://github.com/moby/buildkit/blob/master/frontend/dockerfile/dockerfile_lint_test.go (testExposeProtoCasing)
func TestExposeProtoCasingRule_Check(t *testing.T) {
	t.Parallel()
	r := NewExposeProtoCasingRule()

	testutil.RunRuleTests(t, r, []testutil.RuleTestCase{
		{
			// Ported from BuildKit: EXPOSE 80/TcP 8080/TCP 8080/udp
			// Expects two violations (80/TcP and 8080/TCP), udp is already lowercase.
			Name:           "mixed case protocols (BuildKit test case)",
			Content:        "FROM scratch\nEXPOSE 80/TcP 8080/TCP 8080/udp\n",
			WantViolations: 2,
			WantMessages:   []string{"80/TcP", "8080/TCP"},
		},
		{
			Name:           "all lowercase protocols - no violations",
			Content:        "FROM scratch\nEXPOSE 80/tcp 443/udp\n",
			WantViolations: 0,
		},
		{
			Name:           "no protocol specified - no violations",
			Content:        "FROM scratch\nEXPOSE 80 443\n",
			WantViolations: 0,
		},
		{
			Name:           "single uppercase TCP",
			Content:        "FROM scratch\nEXPOSE 8080/TCP\n",
			WantViolations: 1,
			WantMessages:   []string{"8080/TCP"},
		},
		{
			Name:           "single uppercase UDP",
			Content:        "FROM scratch\nEXPOSE 53/UDP\n",
			WantViolations: 1,
			WantMessages:   []string{"53/UDP"},
		},
		{
			Name:           "port range with uppercase protocol",
			Content:        "FROM scratch\nEXPOSE 8080-8090/TCP\n",
			WantViolations: 1,
			WantMessages:   []string{"8080-8090/TCP"},
		},
		{
			Name:           "multiple EXPOSE instructions",
			Content:        "FROM scratch\nEXPOSE 80/TCP\nEXPOSE 443/UDP\n",
			WantViolations: 2,
			WantMessages:   []string{"80/TCP", "443/UDP"},
		},
		{
			Name:           "mixed - some correct some not",
			Content:        "FROM scratch\nEXPOSE 80/tcp 443/UDP\n",
			WantViolations: 1,
			WantMessages:   []string{"443/UDP"},
		},
		{
			Name:           "multiple stages",
			Content:        "FROM scratch AS base\nEXPOSE 80/TCP\nFROM scratch\nEXPOSE 443/UDP\n",
			WantViolations: 2,
			WantMessages:   []string{"80/TCP", "443/UDP"},
		},
	})
}

func TestExposeProtoCasingRule_Check_ViolationDetails(t *testing.T) {
	t.Parallel()
	r := NewExposeProtoCasingRule()

	input := testutil.MakeLintInput(t, "Dockerfile", "FROM scratch\nEXPOSE 8080/TCP\n")
	violations := r.Check(input)

	require.Len(t, violations, 1)
	v := violations[0]
	assert.Equal(t, "buildkit/ExposeProtoCasing", v.RuleCode)
	assert.Equal(t, "Defined protocol '8080/TCP' in EXPOSE instruction should be lowercase", v.Message)
	assert.Equal(t, "https://docs.docker.com/go/dockerfile/rule/expose-proto-casing/", v.DocURL)
	assert.Equal(t, 2, v.Location.Start.Line)
}
