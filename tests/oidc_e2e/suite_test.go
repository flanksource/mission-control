package oidc_e2e

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"golang.org/x/crypto/bcrypt"
)

var (
	serverURL  string
	serverPort int
	serverCmd  *exec.Cmd
	chromectx  context.Context
	chromeCanc context.CancelFunc
	tmpDir     string
)

func TestOIDCE2E(t *testing.T) {
	RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "OIDC E2E")
}

func freePort() int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return port
}

var _ = ginkgo.BeforeSuite(func() {
	serverPort = freePort()
	serverURL = fmt.Sprintf("http://localhost:%d", serverPort)

	tmpDir = ginkgo.GinkgoT().TempDir()

	hash, err := bcrypt.GenerateFromPassword([]byte("admin"), bcrypt.DefaultCost)
	Expect(err).ToNot(HaveOccurred())
	htpasswdPath := filepath.Join(tmpDir, "htpasswd")
	Expect(os.WriteFile(htpasswdPath, fmt.Appendf(nil, "admin:%s\n", string(hash)), 0600)).To(Succeed())

	dbPath := filepath.Join(tmpDir, ".db")
	Expect(os.MkdirAll(dbPath, 0750)).To(Succeed())

	binPath, err := filepath.Abs(".bin/incident-commander")
	if err != nil {
		binPath = ".bin/incident-commander"
	}
	if _, err := os.Stat(binPath); os.IsNotExist(err) {
		// Try from project root
		binPath, _ = filepath.Abs("../../.bin/incident-commander")
	}
	Expect(binPath).To(BeAnExistingFile(), "binary not found — run 'make dev' first")

	serverCmd = exec.Command(binPath, "serve",
		"--db", fmt.Sprintf("embedded://%s", dbPath),
		"--auth", "basic",
		"--htpasswd-file", htpasswdPath,
		"--oidc",
		"--public-endpoint", serverURL,
		"--httpPort", strconv.Itoa(serverPort),
		"--disable-postgrest",
		"--postgrest-uri", "",
		"--disable-operators",
		"--disable-kubernetes",
	)
	serverCmd.Stdout = ginkgo.GinkgoWriter
	serverCmd.Stderr = ginkgo.GinkgoWriter
	serverCmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	Expect(serverCmd.Start()).To(Succeed())

	Eventually(func() error {
		resp, err := http.Get(serverURL + "/health")
		if err != nil {
			return err
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("health returned %d", resp.StatusCode)
		}
		return nil
	}).WithTimeout(90 * time.Second).WithPolling(time.Second).Should(Succeed())

	chromectx, chromeCanc = chromedp.NewContext(context.Background())
})

var _ = ginkgo.AfterSuite(func() {
	if chromeCanc != nil {
		chromeCanc()
	}
	if serverCmd != nil && serverCmd.Process != nil {
		_ = syscall.Kill(-serverCmd.Process.Pid, syscall.SIGTERM)
		_ = serverCmd.Wait()
	}
})
