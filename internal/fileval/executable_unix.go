//go:build !windows

package fileval

import "os"

func checkExecutable(info os.FileInfo, path string) error {
	if info.Mode()&0o111 != 0 {
		return &ExecutableFileError{Path: path}
	}
	return nil
}
