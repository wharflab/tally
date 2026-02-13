package integration

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/tinovyatkin/tally/internal/registry/testutil"
)

var (
	binaryPath   string
	coverageDir  string
	mockRegistry *testutil.MockRegistry
	acpAgentPath string
)

var errNoRulesSelected = errors.New("selectRules requires at least one rule")

func TestMain(m *testing.M) {
	code, err := runIntegrationTestMain(m)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "integration test setup failed: %v\n", err)
		os.Exit(1)
	}
	os.Exit(code)
}

func runIntegrationTestMain(m *testing.M) (int, error) {
	// Build the binary once before running tests.
	tmpDir, err := os.MkdirTemp("", "tally-test")
	if err != nil {
		return 0, fmt.Errorf("create temporary directory: %w", err)
	}
	defer func() {
		_ = os.RemoveAll(tmpDir)
	}()

	if err := buildIntegrationBinary(tmpDir); err != nil {
		return 0, err
	}

	if err := buildIntegrationAcpAgent(tmpDir); err != nil {
		return 0, err
	}

	if err := setupMockRegistry(tmpDir); err != nil {
		return 0, err
	}

	code := m.Run()
	if mockRegistry != nil {
		mockRegistry.Close()
	}
	return code, nil
}

func buildIntegrationBinary(tmpDir string) error {
	binaryName := "tally"
	if runtime.GOOS == "windows" {
		binaryName = "tally.exe"
	}
	binaryPath = filepath.Join(tmpDir, binaryName)

	// Collect coverage only when GOCOVERDIR is set (Linux CI).
	// Windows CI doesn't upload coverage, so skip instrumentation to avoid
	// concurrent writes to the coverage directory from parallel subtests.
	buildArgs := []string{"build"}
	coverageDir = os.Getenv("GOCOVERDIR")
	if coverageDir != "" {
		absCoverageDir, err := filepath.Abs(coverageDir)
		if err != nil {
			return fmt.Errorf("get absolute coverage directory path: %w", err)
		}
		coverageDir = absCoverageDir
		if err := os.MkdirAll(coverageDir, 0o750); err != nil {
			return fmt.Errorf("create coverage directory %q: %w", coverageDir, err)
		}
		buildArgs = append(buildArgs, "-cover")
	}
	buildArgs = append(buildArgs,
		"-tags", "containers_image_openpgp,containers_image_storage_stub,containers_image_docker_daemon_stub",
		"-o", binaryPath, "github.com/tinovyatkin/tally")

	cmd := exec.Command("go", buildArgs...)
	cmd.Env = append(os.Environ(), "GOEXPERIMENT=jsonv2")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("build integration binary: %w (output: %s)", err, out)
	}
	return nil
}

func buildIntegrationAcpAgent(tmpDir string) error {
	binName := "tally-acp-testagent"
	if runtime.GOOS == "windows" {
		binName += ".exe"
	}
	acpAgentPath = filepath.Join(tmpDir, binName)

	cmd := exec.Command("go", "build", "-trimpath", "-o", acpAgentPath, "github.com/tinovyatkin/tally/internal/ai/acp/testdata/testagent")
	cmd.Env = append(os.Environ(), "GOEXPERIMENT=jsonv2")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("build ACP test agent: %w (output: %s)", err, out)
	}
	return nil
}

// setupMockRegistry starts the mock OCI registry, populates it with test
// images, writes registries.conf, and sets environment variables.
func setupMockRegistry(tmpDir string) error {
	mockRegistry = testutil.New()

	// python:3.12 as single-platform linux/arm64 only — used for platform mismatch tests.
	if _, err := mockRegistry.AddImage(testutil.ImageOpts{
		Repo: "library/python",
		Tag:  "3.12",
		OS:   "linux",
		Arch: "arm64",
		Env: map[string]string{
			"PATH":           "/usr/local/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
			"PYTHON_VERSION": "3.12.0",
			"LANG":           "C.UTF-8",
		},
	}); err != nil {
		mockRegistry.Close()
		return fmt.Errorf("add python:3.12 image: %w", err)
	}

	// multiarch:latest — a multi-arch manifest index with linux/amd64 and linux/arm64.
	// Used to test the collectAvailablePlatforms path when a requested platform
	// (e.g., linux/s390x) is not in the index.
	if _, err := mockRegistry.AddIndex("library/multiarch", "latest", []testutil.ImageOpts{
		{Repo: "library/multiarch", Tag: "latest", OS: "linux", Arch: "amd64",
			Env: map[string]string{"PATH": "/usr/local/bin:/usr/bin:/bin"}},
		{Repo: "library/multiarch", Tag: "latest", OS: "linux", Arch: "arm64",
			Env: map[string]string{"PATH": "/usr/local/bin:/usr/bin:/bin"}},
	}); err != nil {
		mockRegistry.Close()
		return fmt.Errorf("add multiarch:latest index: %w", err)
	}

	// withhealthcheck:latest — image with HEALTHCHECK CMD. Used for DL3057 async tests.
	if _, err := mockRegistry.AddImage(testutil.ImageOpts{
		Repo:        "library/withhealthcheck",
		Tag:         "latest",
		OS:          "linux",
		Arch:        "arm64",
		Env:         map[string]string{"PATH": "/usr/local/bin:/usr/bin:/bin"},
		Healthcheck: []string{"CMD-SHELL", "curl -f http://localhost/ || exit 1"},
	}); err != nil {
		mockRegistry.Close()
		return fmt.Errorf("add withhealthcheck:latest image: %w", err)
	}

	// Delayed images — each has a 30-second artificial delay.
	// Separate repos prevent parallel tests from interfering with each other's
	// request assertions (e.g. fail-fast asserting "no requests for this repo").
	for _, repo := range []string{"library/slowfailfast", "library/slowtimeout"} {
		if _, err := mockRegistry.AddImage(testutil.ImageOpts{
			Repo: repo,
			Tag:  "latest",
			OS:   "linux",
			Arch: "arm64",
			Env:  map[string]string{"PATH": "/usr/local/bin:/usr/bin:/bin"},
		}); err != nil {
			mockRegistry.Close()
			return fmt.Errorf("add %s:latest image: %w", repo, err)
		}
		mockRegistry.SetDelay(repo, 30*time.Second)
	}

	// Write registries.conf redirecting docker.io to the mock server.
	confPath, err := mockRegistry.WriteRegistriesConf(tmpDir, "docker.io")
	if err != nil {
		mockRegistry.Close()
		return fmt.Errorf("create registries.conf: %w", err)
	}
	if err := os.Setenv("CONTAINERS_REGISTRIES_CONF", confPath); err != nil {
		mockRegistry.Close()
		return fmt.Errorf("set CONTAINERS_REGISTRIES_CONF: %w", err)
	}
	// Set default platform to match the mock registry's image platform (linux/arm64).
	if err := os.Setenv("DOCKER_DEFAULT_PLATFORM", "linux/arm64"); err != nil {
		mockRegistry.Close()
		return fmt.Errorf("set DOCKER_DEFAULT_PLATFORM: %w", err)
	}

	// Clear setup requests (image pushes) so only test-time requests are tracked.
	mockRegistry.ResetRequests()
	return nil
}

// selectRules returns args to disable all rules except the specified ones.
// This isolates tests from global rule count changes.
func selectRules(rules ...string) ([]string, error) {
	if len(rules) == 0 {
		return nil, fmt.Errorf("select rules: %w", errNoRulesSelected)
	}
	args := make([]string, 0, 2+2*len(rules))
	args = append(args, "--ignore", "*")
	for _, r := range rules {
		args = append(args, "--select", r)
	}
	return args, nil
}
