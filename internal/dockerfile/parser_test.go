package dockerfile

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name             string
		content          string
		expectedLines    int
		expectedBlank    int
		expectedComments int
	}{
		{
			name:             "simple dockerfile",
			content:          "FROM alpine:3.18\nRUN echo hello\n",
			expectedLines:    2,
			expectedBlank:    0,
			expectedComments: 0,
		},
		{
			name:             "multiline dockerfile",
			content:          "FROM alpine:3.18\nRUN apk add --no-cache \\\n    curl \\\n    wget\nCMD [\"sh\"]\n",
			expectedLines:    5,
			expectedBlank:    0,
			expectedComments: 0,
		},
		{
			name:             "single line no newline",
			content:          "FROM alpine:3.18",
			expectedLines:    1,
			expectedBlank:    0,
			expectedComments: 0,
		},
		{
			name:             "empty lines",
			content:          "FROM alpine:3.18\n\n\nRUN echo hello\n",
			expectedLines:    4,
			expectedBlank:    2,
			expectedComments: 0,
		},
		{
			name:             "with comments",
			content:          "# This is a comment\nFROM alpine:3.18\n# Another comment\nRUN echo hello\n",
			expectedLines:    4,
			expectedBlank:    0,
			expectedComments: 2,
		},
		{
			name:             "mixed blanks and comments",
			content:          "# Header comment\n\nFROM alpine:3.18\n\n# Install packages\nRUN apk add curl\n",
			expectedLines:    6,
			expectedBlank:    2,
			expectedComments: 2,
		},
		{
			name:             "whitespace-only lines count as blank",
			content:          "FROM alpine:3.18\n   \n\t\nRUN echo hello\n",
			expectedLines:    4,
			expectedBlank:    2,
			expectedComments: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temp file
			tmpDir := t.TempDir()
			dockerfilePath := filepath.Join(tmpDir, "Dockerfile")
			if err := os.WriteFile(dockerfilePath, []byte(tt.content), 0o644); err != nil {
				t.Fatal(err)
			}

			result, err := ParseFile(context.Background(), dockerfilePath)
			if err != nil {
				t.Fatalf("ParseFile() error = %v", err)
			}

			if result.TotalLines != tt.expectedLines {
				t.Errorf("TotalLines = %d, want %d", result.TotalLines, tt.expectedLines)
			}

			if result.BlankLines != tt.expectedBlank {
				t.Errorf("BlankLines = %d, want %d", result.BlankLines, tt.expectedBlank)
			}

			if result.CommentLines != tt.expectedComments {
				t.Errorf("CommentLines = %d, want %d", result.CommentLines, tt.expectedComments)
			}
		})
	}
}

func TestCountLines(t *testing.T) {
	tests := []struct {
		name          string
		content       string
		expectedLines int
	}{
		{
			name:          "two lines with newline",
			content:       "line1\nline2\n",
			expectedLines: 2,
		},
		{
			name:          "two lines no trailing newline",
			content:       "line1\nline2",
			expectedLines: 2,
		},
		{
			name:          "single line",
			content:       "line1\n",
			expectedLines: 1,
		},
		{
			name:          "empty file",
			content:       "",
			expectedLines: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			filePath := filepath.Join(tmpDir, "test.txt")
			if err := os.WriteFile(filePath, []byte(tt.content), 0o644); err != nil {
				t.Fatal(err)
			}

			lines, err := CountLines(filePath)
			if err != nil {
				t.Fatalf("CountLines() error = %v", err)
			}

			if lines != tt.expectedLines {
				t.Errorf("CountLines() = %d, want %d", lines, tt.expectedLines)
			}
		})
	}
}
