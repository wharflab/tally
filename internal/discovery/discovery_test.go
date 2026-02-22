package discovery

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultPatterns(t *testing.T) {
	t.Parallel()
	patterns := DefaultPatterns()
	if len(patterns) == 0 {
		t.Fatal("DefaultPatterns() returned empty slice")
	}

	// Check for essential patterns
	expected := map[string]bool{
		"Dockerfile":   false,
		"Dockerfile.*": false,
		"*.Dockerfile": false,
	}

	for _, p := range patterns {
		if _, ok := expected[p]; ok {
			expected[p] = true
		}
	}

	for p, found := range expected {
		if !found {
			t.Errorf("DefaultPatterns() missing expected pattern %q", p)
		}
	}
}

func TestDiscoverFile(t *testing.T) {
	t.Parallel()
	// Create a temporary directory with a Dockerfile
	tmpDir := t.TempDir()
	dockerfilePath := filepath.Join(tmpDir, "Dockerfile")
	if err := os.WriteFile(dockerfilePath, []byte("FROM alpine\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Discover the specific file
	results, err := Discover([]string{dockerfilePath}, Options{})
	if err != nil {
		t.Fatalf("Discover() error: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	absPath, err := filepath.Abs(dockerfilePath)
	if err != nil {
		t.Fatal(err)
	}
	if results[0].Path != absPath {
		t.Errorf("expected path %q, got %q", absPath, results[0].Path)
	}

	if results[0].ConfigRoot != filepath.Dir(absPath) {
		t.Errorf("expected ConfigRoot %q, got %q", filepath.Dir(absPath), results[0].ConfigRoot)
	}
}

func TestDiscoverDirectory(t *testing.T) {
	t.Parallel()
	// Create a temporary directory structure
	tmpDir := t.TempDir()

	// Create files
	files := []string{
		"Dockerfile",
		"Dockerfile.dev",
		"api.Dockerfile",
		"sub/Dockerfile",
		"sub/nested/Dockerfile.prod",
		"not-a-dockerfile.txt",
	}

	for _, f := range files {
		path := filepath.Join(tmpDir, f)
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("FROM alpine\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Discover in directory
	results, err := Discover([]string{tmpDir}, Options{})
	if err != nil {
		t.Fatalf("Discover() error: %v", err)
	}

	// Should find 5 Dockerfiles (not the .txt file)
	if len(results) != 5 {
		t.Errorf("expected 5 results, got %d", len(results))
		for _, r := range results {
			t.Logf("  found: %s", r.Path)
		}
	}

	// Verify no txt files
	for _, r := range results {
		if filepath.Ext(r.Path) == ".txt" {
			t.Errorf("unexpected file discovered: %s", r.Path)
		}
	}
}

func TestDiscoverGlob(t *testing.T) {
	t.Parallel()
	// Create a temporary directory structure
	tmpDir := t.TempDir()

	files := []string{
		"Dockerfile",
		"Dockerfile.dev",
		"api.Dockerfile",
	}

	for _, f := range files {
		path := filepath.Join(tmpDir, f)
		if err := os.WriteFile(path, []byte("FROM alpine\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Use glob pattern
	pattern := filepath.Join(tmpDir, "*.Dockerfile")
	results, err := Discover([]string{pattern}, Options{})
	if err != nil {
		t.Fatalf("Discover() error: %v", err)
	}

	// Should find only api.Dockerfile
	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
		for _, r := range results {
			t.Logf("  found: %s", r.Path)
		}
	}
}

func TestDiscoverExclude(t *testing.T) {
	t.Parallel()
	// Create a temporary directory structure
	tmpDir := t.TempDir()

	files := []string{
		"Dockerfile",
		"test/Dockerfile",
		"vendor/Dockerfile",
		"sub/Dockerfile",
	}

	for _, f := range files {
		path := filepath.Join(tmpDir, f)
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("FROM alpine\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Discover with exclusions
	opts := Options{
		ExcludePatterns: []string{"test/*", "vendor/*"},
	}
	results, err := Discover([]string{tmpDir}, opts)
	if err != nil {
		t.Fatalf("Discover() error: %v", err)
	}

	// Should find 2 Dockerfiles (root and sub/, not test/ or vendor/)
	if len(results) != 2 {
		t.Errorf("expected 2 results, got %d", len(results))
		for _, r := range results {
			t.Logf("  found: %s", r.Path)
		}
	}

	// Verify no excluded files
	for _, r := range results {
		if filepath.Base(filepath.Dir(r.Path)) == "test" ||
			filepath.Base(filepath.Dir(r.Path)) == "vendor" {
			t.Errorf("excluded file discovered: %s", r.Path)
		}
	}
}

func TestDiscoverContextDir(t *testing.T) {
	t.Parallel()
	// Create a temporary directory with a Dockerfile
	tmpDir := t.TempDir()
	dockerfilePath := filepath.Join(tmpDir, "Dockerfile")
	if err := os.WriteFile(dockerfilePath, []byte("FROM alpine\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	contextDir := "/build/context"

	// Discover with context
	opts := Options{
		ContextDir: contextDir,
	}
	results, err := Discover([]string{dockerfilePath}, opts)
	if err != nil {
		t.Fatalf("Discover() error: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	if results[0].ContextDir != contextDir {
		t.Errorf("expected ContextDir %q, got %q", contextDir, results[0].ContextDir)
	}
}

func TestDiscoverDeduplication(t *testing.T) {
	t.Parallel()
	// Create a temporary directory with a Dockerfile
	tmpDir := t.TempDir()
	dockerfilePath := filepath.Join(tmpDir, "Dockerfile")
	if err := os.WriteFile(dockerfilePath, []byte("FROM alpine\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Discover the same file multiple ways
	results, err := Discover([]string{
		dockerfilePath,
		dockerfilePath,                      // duplicate
		tmpDir,                              // will also find the file
		filepath.Join(tmpDir, "Dockerfile"), // same file
	}, Options{})
	if err != nil {
		t.Fatalf("Discover() error: %v", err)
	}

	// Should deduplicate to 1 result
	if len(results) != 1 {
		t.Errorf("expected 1 result after deduplication, got %d", len(results))
		for _, r := range results {
			t.Logf("  found: %s", r.Path)
		}
	}
}

func TestDiscoverNonexistent(t *testing.T) {
	t.Parallel()
	// Discover a pattern that matches nothing
	results, err := Discover([]string{"nonexistent-pattern-*.xyz"}, Options{})
	if err != nil {
		t.Fatalf("Discover() error: %v", err)
	}

	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

// TestDiscoverFileNotFound verifies that a literal (non-glob) path that does
// not exist returns a *FileNotFoundError with the correct Path field.
// This is distinct from TestDiscoverNonexistent, which covers the glob path
// (empty results, no error).
func TestDiscoverFileNotFound(t *testing.T) {
	t.Parallel()

	literal := filepath.Join(t.TempDir(), "nonexistent", "Dockerfile")
	_, err := Discover([]string{literal}, Options{})
	if err == nil {
		t.Fatal("expected an error for a non-existent literal path, got nil")
	}

	var notFound *FileNotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("expected *FileNotFoundError, got %T: %v", err, err)
	}
	if notFound.Path != literal {
		t.Errorf("FileNotFoundError.Path = %q, want %q", notFound.Path, literal)
	}
}

func TestContainsGlobChars(t *testing.T) {
	t.Parallel()

	tests := []struct {
		path string
		want bool
	}{
		{"Dockerfile", false},
		{"path/to/Dockerfile", false},
		{"*.Dockerfile", true},
		{"Dockerfile?", true},
		{"[Dd]ockerfile", true},
		{"]strange]", true},
		// Brace expansion supported by doublestar/v4.
		{"{Dockerfile,Containerfile}", true},
		{"path/{Dockerfile,Containerfile}", true},
		{"only-open{brace", true},
		{"only-close}brace", true},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()
			got := ContainsGlobChars(tt.path)
			if got != tt.want {
				t.Errorf("ContainsGlobChars(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

// TestDiscoverBraceGlob verifies that brace-expansion patterns like
// "{Dockerfile,Containerfile}" are treated as globs and expanded by
// doublestar/v4 rather than passed to os.Stat (which would fail).
func TestDiscoverBraceGlob(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	dockerfilePath := filepath.Join(tmpDir, "Dockerfile")
	containerfilePath := filepath.Join(tmpDir, "Containerfile")
	if err := os.WriteFile(dockerfilePath, []byte("FROM alpine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(containerfilePath, []byte("FROM alpine\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	pattern := filepath.Join(tmpDir, "{Dockerfile,Containerfile}")
	results, err := Discover([]string{pattern}, Options{})
	if err != nil {
		t.Fatalf("Discover() error: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 results for brace pattern, got %d", len(results))
	}
}
