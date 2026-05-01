package ui

import (
	gocontext "context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/flanksource/duty/shutdown"
)

const viteHost = "127.0.0.1"

const viteStopTimeout = 2 * time.Second

type DevServerOptions struct {
	Port       int
	BackendURL string
}

type DevServer struct {
	URL      string
	Port     int
	cancel   gocontext.CancelFunc
	process  *os.Process
	wait     *processWait
	stopOnce sync.Once
}

type processWait struct {
	done chan struct{}
	mu   sync.Mutex
	err  error
}

func StartDevServer(ctx gocontext.Context, opts DevServerOptions) (*DevServer, error) {
	frontendDir, err := findFrontendDir()
	if err != nil {
		return nil, err
	}

	port := opts.Port
	if port == 0 {
		port, err = freePort()
		if err != nil {
			return nil, err
		}
	}

	cmdCtx, cancel := gocontext.WithCancel(ctx)
	cmd := devServerCommand(cmdCtx, frontendDir, port, opts.BackendURL)
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start vite dev server: %w", err)
	}
	wait := newProcessWait(cmd)

	srv := &DevServer{
		URL:     fmt.Sprintf("http://%s:%d", viteHost, port),
		Port:    port,
		cancel:  cancel,
		process: cmd.Process,
		wait:    wait,
	}
	shutdown.AddHookWithPriority("ui-dev-server", shutdown.PriorityIngress, srv.Stop)

	if err := waitForVite(srv.URL, 20*time.Second, wait); err != nil {
		srv.Stop()
		return nil, err
	}

	return srv, nil
}

func (s *DevServer) Stop() {
	if s == nil || s.cancel == nil {
		return
	}
	s.stopOnce.Do(func() {
		s.cancel()
		if s.process != nil {
			_ = signalProcessGroup(s.process, syscall.SIGTERM)
		}
		if waitProcessExit(s.wait, viteStopTimeout) {
			return
		}
		if s.process != nil {
			_ = signalProcessGroup(s.process, syscall.SIGKILL)
			_ = s.process.Kill()
		}
		_ = waitProcessExit(s.wait, viteStopTimeout)
	})
}

func devServerCommand(ctx gocontext.Context, frontendDir string, port int, backendURL string) *exec.Cmd {
	args := []string{"exec", "vite", "--host", viteHost, "--port", strconv.Itoa(port), "--strictPort"}
	cmd := exec.CommandContext(ctx, "pnpm", args...)
	cmd.Dir = frontendDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		_ = signalProcessGroup(cmd.Process, syscall.SIGTERM)
		return nil
	}
	if backendURL != "" {
		cmd.Env = append(cmd.Env, "INCIDENT_COMMANDER_API_URL="+backendURL)
	}
	return cmd
}

func newProcessWait(cmd *exec.Cmd) *processWait {
	wait := &processWait{done: make(chan struct{})}
	go func() {
		err := cmd.Wait()
		wait.mu.Lock()
		wait.err = err
		wait.mu.Unlock()
		close(wait.done)
	}()
	return wait
}

func (w *processWait) Err() error {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.err
}

func waitProcessExit(wait *processWait, timeout time.Duration) bool {
	if wait == nil {
		return true
	}
	select {
	case <-wait.done:
		return true
	case <-time.After(timeout):
		return false
	}
}

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

func findFrontendDir() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		candidate := filepath.Join(dir, "ui", "frontend")
		if _, err := os.Stat(filepath.Join(candidate, "package.json")); err == nil {
			return candidate, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("ui/frontend/package.json not found from %s", dir)
		}
		dir = parent
	}
}

func freePort() (int, error) {
	listener, err := net.Listen("tcp", net.JoinHostPort(viteHost, "0"))
	if err != nil {
		return 0, fmt.Errorf("find free vite port: %w", err)
	}
	defer listener.Close() //nolint:errcheck
	return listener.Addr().(*net.TCPAddr).Port, nil
}

func waitForVite(baseURL string, timeout time.Duration, wait *processWait) error {
	deadline := time.Now().Add(timeout)
	client := http.Client{Timeout: 500 * time.Millisecond}
	for {
		select {
		case <-wait.done:
			err := wait.Err()
			if err != nil {
				return fmt.Errorf("vite dev server exited before becoming ready: %w", err)
			}
			return fmt.Errorf("vite dev server exited before becoming ready")
		default:
		}

		resp, err := client.Get(baseURL + "/ui/")
		if err == nil {
			resp.Body.Close() //nolint:errcheck
			if resp.StatusCode < http.StatusInternalServerError {
				return nil
			}
		}
		if time.Now().After(deadline) {
			if err != nil {
				return fmt.Errorf("vite dev server did not become ready at %s: %w", baseURL, err)
			}
			return fmt.Errorf("vite dev server did not become ready at %s", baseURL)
		}
		time.Sleep(100 * time.Millisecond)
	}
}
