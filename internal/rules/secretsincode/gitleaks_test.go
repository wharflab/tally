package secretsincode

import (
	"testing"

	"github.com/zricethezav/gitleaks/v8/detect"
)

// TestGitleaksDetection verifies that gitleaks detection works as expected.
// This test documents the expected behavior and validates our understanding
// of gitleaks' pattern matching (including entropy filtering).
func TestGitleaksDetection(t *testing.T) {
	d, err := detect.NewDetectorDefaultConfig()
	if err != nil {
		t.Fatalf("failed to create detector: %v", err)
	}

	// Verify gitleaks loaded rules (avoid brittle count assertions)
	if len(d.Config.Rules) == 0 {
		t.Errorf("expected gitleaks to load rules, got %d", len(d.Config.Rules))
	}

	tests := []struct {
		name    string
		content string
		wantAny bool
	}{
		{
			// AWS access token: AKIA + 16 chars from [A-Z2-7]
			name:    "AWS access key ID",
			content: "aws_access_key_id = AKIAABCDEFGH23456723",
			wantAny: true,
		},
		{
			// Classic GitHub PAT: ghp_ followed by 36 alphanumeric chars
			// Note: gitleaks filters low-entropy patterns like "ghp_xxx..."
			// so we use realistic-looking random chars
			name:    "GitHub Classic PAT",
			content: "GITHUB_TOKEN=ghp_SfE7gMq5K9pR2nLwHvYt3dXc8jU6bA1Z0iFo",
			wantAny: true,
		},
		{
			// Private key: needs 64+ chars between BEGIN/END markers
			name: "Private key",
			content: `-----BEGIN RSA PRIVATE KEY-----
MIIEpAIBAAKCAQEA0Z3VS5JJcds3xfn/ygWyF8PbnGy0AHB7MaWdP0rPpJz5AAAA
BBBBCCCCDDDDEEEEFFFFGGGGHHHHIIIIJJJJKKKKLLLLMMMMNNNNOOOOPPPPQQQQ
-----END RSA PRIVATE KEY-----`,
			wantAny: true,
		},
		{
			// Stripe secret key: sk_live_ + alphanumeric
			name:    "Stripe secret key",
			content: `stripe_api_key = sk_live_ABCDEFGHIJKLMNOPabcd1234`,
			wantAny: true,
		},
		{
			name:    "Safe content",
			content: "echo hello world",
			wantAny: false,
		},
		{
			name:    "Port number",
			content: "PORT=8080",
			wantAny: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			findings := d.DetectString(tc.content)
			if tc.wantAny && len(findings) == 0 {
				t.Errorf("expected findings for %q, got none", tc.content)
			}
			if !tc.wantAny && len(findings) > 0 {
				t.Errorf("expected no findings for %q, got %d", tc.content, len(findings))
				for _, f := range findings {
					t.Logf("  - %s: %s", f.RuleID, f.Secret)
				}
			}
		})
	}
}
