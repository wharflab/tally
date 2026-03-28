package context

import (
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestNew(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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

func TestReadFile(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "config.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, err := New(tmpDir, "")
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	content, err := ctx.ReadFile("./config.txt")
	if err != nil {
		t.Fatalf("ReadFile() error: %v", err)
	}
	if string(content) != "hello" {
		t.Fatalf("ReadFile() = %q, want %q", string(content), "hello")
	}

	if _, err := ctx.ReadFile("../outside.txt"); err == nil {
		t.Fatal("expected ReadFile() to reject paths outside the context")
	}
}

func TestReadFile_ConcurrentCallsReuseSingleRead(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	fullPath := filepath.Join(tmpDir, "config.txt")
	if err := os.WriteFile(fullPath, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, err := New(tmpDir, "")
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	var calls atomic.Int32
	firstReadStarted := make(chan struct{})
	secondReadStarted := make(chan struct{}, 1)
	releaseFirstRead := make(chan struct{})
	ctx.readFile = func(path string) ([]byte, error) {
		callNum := calls.Add(1)
		if callNum == 1 {
			close(firstReadStarted)
			<-releaseFirstRead
		} else {
			secondReadStarted <- struct{}{}
		}
		return os.ReadFile(path)
	}

	var wg sync.WaitGroup
	results := make(chan string, 2)
	errs := make(chan error, 2)
	read := func() {
		defer wg.Done()
		content, readErr := ctx.ReadFile("config.txt")
		if readErr != nil {
			errs <- readErr
			return
		}
		results <- string(content)
	}

	wg.Add(1)
	go read()

	<-firstReadStarted

	wg.Add(1)
	go read()

	select {
	case <-secondReadStarted:
		t.Fatal("expected concurrent ReadFile calls to share a single underlying read")
	case <-time.After(100 * time.Millisecond):
	}

	close(releaseFirstRead)
	wg.Wait()
	close(results)
	close(errs)

	for readErr := range errs {
		if readErr != nil {
			t.Fatalf("ReadFile() error: %v", readErr)
		}
	}
	for content := range results {
		if content != "hello" {
			t.Fatalf("ReadFile() = %q, want %q", content, "hello")
		}
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("underlying read count = %d, want 1", got)
	}
}

func TestHeredocFiles(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
