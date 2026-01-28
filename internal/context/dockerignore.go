package context

import (
	"os"
	"path/filepath"

	"github.com/moby/patternmatcher/ignorefile"
)

// dockerignoreNames are the possible names for Docker ignore files.
// .dockerignore is the standard, but we also support containerignore for Podman.
var dockerignoreNames = []string{
	".dockerignore",
	".containerignore",
}

// LoadDockerignore reads .dockerignore patterns from a directory.
// Returns an empty slice if no ignore file exists.
func LoadDockerignore(contextDir string) ([]string, error) {
	for _, name := range dockerignoreNames {
		ignorePath := filepath.Join(contextDir, name)
		patterns, err := loadIgnoreFile(ignorePath)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		if len(patterns) > 0 {
			return patterns, nil
		}
	}
	return nil, nil
}

// loadIgnoreFile reads patterns from a single ignore file.
func loadIgnoreFile(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	return ignorefile.ReadAll(f)
}
