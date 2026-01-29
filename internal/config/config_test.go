package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefault(t *testing.T) {
	cfg := Default()

	if cfg.Output.Format != "text" {
		t.Errorf("Default format = %q, want %q", cfg.Output.Format, "text")
	}

	// Default: 50 lines (P90 of 500 analyzed Dockerfiles)
	maxLines := GetMaxLinesOptions(&cfg.Rules)
	if maxLines.Max != 50 {
		t.Errorf("Default MaxLines.Max = %d, want 50", maxLines.Max)
	}

	// Default: true (count only meaningful lines)
	if !maxLines.SkipBlankLines {
		t.Error("Default MaxLines.SkipBlankLines = false, want true")
	}

	// Default: true (count only instruction lines)
	if !maxLines.SkipComments {
		t.Error("Default MaxLines.SkipComments = false, want true")
	}
}

func TestMaxLinesOptionsEnabled(t *testing.T) {
	tests := []struct {
		max  int
		want bool
	}{
		{0, false},
		{1, true},
		{100, true},
		{-1, false},
	}

	for _, tt := range tests {
		opts := MaxLinesOptions{Max: tt.max}
		// Max > 0 means enabled
		got := opts.Max > 0
		if got != tt.want {
			t.Errorf("MaxLinesOptions{Max: %d} enabled = %v, want %v", tt.max, got, tt.want)
		}
	}
}

func TestDiscover(t *testing.T) {
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

func TestLoad(t *testing.T) {
	tmpDir := t.TempDir()

	dockerfilePath := filepath.Join(tmpDir, "Dockerfile")
	if err := os.WriteFile(dockerfilePath, []byte("FROM alpine"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Run("loads defaults when no config", func(t *testing.T) {
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
	})

	t.Run("loads config file with new format", func(t *testing.T) {
		configPath := filepath.Join(tmpDir, ".tally.toml")
		// New config format with namespaced rules
		configContent := `
[output]
format = "json"

[rules."tally/max-lines"]
enabled = true
max = 500
skip-blank-lines = true
skip-comments = true
`
		if err := os.WriteFile(configPath, []byte(configContent), 0o600); err != nil {
			t.Fatal(err)
		}
		defer os.Remove(configPath)

		cfg, err := Load(dockerfilePath)
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}

		if cfg.Output.Format != "json" {
			t.Errorf("Format = %q, want %q", cfg.Output.Format, "json")
		}

		maxLines := GetMaxLinesOptions(&cfg.Rules)
		if maxLines.Max != 500 {
			t.Errorf("MaxLines.Max = %d, want 500", maxLines.Max)
		}

		if !maxLines.SkipBlankLines {
			t.Error("MaxLines.SkipBlankLines = false, want true")
		}

		if !maxLines.SkipComments {
			t.Error("MaxLines.SkipComments = false, want true")
		}

		if cfg.ConfigFile != configPath {
			t.Errorf("ConfigFile = %q, want %q", cfg.ConfigFile, configPath)
		}
	})

	t.Run("rule enable/disable", func(t *testing.T) {
		configPath := filepath.Join(tmpDir, ".tally.toml")
		configContent := `
[rules."buildkit/MaintainerDeprecated"]
enabled = false

[rules."tally/max-lines"]
enabled = true
max = 100
`
		if err := os.WriteFile(configPath, []byte(configContent), 0o600); err != nil {
			t.Fatal(err)
		}
		defer os.Remove(configPath)

		cfg, err := Load(dockerfilePath)
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}

		// Check buildkit rule is disabled
		enabled := cfg.Rules.IsEnabled("buildkit/MaintainerDeprecated")
		if enabled == nil || *enabled != false {
			t.Error("buildkit/MaintainerDeprecated should be disabled")
		}

		// Check tally rule is enabled
		enabled = cfg.Rules.IsEnabled("tally/max-lines")
		if enabled == nil || *enabled != true {
			t.Error("tally/max-lines should be enabled")
		}

		// Check unconfigured rule returns nil (use default)
		enabled = cfg.Rules.IsEnabled("buildkit/StageNameCasing")
		if enabled != nil {
			t.Errorf("unconfigured rule should return nil, got %v", *enabled)
		}
	})

	t.Run("severity override", func(t *testing.T) {
		configPath := filepath.Join(tmpDir, ".tally.toml")
		configContent := `
[rules."buildkit/StageNameCasing"]
severity = "info"

[rules."tally/max-lines"]
severity = "error"
max = 100
`
		if err := os.WriteFile(configPath, []byte(configContent), 0o600); err != nil {
			t.Fatal(err)
		}
		defer os.Remove(configPath)

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
	})
}

func TestEnvKeyTransform(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"TALLY_OUTPUT_FORMAT", "output.format"},
		{"TALLY_RULES_MAX_LINES_MAX", "rules.max-lines.max"},
		{"TALLY_RULES_MAX_LINES_SKIP_BLANK_LINES", "rules.max-lines.skip-blank-lines"},
		{"TALLY_RULES_MAX_LINES_SKIP_COMMENTS", "rules.max-lines.skip-comments"},
	}

	for _, tt := range tests {
		got := envKeyTransform(tt.input)
		if got != tt.want {
			t.Errorf("envKeyTransform(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestRulesConfigNamespaceDefaults(t *testing.T) {
	rc := &RulesConfig{
		Defaults: map[string]RuleConfig{
			"buildkit/*": {
				Enabled:  boolPtr(false),
				Severity: "info",
			},
		},
		PerRule: map[string]RuleConfig{
			"buildkit/StageNameCasing": {
				Enabled: boolPtr(true), // Override namespace default
			},
		},
	}

	// Rule with explicit config overrides namespace default
	enabled := rc.IsEnabled("buildkit/StageNameCasing")
	if enabled == nil || *enabled != true {
		t.Error("buildkit/StageNameCasing should be enabled (explicit override)")
	}

	// Rule without explicit config uses namespace default
	enabled = rc.IsEnabled("buildkit/MaintainerDeprecated")
	if enabled == nil || *enabled != false {
		t.Error("buildkit/MaintainerDeprecated should be disabled (namespace default)")
	}

	// Severity from namespace default
	sev := rc.GetSeverity("buildkit/MaintainerDeprecated")
	if sev != "info" {
		t.Errorf("GetSeverity() = %q, want %q", sev, "info")
	}

	// Rule outside namespace returns nil (no default)
	enabled = rc.IsEnabled("tally/max-lines")
	if enabled != nil {
		t.Errorf("tally/max-lines should return nil, got %v", *enabled)
	}
}
