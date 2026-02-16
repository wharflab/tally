package integration

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wharflab/tally/internal/dockerfile"
)

func TestGeminiSmoke_FixPreferMultiStageBuild(t *testing.T) {
	if os.Getenv("ACP_ENABLE_GEMINI_TESTS") != "1" {
		t.Skip("set ACP_ENABLE_GEMINI_TESTS=1 to run Gemini ACP smoke tests")
	}
	t.Parallel()

	agentCmd := geminiAcpCommand(t)
	configText := geminiSmokeConfig(agentCmd)
	dockerfilePath, configPath, input := prepareGeminiSmokeWorkspace(t, configText)
	outputStr, fixed := runGeminiSmokeFix(t, configPath, dockerfilePath)
	assertGeminiSmokeApplied(t, outputStr)

	if bytes.Equal(fixed, input) {
		t.Fatalf("expected Dockerfile to change, but it did not")
	}

	parsed, err := dockerfile.Parse(bytes.NewReader(fixed), nil)
	if err != nil {
		t.Fatalf("parse fixed Dockerfile: %v\nDockerfile:\n%s", err, fixed)
	}
	if len(parsed.Stages) < 2 {
		t.Fatalf("expected multi-stage Dockerfile with 2+ stages, got %d\nDockerfile:\n%s", len(parsed.Stages), fixed)
	}
	if !bytes.Contains(fixed, []byte(`CMD ["app"]`)) {
		t.Fatalf("expected CMD to be preserved in final stage\nDockerfile:\n%s", fixed)
	}
	if !bytes.Contains(fixed, []byte("COPY --from=")) {
		t.Fatalf("expected COPY --from=... in a multi-stage refactor\nDockerfile:\n%s", fixed)
	}
}

func geminiAcpCommand(t *testing.T) []string {
	t.Helper()

	geminiBin := os.Getenv("ACP_GEMINI_BIN")
	if geminiBin == "" {
		path, err := exec.LookPath("gemini")
		if err != nil {
			t.Skip("gemini CLI not found on PATH")
		}
		geminiBin = path
	}

	extraArgs := strings.Fields(os.Getenv("ACP_GEMINI_TEST_ARGS"))
	return append([]string{geminiBin, "--experimental-acp"}, extraArgs...)
}

func geminiSmokeConfig(agentCmd []string) string {
	return fmt.Sprintf(`[ai]
enabled = true
timeout = "90s"
redact-secrets = false
command = %s

[rules.tally.prefer-multi-stage-build]
fix = "explicit"
`, tomlStringArray(agentCmd))
}

func prepareGeminiSmokeWorkspace(t *testing.T, configText string) (string, string, []byte) {
	t.Helper()

	fixturePath := filepath.Join("testdata", "ai-autofix-prefer-multi-stage-build", "Dockerfile")
	input, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("read fixture Dockerfile: %v", err)
	}

	tmpDir := t.TempDir()
	dockerfilePath := filepath.Join(tmpDir, "Dockerfile")
	if err := os.WriteFile(dockerfilePath, input, 0o644); err != nil {
		t.Fatalf("write Dockerfile: %v", err)
	}

	configPath := filepath.Join(tmpDir, ".tally.toml")
	if err := os.WriteFile(configPath, []byte(configText), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	return dockerfilePath, configPath, input
}

func runGeminiSmokeFix(t *testing.T, configPath, dockerfilePath string) (string, []byte) {
	t.Helper()

	selectArgs, err := selectRules("tally/prefer-multi-stage-build", "tally/no-unreachable-stages")
	if err != nil {
		t.Fatalf("select rules: %v", err)
	}

	args := make([]string, 0, 16+len(selectArgs))
	args = append(args,
		"lint",
		"--config", configPath,
		"--slow-checks=off",
		"--ignore", "hadolint/DL3057",
		"--fix",
		"--fix-unsafe",
		"--fix-rule", "tally/prefer-multi-stage-build",
	)
	args = append(args, selectArgs...)
	args = append(args, dockerfilePath)

	cmd := exec.Command(binaryPath, args...)
	cmd.Env = append(os.Environ(),
		"GOCOVERDIR="+coverageDir,
	)

	output, runErr := cmd.CombinedOutput()
	// Keep error handling permissive: lint may exit non-zero when violations remain.
	_ = runErr

	outputStr := strings.ReplaceAll(string(output), "\r\n", "\n")
	fixed, err := os.ReadFile(dockerfilePath)
	if err != nil {
		t.Fatalf("read fixed Dockerfile: %v", err)
	}
	return outputStr, fixed
}

func assertGeminiSmokeApplied(t *testing.T, output string) {
	t.Helper()

	gotApplied, ok, err := parseFixedCount(output)
	if err != nil {
		t.Fatalf("parse fixed summary: %v\noutput:\n%s", err, output)
	}
	if !ok || gotApplied != 1 {
		t.Fatalf("expected AI fix to apply (Fixed 1 ...), got %d\noutput:\n%s", gotApplied, output)
	}
}

func tomlStringArray(values []string) string {
	quoted := make([]string, 0, len(values))
	for _, v := range values {
		// Single-quoted TOML strings avoid escape handling for the common case.
		quoted = append(quoted, "'"+strings.ReplaceAll(v, "'", "''")+"'")
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}
