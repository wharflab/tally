package lspserver

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/wharflab/tally/internal/config"
	"github.com/wharflab/tally/internal/linter"
	"github.com/wharflab/tally/internal/registry"
	"github.com/wharflab/tally/internal/rules"
)

type stubImageResolver struct {
	resolveConfig func(ctx context.Context, ref, platform string) (registry.ImageConfig, error)
}

func (r *stubImageResolver) ResolveConfig(ctx context.Context, ref, platform string) (registry.ImageConfig, error) {
	return r.resolveConfig(ctx, ref, platform)
}

func TestResolveConfig_DefaultsSlowChecksOnInLSP(t *testing.T) {
	t.Parallel()

	s := New()
	s.settings = clientSettings{
		Global: folderSettings{
			ConfigurationPreference: config.ConfigurationPreferenceEditorFirst,
			WorkspaceTrusted:        true,
		},
	}
	dir := t.TempDir()
	filePath := filepath.Join(dir, "Dockerfile")
	require.NoError(t, os.WriteFile(filePath, []byte("FROM alpine\n"), 0o644))

	cfg := s.resolveConfig(filePath)
	require.NotNil(t, cfg)
	require.Equal(t, "on", cfg.SlowChecks.Mode)
}

func TestResolveConfig_PreservesExplicitSlowChecksOff(t *testing.T) {
	t.Parallel()

	s := New()
	s.settings = clientSettings{
		Global: folderSettings{
			ConfigurationPreference: config.ConfigurationPreferenceEditorFirst,
			WorkspaceTrusted:        true,
		},
	}
	dir := t.TempDir()
	filePath := filepath.Join(dir, "Dockerfile")
	configPath := filepath.Join(dir, ".tally.toml")

	require.NoError(t, os.WriteFile(filePath, []byte("FROM alpine\n"), 0o644))
	require.NoError(t, os.WriteFile(configPath, []byte("[slow-checks]\nmode = \"off\"\n"), 0o644))

	cfg := s.resolveConfig(filePath)
	require.NotNil(t, cfg)
	require.Equal(t, "off", cfg.SlowChecks.Mode)
}

func TestResolveConfig_DefaultsSlowChecksOffInUntrustedLSP(t *testing.T) {
	t.Parallel()

	s := New()
	s.settings = clientSettings{
		Global: folderSettings{
			ConfigurationPreference: config.ConfigurationPreferenceEditorFirst,
			WorkspaceTrusted:        false,
		},
	}
	dir := t.TempDir()
	filePath := filepath.Join(dir, "Dockerfile")
	require.NoError(t, os.WriteFile(filePath, []byte("FROM alpine\n"), 0o644))

	cfg := s.resolveConfig(filePath)
	require.NotNil(t, cfg)
	require.Equal(t, "off", cfg.SlowChecks.Mode)
}

func TestResolveConfig_PreservesExplicitSlowChecksOnInUntrustedLSP(t *testing.T) {
	t.Parallel()

	s := New()
	s.settings = clientSettings{
		Global: folderSettings{
			ConfigurationPreference: config.ConfigurationPreferenceEditorFirst,
			WorkspaceTrusted:        false,
		},
	}
	dir := t.TempDir()
	filePath := filepath.Join(dir, "Dockerfile")
	configPath := filepath.Join(dir, ".tally.toml")

	require.NoError(t, os.WriteFile(filePath, []byte("FROM alpine\n"), 0o644))
	require.NoError(t, os.WriteFile(configPath, []byte("[slow-checks]\nmode = \"on\"\n"), 0o644))

	cfg := s.resolveConfig(filePath)
	require.NotNil(t, cfg)
	require.Equal(t, "on", cfg.SlowChecks.Mode)
}

func TestLintContentWithConfig_RunsAsyncUndefinedVarResolution(t *testing.T) {
	t.Parallel()

	oldResolver := registry.NewDefaultResolver
	registry.NewDefaultResolver = func() registry.ImageResolver {
		return &stubImageResolver{
			resolveConfig: func(_ context.Context, ref, _ string) (registry.ImageConfig, error) {
				require.Equal(t, "public.ecr.aws/lambda/python:3.12", ref)
				return registry.ImageConfig{
					Env: map[string]string{
						"LAMBDA_TASK_ROOT": "/var/task",
					},
					OS:   "linux",
					Arch: "amd64",
				}, nil
			},
		}
	}
	t.Cleanup(func() {
		registry.NewDefaultResolver = oldResolver
	})

	content := []byte(
		"FROM scratch AS handler\n" +
			"COPY handler.py /handler.py\n" +
			"\n" +
			"FROM public.ecr.aws/lambda/python:3.12\n" +
			"COPY --from=handler /handler.py ${LAMBDA_TASK_ROOT}\n" +
			"CMD [\"handler.handler\"]\n",
	)

	cfg := config.Default()
	cfg.SlowChecks.Mode = "on"

	filePath := filepath.Join(t.TempDir(), "Dockerfile")
	rawResult, err := linter.LintFile(linter.Input{
		FilePath: filePath,
		Content:  content,
		Config:   cfg,
	})
	require.NoError(t, err)
	require.NotEmpty(t, rawResult.AsyncPlan)
	require.True(t, hasRule(rawResult.Violations, "buildkit/UndefinedVar"))

	s := New()
	uri := fileURI(filePath)
	lr := s.lintContentWithConfig(context.Background(), uri, content, cfg, rawResult.ParseResult)
	require.False(t, hasRule(lr.violations, "buildkit/UndefinedVar"))
}

func fileURI(path string) string {
	return (&url.URL{Scheme: "file", Path: filepath.ToSlash(path)}).String()
}

func hasRule(violations []rules.Violation, ruleCode string) bool {
	for _, v := range violations {
		if v.RuleCode == ruleCode {
			return true
		}
	}
	return false
}
