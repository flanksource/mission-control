package harness

import (
	"bytes"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	embeddedPG "github.com/fergusstrange/embedded-postgres"
	"golang.org/x/crypto/bcrypt"
)

// adminEmail is the person Mission Control creates and makes an admin in the basic auth mode.
const adminEmail = "admin@local"

// Server is a Mission Control process started from its binary, with its own database.
type Server struct {
	URL string

	// Admin sets up what the cases need. It's never what a case checks.
	Admin *Caller

	dir      string
	logPath  string
	process  *exec.Cmd
	postgres *embeddedPG.EmbeddedPostgres
	users    []*Caller
	next     int
}

// StartServer starts Mission Control in the basic auth mode, with an htpasswd entry for the admin and for each of the
// users the cases take. Users have to be known up front: Mission Control reads the htpasswd file once, at startup.
//
// The binary is AUTHZ_BINARY, or .bin/incident-commander at the root of the repository (make dev).
// The database is DB_URL when it's set, or an embedded Postgres otherwise.
func StartServer(dir string, users int) (*Server, error) {
	s := &Server{dir: dir, logPath: filepath.Join(dir, "mission-control.log")}

	binary, err := binaryPath()
	if err != nil {
		return nil, err
	}

	dbURL := os.Getenv("DB_URL")
	if dbURL == "" {
		if dbURL, err = s.startPostgres(); err != nil {
			return nil, err
		}
	}

	password := "authz-" + randomSuffix()
	htpasswd := []string{}
	s.Admin = &Caller{name: adminEmail, username: adminEmail, password: password}
	for i := range users {
		email := fmt.Sprintf("authz-user-%03d@authz.test", i)
		s.users = append(s.users, &Caller{name: email, username: email, password: password})
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		return nil, err
	}
	for _, c := range append([]*Caller{s.Admin}, s.users...) {
		htpasswd = append(htpasswd, c.username+":"+string(hash))
	}
	htpasswdPath := filepath.Join(dir, "htpasswd")
	if err := os.WriteFile(htpasswdPath, []byte(strings.Join(htpasswd, "\n")+"\n"), 0o600); err != nil {
		return nil, err
	}

	ports, err := freePorts(3)
	if err != nil {
		return nil, err
	}
	s.URL = fmt.Sprintf("http://127.0.0.1:%d", ports[0])

	log, err := os.Create(s.logPath)
	if err != nil {
		return nil, err
	}
	args := []string{"serve",
		"--db", dbURL,
		"--auth", "basic",
		"--htpasswd-file", htpasswdPath,
		"--httpPort", fmt.Sprint(ports[0]),
		"--upstream-grpc-port", fmt.Sprint(ports[1]),
		"--postgrest-uri", fmt.Sprintf("http://localhost:%d", ports[2]),
		"--public-endpoint", s.URL,
		"--disable-kubernetes",
		"--disable-operators",
		"--log-level", logLevel(),
	}
	// AUTHZ_SERVER_ARGS adds arguments, e.g. -P casbin.cache=false
	args = append(args, strings.Fields(os.Getenv("AUTHZ_SERVER_ARGS"))...)
	s.process = exec.Command(binary, args...)
	s.process.Dir = dir
	s.process.Stdout, s.process.Stderr = log, log
	s.process.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	s.process.Env = append(os.Environ(), "ADMIN_PASSWORD="+password)
	if err := s.process.Start(); err != nil {
		return nil, err
	}

	for _, c := range append([]*Caller{s.Admin}, s.users...) {
		c.server = s
	}

	if err := s.waitReady(3 * time.Minute); err != nil {
		s.Stop()
		return nil, err
	}

	for _, user := range s.users {
		if err := s.createPerson(user); err != nil {
			s.Stop()
			return nil, err
		}
	}
	return s, nil
}

// NewUser returns a person of their own for one case, with no grants.
func (s *Server) NewUser() *Caller {
	if s.next >= len(s.users) {
		panic("harness: more users taken than StartServer was asked for")
	}
	user := s.users[s.next]
	s.next++
	return user
}

