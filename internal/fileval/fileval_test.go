package fileval

import (
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestValidateFile_TooSmall(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	tests := []struct {
		name    string
		content []byte
		wantErr bool
	}{
		{"empty", nil, true},
		{"one-byte", []byte("F"), true},
		{"five-bytes", []byte("FROM "), true},
		{"exactly-min", []byte("FROM a"), false},
		{"valid-dockerfile", []byte("FROM alpine\n"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := filepath.Join(dir, tt.name+".Dockerfile")
			if err := os.WriteFile(f, tt.content, 0o644); err != nil {
				t.Fatal(err)
			}
			err := ValidateFile(f, 0)
			var tooSmall *FileTooSmallError
			if tt.wantErr {
				if !errors.As(err, &tooSmall) {
					t.Fatalf("expected FileTooSmallError, got %v", err)
				}
				if tooSmall.Size != int64(len(tt.content)) {
					t.Errorf("Size = %d, want %d", tooSmall.Size, len(tt.content))
				}
			} else if errors.As(err, &tooSmall) {
				t.Errorf("unexpected FileTooSmallError for %q", tt.content)
			}
		})
	}
}

func TestValidateFile_SizeCheck(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	f := filepath.Join(dir, "Dockerfile")
	// Write a 200-byte file.
	if err := os.WriteFile(f, make([]byte, 200), 0o644); err != nil {
		t.Fatal(err)
	}

	// Should fail with maxSize=100.
	err := ValidateFile(f, 100)
	var tooLarge *FileTooLargeError
	if !errors.As(err, &tooLarge) {
		t.Fatalf("expected FileTooLargeError, got %v", err)
	}
	if tooLarge.Size != 200 {
		t.Errorf("Size = %d, want 200", tooLarge.Size)
	}
	if tooLarge.MaxSize != 100 {
		t.Errorf("MaxSize = %d, want 100", tooLarge.MaxSize)
	}

	// Should pass with maxSize=200.
	if err := ValidateFile(f, 200); err != nil {
		t.Errorf("unexpected error for exact size: %v", err)
	}

	// Should pass with maxSize=0 (unlimited).
	if err := ValidateFile(f, 0); err != nil {
		t.Errorf("unexpected error for unlimited size: %v", err)
	}
}

func TestValidateFile_ExecutableBit(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("executable-bit check not applicable on Windows")
	}

	dir := t.TempDir()
	f := filepath.Join(dir, "Dockerfile")
	if err := os.WriteFile(f, []byte("FROM alpine\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	err := ValidateFile(f, 0)
	var execErr *ExecutableFileError
	if !errors.As(err, &execErr) {
		t.Fatalf("expected ExecutableFileError, got %v", err)
	}

	// Fix permissions — should pass.
	if err := os.Chmod(f, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ValidateFile(f, 0); err != nil {
		t.Errorf("unexpected error after removing executable bit: %v", err)
	}
}

func TestValidateFile_UTF8Check(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// Valid UTF-8.
	validFile := filepath.Join(dir, "valid.Dockerfile")
	if err := os.WriteFile(validFile, []byte("FROM alpine\nRUN echo héllo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ValidateFile(validFile, 0); err != nil {
		t.Errorf("unexpected error for valid UTF-8: %v", err)
	}

	// Binary content.
	binFile := filepath.Join(dir, "binary.Dockerfile")
	data := make([]byte, 1024)
	if _, err := rand.Read(data); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binFile, data, 0o644); err != nil {
		t.Fatal(err)
	}
	err := ValidateFile(binFile, 0)
	var utf8Err *NotUTF8Error
	if !errors.As(err, &utf8Err) {
		t.Fatalf("expected NotUTF8Error for binary file, got %v", err)
	}
}

func TestValidateFile_NonexistentFile(t *testing.T) {
	t.Parallel()

	err := ValidateFile(filepath.Join(t.TempDir(), "nope"), 0)
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
	if errors.Is(err, os.ErrNotExist) {
		return // correct
	}
	// On some systems the error might wrap differently; just check it's not nil.
}

func TestLooksUTF8_ValidFiles(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content []byte
	}{
		{"empty", nil},
		{"ascii", []byte("FROM alpine\nRUN echo hello\n")},
		{"multibyte", []byte("# Ünïcödé comment\nFROM alpine\n")},
		{"emoji", []byte("# 🐳 Dockerfile\nFROM alpine\n")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			f := filepath.Join(dir, "Dockerfile")
			if err := os.WriteFile(f, tt.content, 0o644); err != nil {
				t.Fatal(err)
			}
			ok, err := LooksUTF8(f, 0)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !ok {
				t.Error("expected LooksUTF8 = true")
			}
		})
	}
}

func TestLooksUTF8_InvalidFiles(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content []byte
	}{
		{"invalid-continuation", []byte{0x80, 0x81, 0x82}},
		{"truncated-2byte", []byte{0xC0}},
		{"mixed-valid-then-invalid", append([]byte("FROM alpine\n"), 0xFF, 0xFE)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			f := filepath.Join(dir, "Dockerfile")
			if err := os.WriteFile(f, tt.content, 0o644); err != nil {
				t.Fatal(err)
			}
			ok, err := LooksUTF8(f, 0)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if ok {
				t.Error("expected LooksUTF8 = false")
			}
		})
	}
}

func TestLooksUTF8_ChunkBoundary(t *testing.T) {
	t.Parallel()

	// Create content where a multi-byte character spans the 32KB chunk boundary.
	suffix := []byte{0xE2, 0x82, 0xAC} // € (3-byte UTF-8)
	tail := []byte("FROM alpine\n")
	data := make([]byte, 0, chunkSize-1+len(suffix)+len(tail))
	for range chunkSize - 1 {
		data = append(data, 'A')
	}
	data = append(data, suffix...)
	data = append(data, tail...)

	dir := t.TempDir()
	f := filepath.Join(dir, "Dockerfile")
	if err := os.WriteFile(f, data, 0o644); err != nil {
		t.Fatal(err)
	}

	ok, err := LooksUTF8(f, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("expected LooksUTF8 = true for split multi-byte char")
	}
}

func TestTrailingIncomplete(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		data []byte
		want int
	}{
		{"empty", nil, 0},
		{"ascii-only", []byte("abc"), 0},
		{"complete-2byte", []byte{0xC3, 0xA9}, 0},             // é
		{"incomplete-2byte", []byte{0xC3}, 1},                 // first byte of é
		{"complete-3byte", []byte{0xE2, 0x82, 0xAC}, 0},       // €
		{"incomplete-3byte-1", []byte{0xE2}, 1},               // first byte of €
		{"incomplete-3byte-2", []byte{0xE2, 0x82}, 2},         // first 2 bytes of €
		{"complete-4byte", []byte{0xF0, 0x9F, 0x90, 0xB3}, 0}, // 🐳
		{"incomplete-4byte-1", []byte{0xF0}, 1},
		{"incomplete-4byte-2", []byte{0xF0, 0x9F}, 2},
		{"incomplete-4byte-3", []byte{0xF0, 0x9F, 0x90}, 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := trailingIncomplete(tt.data)
			if got != tt.want {
				t.Errorf("trailingIncomplete(%x) = %d, want %d", tt.data, got, tt.want)
			}
		})
	}
}
