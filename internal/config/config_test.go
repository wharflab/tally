package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefault(t *testing.T) {
	t.Parallel()
	cfg := Default()

	if cfg.Output.Format != "text" {
		t.Errorf("Default format = %q, want %q", cfg.Output.Format, "text")
	}

	// Default config should have empty rules - defaults are owned by rules themselves
	if cfg.Rules.Tally != nil {
		t.Error("Default Rules.Tally should be nil (defaults come from rules)")
	}

	// AI is opt-in and disabled by default.
	if cfg.AI.Enabled {
		t.Error("Default AI.Enabled should be false")
	}
	if cfg.AI.Timeout != "90s" {
		t.Errorf("Default AI.Timeout = %q, want %q", cfg.AI.Timeout, "90s")
	}
	if cfg.AI.MaxInputBytes != 256*1024 {
		t.Errorf("Default AI.MaxInputBytes = %d, want %d", cfg.AI.MaxInputBytes, 256*1024)
	}
	if !cfg.AI.RedactSecrets {
		t.Error("Default AI.RedactSecrets should be true")
	}
	if len(cfg.AI.Command) != 0 {
		t.Error("Default AI.Command should be empty")
	}
}

func TestDiscover(t *testing.T) {
	t.Parallel()
	// Create a temporary directory structure
	tmpDir := t.TempDir()

	// Create nested directories
	subDir := filepath.Join(tmpDir, "project", "src")
	if err := os.MkdirAll(subDir, 0o750); err != nil {
		t.Fatal(err)
	}

	// Create a Dockerfile in the deepest directory
	dockerfilePath := filepath.Join(subDir, "Dockerfile")
	if err := os.WriteFile(dockerfilePath, []byte("FROM alpine"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Run("no config file", func(t *testing.T) {
		result := Discover(dockerfilePath)
		if result != "" {
			t.Errorf("Discover() = %q, want empty string", result)
		}
	})

	t.Run("config in same directory", func(t *testing.T) {
		configPath := filepath.Join(subDir, ".tally.toml")
		if err := os.WriteFile(configPath, []byte("format = \"json\""), 0o600); err != nil {
			t.Fatal(err)
		}
		defer os.Remove(configPath)

		result := Discover(dockerfilePath)
		if result != configPath {
			t.Errorf("Discover() = %q, want %q", result, configPath)
		}
	})

	t.Run("config in parent directory", func(t *testing.T) {
		configPath := filepath.Join(tmpDir, "project", "tally.toml")
		if err := os.WriteFile(configPath, []byte("format = \"json\""), 0o600); err != nil {
			t.Fatal(err)
		}
		defer os.Remove(configPath)

		result := Discover(dockerfilePath)
		if result != configPath {
			t.Errorf("Discover() = %q, want %q", result, configPath)
		}
	})

	t.Run("prefers .tally.toml over tally.toml", func(t *testing.T) {
		hiddenConfig := filepath.Join(subDir, ".tally.toml")
		visibleConfig := filepath.Join(subDir, "tally.toml")

		if err := os.WriteFile(hiddenConfig, []byte("# hidden"), 0o600); err != nil {
			t.Fatal(err)
		}
		defer os.Remove(hiddenConfig)

		if err := os.WriteFile(visibleConfig, []byte("# visible"), 0o600); err != nil {
			t.Fatal(err)
		}
		defer os.Remove(visibleConfig)

		result := Discover(dockerfilePath)
		if result != hiddenConfig {
			t.Errorf("Discover() = %q, want %q (should prefer .tally.toml)", result, hiddenConfig)
		}
	})

	t.Run("closer config wins", func(t *testing.T) {
		// Config in project root
		rootConfig := filepath.Join(tmpDir, "project", "tally.toml")
		if err := os.WriteFile(rootConfig, []byte("# root"), 0o600); err != nil {
			t.Fatal(err)
		}
		defer os.Remove(rootConfig)

		// Config in src directory (closer to Dockerfile)
		srcConfig := filepath.Join(subDir, "tally.toml")
		if err := os.WriteFile(srcConfig, []byte("# src"), 0o600); err != nil {
			t.Fatal(err)
		}
		defer os.Remove(srcConfig)

		result := Discover(dockerfilePath)
		if result != srcConfig {
			t.Errorf("Discover() = %q, want %q (closer config should win)", result, srcConfig)
		}
	})
}

func setupTempProject(t *testing.T) (string, string) {
	t.Helper()

	tmpDir := t.TempDir()
	dockerfilePath := filepath.Join(tmpDir, "Dockerfile")
	if err := os.WriteFile(dockerfilePath, []byte("FROM alpine"), 0o600); err != nil {
		t.Fatal(err)
	}

	return tmpDir, dockerfilePath
}

func TestLoad_DefaultsWhenNoConfig(t *testing.T) {
	t.Parallel()
	_, dockerfilePath := setupTempProject(t)

	cfg, err := Load(dockerfilePath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Output.Format != "text" {
		t.Errorf("Format = %q, want %q", cfg.Output.Format, "text")
	}

	if cfg.ConfigFile != "" {
		t.Errorf("ConfigFile = %q, want empty", cfg.ConfigFile)
	}
}

func TestLoad_ConfigFileWithNestedFormat(t *testing.T) {
	t.Parallel()
	tmpDir, dockerfilePath := setupTempProject(t)

	configPath := filepath.Join(tmpDir, ".tally.toml")
	// Nested config format with namespaced rules
	configContent := `
[output]
format = "json"

[rules.tally.max-lines]
max = 500
skip-blank-lines = true
skip-comments = true
`
	if err := os.WriteFile(configPath, []byte(configContent), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(dockerfilePath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Output.Format != "json" {
		t.Errorf("Format = %q, want %q", cfg.Output.Format, "json")
	}

	// Verify rule options are loaded via GetOptions (generic config access)
	opts := cfg.Rules.GetOptions("tally/max-lines")
	if opts == nil {
		t.Fatal("max-lines options should be loaded from config")
	}
	if maxVal, ok := opts["max"].(int64); !ok || maxVal != 500 {
		t.Errorf("max-lines max = %v, want 500", opts["max"])
	}

	if cfg.ConfigFile != configPath {
		t.Errorf("ConfigFile = %q, want %q", cfg.ConfigFile, configPath)
	}
}

func TestLoad_AIConfig(t *testing.T) {
	t.Parallel()
	tmpDir, dockerfilePath := setupTempProject(t)

	configPath := filepath.Join(tmpDir, ".tally.toml")
	configContent := `
[ai]
enabled = true
timeout = "45s"
max-input-bytes = 1234
redact-secrets = false
command = ["fake-agent", "--acp"]
`
	if err := os.WriteFile(configPath, []byte(configContent), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(dockerfilePath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if !cfg.AI.Enabled {
		t.Error("AI.Enabled should be true")
	}
	if cfg.AI.Timeout != "45s" {
		t.Errorf("AI.Timeout = %q, want %q", cfg.AI.Timeout, "45s")
	}
	if cfg.AI.MaxInputBytes != 1234 {
		t.Errorf("AI.MaxInputBytes = %d, want %d", cfg.AI.MaxInputBytes, 1234)
	}
	if cfg.AI.RedactSecrets {
		t.Error("AI.RedactSecrets should be false")
	}
	if len(cfg.AI.Command) != 2 || cfg.AI.Command[0] != "fake-agent" || cfg.AI.Command[1] != "--acp" {
		t.Errorf("AI.Command = %v, want [fake-agent --acp]", cfg.AI.Command)
	}
}

func TestLoad_RuleIncludeExclude(t *testing.T) {
	t.Parallel()
	tmpDir, dockerfilePath := setupTempProject(t)

	configPath := filepath.Join(tmpDir, ".tally.toml")
	configContent := `
[rules]
include = ["tally/*"]
exclude = ["buildkit/MaintainerDeprecated"]

[rules.tally.max-lines]
severity = "error"
max = 100
`
	if err := os.WriteFile(configPath, []byte(configContent), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(dockerfilePath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Check buildkit rule is disabled via exclude
	enabled := cfg.Rules.IsEnabled("buildkit/MaintainerDeprecated")
	if enabled == nil || *enabled != false {
		t.Error("buildkit/MaintainerDeprecated should be disabled via exclude")
	}

	// Check tally rule is enabled via include
	enabled = cfg.Rules.IsEnabled("tally/max-lines")
	if enabled == nil || *enabled != true {
		t.Error("tally/max-lines should be enabled via include")
	}

	// Check unconfigured rule returns nil (use default)
	enabled = cfg.Rules.IsEnabled("buildkit/StageNameCasing")
	if enabled != nil {
		t.Errorf("unconfigured rule should return nil, got %v", *enabled)
	}
}

func TestLoad_SeverityOverride(t *testing.T) {
	t.Parallel()
	tmpDir, dockerfilePath := setupTempProject(t)

	configPath := filepath.Join(tmpDir, ".tally.toml")
	configContent := `
[rules.buildkit.StageNameCasing]
severity = "info"

[rules.tally.max-lines]
severity = "error"
max = 100
`
	if err := os.WriteFile(configPath, []byte(configContent), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(dockerfilePath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	sev := cfg.Rules.GetSeverity("buildkit/StageNameCasing")
	if sev != "info" {
		t.Errorf("GetSeverity(buildkit/StageNameCasing) = %q, want %q", sev, "info")
	}

	sev = cfg.Rules.GetSeverity("tally/max-lines")
	if sev != "error" {
		t.Errorf("GetSeverity(tally/max-lines) = %q, want %q", sev, "error")
	}

	// Unconfigured rule returns empty string
	sev = cfg.Rules.GetSeverity("buildkit/MaintainerDeprecated")
	if sev != "" {
		t.Errorf("GetSeverity(unconfigured) = %q, want empty", sev)
	}
}

func TestEnvKeyTransform(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  string
	}{
		{"TALLY_OUTPUT_FORMAT", "output.format"},
		{"TALLY_RULES_MAX_LINES_MAX", "rules.max-lines.max"},
		{"TALLY_RULES_MAX_LINES_SKIP_BLANK_LINES", "rules.max-lines.skip-blank-lines"},
		{"TALLY_RULES_MAX_LINES_SKIP_COMMENTS", "rules.max-lines.skip-comments"},
		{"TALLY_AI_ENABLED", "ai.enabled"},
		{"TALLY_AI_TIMEOUT", "ai.timeout"},
		{"TALLY_AI_MAX_INPUT_BYTES", "ai.max-input-bytes"},
		{"TALLY_AI_REDACT_SECRETS", "ai.redact-secrets"},
		{"TALLY_EXPECTED_DIAGNOSTICS", ""},
	}

	for _, tt := range tests {
		got, _ := envKeyTransform(tt.input, "test-value")
		if got != tt.want {
			t.Errorf("envKeyTransform(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestLoad_IgnoresUnknownTallyEnvVars(t *testing.T) {
	t.Setenv("TALLY_EXPECTED_DIAGNOSTICS", "172")

	cfg, err := loadWithConfigPath("")
	if err != nil {
		t.Fatalf("loadWithConfigPath(\"\") error = %v", err)
	}
	if cfg == nil {
		t.Fatal("loadWithConfigPath(\"\") returned nil config")
	}
}

func TestRulesConfigIncludeExclude(t *testing.T) {
	t.Parallel()
	rc := &RulesConfig{
		Include: []string{"buildkit/*", "tally/*", "hadolint/DL3026"},
		Exclude: []string{"hadolint/DL3008"},
		Buildkit: map[string]RuleConfig{
			"StageNameCasing": {
				Severity: "warning",
			},
		},
		Tally: map[string]RuleConfig{
			"max-lines": {
				Severity: "error",
				Options:  map[string]any{"max": 100},
			},
		},
		Hadolint: map[string]RuleConfig{
			"DL3026": {
				Severity: "warning",
				Options: map[string]any{
					"trusted-registries": []string{"docker.io", "gcr.io"},
				},
			},
		},
	}

	// BuildKit rules via include pattern
	enabled := rc.IsEnabled("buildkit/StageNameCasing")
	if enabled == nil || *enabled != true {
		t.Error("buildkit/StageNameCasing should be enabled via include")
	}

	// Include takes precedence over exclude (Ruff-style semantics)
	// If both include and exclude match, include wins
	rc2 := &RulesConfig{
		Include: []string{"buildkit/*"},
		Exclude: []string{"buildkit/*"}, // Even with wildcard exclude, include wins
	}
	enabled = rc2.IsEnabled("buildkit/MaintainerDeprecated")
	if enabled == nil || *enabled != true {
		t.Error("buildkit/MaintainerDeprecated should be enabled - include takes precedence")
	}

	// Tally rules via namespace wildcard
	enabled = rc.IsEnabled("tally/max-lines")
	if enabled == nil || *enabled != true {
		t.Error("tally/max-lines should be enabled via include")
	}

	// Check per-rule options still work
	opts := rc.GetOptions("tally/max-lines")
	if opts == nil {
		t.Fatal("tally/max-lines options should not be nil")
	}
	if maxVal, ok := opts["max"].(int); !ok || maxVal != 100 {
		t.Errorf("tally/max-lines max = %v, want 100", opts["max"])
	}

	// Hadolint rules via specific include
	enabled = rc.IsEnabled("hadolint/DL3026")
	if enabled == nil || *enabled != true {
		t.Error("hadolint/DL3026 should be enabled via include")
	}

	// Exclude disables rules not in include
	enabled = rc.IsEnabled("hadolint/DL3008")
	if enabled == nil || *enabled != false {
		t.Error("hadolint/DL3008 should be disabled via exclude")
	}

	// Unconfigured rule returns nil (not in include or exclude)
	enabled = rc.IsEnabled("hadolint/DL3009")
	if enabled != nil {
		t.Errorf("unconfigured rule should return nil, got %v", *enabled)
	}

	// Universal wildcard "*" matches all rules
	rc3 := &RulesConfig{
		Include: []string{"hadolint/DL3003"},
		Exclude: []string{"*"},
	}
	// Include still takes precedence even with "*" exclude
	enabled = rc3.IsEnabled("hadolint/DL3003")
	if enabled == nil || *enabled != true {
		t.Error("hadolint/DL3003 should be enabled - include takes precedence over * exclude")
	}
	// Other rules are disabled by "*"
	enabled = rc3.IsEnabled("hadolint/DL3008")
	if enabled == nil || *enabled != false {
		t.Error("hadolint/DL3008 should be disabled via * exclude")
	}
}

func TestRulesConfigGetOptionsTyped(t *testing.T) {
	t.Parallel()
	type opts struct {
		MinCommands *int  `koanf:"min-commands"`
		Enabled     *bool `koanf:"enabled"`
	}

	minValue := int64(5)
	rc := &RulesConfig{
		Tally: map[string]RuleConfig{
			"prefer-run-heredoc": {
				Options: map[string]any{
					"min-commands": minValue,
				},
			},
		},
	}

	decoded := DecodeRuleOptions(rc, "tally/prefer-run-heredoc", opts{})
	if decoded.MinCommands == nil || *decoded.MinCommands != 5 {
		t.Fatalf("MinCommands = %v, want 5", decoded.MinCommands)
	}

	// Missing options should return defaults.
	defEnabled := true
	defaults := opts{Enabled: &defEnabled}
	decoded = DecodeRuleOptions(rc, "tally/missing", defaults)
	if decoded.Enabled == nil || *decoded.Enabled != true {
		t.Fatalf("Enabled = %v, want true", decoded.Enabled)
	}
}

func TestLoadWithOverrides_EditorFirst(t *testing.T) {
	t.Parallel()
	tmpDir, dockerfilePath := setupTempProject(t)

	// Filesystem config sets output.format = json.
	configPath := filepath.Join(tmpDir, ".tally.toml")
	configContent := `
[output]
format = "json"
`
	if err := os.WriteFile(configPath, []byte(configContent), 0o600); err != nil {
		t.Fatal(err)
	}

	// Editor override should win in editorFirst.
	overrides := map[string]any{
		"output": map[string]any{
			"format": "text",
		},
	}

	cfg, err := LoadWithOverrides(dockerfilePath, overrides, ConfigurationPreferenceEditorFirst)
	if err != nil {
		t.Fatalf("LoadWithOverrides() error = %v", err)
	}
	if cfg.Output.Format != "text" {
		t.Fatalf("Output.Format = %q, want %q", cfg.Output.Format, "text")
	}
}

func TestLoadWithOverrides_FilesystemFirst_FillsGaps(t *testing.T) {
	t.Parallel()
	tmpDir, dockerfilePath := setupTempProject(t)

	// Filesystem config sets output.format, but not output.show-source.
	configPath := filepath.Join(tmpDir, ".tally.toml")
	configContent := `
[output]
format = "json"
`
	if err := os.WriteFile(configPath, []byte(configContent), 0o600); err != nil {
		t.Fatal(err)
	}

	// Editor override tries to change format (should lose) but also sets show-source (gap fill).
	overrides := map[string]any{
		"output": map[string]any{
			"format":      "text",
			"show-source": false,
		},
	}

	cfg, err := LoadWithOverrides(dockerfilePath, overrides, ConfigurationPreferenceFilesystemFirst)
	if err != nil {
		t.Fatalf("LoadWithOverrides() error = %v", err)
	}
	if cfg.Output.Format != "json" {
		t.Fatalf("Output.Format = %q, want %q", cfg.Output.Format, "json")
	}
	if cfg.Output.ShowSource != false {
		t.Fatalf("Output.ShowSource = %v, want %v", cfg.Output.ShowSource, false)
	}
}

func TestLoadWithOverrides_EditorOnly_SkipsFilesystem(t *testing.T) {
	t.Parallel()
	tmpDir, dockerfilePath := setupTempProject(t)

	// Filesystem config sets output.format = json, but should be ignored.
	configPath := filepath.Join(tmpDir, ".tally.toml")
	configContent := `
[output]
format = "json"
`
	if err := os.WriteFile(configPath, []byte(configContent), 0o600); err != nil {
		t.Fatal(err)
	}

	overrides := map[string]any{
		"output": map[string]any{
			"format": "text",
		},
	}

	cfg, err := LoadWithOverrides(dockerfilePath, overrides, ConfigurationPreferenceEditorOnly)
	if err != nil {
		t.Fatalf("LoadWithOverrides() error = %v", err)
	}
	if cfg.Output.Format != "text" {
		t.Fatalf("Output.Format = %q, want %q", cfg.Output.Format, "text")
	}
	if cfg.ConfigFile != "" {
		t.Fatalf("ConfigFile = %q, want empty (editorOnly)", cfg.ConfigFile)
	}
}
