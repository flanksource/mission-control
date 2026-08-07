package clientcmd

import (
	"context"
	"net/http"
	"net/http/httptest"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type fakeLocalWhoami struct {
	database WhoamiDatabase
}

func (f fakeLocalWhoami) DefaultDBConnection() string {
	return ""
}

func (f fakeLocalWhoami) ProbeDatabase(string) WhoamiDatabase {
	return f.database
}

func (f fakeLocalWhoami) InspectAccessToken(string, string) *AccessTokenStatus {
	return nil
}

var _ = ginkgo.Describe("whoami command", func() {
	ginkgo.It("validates the context token against the whoami endpoint", func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			Expect(r.URL.Path).To(Equal("/auth/whoami"))
			Expect(r.Header.Get("Authorization")).To(Equal("Bearer test-token"))
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"payload":{"user":{"id":"u1","email":"me@example.com"},"roles":["admin"]}}`))
		}))
		defer server.Close()

		report := probeAuth(context.TODO(), &MCContext{
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

		_, _, endpoint, _, err := callWhoami(context.TODO(), server.URL, "test-token")
		Expect(err).ToNot(HaveOccurred())
		Expect(endpoint).To(Equal(server.URL + "/api/auth/whoami"))
	})

	ginkgo.It("names every candidate when all of them fail at the network level", func() {
		// Blocked egress — the sandbox case — fails every candidate with no HTTP status. Reporting
		// only the last one made it look like faro had built the wrong URL. An authoritative HTTP
		// answer still short-circuits, so this must be a transport failure rather than a 4xx.
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		unreachable := server.URL
		server.Close()

		_, _, _, _, err := callWhoami(context.TODO(), unreachable+"/api", "test-token")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring(unreachable + "/api/auth/whoami"))
		Expect(err.Error()).To(ContainSubstring(unreachable + "/auth/whoami"))
	})

	ginkgo.It("keeps an authoritative HTTP rejection as the reported error", func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "tenant disabled", http.StatusForbidden)
		}))
		defer server.Close()

		_, _, endpoint, statusCode, err := callWhoami(context.TODO(), server.URL+"/api", "test-token")
		Expect(err).To(MatchError(ContainSubstring("tenant disabled")))
		Expect(statusCode).To(Equal(http.StatusForbidden))
		Expect(endpoint).To(Equal(server.URL + "/api/auth/whoami"))
	})

	ginkgo.It("does not let a slow candidate consume the next candidate's timeout", func() {
		restore := whoamiEndpointTimeout
		whoamiEndpointTimeout = 200 * time.Millisecond
		defer func() { whoamiEndpointTimeout = restore }()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/auth/whoami" {
				time.Sleep(whoamiEndpointTimeout + 2*time.Second)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"payload":{"user":{"id":"u1"},"roles":[]}}`))
		}))
		defer server.Close()

		_, _, endpoint, _, err := callWhoami(context.TODO(), server.URL+"/api", "test-token")
		Expect(err).ToNot(HaveOccurred())
		Expect(endpoint).To(Equal(server.URL + "/auth/whoami"))
	})

	ginkgo.It("succeeds when no database is configured and auth is ok", func() {
		err := whoamiStatusError(whoamiReport{
			Database: whoamiDatabase{Status: "skipped", Error: "no database configured"},
			Auth:     whoamiAuth{Status: "ok"},
		})
		Expect(err).ToNot(HaveOccurred())
	})

	ginkgo.It("fails when a configured database is unreachable", func() {
		err := whoamiStatusError(whoamiReport{
			Database: whoamiDatabase{Configured: true, Status: "error", Error: "connection refused"},
			Auth:     whoamiAuth{Status: "ok"},
		})
		Expect(err).To(MatchError("whoami status failed: database=error (connection refused)"))
	})

	ginkgo.It("succeeds when the database probe is unavailable but remote auth works", func() {
		err := whoamiStatusError(whoamiReport{
			Database: whoamiDatabase{Status: "skipped"},
			Auth:     whoamiAuth{Status: "ok"},
		})
		Expect(err).ToNot(HaveOccurred())
	})

	ginkgo.It("skips configured database probes in the lean client", func() {
		previous := LocalWhoami
		LocalWhoami = nil
		ginkgo.DeferCleanup(func() {
			LocalWhoami = previous
		})

		report := probeDatabase("postgres://user:password@database.example.com/mission_control")
		Expect(report.Configured).To(BeTrue())
		Expect(report.Status).To(Equal("skipped"))
		Expect(report.URL).ToNot(ContainSubstring("password"))
		Expect(report.Error).To(Equal("database probing is unavailable in this binary"))
	})

	ginkgo.It("preserves the redacted database URL from local probes", func() {
		previous := LocalWhoami
		LocalWhoami = fakeLocalWhoami{database: WhoamiDatabase{
			Status:   "ok",
			Database: "mission_control",
			User:     "mission-control",
		}}
		ginkgo.DeferCleanup(func() {
			LocalWhoami = previous
		})

		report := probeDatabase("postgres://user:password@database.example.com/mission_control")

		Expect(report.Configured).To(BeTrue())
		Expect(report.Status).To(Equal("ok"))
		Expect(report.Database).To(Equal("mission_control"))
		Expect(report.User).To(Equal("mission-control"))
		Expect(report.URL).To(ContainSubstring("database.example.com/mission_control"))
		Expect(report.URL).ToNot(ContainSubstring("password"))
	})

	ginkgo.It("fails when auth status is not ok", func() {
		err := whoamiStatusError(whoamiReport{
			Database: whoamiDatabase{Status: "ok"},
			Auth:     whoamiAuth{Status: "invalid", Error: "token expired"},
		})
		Expect(err).To(MatchError("whoami status failed: auth=invalid (token expired)"))
	})

	ginkgo.It("fails when no token is configured", func() {
		err := whoamiStatusError(whoamiReport{
			Database: whoamiDatabase{Status: "skipped", Error: "no database configured"},
			Auth:     whoamiAuth{Status: "skipped", Error: "no token configured"},
		})
		Expect(err).To(MatchError("whoami status failed: auth=skipped (no token configured)"))
	})

	ginkgo.It("reports every failing probe", func() {
		err := whoamiStatusError(whoamiReport{
			Database: whoamiDatabase{Configured: true, Status: "error", Error: "connection refused"},
			Auth:     whoamiAuth{Status: "invalid", Error: "token expired"},
		})
		Expect(err).To(MatchError("whoami status failed: database=error (connection refused) auth=invalid (token expired)"))
	})
})
