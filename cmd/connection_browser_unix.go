//go:build !windows

package cmd

import (
	"os"
	"os/exec"
	"syscall"

	"github.com/chromedp/chromedp"
)

// browserProcessGroupOption puts Chromium in its own process group so the whole
// group (renderers/helpers) can be signalled together during teardown.
func browserProcessGroupOption() chromedp.ExecAllocatorOption {
	return chromedp.ModifyCmdFunc(func(cmd *exec.Cmd) {
		if cmd.SysProcAttr == nil {
			cmd.SysProcAttr = &syscall.SysProcAttr{}
		}
		cmd.SysProcAttr.Setpgid = true
	})
}

// signalProcess signals the browser's process group, falling back to the single
// process if the group lookup fails.
func signalProcess(proc *os.Process, sig syscall.Signal) {
	if pgid, err := syscall.Getpgid(proc.Pid); err == nil {
		if err := syscall.Kill(-pgid, sig); err == nil {
			return
		}
	}
	_ = proc.Signal(sig)
}
