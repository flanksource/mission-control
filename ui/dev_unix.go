//go:build !windows

package ui

import (
	"os"
	"os/exec"
	"syscall"
)

// setProcessGroup puts the vite child in its own process group so that the
// whole group (vite plus any node workers it spawns) can be signalled together.
func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// signalProcessGroup sends sig to the child's process group, falling back to
// the process itself if the group lookup fails.
func signalProcessGroup(proc *os.Process, sig syscall.Signal) error {
	if proc == nil {
		return nil
	}
	if pgid, err := syscall.Getpgid(proc.Pid); err == nil {
		if err := syscall.Kill(-pgid, sig); err == nil {
			return nil
		}
	}
	return proc.Signal(sig)
}
