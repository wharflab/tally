//go:build !windows

package acp

import (
	"errors"
	"os/exec"
	"syscall"
)

func configureProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		return
	}
	cmd.SysProcAttr.Setpgid = true
}

func killProcessGroup(pid int, sig syscall.Signal) error {
	if pid <= 0 {
		return nil
	}
	// If the leader exits early, the process group can still exist; kill by pgid.
	return syscall.Kill(-pid, sig)
}

func isNoSuchProcess(err error) bool {
	return errors.Is(err, syscall.ESRCH)
}
