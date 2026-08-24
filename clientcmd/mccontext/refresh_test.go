package mccontext

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/flanksource/incident-commander/auth/oidcclient"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// tokenEndpoint counts grant requests so the specs can assert on the one thing
// that matters for a single-use credential: how many times it was spent.
type tokenEndpoint struct {
	server   *httptest.Server
	mu       sync.Mutex
	requests []string
	respond  func(w http.ResponseWriter, presented string)
}

func newTokenEndpoint(respond func(w http.ResponseWriter, presented string)) *tokenEndpoint {
	e := &tokenEndpoint{respond: respond}
	e.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		Expect(r.URL.Path).To(Equal("/token"))
		Expect(r.ParseForm()).To(Succeed())
		Expect(r.Form.Get("grant_type")).To(Equal("refresh_token"))

		presented := r.Form.Get("refresh_token")
		e.mu.Lock()
		e.requests = append(e.requests, presented)
		e.mu.Unlock()

		e.respond(w, presented)
	}))
	return e
}

func (e *tokenEndpoint) count() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.requests)
}

func (e *tokenEndpoint) presented() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string{}, e.requests...)
}

func writeRotatedTokens(w http.ResponseWriter, presented string) {
	w.Header().Set("Content-Type", "application/json")
	Expect(json.NewEncoder(w).Encode(map[string]any{
		"access_token":  "access-after-" + presented,
		"refresh_token": "rotated-" + presented,
		"id_token":      "id-token",
		"expires_in":    3600,
	})).To(Succeed())
}

// seedExpiringContext stores a context whose access token has already expired,
// with the token endpoint pinned so no discovery round-trip is attempted.
func seedExpiringContext(name, tokenEndpointURL, refreshToken string) *MCContext {
	Expect(SaveConfig(&MCConfig{
		CurrentContext:  name,
		CredentialStore: "file",
		Contexts: []MCContext{{
			Name:      name,
			Server:    "http://mission-control.local/api",
			Endpoints: &oidcclient.Discovery{TokenEndpoint: tokenEndpointURL},
			OIDC: &oidcclient.Tokens{
				AccessToken:  "expired-access-token",
				RefreshToken: refreshToken,
				ExpiresAt:    time.Now().Add(-time.Minute),
			},
		}},
	})).To(Succeed())

	cfg, err := LoadConfig()
	Expect(err).ToNot(HaveOccurred())
	mcCtx := cfg.GetContext(name)
	Expect(mcCtx).ToNot(BeNil())
	Expect(mcCtx.OIDC.RefreshToken).To(Equal(refreshToken))
	return mcCtx
}

func storedCredential(name string) *MCContext {
	cfg, err := LoadConfig()
	Expect(err).ToNot(HaveOccurred())
	ctx := cfg.GetContext(name)
	Expect(ctx).ToNot(BeNil())
	return ctx
}

