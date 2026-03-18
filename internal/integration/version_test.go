package integration

import (
	"encoding/json/v2"
	"os"
	"os/exec"
	"testing"

	"github.com/wharflab/tally/internal/version"
)

func TestVersion(t *testing.T) {
	t.Parallel()
	cmd := exec.Command(binaryPath, "version")
	cmd.Env = append(os.Environ(),
		"GOCOVERDIR="+coverageDir,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("version command failed: %v\noutput: %s", err, output)
	}

	// Version output contains "dev" in tests
	if len(output) == 0 {
		t.Error("expected version output, got empty")
	}
}

func TestVersionJSON(t *testing.T) {
	t.Parallel()
	cmd := exec.Command(binaryPath, "version", "--json")
	cmd.Env = append(os.Environ(),
		"GOCOVERDIR="+coverageDir,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("version --json failed: %v\noutput: %s", err, output)
	}

	var info version.Info
	if err := json.Unmarshal(output, &info); err != nil {
		t.Fatalf("failed to parse version JSON: %v\noutput: %s", err, output)
	}

	if info.Version == "" {
		t.Error("expected non-empty version")
	}
	if info.Platform.OS == "" {
		t.Error("expected non-empty platform.os")
	}
	if info.Platform.Arch == "" {
		t.Error("expected non-empty platform.arch")
	}
	if info.GoVersion == "" {
		t.Error("expected non-empty goVersion")
	}

	// CGO info should always be present.
	// When built with CGO_ENABLED=1 (the default), cCompiler should be populated.
	t.Logf("cgo.enabled=%v cgo.cCompiler=%q cgo.glibcVersion=%q",
		info.CGO.Enabled, info.CGO.CCompiler, info.CGO.GlibcVersion)

	// shellcheckVersion may be empty if the WASM module was not rebuilt
	// with the sc_version export. Log it for visibility.
	t.Logf("shellcheckVersion=%q", info.ShellcheckVersion)
}
