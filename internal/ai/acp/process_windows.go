//go:build windows

package acp

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

func configureProcessGroup(cmd *exec.Cmd) {
	// Best-effort: put the agent in its own process group so it is easier to target.
	// This does not sandbox or restrict the agent.
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= syscall.CREATE_NEW_PROCESS_GROUP
}

func killProcessGroup(pid int, sig syscall.Signal) error {
	_ = sig
	if pid <= 0 {
		return nil
	}

	// PROCESS_QUERY_LIMITED_INFORMATION is 0x1000.
	// It's not exposed in the syscall package on all platforms/toolchains.
	const processQueryLimitedInformation uint32 = 0x00001000

	access := uint32(syscall.PROCESS_TERMINATE) | processQueryLimitedInformation
	h, err := syscall.OpenProcess(access, false, uint32(pid))
	if err != nil {
		return fmt.Errorf("open process %d: %w", pid, err)
	}

	termErr := syscall.TerminateProcess(h, 1)
	closeErr := syscall.CloseHandle(h)

	if termErr != nil {
		return errors.Join(fmt.Errorf("terminate process %d: %w", pid, termErr), closeErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close process handle %d: %w", pid, closeErr)
	}
	return nil
}

func isNoSuchProcess(err error) bool {
	if err == nil {
		return false
	}
	// syscall does not expose ERROR_INVALID_PARAMETER (87), which is a common
	// "process not found" result for OpenProcess on Windows.
	const windowsErrorInvalidParameter syscall.Errno = 87

	return errors.Is(err, os.ErrProcessDone) ||
		errors.Is(err, syscall.ERROR_NOT_FOUND) ||
		errors.Is(err, windowsErrorInvalidParameter)
}
