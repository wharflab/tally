package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefault(t *testing.T) {
	cfg := Default()

	if cfg.Format != "text" {
		t.Errorf("Default format = %q, want %q", cfg.Format, "text")
	}

	// Default: 50 lines (P90 of 500 analyzed Dockerfiles)
	if cfg.Rules.MaxLines.Max != 50 {
		t.Errorf("Default MaxLines.Max = %d, want 50", cfg.Rules.MaxLines.Max)
	}

	// Default: true (count only meaningful lines)
	if !cfg.Rules.MaxLines.SkipBlankLines {
		t.Error("Default MaxLines.SkipBlankLines = false, want true")
	}

	// Default: true (count only instruction lines)
	if !cfg.Rules.MaxLines.SkipComments {
		t.Error("Default MaxLines.SkipComments = false, want true")
	}
}

func TestMaxLinesRuleEnabled(t *testing.T) {
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
		rule := MaxLinesRule{Max: tt.max}
		if got := rule.Enabled(); got != tt.want {
			t.Errorf("MaxLinesRule{Max: %d}.Enabled() = %v, want %v", tt.max, got, tt.want)
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

		if cfg.Format != "text" {
			t.Errorf("Format = %q, want %q", cfg.Format, "text")
		}

		if cfg.ConfigFile != "" {
			t.Errorf("ConfigFile = %q, want empty", cfg.ConfigFile)
		}
	})

	t.Run("loads config file", func(t *testing.T) {
		configPath := filepath.Join(tmpDir, ".tally.toml")
		configContent := `
format = "json"

[rules.max-lines]
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

		if cfg.Format != "json" {
			t.Errorf("Format = %q, want %q", cfg.Format, "json")
		}

		if cfg.Rules.MaxLines.Max != 500 {
			t.Errorf("MaxLines.Max = %d, want 500", cfg.Rules.MaxLines.Max)
		}

		if !cfg.Rules.MaxLines.SkipBlankLines {
			t.Error("MaxLines.SkipBlankLines = false, want true")
		}

		if !cfg.Rules.MaxLines.SkipComments {
			t.Error("MaxLines.SkipComments = false, want true")
		}

		if cfg.ConfigFile != configPath {
			t.Errorf("ConfigFile = %q, want %q", cfg.ConfigFile, configPath)
		}
	})

	t.Run("environment variables override config", func(t *testing.T) {
		configPath := filepath.Join(tmpDir, ".tally.toml")
		configContent := `
format = "json"

[rules.max-lines]
max = 500
`
		if err := os.WriteFile(configPath, []byte(configContent), 0o600); err != nil {
			t.Fatal(err)
		}
		defer os.Remove(configPath)

		// Set environment variables
		t.Setenv("TALLY_FORMAT", "text")
		t.Setenv("TALLY_RULES_MAX_LINES_MAX", "100")

		cfg, err := Load(dockerfilePath)
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}

		if cfg.Format != "text" {
			t.Errorf("Format = %q, want %q (env should override)", cfg.Format, "text")
		}

		if cfg.Rules.MaxLines.Max != 100 {
			t.Errorf("MaxLines.Max = %d, want 100 (env should override)", cfg.Rules.MaxLines.Max)
		}
	})
}

func TestEnvKeyTransform(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"TALLY_FORMAT", "format"},
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
