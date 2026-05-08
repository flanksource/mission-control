package cmd

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
		ginkgo.GinkgoT().Setenv("HOME", ginkgo.GinkgoT().TempDir())
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
		Expect(ensureContextToken(&cobra.Command{}, ctx, io.Discard)).To(Succeed())
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
		Expect(ensureContextToken(&cobra.Command{}, ctx, &stderr)).To(Succeed())
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

		stored, err := loadStoredTokens(server.URL)
		Expect(err).ToNot(HaveOccurred())
		Expect(stored.AccessToken).To(Equal("new-token"))
		Expect(stored.RefreshToken).To(Equal("next-refresh-token"))

		reloaded, err := LoadConfig()
		Expect(err).ToNot(HaveOccurred())
		Expect(reloaded.GetContext("local").Token).To(Equal("new-token"))
	})
})
