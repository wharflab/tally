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
		manifestKey, manifestValue, _ := parseManifestLine(scanner.Text())
		if manifestKey == key {
			return manifestValue, true
		}
	}
	return "", false
}

func parseManifestLine(line string) (string, string, bool) {
	if after, ok := strings.CutPrefix(line, " "); ok {
		key, value, ok := strings.Cut(after, " ")
		if !ok {
			key = unescapeManifestPath(key)
			return key, key, false
		}
		return unescapeManifestPath(key), unescapeManifestPath(value), true
	}
	key, value, ok := strings.Cut(line, " ")
	if !ok {
		return key, key, false
	}
	return key, value, true
}

func unescapeManifestPath(path string) string {
	var b strings.Builder
	b.Grow(len(path))
	escaped := false
	for _, r := range path {
		if escaped {
			switch r {
			case 's':
				b.WriteRune(' ')
			case 'n':
				b.WriteRune('\n')
			case 'b':
				b.WriteRune('\\')
			default:
				b.WriteRune('\\')
				b.WriteRune(r)
			}
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			continue
		}
		b.WriteRune(r)
	}
	if escaped {
		b.WriteRune('\\')
	}
	return b.String()
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func TestResolveFromManifestParsesEscapedEntries(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	manifestPath := filepath.Join(tmpDir, "MANIFEST")
	binaryPath := filepath.Join(tmpDir, "go binary")
	manifestLine := " tally_go_sdk/bin/go " + escapeManifestPath(binaryPath) + "\n"
	if err := os.WriteFile(manifestPath, []byte(manifestLine), 0o600); err != nil {
		t.Fatal(err)
	}

	got, ok := resolveFromManifest(manifestPath, "tally_go_sdk/bin/go")
	if !ok {
		t.Fatal("expected manifest entry")
	}
	if got != binaryPath {
		t.Fatalf("manifest value mismatch: %q", got)
	}
}

func escapeManifestPath(path string) string {
	var b []rune
	for _, r := range path {
		switch r {
		case ' ':
			b = append(b, '\\', 's')
		case '\n':
			b = append(b, '\\', 'n')
		case '\\':
			b = append(b, '\\', 'b')
		default:
			b = append(b, r)
		}
	}
	return string(b)
}
