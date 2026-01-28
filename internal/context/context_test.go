package context

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNew(t *testing.T) {
	tmpDir := t.TempDir()

	ctx, err := New(tmpDir, "")
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	absDir, err := filepath.Abs(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	if ctx.ContextDir != absDir {
		t.Errorf("ContextDir = %q, want %q", ctx.ContextDir, absDir)
	}
}

func TestIsIgnored_NoIgnoreFile(t *testing.T) {
	tmpDir := t.TempDir()

	ctx, err := New(tmpDir, "")
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	ignored, err := ctx.IsIgnored("anything.txt")
	if err != nil {
		t.Fatalf("IsIgnored() error: %v", err)
	}

	if ignored {
		t.Error("expected nothing to be ignored without .dockerignore")
	}
}

func TestIsIgnored_WithIgnoreFile(t *testing.T) {
	tmpDir := t.TempDir()

	// Create .dockerignore
	ignoreContent := `
# Comment line
*.log
temp/
!important.log
`
	if err := os.WriteFile(filepath.Join(tmpDir, ".dockerignore"), []byte(ignoreContent), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, err := New(tmpDir, "")
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	tests := []struct {
		path string
		want bool
	}{
		{"test.log", true},
		{"foo.log", true},
		{"important.log", false}, // Negated pattern
		{"temp/file.txt", true},
		{"src/main.go", false},
		{"readme.md", false},
	}

	for _, tc := range tests {
		ignored, err := ctx.IsIgnored(tc.path)
		if err != nil {
			t.Errorf("IsIgnored(%q) error: %v", tc.path, err)
			continue
		}
		if ignored != tc.want {
			t.Errorf("IsIgnored(%q) = %v, want %v", tc.path, ignored, tc.want)
		}
	}
}

func TestFileExists(t *testing.T) {
	tmpDir := t.TempDir()

	// Create some files
	if err := os.WriteFile(filepath.Join(tmpDir, "exists.txt"), []byte("content"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(tmpDir, "subdir"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "subdir", "nested.txt"), []byte("content"), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, err := New(tmpDir, "")
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	tests := []struct {
		path string
		want bool
	}{
		{"exists.txt", true},
		{"missing.txt", false},
		{"subdir/nested.txt", true},
		{"subdir/missing.txt", false},
		{"subdir", false}, // directories return false
	}

	for _, tc := range tests {
		exists := ctx.FileExists(tc.path)
		if exists != tc.want {
			t.Errorf("FileExists(%q) = %v, want %v", tc.path, exists, tc.want)
		}
	}
}

func TestHeredocFiles(t *testing.T) {
	tmpDir := t.TempDir()

	ctx, err := New(tmpDir, "")
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	// Initially not a heredoc file
	if ctx.IsHeredocFile("/app/script.sh") {
		t.Error("expected IsHeredocFile to return false initially")
	}

	// Add as heredoc file
	ctx.AddHeredocFile("/app/script.sh")

	if !ctx.IsHeredocFile("/app/script.sh") {
		t.Error("expected IsHeredocFile to return true after AddHeredocFile")
	}
}

func TestWithHeredocFiles(t *testing.T) {
	tmpDir := t.TempDir()

	heredocs := map[string]bool{
		"/app/script.sh":  true,
		"/app/config.txt": true,
	}

	ctx, err := New(tmpDir, "", WithHeredocFiles(heredocs))
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	if !ctx.IsHeredocFile("/app/script.sh") {
		t.Error("expected script.sh to be heredoc file")
	}
	if !ctx.IsHeredocFile("/app/config.txt") {
		t.Error("expected config.txt to be heredoc file")
	}
	if ctx.IsHeredocFile("/app/other.txt") {
		t.Error("expected other.txt to not be heredoc file")
	}
}

func TestPatterns(t *testing.T) {
	tmpDir := t.TempDir()

	// Create .dockerignore
	ignoreContent := `*.log
temp/
`
	if err := os.WriteFile(filepath.Join(tmpDir, ".dockerignore"), []byte(ignoreContent), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, err := New(tmpDir, "")
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	patterns := ctx.Patterns()
	if len(patterns) != 2 {
		t.Errorf("expected 2 patterns, got %d: %v", len(patterns), patterns)
	}
}

func TestHasIgnoreFile(t *testing.T) {
	tmpDir := t.TempDir()

	ctx, err := New(tmpDir, "")
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	if ctx.HasIgnoreFile() {
		t.Error("expected HasIgnoreFile to return false without .dockerignore")
	}

	// Create .dockerignore
	if err := os.WriteFile(filepath.Join(tmpDir, ".dockerignore"), []byte("*.log\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx2, err := New(tmpDir, "")
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	if !ctx2.HasIgnoreFile() {
		t.Error("expected HasIgnoreFile to return true with .dockerignore")
	}
}

func TestContainerignore(t *testing.T) {
	tmpDir := t.TempDir()

	// Create .containerignore (Podman)
	if err := os.WriteFile(filepath.Join(tmpDir, ".containerignore"), []byte("*.log\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, err := New(tmpDir, "")
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	ignored, err := ctx.IsIgnored("test.log")
	if err != nil {
		t.Fatalf("IsIgnored() error: %v", err)
	}

	if !ignored {
		t.Error("expected .containerignore to be respected")
	}
}
