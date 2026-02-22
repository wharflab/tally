//go:build windows

package fileval

import "os"

func checkExecutable(_ os.FileInfo, _ string) error {
	return nil
}
