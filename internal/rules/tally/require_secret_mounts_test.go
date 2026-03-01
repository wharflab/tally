package tally

import (
	"strings"
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/wharflab/tally/internal/testutil"
)

func TestRequireSecretMountsMetadata(t *testing.T) {
	t.Parallel()
	snaps.MatchStandaloneJSON(t, NewRequireSecretMountsRule().Metadata())
}

func TestRequireSecretMountsDefaultConfig(t *testing.T) {
	t.Parallel()
	cfg := DefaultRequireSecretMountsConfig()
	if cfg.Commands == nil {
		t.Fatal("expected non-nil Commands map")
	}
	if len(cfg.Commands) != 0 {
		t.Fatalf("expected empty Commands map, got %d entries", len(cfg.Commands))
	}
}

func TestRequireSecretMountsValidateConfig(t *testing.T) {
	t.Parallel()
	rule := NewRequireSecretMountsRule()

	t.Run("valid config", func(t *testing.T) {
		cfg := map[string]any{
			"commands": map[string]any{
				"pip": map[string]any{
					"id":     "pipconf",
					"target": "/root/.config/pip/pip.conf",
				},
			},
		}
		if err := rule.ValidateConfig(cfg); err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
	})

	t.Run("nil config", func(t *testing.T) {
		if err := rule.ValidateConfig(nil); err != nil {
			t.Fatalf("expected no error for nil config, got: %v", err)
		}
	})

	t.Run("invalid config - extra property", func(t *testing.T) {
		cfg := map[string]any{
			"not-a-field": true,
		}
		if err := rule.ValidateConfig(cfg); err == nil {
			t.Fatal("expected error for invalid config")
		}
	})

	t.Run("invalid config - missing id", func(t *testing.T) {
		cfg := map[string]any{
			"commands": map[string]any{
				"pip": map[string]any{
					"target": "/root/.config/pip/pip.conf",
				},
			},
		}
		if err := rule.ValidateConfig(cfg); err == nil {
			t.Fatal("expected error for missing id")
		}
	})

	t.Run("invalid config - missing target", func(t *testing.T) {
		cfg := map[string]any{
			"commands": map[string]any{
				"pip": map[string]any{
					"id": "pipconf",
				},
			},
		}
		if err := rule.ValidateConfig(cfg); err == nil {
			t.Fatal("expected error for missing target")
		}
	})
}

func pipConfig() RequireSecretMountsConfig {
	return RequireSecretMountsConfig{
		Commands: map[string]SecretMountSpec{
			"pip": {ID: "pipconf", Target: "/root/.config/pip/pip.conf"},
		},
	}
}

func multiConfig() RequireSecretMountsConfig {
	return RequireSecretMountsConfig{
		Commands: map[string]SecretMountSpec{
			"pip": {ID: "pipconf", Target: "/root/.config/pip/pip.conf"},
			"npm": {ID: "npmrc", Target: "/root/.npmrc"},
		},
	}
}

