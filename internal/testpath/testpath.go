package testpath

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	pathpkg "path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
)

var manifestCache struct {
	once sync.Once
	data map[string]string
	err  error
}

// Resolve returns the filesystem path for a test data path. It supports both
// ordinary relative paths and Bazel's Windows runfiles manifest.
func Resolve(name string) string {
	if existing(name) {
		return name
	}
	if resolved, ok := resolveRunfile(name); ok {
		return resolved
	}
	return name
}

func ReadFile(name string) ([]byte, error) {
	return os.ReadFile(Resolve(name))
}

func CopyTree(src, dst string) error {
	resolved := Resolve(src)
	if info, err := os.Stat(resolved); err == nil {
		return copyResolvedTree(resolved, src, dst, info)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return copyManifestTree(src, dst)
}

func copyResolvedTree(resolved, src, dst string, info fs.FileInfo) error {
	if !info.IsDir() {
		return copyFile(resolved, filepath.Join(dst, filepath.Base(src)), info.Mode().Perm())
	}
	return filepath.WalkDir(resolved, func(filePath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(resolved, filePath)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o750)
		}
		info, err := os.Stat(filePath)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 {
			return nil
		}
		// Symlinks are intentionally dereferenced by copyFile, matching Bazel's
		// runfiles-tree behavior for these test workspaces.
		return copyFile(filePath, target, info.Mode().Perm())
	})
}

func copyManifestTree(src, dst string) error {
	manifest, err := runfilesManifest()
	if err != nil {
		return err
	}
	for _, prefix := range candidateKeys(src) {
		prefix = strings.TrimSuffix(prefix, "/")
		if prefix == "" {
			continue
		}
		copied := false
		for key, value := range manifest {
			if key != prefix && !strings.HasPrefix(key, prefix+"/") {
				continue
			}
			rel := strings.TrimPrefix(key, prefix)
			rel = strings.TrimPrefix(rel, "/")
			if rel == "" {
				rel = filepath.Base(src)
			}
			info, err := os.Stat(value)
			if err != nil {
				return err
			}
			if info.IsDir() {
				continue
			}
			if err := copyFile(value, filepath.Join(dst, filepath.FromSlash(rel)), info.Mode().Perm()); err != nil {
				return err
			}
			copied = true
		}
		if copied {
			return nil
		}
	}
	return fmt.Errorf(
		"runfiles tree %q not found in manifest; tried %v against %d manifest entries (sample keys: %v)",
		src,
		candidateKeys(src),
		len(manifest),
		sampleManifestKeys(manifest, 5),
	)
}

func copyFile(src, dst string, perm fs.FileMode) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		return err
	}
	//nolint:gosec // Test helper copies only Bazel-declared runfiles or local test data.
	return os.WriteFile(dst, data, perm)
}

func resolveRunfile(name string) (string, bool) {
	for _, dir := range runfilesDirs() {
		for _, key := range candidateKeys(name) {
			candidate := filepath.Join(dir, filepath.FromSlash(key))
			if existing(candidate) {
				return candidate, true
			}
		}
	}

	manifest, err := runfilesManifest()
	if err != nil {
		return "", false
	}
	for _, key := range candidateKeys(name) {
		if value, ok := manifest[key]; ok && existing(value) {
			return value, true
		}
	}
	return "", false
}

func runfilesDirs() []string {
	var out []string
	for _, dir := range []string{os.Getenv("RUNFILES_DIR"), os.Getenv("TEST_SRCDIR")} {
		if dir == "" {
			continue
		}
		if !slices.Contains(out, dir) {
			out = append(out, dir)
		}
	}
	return out
}

func runfilesManifest() (map[string]string, error) {
	manifestCache.once.Do(func() {
		manifestCache.data = make(map[string]string)
		manifestPath := os.Getenv("RUNFILES_MANIFEST_FILE")
		if manifestPath == "" {
			return
		}
		//nolint:gosec // Bazel owns RUNFILES_MANIFEST_FILE in tests.
		file, err := os.Open(manifestPath)
		if err != nil {
			manifestCache.err = err
			return
		}
		defer file.Close()

		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			line := scanner.Text()
			key, value, ok := parseManifestLine(line)
			if !ok {
				manifestCache.data[key] = value
				continue
			}
			manifestCache.data[key] = value
		}
		manifestCache.err = scanner.Err()
	})
	return manifestCache.data, manifestCache.err
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

func sampleManifestKeys(manifest map[string]string, limit int) []string {
	if limit <= 0 || len(manifest) == 0 {
		return nil
	}
	keys := make([]string, 0, len(manifest))
	for key := range manifest {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	if len(keys) > limit {
		keys = keys[:limit]
	}
	return keys
}

func candidateKeys(name string) []string {
	seen := make(map[string]struct{})
	keys := make([]string, 0, 12)
	add := func(key string) {
		key = strings.TrimPrefix(key, "./")
		key = strings.TrimSuffix(key, "/")
		if key == "." || key == "" {
			return
		}
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		keys = append(keys, key)
	}

	slash := filepath.ToSlash(name)
	add(slash)
	add(pathpkg.Clean(slash))

	trimmed := slash
	for strings.HasPrefix(trimmed, "../") {
		trimmed = strings.TrimPrefix(trimmed, "../")
		add(trimmed)
		add(pathpkg.Clean(trimmed))
	}

	if pkg := testPackage(); pkg != "" {
		add(pathpkg.Clean(pkg + "/" + slash))
	}

	base := append([]string(nil), keys...)
	for _, workspace := range workspaces() {
		for _, key := range base {
			if strings.HasPrefix(key, workspace+"/") {
				continue
			}
			add(workspace + "/" + key)
		}
	}
	return keys
}

func testPackage() string {
	target := os.Getenv("TEST_TARGET")
	target = strings.TrimPrefix(target, "@@")
	target = strings.TrimPrefix(target, "@")
	if idx := strings.Index(target, "//"); idx >= 0 {
		target = target[idx+2:]
	}
	pkg, _, _ := strings.Cut(target, ":")
	return strings.Trim(pkg, "/")
}

func workspaces() []string {
	var out []string
	for _, name := range []string{os.Getenv("TEST_WORKSPACE"), "_main", "__main__"} {
		if name == "" {
			continue
		}
		if !slices.Contains(out, name) {
			out = append(out, name)
		}
	}
	return out
}

func existing(name string) bool {
	_, err := os.Stat(name)
	return err == nil
}
