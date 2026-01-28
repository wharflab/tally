package reporter

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestParseFormat(t *testing.T) {
	tests := []struct {
		input    string
		expected Format
		wantErr  bool
	}{
		{"text", FormatText, false},
		{"", FormatText, false},
		{"json", FormatJSON, false},
		{"sarif", FormatSARIF, false},
		{"github-actions", FormatGitHubActions, false},
		{"github", FormatGitHubActions, false},
		{"unknown", "", true},
		{"TEXT", "", true}, // Case sensitive
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			format, err := ParseFormat(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseFormat(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if !tt.wantErr && format != tt.expected {
				t.Errorf("ParseFormat(%q) = %v, want %v", tt.input, format, tt.expected)
			}
		})
	}
}

func TestNew(t *testing.T) {
	tests := []struct {
		name    string
		format  Format
		wantErr bool
	}{
		{"text", FormatText, false},
		{"json", FormatJSON, false},
		{"sarif", FormatSARIF, false},
		{"github-actions", FormatGitHubActions, false},
		{"unknown", Format("unknown"), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			opts := Options{
				Format: tt.format,
				Writer: &buf,
			}
			rep, err := New(opts)
			if (err != nil) != tt.wantErr {
				t.Errorf("New() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && rep == nil {
				t.Error("New() returned nil reporter")
			}
		})
	}
}

func TestGetWriter(t *testing.T) {
	tests := []struct {
		path     string
		wantErr  bool
		expected string // "stdout", "stderr", or "file"
	}{
		{"stdout", false, "stdout"},
		{"", false, "stdout"},
		{"stderr", false, "stderr"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			w, closer, err := GetWriter(tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetWriter(%q) error = %v, wantErr %v", tt.path, err, tt.wantErr)
			}
			if err != nil {
				return
			}

			switch tt.expected {
			case "stdout":
				if w != os.Stdout {
					t.Errorf("GetWriter(%q) did not return stdout", tt.path)
				}
			case "stderr":
				if w != os.Stderr {
					t.Errorf("GetWriter(%q) did not return stderr", tt.path)
				}
			}

			if closer == nil {
				t.Error("GetWriter() returned nil closer")
			}
			if err := closer(); err != nil {
				t.Errorf("closer() error = %v", err)
			}
		})
	}
}

func TestGetWriterFile(t *testing.T) {
	// Test file output
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "output.txt")

	w, closer, err := GetWriter(filePath)
	if err != nil {
		t.Fatalf("GetWriter() error = %v", err)
	}

	// Write something
	_, err = w.Write([]byte("test"))
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	// Close and verify
	if err := closer(); err != nil {
		t.Fatalf("closer() error = %v", err)
	}

	// Verify file exists and has content
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(content) != "test" {
		t.Errorf("File content = %q, want %q", string(content), "test")
	}
}

func TestGetWriterInvalidPath(t *testing.T) {
	// Test invalid file path
	_, _, err := GetWriter("/nonexistent/directory/file.txt")
	if err == nil {
		t.Error("GetWriter() with invalid path should return error")
	}
}

func TestDefaultOptions(t *testing.T) {
	opts := DefaultOptions()

	if opts.Format != FormatText {
		t.Errorf("Default format = %v, want %v", opts.Format, FormatText)
	}
	if opts.Writer != os.Stdout {
		t.Error("Default writer should be stdout")
	}
	if opts.Color != nil {
		t.Error("Default color should be nil (auto-detect)")
	}
	if !opts.ShowSource {
		t.Error("Default ShowSource should be true")
	}
	if opts.ToolName != "tally" {
		t.Errorf("Default ToolName = %q, want %q", opts.ToolName, "tally")
	}
}
