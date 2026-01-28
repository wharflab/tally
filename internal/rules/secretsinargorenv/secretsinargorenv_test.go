package secretsinargorenv

import (
	"testing"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/tinovyatkin/tally/internal/rules"
)

func TestMetadata(t *testing.T) {
	r := New()
	meta := r.Metadata()

	if meta.Code != "buildkit/SecretsUsedInArgOrEnv" {
		t.Errorf("expected code %q, got %q", "buildkit/SecretsUsedInArgOrEnv", meta.Code)
	}

	if meta.Category != "security" {
		t.Errorf("expected category %q, got %q", "security", meta.Category)
	}

	if !meta.EnabledByDefault {
		t.Error("expected rule to be enabled by default")
	}
}

func TestCheck_ARGWithSecret(t *testing.T) {
	r := New()

	input := rules.LintInput{
		File: "Dockerfile",
		Stages: []instructions.Stage{
			{
				Commands: []instructions.Command{
					&instructions.ArgCommand{
						Args: []instructions.KeyValuePairOptional{
							{Key: "API_KEY", Value: nil},
						},
					},
				},
			},
		},
	}

	violations := r.Check(input)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}

	if violations[0].RuleCode != "buildkit/SecretsUsedInArgOrEnv" {
		t.Errorf("expected code %q, got %q", "buildkit/SecretsUsedInArgOrEnv", violations[0].RuleCode)
	}
}

func TestCheck_ENVWithSecret(t *testing.T) {
	r := New()

	input := rules.LintInput{
		File: "Dockerfile",
		Stages: []instructions.Stage{
			{
				Commands: []instructions.Command{
					&instructions.EnvCommand{
						Env: []instructions.KeyValuePair{
							{Key: "DATABASE_PASSWORD", Value: "secret123"},
						},
					},
				},
			},
		},
	}

	violations := r.Check(input)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}
}

func TestCheck_MetaARGWithSecret(t *testing.T) {
	r := New()

	val := "secretvalue"
	input := rules.LintInput{
		File: "Dockerfile",
		MetaArgs: []instructions.ArgCommand{
			{
				Args: []instructions.KeyValuePairOptional{
					{Key: "AUTH_TOKEN", Value: &val},
				},
			},
		},
		Stages: []instructions.Stage{{}},
	}

	violations := r.Check(input)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation for meta ARG, got %d", len(violations))
	}
}

func TestCheck_NoSecrets(t *testing.T) {
	r := New()

	input := rules.LintInput{
		File: "Dockerfile",
		Stages: []instructions.Stage{
			{
				Commands: []instructions.Command{
					&instructions.ArgCommand{
						Args: []instructions.KeyValuePairOptional{
							{Key: "VERSION", Value: nil},
						},
					},
					&instructions.EnvCommand{
						Env: []instructions.KeyValuePair{
							{Key: "PORT", Value: "8080"},
						},
					},
				},
			},
		},
	}

	violations := r.Check(input)
	if len(violations) != 0 {
		t.Errorf("expected no violations, got %d", len(violations))
	}
}

func TestCheck_PublicKeyAllowed(t *testing.T) {
	r := New()

	input := rules.LintInput{
		File: "Dockerfile",
		Stages: []instructions.Stage{
			{
				Commands: []instructions.Command{
					&instructions.ArgCommand{
						Args: []instructions.KeyValuePairOptional{
							{Key: "PUBLIC_KEY", Value: nil}, // "public" overrides "key"
						},
					},
				},
			},
		},
	}

	violations := r.Check(input)
	if len(violations) != 0 {
		t.Errorf("expected no violations for PUBLIC_KEY, got %d", len(violations))
	}
}

func TestIsSecretKey(t *testing.T) {
	tests := []struct {
		key  string
		want bool
	}{
		// Should match
		{"API_KEY", true},
		{"apikey", true},
		{"APIKEY", true},
		{"api_key", true},
		{"PASSWORD", true},
		{"DB_PASSWORD", true},
		{"passwd", true},
		{"pword", true},
		{"SECRET", true},
		{"MY_SECRET_VALUE", true},
		{"TOKEN", true},
		{"AUTH_TOKEN", true},
		{"auth", true},
		{"CREDENTIAL", true},
		{"credentials", true},

		// Should not match
		{"VERSION", false},
		{"PORT", false},
		{"NAME", false},
		{"PUBLIC_KEY", false}, // "public" overrides "key"
		{"KEYBOARD", false},   // No word boundary before "key" (BuildKit behavior)

		// Edge cases
		{"", false},
		{"X", false},
	}

	for _, tc := range tests {
		got := isSecretKey(tc.key)
		if got != tc.want {
			t.Errorf("isSecretKey(%q) = %v, want %v", tc.key, got, tc.want)
		}
	}
}

func TestCheck_MultipleSecrets(t *testing.T) {
	r := New()

	input := rules.LintInput{
		File: "Dockerfile",
		Stages: []instructions.Stage{
			{
				Commands: []instructions.Command{
					&instructions.ArgCommand{
						Args: []instructions.KeyValuePairOptional{
							{Key: "API_KEY", Value: nil},
							{Key: "VERSION", Value: nil}, // Not a secret
						},
					},
					&instructions.EnvCommand{
						Env: []instructions.KeyValuePair{
							{Key: "PASSWORD", Value: "secret"},
							{Key: "TOKEN", Value: "abc123"},
						},
					},
				},
			},
		},
	}

	violations := r.Check(input)
	if len(violations) != 3 {
		t.Errorf("expected 3 violations, got %d", len(violations))
	}
}
