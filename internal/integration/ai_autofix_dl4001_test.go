package integration

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestAIAutofixDL4001CommandFamilyNormalize(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	dockerfilePath := filepath.Join(tmpDir, "Dockerfile")
	input := `FROM ubuntu:22.04
RUN wget -qO- https://example.com/bootstrap.sh >/dev/null
RUN curl -sS https://example.com/install.sh | sh
`
	if err := os.WriteFile(dockerfilePath, []byte(input), 0o644); err != nil {
		t.Fatalf("write Dockerfile: %v", err)
	}

	configPath := filepath.Join(tmpDir, ".tally.toml")
	config := fmt.Sprintf(`[ai]
enabled = true
timeout = "10s"
redact-secrets = false
command = ['%s', '-mode=command_family_normalize']

[rules.hadolint.DL4001]
fix = "explicit"
`, acpAgentPath)
	if err := os.WriteFile(configPath, []byte(config), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	args := []string{
		"lint",
		"--config", configPath,
		"--fix",
		"--fix-unsafe",
		"--fix-rule", "hadolint/DL4001",
		"--ignore", "*",
		"--select", "hadolint/DL4001",
		dockerfilePath,
	}
	cmd := exec.Command(binaryPath, args...)
	cmd.Env = append(os.Environ(), "GOCOVERDIR="+coverageDir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("lint --fix failed: %v\noutput:\n%s", err, output)
	}

	fixed, err := os.ReadFile(dockerfilePath)
	if err != nil {
		t.Fatalf("read fixed Dockerfile: %v", err)
	}

	fixedText := string(fixed)
	if strings.Contains(fixedText, "RUN curl -sS https://example.com/install.sh | sh") {
		t.Fatalf("expected curl command to be rewritten, got:\n%s", fixedText)
	}
	if !strings.Contains(fixedText, "RUN wget -nv -O- https://example.com/install.sh | sh") {
		t.Fatalf("expected wget rewrite in fixed Dockerfile, got:\n%s", fixedText)
	}

	gotApplied, ok, err := parseFixedCount(string(output))
	if err != nil {
		t.Fatalf("parse fixed summary: %v\noutput:\n%s", err, output)
	}
	if !ok {
		t.Fatalf("expected fixed summary in output, got:\n%s", output)
	}
	if gotApplied != 1 {
		t.Fatalf("expected 1 fix applied, got %d\noutput:\n%s", gotApplied, output)
	}
}
