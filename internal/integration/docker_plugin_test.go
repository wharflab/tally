package integration

import (
	"encoding/json/v2"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/docker/cli/cli-plugins/metadata"
)

func TestDockerCLIPluginMetadata(t *testing.T) {
	t.Parallel()

	pluginPath := dockerPluginPath(t)
	cmd := exec.Command(pluginPath, metadata.MetadataSubcommandName)
	cmd.Env = append(os.Environ(), "GOCOVERDIR="+coverageDir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("metadata command failed: %v\noutput:\n%s", err, output)
	}

	var got metadata.Metadata
	if err := json.Unmarshal(output, &got); err != nil {
		t.Fatalf("parse metadata JSON: %v\noutput:\n%s", err, output)
	}
	if got.SchemaVersion != "0.1.0" {
		t.Fatalf("SchemaVersion = %q, want 0.1.0", got.SchemaVersion)
	}
	if got.Vendor != "Wharflab" {
		t.Fatalf("Vendor = %q, want Wharflab", got.Vendor)
	}
	if got.Version == "" {
		t.Fatal("Version should not be empty")
	}
}

func TestDockerCLIPluginLintCommand(t *testing.T) {
	t.Parallel()

	pluginPath := dockerPluginPath(t)
	tmpDir := t.TempDir()
	dockerfilePath := filepath.Join(tmpDir, "Dockerfile")
	if err := os.WriteFile(dockerfilePath, []byte("FROM alpine:3.20\n"), 0o644); err != nil {
		t.Fatalf("write Dockerfile: %v", err)
	}
	configPath := filepath.Join(tmpDir, ".tally.toml")
	if err := os.WriteFile(configPath, []byte(""), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cmd := exec.Command(pluginPath, "lint", "--config", configPath, "--format", "json", "--ignore", "*", dockerfilePath)
	cmd.Env = append(os.Environ(), "GOCOVERDIR="+coverageDir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("plugin lint failed: %v\noutput:\n%s", err, output)
	}
	if !strings.Contains(string(output), `"files_scanned"`) {
		t.Fatalf("expected JSON lint output with files_scanned, got:\n%s", output)
	}
}

func TestDockerCLIPluginVersionFlag(t *testing.T) {
	t.Parallel()

	pluginPath := dockerPluginPath(t)
	cmd := exec.Command(pluginPath, "lint", "--version")
	cmd.Env = append(os.Environ(), "GOCOVERDIR="+coverageDir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("plugin version failed: %v\noutput:\n%s", err, output)
	}
	if !strings.Contains(string(output), "tally version ") {
		t.Fatalf("expected tally version output, got:\n%s", output)
	}
}

func TestDockerCLIPluginSeparatesDockerAndTallyConfigFlags(t *testing.T) {
	t.Parallel()

	pluginPath := dockerPluginPath(t)
	tmpDir := t.TempDir()
	dockerConfigDir := filepath.Join(tmpDir, "docker-config")
	if err := os.MkdirAll(dockerConfigDir, 0o750); err != nil {
		t.Fatalf("create Docker config dir: %v", err)
	}
	dockerfilePath := filepath.Join(tmpDir, "Dockerfile")
	if err := os.WriteFile(dockerfilePath, []byte("FROM alpine:3.20\n"), 0o644); err != nil {
		t.Fatalf("write Dockerfile: %v", err)
	}
	tallyConfigPath := filepath.Join(tmpDir, ".tally.toml")
	if err := os.WriteFile(tallyConfigPath, []byte(""), 0o644); err != nil {
		t.Fatalf("write tally config: %v", err)
	}

	cmd := exec.Command(
		pluginPath,
		"--config", dockerConfigDir,
		"lint",
		"--config", tallyConfigPath,
		"--format", "json",
		"--ignore", "*",
		dockerfilePath,
	)
	cmd.Env = append(os.Environ(), "GOCOVERDIR="+coverageDir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("plugin lint with Docker global --config failed: %v\noutput:\n%s", err, output)
	}
	if !strings.Contains(string(output), `"files_scanned"`) {
		t.Fatalf("expected JSON lint output with files_scanned, got:\n%s", output)
	}
}

func dockerPluginPath(t *testing.T) string {
	t.Helper()

	pluginName := "docker-lint"
	if runtime.GOOS == "windows" {
		pluginName += ".exe"
	}
	pluginPath := filepath.Join(t.TempDir(), pluginName)
	if runtime.GOOS == "windows" {
		input, err := os.ReadFile(binaryPath)
		if err != nil {
			t.Fatalf("read integration binary: %v", err)
		}
		if err := os.WriteFile(pluginPath, input, 0o755); err != nil {
			t.Fatalf("copy plugin binary: %v", err)
		}
		return pluginPath
	}

	if err := os.Symlink(binaryPath, pluginPath); err != nil {
		if errors.Is(err, os.ErrPermission) {
			t.Skipf("symlink not permitted: %v", err)
		}
		t.Fatalf("create plugin symlink: %v", err)
	}
	return pluginPath
}
