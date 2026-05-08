package cmd

import (
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/flanksource/incident-commander/auth/oidcclient"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("whoami command", func() {
	ginkgo.It("validates the context token against the whoami endpoint", func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			Expect(r.URL.Path).To(Equal("/auth/whoami"))
			Expect(r.Header.Get("Authorization")).To(Equal("Bearer test-token"))
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"payload":{"user":{"id":"u1","email":"me@example.com"},"roles":["admin"]}}`))
		}))
		defer server.Close()

		report := probeAuth(nil, &MCConfig{}, &MCContext{
			Name:   "test",
			Server: server.URL,
			Token:  "test-token",
		}, "", false)

		Expect(report.Status).To(Equal("ok"))
		Expect(report.Endpoint).To(Equal(server.URL + "/auth/whoami"))
		Expect(report.User["email"]).To(Equal("me@example.com"))
		Expect(report.Roles).To(Equal([]string{"admin"}))
	})

	ginkgo.It("falls back to api-prefixed whoami endpoints", func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/auth/whoami" {
				http.NotFound(w, r)
				return
			}
			Expect(r.URL.Path).To(Equal("/api/auth/whoami"))
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"payload":{"user":{"id":"u1"},"roles":[]}}`))
		}))
		defer server.Close()

		_, _, endpoint, _, err := callWhoami(nil, server.URL, "test-token")
		Expect(err).ToNot(HaveOccurred())
		Expect(endpoint).To(Equal(server.URL + "/api/auth/whoami"))
	})

	ginkgo.It("fails when database status is not ok", func() {
		err := whoamiStatusError(whoamiReport{
			Database: whoamiDatabase{Status: "error"},
			Auth:     whoamiAuth{Status: "ok"},
		})
		Expect(err).To(MatchError("whoami status failed: database=error auth=ok"))
	})

	ginkgo.It("fails when auth status is not ok", func() {
		err := whoamiStatusError(whoamiReport{
			Database: whoamiDatabase{Status: "ok"},
			Auth:     whoamiAuth{Status: "invalid"},
		})
		Expect(err).To(MatchError("whoami status failed: database=ok auth=invalid"))
	})

	ginkgo.It("prefers a valid stored OIDC token over a stale context JWT", func() {
		Expect(shouldUseStoredOIDCToken(
			"old.jwt.token",
			"previous.jwt.token",
			&oidcclient.Tokens{
				AccessToken: "fresh.jwt.token",
				ExpiresAt:   time.Now().Add(time.Hour),
			},
		)).To(BeTrue())
	})

	ginkgo.It("does not replace Mission Control access tokens with stored OIDC tokens", func() {
		Expect(shouldUseStoredOIDCToken(
			"password.salt.1.8192.1",
			"previous.jwt.token",
			&oidcclient.Tokens{
				AccessToken: "fresh.jwt.token",
				ExpiresAt:   time.Now().Add(time.Hour),
			},
		)).To(BeFalse())
	})
})
