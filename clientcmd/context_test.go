package clientcmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/flanksource/incident-commander/auth/oidcclient"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spf13/cobra"
)

var _ = ginkgo.Describe("context token resolution", func() {
	var oldOIDCLogin func(*cobra.Command, string, io.Writer) (*oidcclient.Tokens, error)

	ginkgo.BeforeEach(func() {
		oldOIDCLogin = oidcLogin
		configDir := ginkgo.GinkgoT().TempDir()
		ginkgo.GinkgoT().Setenv("HOME", configDir)
		ginkgo.GinkgoT().Setenv("XDG_CONFIG_HOME", configDir)
	})

	ginkgo.AfterEach(func() {
		oidcLogin = oldOIDCLogin
		contextAddName = ""
		contextAddServer = ""
		contextAddDB = ""
		contextAddToken = ""
		contextAddUse = false
	})

	ginkgo.It("uses embedded OIDC tokens before starting OIDC login", func() {
		server := "http://mission-control.local"
		oidcLogin = func(*cobra.Command, string, io.Writer) (*oidcclient.Tokens, error) {
			return nil, fmt.Errorf("unexpected login")
		}

		ctx := &MCContext{Name: "local", Server: server, OIDC: &oidcclient.Tokens{AccessToken: "stored-token"}}
		Expect(EnsureContextToken(&cobra.Command{}, ctx, io.Discard)).To(Succeed())
		Expect(ctx.AccessToken()).To(Equal("stored-token"))
	})

	ginkgo.It("starts OIDC login when no usable token is available", func() {
		var stderr bytes.Buffer
		oidcLogin = func(_ *cobra.Command, server string, status io.Writer) (*oidcclient.Tokens, error) {
			Expect(server).To(Equal("http://mission-control.local"))
			fmt.Fprint(status, "login started")
			return &oidcclient.Tokens{AccessToken: "oauth-token"}, nil
		}

		ctx := &MCContext{Name: "local", Server: "http://mission-control.local"}
		Expect(EnsureContextToken(&cobra.Command{}, ctx, &stderr)).To(Succeed())
		Expect(ctx.Token).To(BeEmpty())
		Expect(ctx.OIDC.AccessToken).To(Equal("oauth-token"))
		Expect(stderr.String()).To(ContainSubstring("starting OIDC login"))
		Expect(stderr.String()).To(ContainSubstring("login started"))
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
		token, err := contextTokenProvider(mcCtx)(context.Background())

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

	ginkgo.It("starts OIDC login against the frontend URL for resolved /api contexts", func() {
		var loginServer string
		oidcLogin = func(_ *cobra.Command, server string, status io.Writer) (*oidcclient.Tokens, error) {
			loginServer = server
			return &oidcclient.Tokens{AccessToken: "oauth-token"}, nil
		}

		ctx := &MCContext{Name: "local", Server: "http://mission-control.local/api"}
		Expect(EnsureContextToken(&cobra.Command{}, ctx, io.Discard)).To(Succeed())
		Expect(ctx.OIDC.AccessToken).To(Equal("oauth-token"))
		Expect(loginServer).To(Equal("http://mission-control.local"))
	})

	ginkgo.It("uses a manual context token when no OIDC tokens are configured", func() {
		ctx := &MCContext{Name: "user-b", Server: "http://mission-control.local", Token: "user-b.jwt.token"}
		token, err := resolveContextToken(ctx)

		Expect(err).ToNot(HaveOccurred())
		Expect(token).To(Equal("user-b.jwt.token"))
		Expect(ctx.Token).To(Equal("user-b.jwt.token"))
	})

	ginkgo.It("uses embedded OIDC tokens for same-server contexts", func() {
		server := "http://mission-control.local"
		cfg := &MCConfig{Contexts: []MCContext{
			{Name: "user-a", Server: server, OIDC: &oidcclient.Tokens{AccessToken: "user-a.jwt.token", ExpiresAt: time.Now().Add(time.Hour)}},
			{Name: "user-b", Server: server, OIDC: &oidcclient.Tokens{AccessToken: "user-b.jwt.token", ExpiresAt: time.Now().Add(time.Hour)}},
		}}

		ctx := cfg.GetContext("user-a")
		token, err := resolveContextToken(ctx)

		Expect(err).ToNot(HaveOccurred())
		Expect(token).To(Equal("user-a.jwt.token"))
		Expect(ctx.OIDC.AccessToken).To(Equal("user-a.jwt.token"))
	})

	ginkgo.It("starts a fresh OIDC login when adding another context for the same server", func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/health" {
				_, _ = w.Write([]byte("OK"))
				return
			}
			http.NotFound(w, r)
		}))
		defer server.Close()

		Expect(SaveConfig(&MCConfig{
			CurrentContext: "admin",
			Contexts: []MCContext{{
				Name:   "admin",
				Server: server.URL + "/api",
				OIDC: &oidcclient.Tokens{
					AccessToken:  "admin.jwt.token",
					RefreshToken: "admin-refresh-token",
					ExpiresAt:    time.Now().Add(time.Hour),
				},
			}},
		})).To(Succeed())

		var loginCount int
		oidcLogin = func(_ *cobra.Command, loginServer string, _ io.Writer) (*oidcclient.Tokens, error) {
			loginCount++
			Expect(loginServer).To(Equal(server.URL))
			return &oidcclient.Tokens{
				AccessToken:  "viewer.jwt.token",
				RefreshToken: "viewer-refresh-token",
				ExpiresAt:    time.Now().Add(time.Hour),
			}, nil
		}

		contextAddName = "viewer"
		contextAddServer = server.URL
		contextAddUse = true

		cmd := &cobra.Command{}
		cmd.SetOut(io.Discard)
		cmd.SetErr(io.Discard)
		cmd.Flags().String("server", "", "")
		cmd.Flags().String("token", "", "")
		cmd.Flags().String("db-url", "", "")
		Expect(cmd.Flags().Set("server", server.URL)).To(Succeed())

		Expect(contextAddCmd.RunE(cmd, nil)).To(Succeed())

		Expect(loginCount).To(Equal(1))
		cfg, err := LoadConfig()
		Expect(err).ToNot(HaveOccurred())
		Expect(cfg.CurrentContext).To(Equal("viewer"))
		Expect(cfg.GetContext("admin").OIDC.AccessToken).To(Equal("admin.jwt.token"))
		Expect(cfg.GetContext("viewer").Token).To(BeEmpty())
		Expect(cfg.GetContext("viewer").OIDC.AccessToken).To(Equal("viewer.jwt.token"))
	})
})

