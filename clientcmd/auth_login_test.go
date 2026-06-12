package clientcmd

import (
	"io"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/flanksource/incident-commander/auth/oidcclient"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spf13/cobra"
)

var _ = ginkgo.Describe("auth login", func() {
	var oldOIDCLogin func(*cobra.Command, string, io.Writer) (*oidcclient.Tokens, string, error)

	ginkgo.BeforeEach(func() {
		oldOIDCLogin = oidcLogin
		dir := ginkgo.GinkgoT().TempDir()
		ginkgo.GinkgoT().Setenv("HOME", dir)
		ginkgo.GinkgoT().Setenv("XDG_CONFIG_HOME", dir)
	})

	ginkgo.AfterEach(func() {
		oidcLogin = oldOIDCLogin
		loginServer = ""
		loginToken = ""
	})

	ginkgo.It("stores token contexts with the resolved frontend /api base", func() {
		server := authHealthServer(true)
		defer server.Close()
		loginServer = server.URL
		loginToken = "my-access-token"

		cmd := &cobra.Command{}
		cmd.SetOut(io.Discard)
		Expect(runAuthLogin(cmd, nil)).To(Succeed())

		stored, err := LoadStoredTokens(server.URL)
		Expect(err).ToNot(HaveOccurred())
		Expect(stored.AccessToken).To(Equal("my-access-token"))
		expectLoginContext(server.URL+"/api", "my-access-token")
	})

	ginkgo.It("stores token contexts with the direct backend base", func() {
		server := authHealthServer(false)
		defer server.Close()
		loginServer = server.URL + "/"
		loginToken = "tok2"

		cmd := &cobra.Command{}
		cmd.SetOut(io.Discard)
		Expect(runAuthLogin(cmd, nil)).To(Succeed())

		stored, err := LoadStoredTokens(server.URL)
		Expect(err).ToNot(HaveOccurred())
		Expect(stored.AccessToken).To(Equal("tok2"))
		expectLoginContext(server.URL, "tok2")
	})

	ginkgo.It("runs OIDC login against the frontend URL for resolved /api contexts", func() {
		server := authHealthServer(true)
		defer server.Close()
		loginServer = server.URL

		var gotLoginServer string
		oidcLogin = func(_ *cobra.Command, server string, _ io.Writer) (*oidcclient.Tokens, string, error) {
			gotLoginServer = server
			return &oidcclient.Tokens{AccessToken: "oauth-token", ExpiresAt: time.Now().Add(time.Hour)}, "tokens.json", nil
		}

		cmd := &cobra.Command{}
		cmd.SetOut(io.Discard)
		Expect(runAuthLogin(cmd, nil)).To(Succeed())

		Expect(gotLoginServer).To(Equal(server.URL))
		expectLoginContext(server.URL+"/api", "oauth-token")
	})
})

func authHealthServer(frontend bool) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/health":
			if frontend {
				_, _ = w.Write([]byte("OK"))
				return
			}
			http.NotFound(w, r)
		case "/health":
			_, _ = w.Write([]byte("OK"))
		default:
			http.NotFound(w, r)
		}
	}))
}

func expectLoginContext(serverURL, token string) {
	cfg, err := LoadConfig()
	Expect(err).ToNot(HaveOccurred())
	name := ServerToContextName(serverURL)
	Expect(cfg.CurrentContext).To(Equal(name))
	ctx := cfg.GetContext(name)
	Expect(ctx).ToNot(BeNil())
	Expect(ctx.Server).To(Equal(serverURL))
	Expect(ctx.Token).To(Equal(token))
}
