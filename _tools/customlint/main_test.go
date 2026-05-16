package customlint

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	_ = os.Unsetenv("GOEXPERIMENT")
	prependConfiguredGoToPath()
	os.Exit(m.Run())
}

func prependConfiguredGoToPath() {
	goBinary := os.Getenv("TALLY_GO_BINARY")
	if goBinary == "" {
		return
	}
	goBinary = resolveRunfile(goBinary)
	if _, err := os.Stat(goBinary); err != nil {
		return
	}
	wrapperDir, err := os.MkdirTemp("", "customlint-go-wrapper")
	if err == nil {
		_ = os.Setenv("TALLY_REAL_GO_BINARY", goBinary)
		wrapper := filepath.Join(wrapperDir, "go")
		const script = `#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "env" && "${2:-}" == "GOROOT" ]]; then
  printf 'GOROOT\n'
  exit 0
fi
exec "${TALLY_REAL_GO_BINARY}" "$@"
`
		if writeErr := os.WriteFile(wrapper, []byte(script), 0o700); writeErr == nil {
			goBinary = wrapper
		}
	}
	path := filepath.Dir(goBinary)
	if existing := os.Getenv("PATH"); existing != "" {
		path += string(os.PathListSeparator) + existing
	}
	_ = os.Setenv("PATH", path)
}

func resolveRunfile(path string) string {
	if filepath.IsAbs(path) || exists(path) {
		return path
	}
	if manifest := os.Getenv("RUNFILES_MANIFEST_FILE"); manifest != "" {
		if resolved, ok := resolveFromManifest(manifest, path); ok {
			return resolved
		}
	}
	for _, dir := range []string{os.Getenv("RUNFILES_DIR"), os.Getenv("TEST_SRCDIR")} {
		if dir == "" {
			continue
		}
		candidate := filepath.Join(dir, filepath.FromSlash(path))
		if exists(candidate) {
			return candidate
		}
	}
	return path
}

func resolveFromManifest(manifestPath, key string) (string, bool) {
	file, err := os.Open(manifestPath)
	if err != nil {
		return "", false
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		manifestKey, manifestValue, ok := strings.Cut(line, " ")
		if !ok {
			manifestValue = manifestKey
		}
		if manifestKey == key {
			return manifestValue, true
		}
	}
	return "", false
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