var _ = ginkgo.Describe("context remove", func() {
	ginkgo.BeforeEach(func() {
		configDir := ginkgo.GinkgoT().TempDir()
		ginkgo.GinkgoT().Setenv("HOME", configDir)
		ginkgo.GinkgoT().Setenv("XDG_CONFIG_HOME", configDir)
	})

	ginkgo.It("removes the named context", func() {
		Expect(SaveConfig(&MCConfig{
			CurrentContext: "local",
			Contexts: []MCContext{
				{Name: "local", Server: "http://local"},
				{Name: "beta", Server: "http://beta"},
			},
		})).To(Succeed())

		var stdout bytes.Buffer
		cmd := &cobra.Command{}
		cmd.SetOut(&stdout)

		Expect(contextRemoveCmd.RunE(cmd, []string{"beta"})).To(Succeed())

		cfg, err := LoadConfig()
		Expect(err).ToNot(HaveOccurred())
		Expect(cfg.GetContext("beta")).To(BeNil())
		Expect(cfg.GetContext("local")).ToNot(BeNil())
		Expect(cfg.CurrentContext).To(Equal("local"))
		Expect(stdout.String()).To(ContainSubstring(`Removed context "beta"`))
	})

	ginkgo.It("switches to the only remaining context when removing the current context", func() {
		Expect(SaveConfig(&MCConfig{
			CurrentContext: "local",
			Contexts: []MCContext{
				{Name: "local", Server: "http://local"},
				{Name: "beta", Server: "http://beta"},
			},
		})).To(Succeed())

		var stdout bytes.Buffer
		cmd := &cobra.Command{}
		cmd.SetOut(&stdout)
		Expect(contextRemoveCmd.RunE(cmd, []string{"local"})).To(Succeed())

		cfg, err := LoadConfig()
		Expect(err).ToNot(HaveOccurred())
		Expect(cfg.Contexts).To(HaveLen(1))
		Expect(cfg.GetContext("beta")).ToNot(BeNil())
		Expect(cfg.CurrentContext).To(Equal("beta"))
		Expect(stdout.String()).To(ContainSubstring(`Switched to context "beta"`))
	})

	ginkgo.It("clears the current context when no contexts remain", func() {
		Expect(SaveConfig(&MCConfig{
			CurrentContext: "local",
			Contexts:       []MCContext{{Name: "local", Server: "http://local"}},
		})).To(Succeed())

		cmd := &cobra.Command{}
		cmd.SetOut(io.Discard)
		Expect(contextRemoveCmd.RunE(cmd, []string{"local"})).To(Succeed())

		cfg, err := LoadConfig()
		Expect(err).ToNot(HaveOccurred())
		Expect(cfg.Contexts).To(BeEmpty())
		Expect(cfg.CurrentContext).To(BeEmpty())
	})

	ginkgo.It("returns an error for missing contexts", func() {
		Expect(SaveConfig(&MCConfig{
			Contexts: []MCContext{{Name: "local", Server: "http://local"}},
		})).To(Succeed())

		err := contextRemoveCmd.RunE(&cobra.Command{}, []string{"missing"})

		Expect(err).To(MatchError(`context "missing" not found`))
	})
})

