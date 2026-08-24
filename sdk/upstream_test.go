package sdk

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/upstream"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("upstream push", func() {
	analyses := func(n int) []models.ConfigAnalysis {
		out := make([]models.ConfigAnalysis, n)
		for i := range out {
			out[i] = models.ConfigAnalysis{Analyzer: "tls-version", Severity: models.SeverityHigh}
		}
		return out
	}

	ginkgo.It("posts the batch to /upstream/push under the agent's name", func() {
		var got upstream.PushData
		var gotAgent, gotMethod, gotPath string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotMethod, gotPath = r.Method, r.URL.Path
			gotAgent = r.URL.Query().Get(upstream.AgentNameQueryParam)
			Expect(json.NewDecoder(r.Body).Decode(&got)).To(Succeed())
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		err := New(server.URL, "tok").PushUpstream(context.Background(), "recon",
			&upstream.PushData{ConfigAnalysis: analyses(2)})

		Expect(err).ToNot(HaveOccurred())
		Expect(gotMethod).To(Equal(http.MethodPost))
		Expect(gotPath).To(Equal("/upstream/push"))
		Expect(gotAgent).To(Equal("recon"))
		Expect(got.ConfigAnalysis).To(HaveLen(2))
		Expect(got.ConfigAnalysis[0].Analyzer).To(Equal("tls-version"))
	})

	// An empty batch is the normal outcome of a scan whose findings were all
	// filtered out. Sending it would create the agent and record a push that
	// carried nothing.
	ginkgo.It("sends nothing when the batch is empty", func() {
		var requests int
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			requests++
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		client := New(server.URL, "tok")
		Expect(client.PushUpstream(context.Background(), "recon", &upstream.PushData{})).To(Succeed())
		Expect(client.PushUpstream(context.Background(), "recon", nil)).To(Succeed())
		Expect(requests).To(Equal(0))
	})

	ginkgo.It("refuses to push without an agent name", func() {
		err := New("http://unreachable.invalid", "tok").
			PushUpstream(context.Background(), "", &upstream.PushData{ConfigAnalysis: analyses(1)})
		Expect(err).To(MatchError(ContainSubstring("agent name is required")))
	})

	// A 403 here means the token's role lacks agent-push, which is a
	// permissions problem the operator has to fix — not a transport failure.
	ginkgo.It("names the missing permission on a forbidden response", func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":"access denied"}`))
		}))
		defer server.Close()

		err := New(server.URL, "tok").PushUpstream(context.Background(), "recon",
			&upstream.PushData{ConfigAnalysis: analyses(1)})

		Expect(err).To(MatchError(ContainSubstring("agent-push")))
		Expect(err).To(MatchError(ContainSubstring("recon")))
		Expect(err).To(MatchError(ContainSubstring("access denied")))
	})

	ginkgo.It("surfaces a server error body", func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"config_analysis violates foreign key constraint"}`))
		}))
		defer server.Close()

		err := New(server.URL, "tok").PushUpstream(context.Background(), "recon",
			&upstream.PushData{ConfigAnalysis: analyses(1)})

		Expect(err).To(MatchError(ContainSubstring("foreign key")))
	})
})
