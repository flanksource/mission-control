package cmd

import (
	gocontext "context"
	"fmt"
	"os"
	"syscall"
	"time"

	"github.com/chromedp/chromedp"
)

const (
	gracefulCloseTimeout = 3 * time.Second
	sigtermWait          = 2 * time.Second
	procExitWait         = 2 * time.Second
)

// closeBrowserWithEscalation tears down a chromedp browser with bounded
// timeouts, escalating from graceful CDP close to SIGTERM to SIGKILL.
// It never blocks for more than ~gracefulCloseTimeout + sigtermWait +
// procExitWait, even if the browser is wedged.
func closeBrowserWithEscalation(browserCtx gocontext.Context, browserCancel, allocCancel gocontext.CancelFunc) {
	proc := browserProcess(browserCtx)

	// 1. Graceful: CDP Browser.close with a bounded wait.
	tctx, tcancel := gocontext.WithTimeout(gocontext.Background(), gracefulCloseTimeout)
	done := make(chan error, 1)
	go func() { done <- chromedp.Cancel(browserCtx) }()
	select {
	case <-done:
		tcancel()
		allocCancel()
		return
	case <-tctx.Done():
		fmt.Fprintln(os.Stderr, "Graceful browser close timed out, escalating...")
	}
	tcancel()

	// 2. Force chromedp's own cancel path (exec.CommandContext SIGKILLs chrome).
	browserCancel()
	allocCancel()
	if proc == nil || waitProcessExit(proc, procExitWait) {
		return
	}

	// 3. SIGTERM the process group. Falls back to single pid on failure.
	signalProcess(proc, syscall.SIGTERM)
	if waitProcessExit(proc, sigtermWait) {
		return
	}

	// 4. SIGKILL + os.Process.Kill as the last resort.
	fmt.Fprintf(os.Stderr, "Force killing browser (pid=%d)\n", proc.Pid)
	signalProcess(proc, syscall.SIGKILL)
	_ = proc.Kill()
}

func browserProcess(ctx gocontext.Context) *os.Process {
	c := chromedp.FromContext(ctx)
	if c == nil || c.Browser == nil {
		return nil
	}
	return c.Browser.Process()
}

func signalProcess(proc *os.Process, sig syscall.Signal) {
	if pgid, err := syscall.Getpgid(proc.Pid); err == nil {
		if err := syscall.Kill(-pgid, sig); err == nil {
			return
		}
	}
	_ = proc.Signal(sig)
}

func waitProcessExit(proc *os.Process, d time.Duration) bool {
	done := make(chan struct{})
	go func() {
		_, _ = proc.Wait()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-time.After(d):
		return false
	}
}
