package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// benchBinaryPath holds the path to the built binary for benchmarks.
// It's initialized once via TestMain (which runs before benchmarks).
var benchBinaryPath string

func init() {
	// Use the same binary as integration tests if already built
	if binaryPath != "" {
		benchBinaryPath = binaryPath
	}
}

// ensureBinary builds the binary if not already built by TestMain.
func ensureBinary(b *testing.B) {
	b.Helper()
	if benchBinaryPath != "" {
		return
	}

	// Build the binary (this happens when running benchmarks without tests)
	tmpDir := b.TempDir()

	binaryName := "tally"
	if runtime.GOOS == "windows" {
		binaryName = "tally.exe"
	}
	benchBinaryPath = filepath.Join(tmpDir, binaryName)

	cmd := exec.Command("go", "build", "-o", benchBinaryPath, "github.com/tinovyatkin/tally")
	cmd.Env = append(os.Environ(), "GOEXPERIMENT=jsonv2")
	if out, err := cmd.CombinedOutput(); err != nil {
		b.Fatalf("failed to build binary: %s", out)
	}
}

// BenchmarkDiscovery measures the performance of discovering and linting
// all Dockerfiles in the integration testdata directory.
// This exercises file I/O, directory traversal, and the full linting pipeline.
func BenchmarkDiscovery(b *testing.B) {
	ensureBinary(b)

	absTestdataDir, err := filepath.Abs("testdata")
	if err != nil {
		b.Fatal(err)
	}

	// Verify the directory exists
	if _, err := os.Stat(absTestdataDir); os.IsNotExist(err) {
		b.Fatalf("testdata directory does not exist: %s", absTestdataDir)
	}

	b.ResetTimer()
	for b.Loop() {
		cmd := exec.Command(benchBinaryPath, "lint", "--format", "json", absTestdataDir)
		// Suppress output, we only care about timing
		cmd.Stdout = nil
		cmd.Stderr = nil
		// Ignore exit code - some test fixtures have intentional violations
		_ = cmd.Run() //nolint:errcheck // intentionally ignoring exit code
	}
}

// BenchmarkComplexAria measures performance on a complex, real-world Dockerfile.
// This Dockerfile features multi-stage builds, heredocs, and many RUN commands.
func BenchmarkComplexAria(b *testing.B) {
	ensureBinary(b)

	dockerfile := filepath.Join("testdata", "bench-complex-aria", "Dockerfile")
	absPath, err := filepath.Abs(dockerfile)
	if err != nil {
		b.Fatal(err)
	}

	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		b.Fatalf("benchmark fixture does not exist: %s", absPath)
	}

	b.ResetTimer()
	for b.Loop() {
		cmd := exec.Command(benchBinaryPath, "lint", "--format", "json", absPath)
		cmd.Stdout = nil
		cmd.Stderr = nil
		_ = cmd.Run() //nolint:errcheck // intentionally ignoring exit code
	}
}

// BenchmarkComplexNolus measures performance on a complex multi-stage Containerfile.
// This file has many stages, ARG/ENV declarations, and advanced BuildKit features.
func BenchmarkComplexNolus(b *testing.B) {
	ensureBinary(b)

	containerfile := filepath.Join("testdata", "bench-complex-nolus", "Containerfile")
	absPath, err := filepath.Abs(containerfile)
	if err != nil {
		b.Fatal(err)
	}

	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		b.Fatalf("benchmark fixture does not exist: %s", absPath)
	}

	b.ResetTimer()
	for b.Loop() {
		cmd := exec.Command(benchBinaryPath, "lint", "--format", "json", absPath)
		cmd.Stdout = nil
		cmd.Stderr = nil
		_ = cmd.Run() //nolint:errcheck // intentionally ignoring exit code
	}
}

// BenchmarkRealWorldFix measures performance on a real-world Dockerfile with many
// linting violations and auto-fix opportunities. This exercises the full linting
// pipeline including fix generation for DL3003, DL3027, and other hadolint rules.
func BenchmarkRealWorldFix(b *testing.B) {
	ensureBinary(b)

	dockerfile := filepath.Join("testdata", "benchmark-real-world-fix", "Dockerfile")
	absPath, err := filepath.Abs(dockerfile)
	if err != nil {
		b.Fatal(err)
	}

	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		b.Fatalf("benchmark fixture does not exist: %s", absPath)
	}

	b.ResetTimer()
	for b.Loop() {
		cmd := exec.Command(benchBinaryPath, "lint", "--format", "json", absPath)
		cmd.Stdout = nil
		cmd.Stderr = nil
		_ = cmd.Run() //nolint:errcheck // intentionally ignoring exit code
	}
}
