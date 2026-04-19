package integration

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/wharflab/tally/internal/ai/autofixdata"
	"github.com/wharflab/tally/internal/dockerfile"
	"github.com/wharflab/tally/internal/shell"
)

func TestGeminiSmoke_FixPreferMultiStageBuild(t *testing.T) {
	if os.Getenv("ACP_ENABLE_GEMINI_TESTS") != "1" {
		t.Skip("set ACP_ENABLE_GEMINI_TESTS=1 to run Gemini ACP smoke tests")
	}
	t.Parallel()

	agentCmd := geminiAcpCommand(t)
	configText := geminiSmokeConfigMultiStage(agentCmd)
	runCfg := geminiSmokeRunConfig{
		fixtureDir:      "ai-autofix-prefer-multi-stage-build",
		fixRule:         "tally/prefer-multi-stage-build",
		selectRuleCodes: []string{"tally/prefer-multi-stage-build", "tally/no-unreachable-stages"},
		configText:      configText,
		extraLintIgnore: []string{"hadolint/DL3057"},
	}
	dockerfilePath, input, outputStr, fixed := runGeminiSmoke(t, runCfg)
	_ = dockerfilePath
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

// TestGeminiSmoke_FixBothMultiStageAndUVOverConda exercises BOTH AI objectives
// (prefer-multi-stage-build and gpu/prefer-uv-over-conda) on a single
// Dockerfile that triggers both rules. It runs the CLI twice because each AI
// resolver returns a whole-file TextEdit; two AI objectives cannot land in one
// pass without edit conflicts.
//
// Pass 1: migrate conda → uv (single-stage output).
// Pass 2: convert to multi-stage, preserving the uv migration.
//
// This test is the end-to-end proof that Gemini can route to each ObjectiveKind
// and that the two fixes compose correctly on the same file.
func TestGeminiSmoke_FixBothMultiStageAndUVOverConda(t *testing.T) {
	if os.Getenv("ACP_ENABLE_GEMINI_TESTS") != "1" {
		t.Skip("set ACP_ENABLE_GEMINI_TESTS=1 to run Gemini ACP smoke tests")
	}
	t.Parallel()

	agentCmd := geminiAcpCommand(t)
	configText := geminiSmokeConfigBoth(agentCmd)

	// Pass 1: uv migration.
	uvCfg := geminiSmokeRunConfig{
		fixtureDir:      "ai-autofix-both-prefer-multistage-and-uv-over-conda",
		fixRule:         "tally/gpu/prefer-uv-over-conda",
		selectRuleCodes: []string{"tally/gpu/prefer-uv-over-conda", "tally/prefer-multi-stage-build", "tally/no-unreachable-stages"},
		configText:      configText,
	}
	dockerfilePath, input, output1, fixed1 := runGeminiSmoke(t, uvCfg)
	assertGeminiSmokeApplied(t, output1)

	if bytes.Equal(fixed1, input) {
		t.Fatalf("pass 1: expected Dockerfile to change, but it did not")
	}

	parsed1, err := dockerfile.Parse(bytes.NewReader(fixed1), nil)
	if err != nil {
		t.Fatalf("pass 1: parse fixed Dockerfile: %v\n%s", err, fixed1)
	}
	if len(parsed1.Stages) != 1 {
		t.Fatalf("pass 1: expected still single-stage after UV migration, got %d\n%s", len(parsed1.Stages), fixed1)
	}
	assertNoCondaPythonInstall(t, "pass 1", fixed1)
	assertHasUV(t, "pass 1", fixed1)
	assertPreservedRuntime(t, "pass 1", fixed1)

	// Pass 2: multi-stage conversion.
	msCfg := geminiSmokeRunConfig{
		fixtureDir:      "", // reuse the file already written by pass 1
		existingPath:    dockerfilePath,
		fixRule:         "tally/prefer-multi-stage-build",
		selectRuleCodes: []string{"tally/prefer-multi-stage-build", "tally/no-unreachable-stages"},
		configText:      configText,
		extraLintIgnore: []string{"hadolint/DL3057"},
	}
	_, _, output2, fixed2 := runGeminiSmoke(t, msCfg)
	assertGeminiSmokeApplied(t, output2)

	if bytes.Equal(fixed2, fixed1) {
		t.Fatalf("pass 2: expected Dockerfile to change, but it did not\n%s", fixed2)
	}

	parsed2, err := dockerfile.Parse(bytes.NewReader(fixed2), nil)
	if err != nil {
		t.Fatalf("pass 2: parse fixed Dockerfile: %v\n%s", err, fixed2)
	}
	if len(parsed2.Stages) < 2 {
		t.Fatalf("pass 2: expected multi-stage Dockerfile with 2+ stages, got %d\n%s", len(parsed2.Stages), fixed2)
	}
	if !bytes.Contains(fixed2, []byte("COPY --from=")) {
		t.Fatalf("pass 2: expected COPY --from=... after multi-stage conversion\n%s", fixed2)
	}
	assertNoCondaPythonInstall(t, "pass 2", fixed2)
	// The multi-stage conversion may split installs across stages; final stage
	// must still be functional but uv may have moved. Verify at file level.
	assertHasUV(t, "pass 2", fixed2)
	assertPreservedRuntime(t, "pass 2", fixed2)
}

// --- Shared Gemini smoke helpers ---

type geminiSmokeRunConfig struct {
	fixtureDir      string
	existingPath    string // if set, reuse an already-prepared file path
	fixRule         string
	selectRuleCodes []string
	configText      string
	extraLintIgnore []string
}

// runGeminiSmoke prepares a workspace (if fixtureDir is non-empty) or reuses
// an existing file (existingPath), runs tally lint --fix --fix-unsafe
// targeting cfg.fixRule, and returns the dockerfile path, the original
// input bytes, the CLI stdout, and the post-fix file bytes.
func runGeminiSmoke(t *testing.T, cfg geminiSmokeRunConfig) (string, []byte, string, []byte) {
	t.Helper()

	var (
		dockerfilePath string
		configPath     string
		input          []byte
	)
	if cfg.existingPath != "" {
		dockerfilePath = cfg.existingPath
		bytesRead, err := os.ReadFile(dockerfilePath)
		if err != nil {
			t.Fatalf("read existing Dockerfile: %v", err)
		}
		input = bytesRead
		// Reuse the config co-located with the file if present.
		configPath = filepath.Join(filepath.Dir(dockerfilePath), ".tally.toml")
		if _, err := os.Stat(configPath); err != nil {
			// Write a fresh config beside the file.
			if err := os.WriteFile(configPath, []byte(cfg.configText), 0o644); err != nil {
				t.Fatalf("write config: %v", err)
			}
		}
	} else {
		dockerfilePath, configPath, input = prepareGeminiSmokeWorkspaceFromFixture(t, cfg.fixtureDir, cfg.configText)
	}

	selectArgs, err := selectRules(cfg.selectRuleCodes...)
	if err != nil {
		t.Fatalf("select rules: %v", err)
	}

	args := make([]string, 0, 16+len(selectArgs)+len(cfg.extraLintIgnore)*2)
	args = append(args,
		"lint",
		"--config", configPath,
		"--slow-checks=off",
	)
	for _, ign := range cfg.extraLintIgnore {
		args = append(args, "--ignore", ign)
	}
	args = append(args,
		"--fix",
		"--fix-unsafe",
		"--fix-rule", cfg.fixRule,
	)
	args = append(args, selectArgs...)
	args = append(args, dockerfilePath)

	cmd := exec.Command(binaryPath, args...)
	cmd.Env = append(os.Environ(),
		"GOCOVERDIR="+coverageDir,
	)

	outputBytes, runErr := cmd.CombinedOutput()
	_ = runErr // tally exits non-zero when unresolved violations remain.

	outputStr := strings.ReplaceAll(string(outputBytes), "\r\n", "\n")
	fixed, err := os.ReadFile(dockerfilePath)
	if err != nil {
		t.Fatalf("read fixed Dockerfile: %v", err)
	}
	return dockerfilePath, input, outputStr, fixed
}

func prepareGeminiSmokeWorkspaceFromFixture(t *testing.T, fixtureDir, configText string) (string, string, []byte) {
	t.Helper()

	fixturePath := filepath.Join("testdata", fixtureDir, "Dockerfile")
	input, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("read fixture Dockerfile %q: %v", fixturePath, err)
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
	baseArgs := make([]string, 0, 4+len(extraArgs))
	baseArgs = append(
		baseArgs,
		geminiBin,
		"--experimental-acp",
		"--allowed-mcp-server-names=none",
		"--model=gemini-3-flash-preview",
	)
	return append(baseArgs, extraArgs...)
}

func geminiSmokeConfigMultiStage(agentCmd []string) string {
	return fmt.Sprintf(`[ai]
enabled = true
timeout = "5m"
redact-secrets = false
command = %s

[rules.tally.prefer-multi-stage-build]
fix = "explicit"
`, tomlStringArray(agentCmd))
}

func geminiSmokeConfigBoth(agentCmd []string) string {
	return fmt.Sprintf(`[ai]
enabled = true
timeout = "5m"
redact-secrets = false
command = %s

[rules.tally.prefer-multi-stage-build]
fix = "explicit"

[rules.tally."gpu/prefer-uv-over-conda"]
fix = "explicit"
`, tomlStringArray(agentCmd))
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

// condaMLRegressionPackages is the set of ML packages the gemini smoke test
// considers a migration regression if a conda-family manager still installs
// any of them after the fix has been applied.
var condaMLRegressionPackages = map[string]bool{
	"torch":        true,
	"torchvision":  true,
	"torchaudio":   true,
	"pytorch":      true,
	"pytorch-cuda": true,
	"tensorflow":   true,
	"jax":          true,
	"numpy":        true,
	"scipy":        true,
	"transformers": true,
	"flash-attn":   true,
	"xformers":     true,
}

// uvPresenceRe detects that uv is installed or used anywhere in the file.
var uvPresenceRe = regexp.MustCompile(
	`(?mi)\b(?:uv\s+pip|pip\s+install\s+.*\buv\b|pipx\s+install\s+uv|uv\s+sync|uv\s+add|uv\s+venv)\b`,
)

// assertNoCondaPythonInstall parses the fixed Dockerfile via BuildKit and walks
// every RUN instruction, asserting that no conda-family install command
// targets a known ML package. The shell-AST-based install extractor already
// handles backslash-newline continuations, heredocs, and quoting, which a
// single-line regex would miss.
func assertNoCondaPythonInstall(t *testing.T, label string, fixed []byte) {
	t.Helper()
	parsed, err := dockerfile.Parse(bytes.NewReader(fixed), nil)
	if err != nil {
		t.Fatalf("%s: parse fixed Dockerfile: %v\n%s", label, err, fixed)
	}
	for _, stage := range parsed.Stages {
		for _, cmd := range stage.Commands {
			run, ok := cmd.(*instructions.RunCommand)
			if !ok {
				continue
			}
			script := runScriptText(run)
			if script == "" {
				continue
			}
			for _, ic := range shell.FindInstallPackages(script, shell.VariantBash) {
				if !isCondaFamilyManager(ic.Manager) {
					continue
				}
				for _, pkg := range ic.Packages {
					name := strings.ToLower(strings.TrimSpace(pkg.Normalized))
					if idx := strings.IndexAny(name, "=<>! "); idx > 0 {
						name = name[:idx]
					}
					if _, after, ok := strings.Cut(name, "::"); ok {
						name = after
					}
					if condaMLRegressionPackages[name] {
						t.Fatalf(
							"%s: conda Python install still present after fix (%s install %s):\n%s",
							label, ic.Manager, name, fixed,
						)
					}
				}
			}
		}
	}
}

// isCondaFamilyManager matches the set of install managers recognized as
// conda-family by shell.installManagers.
func isCondaFamilyManager(manager string) bool {
	switch manager {
	case "conda", "mamba", "micromamba":
		return true
	}
	return false
}

// runScriptText returns the RUN script to feed into shell.FindInstallPackages.
// All heredoc bodies are concatenated (a RUN can declare multiple) so a
// conda install hidden inside a later heredoc still gets scanned; shell-form
// RUN args fall back to joining CmdLine with spaces so the shell AST parser
// can process them uniformly.
func runScriptText(run *instructions.RunCommand) string {
	if run == nil {
		return ""
	}
	if len(run.Files) > 0 {
		parts := make([]string, 0, len(run.Files))
		for _, f := range run.Files {
			parts = append(parts, f.Data)
		}
		return strings.Join(parts, "\n")
	}
	return strings.Join(run.CmdLine, " ")
}

func assertHasUV(t *testing.T, label string, fixed []byte) {
	t.Helper()
	if !uvPresenceRe.Match(fixed) {
		t.Fatalf("%s: uv not present in fixed Dockerfile:\n%s", label, fixed)
	}
}

// assertPreservedRuntime parses the fixed Dockerfile and verifies that the
// final stage still carries the original CMD and WORKDIR. A byte-level
// substring check would falsely accept a rewrite that kept CMD/WORKDIR in a
// builder stage while dropping or mutating them in the runtime stage.
func assertPreservedRuntime(t *testing.T, label string, fixed []byte) {
	t.Helper()
	parsed, err := dockerfile.Parse(bytes.NewReader(fixed), nil)
	if err != nil {
		t.Fatalf("%s: parse fixed Dockerfile: %v\n%s", label, err, fixed)
	}
	if len(parsed.Stages) == 0 {
		t.Fatalf("%s: fixed Dockerfile has no stages:\n%s", label, fixed)
	}

	rt := autofixdata.ExtractFinalStageRuntime(parsed)
	wantCmdLine := []string{"python", "-m", "app"}
	if rt.Cmd == nil || !slices.Equal(rt.Cmd.CmdLine, wantCmdLine) {
		t.Fatalf("%s: final-stage CMD not preserved (want %v):\n%s", label, wantCmdLine, fixed)
	}
	if rt.Workdir == nil || strings.TrimSpace(rt.Workdir.Path) != "/app" {
		t.Fatalf("%s: final-stage WORKDIR not preserved (want /app):\n%s", label, fixed)
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
