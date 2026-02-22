// Package fileval provides pre-parse file validation checks for tally.
//
// These checks run before BuildKit parsing to fail fast on files that clearly
// aren't valid Dockerfiles: binary files, oversized files, and executable files.
package fileval

import (
	"fmt"
	"os"
)

// minDockerfileSize is the size of the smallest valid Dockerfile: "FROM a" (6 bytes).
const minDockerfileSize = 6

// FileTooSmallError is returned when a file is too small to be a valid Dockerfile.
type FileTooSmallError struct {
	Path string
	Size int64
}

func (e *FileTooSmallError) Error() string {
	return fmt.Sprintf(
		"file is too small for a valid Dockerfile (%d bytes; minimum is %d)",
		e.Size, minDockerfileSize,
	)
}

// FileTooLargeError is returned when a file exceeds the configured maximum size.
type FileTooLargeError struct {
	Path    string
	Size    int64
	MaxSize int64
}

func (e *FileTooLargeError) Error() string {
	return fmt.Sprintf(
		"file too large (%d > %d bytes); increase [file-validation] max-file-size in .tally.toml to override",
		e.Size, e.MaxSize,
	)
}

// ExecutableFileError is returned when a Dockerfile has the executable bit set.
type ExecutableFileError struct {
	Path string
}

func (e *ExecutableFileError) Error() string {
	return "unexpected executable Dockerfile"
}

// NotUTF8Error is returned when a file does not appear to be valid UTF-8 text.
type NotUTF8Error struct {
	Path string
}

func (e *NotUTF8Error) Error() string {
	return "file does not appear to be valid UTF-8 text"
}

// ValidateFile runs pre-parse validation checks on a file:
//  1. Minimum size check (must be at least "FROM a")
//  2. Maximum size check (when maxSize > 0)
//  3. Executable-bit check (Unix only)
//  4. UTF-8 smoke check
func ValidateFile(path string, maxSize int64) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}

	// 1. Minimum size check.
	if info.Size() < minDockerfileSize {
		return &FileTooSmallError{Path: path, Size: info.Size()}
	}

	// 2. Maximum size check.
	if maxSize > 0 && info.Size() > maxSize {
		return &FileTooLargeError{Path: path, Size: info.Size(), MaxSize: maxSize}
	}

	// 2. Executable-bit check (platform-specific).
	if err := checkExecutable(info, path); err != nil {
		return err
	}

	// 3. UTF-8 smoke check.
	// Use maxSize as the read limit when positive; otherwise read up to 1 MB.
	readLimit := maxSize
	if readLimit <= 0 {
		readLimit = 1 << 20 // 1 MB
	}
	ok, err := LooksUTF8(path, readLimit)
	if err != nil {
		return err
	}
	if !ok {
		return &NotUTF8Error{Path: path}
	}

	return nil
}
