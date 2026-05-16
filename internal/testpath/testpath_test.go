package testpath

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestRunfilesManifestParsesEscapedAndLineOnlyEntries(t *testing.T) {
	resetManifestCache()
	t.Cleanup(resetManifestCache)

	manifestPath := filepath.Join(t.TempDir(), "MANIFEST")
	if err := os.WriteFile(
		manifestPath,
		[]byte(" dir\\swith\\sspaces /tmp/target\\swith\\sspaces\nline-only-entry\n h/\\n\\bi /tmp/\\snewline\\nbackslash\\b\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RUNFILES_MANIFEST_FILE", manifestPath)

	manifest, err := runfilesManifest()
	if err != nil {
		t.Fatal(err)
	}

	if got := manifest["dir with spaces"]; got != "/tmp/target with spaces" {
		t.Fatalf("escaped key/value mismatch: %q", got)
	}
	if got := manifest["line-only-entry"]; got != "line-only-entry" {
		t.Fatalf("line-only entry mismatch: %q", got)
	}
	if got := manifest["h/\n\\i"]; got != "/tmp/ newline\nbackslash\\" {
		t.Fatalf("fully escaped entry mismatch: %q", got)
	}
}

func TestRunfilesManifestPreservesUnescapedWindowsPaths(t *testing.T) {
	t.Parallel()

	key, value, ok := parseManifestLine(`workspace/file C:\tmp\source.txt`)
	if !ok {
		t.Fatal("expected key/value manifest line")
	}
	if key != "workspace/file" {
		t.Fatalf("key mismatch: %q", key)
	}
	if value != `C:\tmp\source.txt` {
		t.Fatalf("value mismatch: %q", value)
	}
}

func TestCopyTreeCopiesManifestPrefixWithEscapedKey(t *testing.T) {
	resetManifestCache()
	t.Cleanup(resetManifestCache)

	tmpDir := t.TempDir()
	src := filepath.Join(tmpDir, "source file.txt")
	if err := os.WriteFile(src, []byte("content"), 0o600); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(tmpDir, "MANIFEST")
	manifestLine := " dir\\swith\\sspaces/nested/file.txt " + escapeManifestPath(src) + "\n"
	if err := os.WriteFile(manifestPath, []byte(manifestLine), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RUNFILES_MANIFEST_FILE", manifestPath)
	t.Setenv("TEST_WORKSPACE", "")
	t.Setenv("TEST_TARGET", "")

	dst := filepath.Join(tmpDir, "dst")
	if err := CopyTree("dir with spaces", dst); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dst, "nested", "file.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "content" {
		t.Fatalf("copied content mismatch: %q", data)
	}
}

func resetManifestCache() {
	manifestCache.once = sync.Once{}
	manifestCache.data = nil
	manifestCache.err = nil
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
