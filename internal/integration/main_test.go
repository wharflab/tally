package integration

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/wharflab/tally/internal/registry/testutil"
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

	// The two `go build` invocations and the in-process mock registry setup
	// are independent, so run them concurrently. Each step takes ~0.5–1.5s on
	// a warm cache; serial execution adds 1.5–3s of latency before any test
	// can start.
	var (
		wg              sync.WaitGroup
		binErr, acpErr  error
		registryErr     error
		registryTmpConf string
	)
	wg.Add(3)
	go func() {
		defer wg.Done()
		binErr = buildIntegrationBinary(tmpDir)
	}()
	go func() {
		defer wg.Done()
		acpErr = buildIntegrationAcpAgent(tmpDir)
	}()
	go func() {
		defer wg.Done()
		registryTmpConf, registryErr = prepareMockRegistry(tmpDir)
	}()
	wg.Wait()

	if binErr != nil {
		return 0, binErr
	}
	if acpErr != nil {
		return 0, acpErr
	}
	if registryErr != nil {
		return 0, registryErr
	}
	if err := finalizeMockRegistry(registryTmpConf); err != nil {
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
	extraArgs := []string{
		"-tags", "containers_image_openpgp,containers_image_storage_stub,containers_image_docker_daemon_stub",
	}
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
		extraArgs = append(extraArgs, "-cover", "-covermode=atomic")
	}

	if err := installCachedBinary("github.com/wharflab/tally", binaryName, binaryPath, extraArgs); err != nil {
		return fmt.Errorf("build integration binary: %w", err)
	}
	return nil
}

func buildIntegrationAcpAgent(tmpDir string) error {
	binName := "tally-acp-testagent"
	if runtime.GOOS == "windows" {
		binName += ".exe"
	}
	acpAgentPath = filepath.Join(tmpDir, binName)

	// `go install`'s output binary name is derived from the import path's last
	// segment ("testagent"), not the destination filename, so look up the
	// cached binary by its install-time name.
	installedName := "testagent"
	if runtime.GOOS == "windows" {
		installedName += ".exe"
	}
	if err := installCachedBinary(
		"github.com/wharflab/tally/internal/ai/acp/testdata/testagent",
		installedName, acpAgentPath, []string{"-trimpath"},
	); err != nil {
		return fmt.Errorf("build ACP test agent: %w", err)
	}
	return nil
}

// installCachedBinary builds importPath via `go install` to a stable per-user
// cache directory (so Go's link cache hits across `go test` runs), then copies
// the resulting binary to dst. With `go build -o /tmp/unique-path`, Go always
// re-links because the output path is part of the action ID; `go install` to
// a fixed GOBIN reuses the linked binary on subsequent runs (~1.5s vs ~7s for
// the main tally binary on this codebase).
//
// extraArgs are forwarded verbatim to `go install` (e.g. `-tags`, `-cover`).
func installCachedBinary(importPath, binName, dst string, extraArgs []string) error {
	cacheDir, err := integrationBinaryCacheDir()
	if err != nil {
		return err
	}

	args := append([]string{"install"}, extraArgs...)
	args = append(args, importPath)
	cmd := exec.Command("go", args...)
	cmd.Env = append(os.Environ(), "GOEXPERIMENT=jsonv2", "GOBIN="+cacheDir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("go install %s: %w (output: %s)", importPath, err, out)
	}

	src := filepath.Join(cacheDir, binName)
	if err := copyFileExclusive(src, dst); err != nil {
		return fmt.Errorf("copy %s to %s: %w", src, dst, err)
	}
	return nil
}

// integrationBinaryCacheDir returns the per-user directory where this package
// caches integration-test binaries. It's outside of the test's tmpDir so the
// cache survives between `go test` runs and Go's link cache can hit.
func integrationBinaryCacheDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		// Fall back to /tmp; UserCacheDir only fails when HOME is unset.
		base = os.TempDir()
	}
	dir := filepath.Join(base, "tally-integration-test-bin")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", fmt.Errorf("create binary cache dir %q: %w", dir, err)
	}
	return dir, nil
}

// copyFileExclusive copies src to dst with owner-only exec permissions.
// The destination is overwritten if it already exists. The binary is meant
// to be invoked by this user only, from this user's tmpDir.
func copyFileExclusive(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o755)
}