func TestRequireSecretMountsCheck(t *testing.T) {
	t.Parallel()
	testutil.RunRuleTests(t, NewRequireSecretMountsRule(), []testutil.RuleTestCase{
		{
			Name: "empty config - no violations",
			Content: `FROM python:3.12-slim
RUN pip install -r requirements.txt
`,
			Config:         RequireSecretMountsConfig{Commands: map[string]SecretMountSpec{}},
			WantViolations: 0,
		},
		{
			Name: "command present no mount - violation",
			Content: `FROM python:3.12-slim
RUN pip install -r requirements.txt
`,
			Config:         pipConfig(),
			WantViolations: 1,
			WantMessages:   []string{"missing required secret mount for 'pip'"},
		},
		{
			Name: "command present correct mount - no violation",
			Content: `FROM python:3.12-slim
RUN --mount=type=secret,id=pipconf,target=/root/.config/pip/pip.conf pip install -r requirements.txt
`,
			Config:         pipConfig(),
			WantViolations: 0,
		},
		{
			Name: "correct id wrong target - still missing",
			Content: `FROM python:3.12-slim
RUN --mount=type=secret,id=pipconf,target=/wrong/path pip install flask
`,
			Config:         pipConfig(),
			WantViolations: 1,
			WantMessages:   []string{"missing required secret mount for 'pip'"},
		},
		{
			Name: "wrong id correct target - violation",
			Content: `FROM python:3.12-slim
RUN --mount=type=secret,id=wrong,target=/root/.config/pip/pip.conf pip install flask
`,
			Config:         pipConfig(),
			WantViolations: 1,
			WantMessages:   []string{"missing required secret mount for 'pip'"},
		},
		{
			Name: "same id different targets across commands - both needed",
			Content: `FROM python:3.12-slim
RUN pip install flask && npm install express
`,
			Config: RequireSecretMountsConfig{
				Commands: map[string]SecretMountSpec{
					"pip": {ID: "env", Target: "/root/.config/pip/pip.conf"},
					"npm": {ID: "env", Target: "/root/.npmrc"},
				},
			},
			WantViolations: 1,
		},
		{
			Name: "same id different targets - one present one missing",
			Content: `FROM python:3.12-slim
RUN --mount=type=secret,id=env,target=/root/.npmrc pip install flask && npm install express
`,
			Config: RequireSecretMountsConfig{
				Commands: map[string]SecretMountSpec{
					"pip": {ID: "env", Target: "/root/.config/pip/pip.conf"},
					"npm": {ID: "env", Target: "/root/.npmrc"},
				},
			},
			WantViolations: 1,
			WantMessages:   []string{"missing required secret mount for 'pip'"},
		},
		{
			Name: "same id different targets - both present",
			Content: `FROM python:3.12-slim
RUN --mount=type=secret,id=env,target=/root/.config/pip/pip.conf --mount=type=secret,id=env,target=/root/.npmrc pip install flask && npm install express
`,
			Config: RequireSecretMountsConfig{
				Commands: map[string]SecretMountSpec{
					"pip": {ID: "env", Target: "/root/.config/pip/pip.conf"},
					"npm": {ID: "env", Target: "/root/.npmrc"},
				},
			},
			WantViolations: 0,
		},
		{
			Name: "multiple commands in one RUN - single combined violation",
			Content: `FROM python:3.12-slim
RUN pip install flask && npm install express
`,
			Config:         multiConfig(),
			WantViolations: 1,
		},
		{
			Name: "command behind env wrapper - still detected",
			Content: `FROM python:3.12-slim
RUN env PIP_INDEX_URL=https://example.com/simple pip install -r requirements.txt
`,
			Config:         pipConfig(),
			WantViolations: 1,
			WantMessages:   []string{"missing required secret mount for 'pip'"},
		},
		{
			Name: "unconfigured command - no violation",
			Content: `FROM python:3.12-slim
RUN apt-get update && apt-get install -y curl
`,
			Config:         pipConfig(),
			WantViolations: 0,
		},
		{
			Name: "non-RUN instructions - no violation",
			Content: `FROM python:3.12-slim
COPY requirements.txt .
ENV PIP_INDEX_URL=https://example.com/simple/
`,
			Config:         pipConfig(),
			WantViolations: 0,
		},
		{
			Name: "exec-form RUN - skipped",
			Content: `FROM python:3.12-slim
RUN ["pip", "install", "flask"]
`,
			Config:         pipConfig(),
			WantViolations: 0,
		},
		{
			Name: "existing cache mount with missing secret - fix preserves cache mount",
			Content: `FROM python:3.12-slim
RUN --mount=type=cache,target=/root/.cache/pip pip install -r requirements.txt
`,
			Config:         pipConfig(),
			WantViolations: 1,
		},
		{
			Name: "nil config - no violations",
			Content: `FROM python:3.12-slim
RUN pip install -r requirements.txt
`,
			Config:         nil,
			WantViolations: 0,
		},
		{
			Name: "dedup - multiple pip commands same RUN one violation",
			Content: `FROM python:3.12-slim
RUN pip install flask && pip install django
`,
			Config:         pipConfig(),
			WantViolations: 1,
		},
		{
			Name: "env mount - missing",
			Content: `FROM alpine:3.21
RUN gh auth login
`,
			Config: RequireSecretMountsConfig{
				Commands: map[string]SecretMountSpec{
					"gh": {ID: "gh-token", Env: "GH_TOKEN"},
				},
			},
			WantViolations: 1,
			WantMessages:   []string{"missing required secret mount for 'gh' (id=gh-token, env=GH_TOKEN)"},
		},
		{
			Name: "env mount - present",
			Content: `FROM alpine:3.21
RUN --mount=type=secret,id=gh-token,env=GH_TOKEN gh auth login
`,
			Config: RequireSecretMountsConfig{
				Commands: map[string]SecretMountSpec{
					"gh": {ID: "gh-token", Env: "GH_TOKEN"},
				},
			},
			WantViolations: 0,
		},
		{
			Name: "multiple env mounts for same command - aws credentials",
			Content: `FROM amazon/aws-cli:latest
RUN aws s3 cp s3://bucket/file .
`,
			Config: RequireSecretMountsConfig{
				Commands: map[string]SecretMountSpec{
					"aws": {ID: "aws-creds", Target: "/root/.aws/credentials"},
				},
			},
			WantViolations: 1,
		},
	})
}

func TestRequireSecretMountsCheckWithFixes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
		config  RequireSecretMountsConfig
		wantFix string // substring expected in the fix NewText
	}{
		{
			name: "adds secret mount to bare RUN",
			content: `FROM python:3.12-slim
RUN pip install -r requirements.txt
`,
			config:  pipConfig(),
			wantFix: "--mount=type=secret,id=pipconf,target=/root/.config/pip/pip.conf",
		},
		{
			name: "insertion does not overwrite existing cache mount",
			content: `FROM python:3.12-slim
RUN --mount=type=cache,target=/root/.cache/pip pip install -r requirements.txt
`,
			config:  pipConfig(),
			wantFix: "--mount=type=secret,id=pipconf,target=/root/.config/pip/pip.conf",
		},
		{
			name: "env mount fix",
			content: `FROM alpine:3.21
RUN gh auth login
`,
			config: RequireSecretMountsConfig{
				Commands: map[string]SecretMountSpec{
					"gh": {ID: "gh-token", Env: "GH_TOKEN"},
				},
			},
			wantFix: "--mount=type=secret,id=gh-token,env=GH_TOKEN",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := testutil.MakeLintInputWithConfig(t, "Dockerfile", tt.content, tt.config)
			violations := NewRequireSecretMountsRule().Check(input)
			if len(violations) == 0 {
				t.Fatal("expected at least one violation")
			}
			v := violations[0]
			if v.SuggestedFix == nil {
				t.Fatal("expected a suggested fix")
			}
			if len(v.SuggestedFix.Edits) == 0 {
				t.Fatal("expected at least one edit")
			}
			edit := v.SuggestedFix.Edits[0]
			if !strings.Contains(edit.NewText, tt.wantFix) {
				t.Errorf("fix NewText = %q, want substring %q", edit.NewText, tt.wantFix)
			}
		})
	}
}