var _ = ginkgo.Describe("OIDC refresh", func() {
	var dir string

	ginkgo.BeforeEach(func() {
		dir = ginkgo.GinkgoT().TempDir()
		ginkgo.GinkgoT().Setenv("HOME", dir)
		ginkgo.GinkgoT().Setenv("XDG_CONFIG_HOME", dir)
	})

	ginkgo.It("persists the rotated refresh token", func() {
		endpoint := newTokenEndpoint(writeRotatedTokens)
		defer endpoint.server.Close()

		mcCtx := seedExpiringContext("beta", endpoint.server.URL+"/token", "refresh-1")
		Expect(RefreshContextToken(mcCtx)).To(Succeed())

		Expect(endpoint.count()).To(Equal(1))
		Expect(mcCtx.OIDC.AccessToken).To(Equal("access-after-refresh-1"))
		Expect(mcCtx.OIDC.RefreshToken).To(Equal("rotated-refresh-1"))

		stored := storedCredential("beta")
		Expect(stored.OIDC.AccessToken).To(Equal("access-after-refresh-1"))
		Expect(stored.OIDC.RefreshToken).To(Equal("rotated-refresh-1"))
		Expect(stored.NeedsReauth).To(BeEmpty())
	})

	ginkgo.It("keeps secrets out of config.json", func() {
		endpoint := newTokenEndpoint(writeRotatedTokens)
		defer endpoint.server.Close()

		mcCtx := seedExpiringContext("beta", endpoint.server.URL+"/token", "refresh-1")
		Expect(RefreshContextToken(mcCtx)).To(Succeed())

		config, err := os.ReadFile(configPath())
		Expect(err).ToNot(HaveOccurred())
		Expect(string(config)).ToNot(ContainSubstring("refresh-1"))
		Expect(string(config)).ToNot(ContainSubstring("rotated-refresh-1"))
		Expect(string(config)).ToNot(ContainSubstring("access-after-refresh-1"))

		creds, err := os.ReadFile(filepath.Join(configDir(), "credentials.json"))
		Expect(err).ToNot(HaveOccurred())
		Expect(string(creds)).To(ContainSubstring("rotated-refresh-1"))

		info, err := os.Stat(filepath.Join(configDir(), "credentials.json"))
		Expect(err).ToNot(HaveOccurred())
		Expect(info.Mode().Perm()).To(Equal(os.FileMode(0600)))
	})

	ginkgo.It("does not send a grant request when the credential store is unwritable", func() {
		if os.Geteuid() == 0 {
			ginkgo.Skip("root bypasses directory permissions")
		}

		endpoint := newTokenEndpoint(writeRotatedTokens)
		defer endpoint.server.Close()

		mcCtx := seedExpiringContext("beta", endpoint.server.URL+"/token", "refresh-1")

		Expect(os.Chmod(configDir(), 0500)).To(Succeed())
		defer func() { _ = os.Chmod(configDir(), 0700) }()

		err := RefreshContextToken(mcCtx)

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring(`refusing to rotate the refresh token for context "beta"`))
		Expect(err.Error()).To(ContainSubstring("credential store (file) is not writable"))
		Expect(endpoint.count()).To(BeZero(), "the refresh token must not be spent when it cannot be stored")
	})

	ginkgo.It("fails loudly when the rotated token cannot be saved", func() {
		if os.Geteuid() == 0 {
			ginkgo.Skip("root bypasses directory permissions")
		}

		// The store goes read-only between the pre-flight check and the save —
		// the mount-went-away race the loud error exists for.
		endpoint := newTokenEndpoint(func(w http.ResponseWriter, presented string) {
			Expect(os.Chmod(configDir(), 0500)).To(Succeed())
			writeRotatedTokens(w, presented)
		})
		defer endpoint.server.Close()

		mcCtx := seedExpiringContext("beta", endpoint.server.URL+"/token", "refresh-1")

		err := RefreshContextToken(mcCtx)
		Expect(os.Chmod(configDir(), 0700)).To(Succeed())

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring(`the refresh token for context "beta" was rotated but could not be saved`))
		Expect(err.Error()).To(ContainSubstring("so it is now lost"))
	})

	ginkgo.It("marks the context for re-authentication on invalid_grant", func() {
		endpoint := newTokenEndpoint(func(w http.ResponseWriter, _ string) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
		})
		defer endpoint.server.Close()

		mcCtx := seedExpiringContext("beta", endpoint.server.URL+"/token", "spent-token")

		err := RefreshContextToken(mcCtx)

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring(`context "beta" needs re-authentication (refresh token rejected)`))
		Expect(err.Error()).To(ContainSubstring("auth login --server http://mission-control.local/api"))
		Expect(endpoint.count()).To(Equal(1), "a rejected single-use token must never be retried")

		Expect(mcCtx.HasAuth()).To(BeFalse())
		stored := storedCredential("beta")
		Expect(stored.NeedsReauth).To(Equal("refresh token rejected"))
		Expect(stored.OIDC).To(BeNil())
		Expect(stored.HasAuth()).To(BeFalse())
	})

	ginkgo.It("refuses to spend a credential already marked for re-authentication", func() {
		endpoint := newTokenEndpoint(writeRotatedTokens)
		defer endpoint.server.Close()

		mcCtx := seedExpiringContext("beta", endpoint.server.URL+"/token", "refresh-1")
		mcCtx.NeedsReauth = "refresh token rejected"

		_, err := ResolveContextToken(mcCtx)

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("needs re-authentication"))
		Expect(endpoint.count()).To(BeZero())
	})

	ginkgo.It("leaves the stored token untouched when the outcome is unknown", func() {
		endpoint := newTokenEndpoint(func(w http.ResponseWriter, _ string) {
			http.Error(w, "gateway timeout", http.StatusGatewayTimeout)
		})
		defer endpoint.server.Close()

		mcCtx := seedExpiringContext("beta", endpoint.server.URL+"/token", "refresh-1")

		err := RefreshContextToken(mcCtx)

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("refresh OIDC token for http://mission-control.local/api"))
		Expect(endpoint.count()).To(Equal(1), "an unknown outcome must not be retried")

		stored := storedCredential("beta")
		Expect(stored.NeedsReauth).To(BeEmpty())
		Expect(stored.OIDC.RefreshToken).To(Equal("refresh-1"))
	})

	ginkgo.It("spends the refresh token once when two refreshes race", func() {
		release := make(chan struct{})
		endpoint := newTokenEndpoint(func(w http.ResponseWriter, presented string) {
			<-release
			writeRotatedTokens(w, presented)
		})
		defer endpoint.server.Close()

		seedExpiringContext("beta", endpoint.server.URL+"/token", "refresh-1")

		results := make([]error, 2)
		contexts := make([]*MCContext, 2)
		var wg sync.WaitGroup
		for i := range results {
			wg.Add(1)
			go func() {
				defer ginkgo.GinkgoRecover()
				defer wg.Done()
				cfg, err := LoadConfig()
				if err != nil {
					results[i] = err
					return
				}
				contexts[i] = cfg.GetContext("beta")
				results[i] = RefreshContextToken(contexts[i])
			}()
		}

		Eventually(endpoint.count).Should(Equal(1))
		close(release)
		wg.Wait()

		Expect(results[0]).ToNot(HaveOccurred())
		Expect(results[1]).ToNot(HaveOccurred())
		Expect(endpoint.presented()).To(Equal([]string{"refresh-1"}), "the loser must not re-spend the token")
		for _, ctx := range contexts {
			Expect(ctx.OIDC.AccessToken).To(Equal("access-after-refresh-1"))
			Expect(ctx.OIDC.RefreshToken).To(Equal("rotated-refresh-1"))
		}
	})

	ginkgo.It("refreshes expiring embedded OIDC tokens for API clients", func() {
		var tokenRequests int
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/.well-known/openid-configuration":
				w.Header().Set("Content-Type", "application/json")
				Expect(json.NewEncoder(w).Encode(map[string]string{
					"authorization_endpoint": r.Host + "/authorize",
					"token_endpoint":         "http://" + r.Host + "/token",
					"userinfo_endpoint":      r.Host + "/userinfo",
				})).To(Succeed())
			case "/token":
				tokenRequests++
				Expect(r.ParseForm()).To(Succeed())
				Expect(r.Form.Get("grant_type")).To(Equal("refresh_token"))
				Expect(r.Form.Get("refresh_token")).To(Equal("refresh-token"))
				w.Header().Set("Content-Type", "application/json")
				Expect(json.NewEncoder(w).Encode(map[string]any{
					"access_token":  "new-token",
					"refresh_token": "next-refresh-token",
					"id_token":      "id-token",
					"expires_in":    3600,
				})).To(Succeed())
			default:
				http.NotFound(w, r)
			}
		}))
		defer server.Close()

		cfg := &MCConfig{
			CurrentContext: "local",
			Contexts: []MCContext{{
				Name:   "local",
				Server: server.URL,
				OIDC: &oidcclient.Tokens{
					AccessToken:  "old-token",
					RefreshToken: "refresh-token",
					ExpiresAt:    time.Now().Add(-time.Minute),
				},
			}},
		}
		Expect(SaveConfig(cfg)).To(Succeed())

		mcCtx := cfg.GetContext("local")
		token, err := ContextTokenProvider(mcCtx)(context.Background())

		Expect(err).ToNot(HaveOccurred())
		Expect(token).To(Equal("new-token"))
		Expect(mcCtx.Token).To(BeEmpty())
		Expect(mcCtx.OIDC.AccessToken).To(Equal("new-token"))
		Expect(mcCtx.OIDC.RefreshToken).To(Equal("next-refresh-token"))
		Expect(tokenRequests).To(Equal(1))

		reloaded, err := LoadConfig()
		Expect(err).ToNot(HaveOccurred())
		Expect(reloaded.GetContext("local").Token).To(BeEmpty())
		Expect(reloaded.GetContext("local").OIDC.AccessToken).To(Equal("new-token"))
	})

})
