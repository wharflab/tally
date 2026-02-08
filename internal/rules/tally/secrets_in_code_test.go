package tally

import (
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/tinovyatkin/tally/internal/rules"
)

func TestSecretsInCodeRule_Metadata(t *testing.T) {
	t.Parallel()
	snaps.MatchStandaloneJSON(t, NewSecretsInCodeRule().Metadata())
}

func TestSecretsInCodeRule_Check_AWSKeyInHeredoc(t *testing.T) {
	t.Parallel()
	r := NewSecretsInCodeRule()

	input := rules.LintInput{
		File: "Dockerfile",
		Stages: []instructions.Stage{
			{
				Commands: []instructions.Command{
					&instructions.RunCommand{
						ShellDependantCmdLine: instructions.ShellDependantCmdLine{
							Files: []instructions.ShellInlineFile{
								{
									// gitleaks:allow
									// AWS access key: AKIA + 16 chars from [A-Z2-7]
									Data: `AWS_ACCESS_KEY_ID=AKIAABCDEFGH23456723
AWS_SECRET_ACCESS_KEY=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY`,
								},
							},
						},
					},
				},
			},
		},
	}

	violations := r.Check(input)
	if len(violations) == 0 {
		t.Fatal("expected violations for AWS keys in heredoc, got none")
	}

	// Should detect AWS key
	found := false
	for _, v := range violations {
		if v.RuleCode == "tally/secrets-in-code" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected tally/secrets-in-code violation")
	}
}

func TestSecretsInCodeRule_Check_PrivateKeyInCopyHeredoc(t *testing.T) {
	t.Parallel()
	r := NewSecretsInCodeRule()

	input := rules.LintInput{
		File: "Dockerfile",
		Stages: []instructions.Stage{
			{
				Commands: []instructions.Command{
					&instructions.CopyCommand{
						SourcesAndDest: instructions.SourcesAndDest{
							SourceContents: []instructions.SourceContent{
								{
									// gitleaks:allow
									Path: "/root/.ssh/id_rsa",
									Data: `-----BEGIN RSA PRIVATE KEY-----
MIIEpAIBAAKCAQEA0Z3VS5JJcds3xfn/ygWyF8PbnGy0AHB7MaWdP0rPpJz5
-----END RSA PRIVATE KEY-----`,
								},
							},
						},
					},
				},
			},
		},
	}

	violations := r.Check(input)
	if len(violations) == 0 {
		t.Fatal("expected violations for private key in COPY heredoc, got none")
	}
}

func TestSecretsInCodeRule_Check_GitHubTokenInEnv(t *testing.T) {
	t.Parallel()
	r := NewSecretsInCodeRule()

	input := rules.LintInput{
		File: "Dockerfile",
		Stages: []instructions.Stage{
			{
				Commands: []instructions.Command{
					&instructions.EnvCommand{
						Env: []instructions.KeyValuePair{
							// gitleaks:allow
							// GitHub PAT: ghp_ + 36 alphanumeric (realistic entropy)
							{Key: "GITHUB_TOKEN", Value: "ghp_SfE7gMq5K9pR2nLwHvYt3dXc8jU6bA1Z0iFo"},
						},
					},
				},
			},
		},
	}

	violations := r.Check(input)
	if len(violations) == 0 {
		t.Fatal("expected violations for GitHub token in ENV, got none")
	}
}

func TestSecretsInCodeRule_Check_NoSecrets(t *testing.T) {
	t.Parallel()
	r := NewSecretsInCodeRule()

	input := rules.LintInput{
		File: "Dockerfile",
		Stages: []instructions.Stage{
			{
				Commands: []instructions.Command{
					&instructions.RunCommand{
						ShellDependantCmdLine: instructions.ShellDependantCmdLine{
							CmdLine: []string{"echo", "hello world"},
						},
					},
					&instructions.EnvCommand{
						Env: []instructions.KeyValuePair{
							{Key: "PORT", Value: "8080"},
							{Key: "NODE_ENV", Value: "production"},
						},
					},
				},
			},
		},
	}

	violations := r.Check(input)
	if len(violations) != 0 {
		t.Errorf("expected no violations for safe content, got %d", len(violations))
		for _, v := range violations {
			t.Logf("  - %s: %s", v.RuleCode, v.Message)
		}
	}
}

func TestSecretsInCodeRule_Check_ARGDefaultWithSecret(t *testing.T) {
	t.Parallel()
	r := NewSecretsInCodeRule()

	// gitleaks:allow
	// GitHub PAT: ghp_ + 36 alphanumeric (realistic entropy)
	secretValue := "ghp_SfE7gMq5K9pR2nLwHvYt3dXc8jU6bA1Z0iFo" //nolint:gosec // test data
	input := rules.LintInput{
		File: "Dockerfile",
		MetaArgs: []instructions.ArgCommand{
			{
				Args: []instructions.KeyValuePairOptional{
					{Key: "GITHUB_TOKEN", Value: &secretValue},
				},
			},
		},
		Stages: []instructions.Stage{{}},
	}

	violations := r.Check(input)
	if len(violations) == 0 {
		t.Fatal("expected violations for GitHub token in ARG default, got none")
	}
}

func TestRedact(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  string
	}{
		{"short", "***"},
		{"12345678", "***"},
		{"123456789", "1234...6789"},
		// gitleaks:allow
		{"ghp_SfE7gMq5K9pR2nLwHvYt3dXc8jU6bA1Z0iFo", "ghp_...0iFo"},
	}

	for _, tc := range tests {
		got := redact(tc.input)
		if got != tc.want {
			t.Errorf("redact(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestSecretsInCodeRule_Check_SecretInRunCommand(t *testing.T) {
	t.Parallel()
	r := NewSecretsInCodeRule()

	// gitleaks:allow
	// GitHub PAT: ghp_ + 36 alphanumeric (realistic entropy)
	//nolint:gosec // test data
	token := "ghp_SfE7gMq5K9pR2nLwHvYt3dXc8jU6bA1Z0iFo"
	input := rules.LintInput{
		File: "Dockerfile",
		Stages: []instructions.Stage{
			{
				Commands: []instructions.Command{
					&instructions.RunCommand{
						ShellDependantCmdLine: instructions.ShellDependantCmdLine{
							CmdLine: []string{
								"curl", "-H", "Authorization: Bearer " + token, "https://api.github.com",
							},
						},
					},
				},
			},
		},
	}

	violations := r.Check(input)
	if len(violations) == 0 {
		t.Fatal("expected violations for GitHub token in RUN command, got none")
	}
}