// prepareMockRegistry does all the work that doesn't have to mutate process
// state: starting the mock server, building/pushing the test images, and
// writing registries.conf. It returns the path to that conf file so
// finalizeMockRegistry can publish it via os.Setenv.
//
// This split lets TestMain run the heavy image builds concurrently with the
// `go build` invocations. Image pushes run concurrently against the
// in-memory registry; AddImage / AddIndex are independent calls and the
// registry handler is goroutine-safe.
func prepareMockRegistry(tmpDir string) (string, error) {
	mockRegistry = testutil.New()

	type imageJob struct {
		label string
		run   func() error
	}
	// 3 fixed jobs + one per delayed-image repo (currently 2).
	jobs := make([]imageJob, 0, 5)
	jobs = append(jobs,
		imageJob{
			// python:3.12 as single-platform linux/arm64 only — used for platform mismatch tests.
			label: "add python:3.12 image",
			run: func() error {
				_, err := mockRegistry.AddImage(testutil.ImageOpts{
					Repo: "library/python",
					Tag:  "3.12",
					OS:   "linux",
					Arch: "arm64",
					Env: map[string]string{
						"PATH":           "/usr/local/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
						"PYTHON_VERSION": "3.12.0",
						"LANG":           "C.UTF-8",
					},
				})
				return err
			},
		},
		imageJob{
			// multiarch:latest — a multi-arch manifest index with linux/amd64 and linux/arm64.
			// Used to test the collectAvailablePlatforms path when a requested platform
			// (e.g., linux/s390x) is not in the index.
			label: "add multiarch:latest index",
			run: func() error {
				_, err := mockRegistry.AddIndex("library/multiarch", "latest", []testutil.ImageOpts{
					{Repo: "library/multiarch", Tag: "latest", OS: "linux", Arch: "amd64",
						Env: map[string]string{"PATH": "/usr/local/bin:/usr/bin:/bin"}},
					{Repo: "library/multiarch", Tag: "latest", OS: "linux", Arch: "arm64",
						Env: map[string]string{"PATH": "/usr/local/bin:/usr/bin:/bin"}},
				})
				return err
			},
		},
		imageJob{
			// withhealthcheck:latest — image with HEALTHCHECK CMD. Used for DL3057 async tests.
			label: "add withhealthcheck:latest image",
			run: func() error {
				_, err := mockRegistry.AddImage(testutil.ImageOpts{
					Repo:        "library/withhealthcheck",
					Tag:         "latest",
					OS:          "linux",
					Arch:        "arm64",
					Env:         map[string]string{"PATH": "/usr/local/bin:/usr/bin:/bin"},
					Healthcheck: []string{"CMD-SHELL", "curl -f http://localhost/ || exit 1"},
				})
				return err
			},
		},
	)
	// Delayed images — each has a 30-second artificial delay.
	// Separate repos prevent parallel tests from interfering with each other's
	// request assertions (e.g. fail-fast asserting "no requests for this repo").
	for _, repo := range []string{"library/slowfailfast", "library/slowtimeout"} {
		jobs = append(jobs, imageJob{
			label: fmt.Sprintf("add %s:latest image", repo),
			run: func() error {
				_, err := mockRegistry.AddImage(testutil.ImageOpts{
					Repo: repo,
					Tag:  "latest",
					OS:   "linux",
					Arch: "arm64",
					Env:  map[string]string{"PATH": "/usr/local/bin:/usr/bin:/bin"},
				})
				if err != nil {
					return err
				}
				mockRegistry.SetDelay(repo, 30*time.Second)
				return nil
			},
		})
	}

	var wg sync.WaitGroup
	errs := make([]error, len(jobs))
	for i, job := range jobs {
		wg.Add(1)
		go func(i int, job imageJob) {
			defer wg.Done()
			if err := job.run(); err != nil {
				errs[i] = fmt.Errorf("%s: %w", job.label, err)
			}
		}(i, job)
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			mockRegistry.Close()
			return "", err
		}
	}

	// Write registries.conf redirecting docker.io to the mock server. The
	// caller is responsible for publishing it via os.Setenv (see
	// finalizeMockRegistry) so multiple goroutines don't race on process
	// state during TestMain setup.
	confPath, err := mockRegistry.WriteRegistriesConf(tmpDir, "docker.io")
	if err != nil {
		mockRegistry.Close()
		return "", fmt.Errorf("create registries.conf: %w", err)
	}
	return confPath, nil
}

// finalizeMockRegistry publishes the env vars that point tally at the mock
// registry and resets the recorded requests so only test-time pulls show up.
// Must run on the goroutine that started TestMain (after concurrent setup
// completes) because os.Setenv mutates process-wide state.
func finalizeMockRegistry(confPath string) error {
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