var _ = ginkgo.Describe("API base resolution", func() {
	ginkgo.BeforeEach(func() {
		configDir := ginkgo.GinkgoT().TempDir()
		ginkgo.GinkgoT().Setenv("HOME", configDir)
		ginkgo.GinkgoT().Setenv("XDG_CONFIG_HOME", configDir)
	})

	ginkgo.AfterEach(func() {
		contextAddName = ""
		contextAddServer = ""
		contextAddDB = ""
		contextAddToken = ""
		contextAddUse = false
	})

	ginkgo.It("prefers the frontend /api health endpoint", func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/health":
				_, _ = w.Write([]byte("OK"))
			case "/health":
				_, _ = w.Write([]byte("frontend OK"))
			default:
				http.NotFound(w, r)
			}
		}))
		defer server.Close()

		resolved, err := ResolveAPIBase(server.URL)

		Expect(err).ToNot(HaveOccurred())
		Expect(resolved).To(Equal(server.URL + "/api"))
	})

	ginkgo.It("falls back to direct backend health", func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/health":
				_, _ = w.Write([]byte("OK"))
			default:
				http.NotFound(w, r)
			}
		}))
		defer server.Close()

		resolved, err := ResolveAPIBase(server.URL)

		Expect(err).ToNot(HaveOccurred())
		Expect(resolved).To(Equal(server.URL))
	})

	ginkgo.It("stores the resolved API base for context add", func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/health" {
				_, _ = w.Write([]byte("OK"))
				return
			}
			http.NotFound(w, r)
		}))
		defer server.Close()

		contextAddName = "beta"
		contextAddServer = server.URL
		contextAddToken = "token"
		contextAddUse = true

		cmd := &cobra.Command{}
		cmd.SetOut(io.Discard)
		cmd.Flags().String("server", "", "")
		cmd.Flags().String("token", "", "")
		cmd.Flags().String("db-url", "", "")
		Expect(cmd.Flags().Set("server", server.URL)).To(Succeed())
		Expect(cmd.Flags().Set("token", "token")).To(Succeed())

		Expect(contextAddCmd.RunE(cmd, nil)).To(Succeed())

		cfg, err := LoadConfig()
		Expect(err).ToNot(HaveOccurred())
		Expect(cfg.CurrentContext).To(Equal("beta"))
		ctx := cfg.GetContext("beta")
		Expect(ctx.Server).To(Equal(server.URL + "/api"))
	})
})
