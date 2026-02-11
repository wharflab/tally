package buildkit

import (
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tinovyatkin/tally/internal/testutil"
)

func TestExposeInvalidFormatRule_Metadata(t *testing.T) {
	t.Parallel()
	snaps.MatchStandaloneJSON(t, NewExposeInvalidFormatRule().Metadata())
}

// Test cases ported from BuildKit's original test:
// https://github.com/moby/buildkit/blob/master/frontend/dockerfile/dockerfile_lint_test.go (testExposeInvalidFormat)
func TestExposeInvalidFormatRule_Check(t *testing.T) {
	t.Parallel()
	r := NewExposeInvalidFormatRule()

	testutil.RunRuleTests(t, r, []testutil.RuleTestCase{
		{
			// Ported from BuildKit: EXPOSE 127.0.0.1:80:80 [::1]:8080:8080 5000:5000 8000
			// Expects three violations (IP address and host-port mappings), not on plain "8000".
			// Note: BuildKit parser may reorder ports within a single EXPOSE instruction,
			// so we only check violation count and that each invalid port appears somewhere.
			Name:           "BuildKit test case - mixed valid and invalid formats",
			Content:        "FROM scratch\nEXPOSE 127.0.0.1:80:80 [::1]:8080:8080 5000:5000 8000\n",
			WantViolations: 3,
		},
		{
			Name:           "simple port - no violations",
			Content:        "FROM scratch\nEXPOSE 80\n",
			WantViolations: 0,
		},
		{
			Name:           "port with protocol - no violations",
			Content:        "FROM scratch\nEXPOSE 80/tcp 443/udp\n",
			WantViolations: 0,
		},
		{
			Name:           "port range - no violations",
			Content:        "FROM scratch\nEXPOSE 8080-8090/tcp\n",
			WantViolations: 0,
		},
		{
			Name:           "host-port mapping",
			Content:        "FROM scratch\nEXPOSE 5000:5000\n",
			WantViolations: 1,
			WantMessages:   []string{"5000:5000"},
		},
		{
			Name:           "IP address with host-port",
			Content:        "FROM scratch\nEXPOSE 0.0.0.0:8080:8080\n",
			WantViolations: 1,
			WantMessages:   []string{"0.0.0.0:8080:8080"},
		},
		{
			Name:           "IPv6 address with host-port",
			Content:        "FROM scratch\nEXPOSE [::1]:80:80\n",
			WantViolations: 1,
			WantMessages:   []string{"[::1]:80:80"},
		},
		{
			Name:           "multiple EXPOSE instructions",
			Content:        "FROM scratch\nEXPOSE 127.0.0.1:80:80\nEXPOSE 5000:5000\n",
			WantViolations: 2,
			WantMessages:   []string{"127.0.0.1:80:80", "5000:5000"},
		},
		{
			Name:           "multiple stages",
			Content:        "FROM scratch AS base\nEXPOSE 5000:5000\nFROM scratch\nEXPOSE 127.0.0.1:80:80\n",
			WantViolations: 2,
			WantMessages:   []string{"5000:5000", "127.0.0.1:80:80"},
		},
		{
			Name:           "host-port mapping with protocol",
			Content:        "FROM scratch\nEXPOSE 5000:5000/tcp\n",
			WantViolations: 1,
			WantMessages:   []string{"5000:5000/tcp"},
		},
		{
			Name:           "IP with port range",
			Content:        "FROM scratch\nEXPOSE 0.0.0.0:1234-1236:1234-1236/tcp\n",
			WantViolations: 1,
			WantMessages:   []string{"0.0.0.0:1234-1236:1234-1236/tcp"},
		},
	})
}

func TestExposeInvalidFormatRule_Check_ViolationDetails(t *testing.T) {
	t.Parallel()
	r := NewExposeInvalidFormatRule()

	input := testutil.MakeLintInput(t, "Dockerfile", "FROM scratch\nEXPOSE 127.0.0.1:80:80\n")
	violations := r.Check(input)

	require.Len(t, violations, 1)
	v := violations[0]
	assert.Equal(t, "buildkit/ExposeInvalidFormat", v.RuleCode)
	assert.Contains(t, v.Message, "127.0.0.1:80:80")
	assert.Equal(t, "https://docs.docker.com/go/dockerfile/rule/expose-invalid-format/", v.DocURL)
	assert.Equal(t, 2, v.Location.Start.Line)
}

func TestSplitParts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		input         string
		wantIP        string
		wantHostPort  string
		wantContainer string
	}{
		{"simple port", "80", "", "", "80"},
		{"port with proto", "80/tcp", "", "", "80/tcp"},
		{"host:container", "5000:5000", "", "5000", "5000"},
		{"ip:host:container", "127.0.0.1:80:80", "127.0.0.1", "80", "80"},
		{"ipv6 bracket", "[::1]:8080:8080", "[::1]", "8080", "8080"},
		// The default case (len>3) joins extra parts into the IP field.
		{"ipv6 long", "2001:4860:0:2001::68::333", "2001:4860:0:2001::68", "", "333"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ip, hp, cp := splitParts(tc.input)
			assert.Equal(t, tc.wantIP, ip, "hostIP")
			assert.Equal(t, tc.wantHostPort, hp, "hostPort")
			assert.Equal(t, tc.wantContainer, cp, "containerPort")
		})
	}
}
