//go:build windows

package clientcmd

import (
	"os"
	"os/exec"
	"syscall"

	"github.com/chromedp/chromedp"
)

// browserProcessGroupOption is a no-op on Windows, which has no POSIX process
// groups; chromedp's own teardown handles the browser process.
func browserProcessGroupOption() chromedp.ExecAllocatorOption {
	return chromedp.ModifyCmdFunc(func(cmd *exec.Cmd) {})
}

// signalProcess signals the process directly on Windows; there is no process
// group to fan the signal out to.
func signalProcess(proc *os.Process, sig syscall.Signal) {
	_ = proc.Signal(sig)
}
