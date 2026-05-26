package plugins

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

	dutyContext "github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/tests/setup"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/auth"
	"github.com/google/uuid"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"golang.org/x/crypto/bcrypt"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

var (
	defaultContext dutyContext.Context
	serverURL      string
	serverCmd      *exec.Cmd
	testEnv        *envtest.Environment
	k8sClient      client.Client
	tmpDir         string
	pluginBinDir   string

	goodUser     models.Person
	noInvokeUser models.Person
	noConfigUser models.Person
)

func TestPluginsE2E(t *testing.T) {
	RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Plugins E2E")
}

var _ = ginkgo.BeforeSuite(func() {
	Expect(os.Unsetenv(setup.DUTY_DB_URL)).To(Succeed())
	defaultContext = setup.BeforeSuiteFn()

	goodUser = createPerson("Plugin Good", "plugin-good@test.local")
	noInvokeUser = createPerson("Plugin No Invoke", "plugin-no-invoke@test.local")
	noConfigUser = createPerson("Plugin No Config", "plugin-no-config@test.local")

	tmpDir = ginkgo.GinkgoT().TempDir()
	pluginBinDir = filepath.Join(tmpDir, "plugins")
	Expect(os.MkdirAll(pluginBinDir, 0750)).To(Succeed())
	buildHasherPlugin(pluginBinDir)

	kubeconfigPath := startEnvtest()
	startMissionControl(kubeconfigPath)
})

var _ = ginkgo.AfterSuite(func() {
	if serverCmd != nil && serverCmd.Process != nil {
		_ = syscall.Kill(-serverCmd.Process.Pid, syscall.SIGTERM)
		_ = serverCmd.Wait()
	}
	if testEnv != nil {
		_ = testEnv.Stop()
	}
	setup.AfterSuiteFn()
})

func createPerson(name, email string) models.Person {
	person := models.Person{ID: uuid.New(), Name: name, Email: email}
	Expect(defaultContext.DB().Create(&person).Error).To(Succeed())
	return person
}

func buildHasherPlugin(binDir string) {
	repoRoot := findRepoRoot()
	cmd := exec.Command("go", "build", "-o", filepath.Join(binDir, "hasher"), "./tests/e2e/plugins/testdata/plugins/hasher")
	cmd.Dir = repoRoot
	cmd.Stdout = ginkgo.GinkgoWriter
	cmd.Stderr = ginkgo.GinkgoWriter
	Expect(cmd.Run()).To(Succeed())
}

func startEnvtest() string {
	repoRoot := findRepoRoot()
	scheme := runtime.NewScheme()
	Expect(clientgoscheme.AddToScheme(scheme)).To(Succeed())
	Expect(v1.AddToScheme(scheme)).To(Succeed())

	binaryAssetsDir, err := envtest.SetupEnvtestDefaultBinaryAssetsDirectory()
	Expect(err).ToNot(HaveOccurred())
	testEnv = &envtest.Environment{
		Scheme:                scheme,
		CRDDirectoryPaths:     []string{filepath.Join(repoRoot, "config", "crds")},
		ErrorIfCRDPathMissing: true,
		DownloadBinaryAssets:  true,
		BinaryAssetsDirectory: binaryAssetsDir,
	}

	cfg, err := testEnv.Start()
	Expect(err).ToNot(HaveOccurred())
	Expect(cfg).ToNot(BeNil())

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme})
	Expect(err).ToNot(HaveOccurred())
	ensureNamespace("default")

	kubeconfigPath := filepath.Join(tmpDir, "kubeconfig")
	Expect(os.WriteFile(kubeconfigPath, testEnv.KubeConfig, 0600)).To(Succeed())
	return kubeconfigPath
}

func ensureNamespace(name string) {
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}}
	err := k8sClient.Create(context.Background(), ns)
	if client.IgnoreAlreadyExists(err) != nil {
		Expect(err).ToNot(HaveOccurred())
	}
}

func startMissionControl(kubeconfigPath string) {
	htpasswdPath := filepath.Join(tmpDir, "htpasswd")
	Expect(os.WriteFile(htpasswdPath, []byte(htpasswdContent()), 0600)).To(Succeed())

	serverPort := freePort()
	serverURL = fmt.Sprintf("http://127.0.0.1:%d", serverPort)

	binPath := filepath.Join(findRepoRoot(), ".bin", "incident-commander")
	Expect(binPath).To(BeAnExistingFile(), "binary not found — run `make dev` first")

	dbURL := defaultContext.Value("db_url").(string)
	serverCmd = exec.Command(binPath, "serve",
		"--db", dbURL,
		"--auth", "basic",
		"--htpasswd-file", htpasswdPath,
		"--frontend-url", serverURL,
		"--public-endpoint", serverURL,
		"--httpPort", strconv.Itoa(serverPort),
		"--disable-postgrest",
		"--postgrest-uri", "",
	)
	serverCmd.Stdout = ginkgo.GinkgoWriter
	serverCmd.Stderr = ginkgo.GinkgoWriter
	serverCmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	serverCmd.Env = append(os.Environ(),
		"KUBECONFIG="+kubeconfigPath,
		"MISSION_CONTROL_PLUGIN_PATH="+pluginBinDir,
	)
	Expect(serverCmd.Start()).To(Succeed())

	Eventually(func() error {
		resp, err := http.Get(serverURL + "/health")
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("health returned %d", resp.StatusCode)
		}
		return nil
	}).WithTimeout(90 * time.Second).WithPolling(time.Second).Should(Succeed())
}

func htpasswdContent() string {
	users := []struct {
		username string
		password string
	}{
		{auth.AdminEmail, auth.DefaultAdminPassword},
		{goodUser.Email, "test-password"},
		{noInvokeUser.Email, "test-password"},
		{noConfigUser.Email, "test-password"},
	}

	var out string
	for _, user := range users {
		hash, err := bcrypt.GenerateFromPassword([]byte(user.password), bcrypt.DefaultCost)
		Expect(err).ToNot(HaveOccurred())
		out += fmt.Sprintf("%s:%s\n", user.username, string(hash))
	}
	return out
}

func freePort() int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	Expect(err).ToNot(HaveOccurred())
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func findRepoRoot() string {
	dir, err := os.Getwd()
	Expect(err).ToNot(HaveOccurred())
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		Expect(parent).ToNot(Equal(dir), "could not find repo root from %s", dir)
		dir = parent
	}
}
