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
	var oldOIDCLogin func(*cobra.Command, string, io.Writer) (*oidcclient.Tokens, string, error)

	ginkgo.BeforeEach(func() {
		oldOIDCLogin = oidcLogin
		configDir := ginkgo.GinkgoT().TempDir()
		ginkgo.GinkgoT().Setenv("HOME", configDir)
		ginkgo.GinkgoT().Setenv("XDG_CONFIG_HOME", configDir)
	})

	ginkgo.AfterEach(func() {
		oidcLogin = oldOIDCLogin
	})

	ginkgo.It("reuses a stored access token before starting OIDC login", func() {
		server := "http://mission-control.local"
		_, err := storeTokens(server, &oidcclient.Tokens{
			AccessToken: "stored-token",
			ExpiresAt:   time.Now().Add(time.Hour),
		})
		Expect(err).ToNot(HaveOccurred())

		oidcLogin = func(*cobra.Command, string, io.Writer) (*oidcclient.Tokens, string, error) {
			return nil, "", fmt.Errorf("unexpected login")
		}

		ctx := &MCContext{Name: "local", Server: server}
		Expect(EnsureContextToken(&cobra.Command{}, ctx, io.Discard)).To(Succeed())
		Expect(ctx.Token).To(Equal("stored-token"))
	})

	ginkgo.It("starts OIDC login when no usable token is available", func() {
		var stderr bytes.Buffer
		oidcLogin = func(_ *cobra.Command, server string, status io.Writer) (*oidcclient.Tokens, string, error) {
			Expect(server).To(Equal("http://mission-control.local"))
			fmt.Fprint(status, "login started")
			return &oidcclient.Tokens{AccessToken: "oauth-token"}, "", nil
		}

		ctx := &MCContext{Name: "local", Server: "http://mission-control.local"}
		Expect(EnsureContextToken(&cobra.Command{}, ctx, &stderr)).To(Succeed())
		Expect(ctx.Token).To(Equal("oauth-token"))
		Expect(stderr.String()).To(ContainSubstring("starting OIDC login"))
		Expect(stderr.String()).To(ContainSubstring("login started"))
	})

	ginkgo.It("refreshes expiring stored OIDC tokens for API clients", func() {
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
				Token:  "old-token",
			}},
		}
		Expect(SaveConfig(cfg)).To(Succeed())
		_, err := storeTokens(server.URL, &oidcclient.Tokens{
			AccessToken:  "old-token",
			RefreshToken: "refresh-token",
			ExpiresAt:    time.Now().Add(-time.Minute),
		})
		Expect(err).ToNot(HaveOccurred())

		mcCtx := cfg.GetContext("local")
		token, err := contextTokenProvider(mcCtx)(context.Background())

		Expect(err).ToNot(HaveOccurred())
		Expect(token).To(Equal("new-token"))
		Expect(mcCtx.Token).To(Equal("new-token"))
		Expect(tokenRequests).To(Equal(1))

		stored, err := LoadStoredTokens(server.URL)
		Expect(err).ToNot(HaveOccurred())
		Expect(stored.AccessToken).To(Equal("new-token"))
		Expect(stored.RefreshToken).To(Equal("next-refresh-token"))

		reloaded, err := LoadConfig()
		Expect(err).ToNot(HaveOccurred())
		Expect(reloaded.GetContext("local").Token).To(Equal("new-token"))
	})

	ginkgo.It("reuses frontend OIDC tokens for resolved /api contexts", func() {
		server := "http://mission-control.local"
		_, err := storeTokens(server, &oidcclient.Tokens{
			AccessToken: "stored-token",
			ExpiresAt:   time.Now().Add(time.Hour),
		})
		Expect(err).ToNot(HaveOccurred())

		oidcLogin = func(*cobra.Command, string, io.Writer) (*oidcclient.Tokens, string, error) {
			return nil, "", fmt.Errorf("unexpected login")
		}

		ctx := &MCContext{Name: "local", Server: server + "/api"}
		Expect(EnsureContextToken(&cobra.Command{}, ctx, io.Discard)).To(Succeed())
		Expect(ctx.Token).To(Equal("stored-token"))
	})

	ginkgo.It("starts OIDC login against the frontend URL for resolved /api contexts", func() {
		var loginServer string
		oidcLogin = func(_ *cobra.Command, server string, status io.Writer) (*oidcclient.Tokens, string, error) {
			loginServer = server
			return &oidcclient.Tokens{AccessToken: "oauth-token"}, "", nil
		}

		ctx := &MCContext{Name: "local", Server: "http://mission-control.local/api"}
		Expect(EnsureContextToken(&cobra.Command{}, ctx, io.Discard)).To(Succeed())
		Expect(ctx.Token).To(Equal("oauth-token"))
		Expect(loginServer).To(Equal("http://mission-control.local"))
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

	ginkgo.It("clears the current context when removing it", func() {
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