// Stop stops Mission Control and its database.
func (s *Server) Stop() {
	if s.process != nil && s.process.Process != nil {
		_ = syscall.Kill(-s.process.Process.Pid, syscall.SIGTERM)
		done := make(chan struct{})
		go func() { _ = s.process.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(20 * time.Second):
			_ = syscall.Kill(-s.process.Process.Pid, syscall.SIGKILL)
		}
	}
	if s.postgres != nil {
		_ = s.postgres.Stop()
	}
}

// LogTail returns the end of Mission Control's log, to explain a failure.
func (s *Server) LogTail(lines int) string {
	content, err := os.ReadFile(s.logPath)
	if err != nil {
		return err.Error()
	}
	all := strings.Split(strings.TrimRight(string(content), "\n"), "\n")
	return strings.Join(all[max(0, len(all)-lines):], "\n")
}

func (s *Server) startPostgres() (string, error) {
	ports, err := freePorts(1)
	if err != nil {
		return "", err
	}
	var log bytes.Buffer
	config := embeddedPG.DefaultConfig().
		Port(uint32(ports[0])).
		Database("mission_control").
		Username("postgres").
		Password("postgres").
		RuntimePath(filepath.Join(s.dir, "pg-runtime")).
		DataPath(filepath.Join(s.dir, "pg-data")).
		Version(embeddedPG.V16).
		StartTimeout(2 * time.Minute).
		Logger(&log)
	s.postgres = embeddedPG.NewDatabase(config)
	if err := s.postgres.Start(); err != nil {
		return "", fmt.Errorf("starting postgres: %w: %s", err, log.String())
	}
	return fmt.Sprintf("postgres://postgres:postgres@localhost:%d/mission_control?sslmode=disable", ports[0]), nil
}

func (s *Server) waitReady(timeout time.Duration) error {
	exited := make(chan error, 1)
	go func() { exited <- s.process.Wait() }()

	var last Response
	deadline := time.After(timeout)
	for {
		select {
		case err := <-exited:
			s.process = nil
			return fmt.Errorf("mission control exited before it was ready: %v\n%s", err, s.LogTail(40))
		case <-deadline:
			return fmt.Errorf("mission control wasn't ready after %s, last answering %s\n%s", timeout, last, s.LogTail(40))
		case <-time.After(500 * time.Millisecond):
		}

		// The admin is made an admin last thing before the server listens, and PostgREST starts on its own
		if last = s.Admin.Do(Request{Method: http.MethodGet, Path: "/db/people?limit=1"}); last.Status == http.StatusOK {
			return nil
		}
	}
}

// createPerson creates the user's person through the database API, as the admin.
// Mission Control doesn't create one when a basic auth user signs in.
func (s *Server) createPerson(user *Caller) error {
	resp := s.Admin.Do(Request{Method: http.MethodPost, Path: "/db/people", Body: map[string]any{
		"name": strings.TrimSuffix(user.username, "@authz.test"), "email": user.username,
	}})
	if resp.Status != http.StatusCreated {
		return fmt.Errorf("creating person %s: %s", user.username, resp)
	}
	return nil
}

// logLevel is Mission Control's log level: AUTHZ_LOG_LEVEL, or info.
func logLevel() string {
	if level := os.Getenv("AUTHZ_LOG_LEVEL"); level != "" {
		return level
	}
	return "info"
}

func binaryPath() (string, error) {
	if path := os.Getenv("AUTHZ_BINARY"); path != "" {
		return path, nil
	}

	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			path := filepath.Join(dir, ".bin", "incident-commander")
			if _, err := os.Stat(path); err != nil {
				return "", fmt.Errorf("mission control binary not found at %s: build it with make dev, or set AUTHZ_BINARY", path)
			}
			return path, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("repository root not found: set AUTHZ_BINARY")
		}
		dir = parent
	}
}

func freePorts(n int) ([]int, error) {
	var ports []int
	var listeners []net.Listener
	defer func() {
		for _, l := range listeners {
			_ = l.Close()
		}
	}()
	for range n {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return nil, err
		}
		listeners = append(listeners, l)
		ports = append(ports, l.Addr().(*net.TCPAddr).Port)
	}
	return ports, nil
}
