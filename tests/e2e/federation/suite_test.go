package federation

import (
	gocontext "context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/flanksource/deps"
	"github.com/flanksource/duty"
	dutyApi "github.com/flanksource/duty/api"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/tests/fixtures/dummy"
	"github.com/flanksource/duty/tests/setup"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"golang.org/x/crypto/bcrypt"

	"github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/auth"
	"github.com/flanksource/incident-commander/auth/signing"
	echoSrv "github.com/flanksource/incident-commander/echo"
	"github.com/flanksource/incident-commander/events"
	"github.com/flanksource/incident-commander/playbook"
	"github.com/flanksource/incident-commander/vars"
)

func TestFederationE2E(t *testing.T) {
	RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Federation E2E")
}

var (
	DefaultContext context.Context
	server         *httptest.Server
	postgrest      *exec.Cmd
)

func postgrestBinary() string {
	if bin := os.Getenv("POSTGREST_BIN"); bin != "" {
		return bin
	}

	binDir, err := filepath.Abs("../../../.bin")
	Expect(err).ToNot(HaveOccurred())

	result, err := deps.InstallWithContext(gocontext.Background(), "postgrest",
		dutyApi.DefaultConfig.Postgrest.Version,
		deps.WithBinDir(binDir),
		deps.WithOS(runtime.GOOS, dutyApi.DefaultConfig.Postgrest.Arch))
	Expect(err).ToNot(HaveOccurred(), "failed to install PostgREST. Set POSTGREST_BIN to use a local binary")
	return filepath.Join(result.BinDir, "postgrest")
}

var _ = ginkgo.BeforeSuite(func() {
	DefaultContext = setup.BeforeSuiteFn()
	api.SystemUserID = &dummy.JohnDoe.ID

	tmp := ginkgo.GinkgoT().TempDir()
	_, _, err := signing.Initialize(filepath.Join(tmp, "signing.key"))
	Expect(err).ToNot(HaveOccurred())

	jwk, err := signing.PublicJWK()
	Expect(err).ToNot(HaveOccurred())
	port := duty.FreePort()
	postgrest = exec.Command(postgrestBinary())
	postgrest.Env = append(os.Environ(),
		fmt.Sprintf("PGRST_SERVER_PORT=%d", port),
		"PGRST_DB_URI="+DefaultContext.Value("db_url").(string),
		"PGRST_DB_SCHEMA=public",
		"PGRST_DB_ANON_ROLE=",
		"PGRST_JWT_SECRET="+jwk,
		"PGRST_JWT_AUD="+string(signing.AudiencePostgREST),
		"PGRST_LOG_LEVEL=error",
	)
	postgrest.Stdout = ginkgo.GinkgoWriter
	postgrest.Stderr = ginkgo.GinkgoWriter
	Expect(postgrest.Start()).To(Succeed())

	postgrestURL := fmt.Sprintf("http://localhost:%d", port)
	Eventually(func() error {
		resp, err := http.Get(postgrestURL + "/")
		if err == nil {
			resp.Body.Close()
		}
		return err
	}, 30*time.Second, 200*time.Millisecond).Should(Succeed())
	dutyApi.DefaultConfig.Postgrest.URL = postgrestURL

	hash, err := bcrypt.GenerateFromPassword([]byte("admin"), bcrypt.DefaultCost)
	Expect(err).ToNot(HaveOccurred())
	htpasswd := filepath.Join(tmp, "htpasswd")
	Expect(os.WriteFile(htpasswd, []byte(auth.AdminName+":"+string(hash)+"\n"), 0o600)).To(Succeed())
	auth.HtpasswdFile = htpasswd
	vars.AuthMode = auth.Basic

	e := echoSrv.New(DefaultContext)
	events.StartConsumers(DefaultContext)
	Expect(playbook.StartPlaybookConsumers(DefaultContext)).To(Succeed())

	server = httptest.NewServer(e)
})

var _ = ginkgo.AfterSuite(func() {
	if server != nil {
		server.Close()
	}
	if postgrest != nil && postgrest.Process != nil {
		_ = postgrest.Process.Kill()
		_ = postgrest.Wait()
	}
	setup.AfterSuiteFn()
})
