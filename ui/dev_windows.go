//go:build windows

package ui

import (
	"os"
	"os/exec"
	"syscall"
)

// setProcessGroup is a no-op on Windows, which has no POSIX process groups.
func setProcessGroup(cmd *exec.Cmd) {}

// signalProcessGroup signals the process directly on Windows; there is no
// process group to fan the signal out to.
func signalProcessGroup(proc *os.Process, sig syscall.Signal) error {
	if proc == nil {
		return nil
	}
	return proc.Signal(sig)
}
