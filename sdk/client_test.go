package sdk

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/flanksource/commons/har"
	"github.com/flanksource/duty/models"
	"github.com/google/uuid"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	icapi "github.com/flanksource/incident-commander/api"
	"github.com/flanksource/incident-commander/pkg/httpobservability"
)

func TestSDK(t *testing.T) {
	RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "SDK")
}

var _ = ginkgo.Describe("GetConnection HTML detection", func() {
	ginkgo.It("uses the shared HAR collector automatically", func() {
		collector := har.NewCollector(har.HARConfig{CaptureContentTypes: []string{"application/json"}})
		restore := httpobservability.SetHARCollector(collector)
		defer restore()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			Expect(r.URL.Path).To(Equal("/auth/whoami"))
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"payload":{"user":{"id":"u1"},"roles":[]}}`))
		}))
		defer server.Close()

		_, _, err := New(server.URL, "fake-token").Whoami(context.Background())
		Expect(err).ToNot(HaveOccurred())
		Expect(collector.Entries()).ToNot(BeEmpty())
	})

	ginkgo.It("uses the token provider for each request", func() {
		seen := []string{}
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			Expect(r.URL.Path).To(Equal("/auth/whoami"))
			seen = append(seen, r.Header.Get("Authorization"))
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"payload":{"user":{"id":"u1"},"roles":[]}}`))
		}))
		defer server.Close()

		calls := 0
		client := New(server.URL, "", WithTokenProvider(func(context.Context) (string, error) {
			calls++
			if calls == 1 {
				return "token-one", nil
			}
			return "token-two", nil
		}))

		_, _, err := client.Whoami(context.Background())
		Expect(err).ToNot(HaveOccurred())
		_, _, err = client.Whoami(context.Background())
		Expect(err).ToNot(HaveOccurred())

		Expect(seen).To(Equal([]string{"Bearer token-one", "Bearer token-two"}))
	})

	ginkgo.It("returns token provider errors before issuing a request", func() {
		called := false
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		_, _, err := New(server.URL, "", WithTokenProvider(func(context.Context) (string, error) {
			return "", errors.New("refresh failed")
		})).Whoami(context.Background())

		Expect(err).To(MatchError("refresh failed"))
		Expect(called).To(BeFalse())
	})

	ginkgo.It("returns ErrHTMLResponse when server returns HTML with 200 OK", func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`<!DOCTYPE html><html><body>Frontend</body></html>`))
		}))
		defer server.Close()

		client := New(server.URL, "fake-token")
		_, err := client.GetConnection("any", "default")
		Expect(errors.Is(err, ErrHTMLResponse)).To(BeTrue(), "got: %v", err)
	})

	ginkgo.It("returns ErrHTMLResponse when body starts with '<' even without HTML content-type", func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`<html>no content type header</html>`))
		}))
		defer server.Close()

		client := New(server.URL, "fake-token")
		_, err := client.GetConnection("any", "default")
		Expect(errors.Is(err, ErrHTMLResponse)).To(BeTrue(), "got: %v", err)
	})

	ginkgo.It("decodes JSON successfully when server returns valid JSON", func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[{"name":"azure-bearer","namespace":"monitoring","type":"http"}]`))
		}))
		defer server.Close()

		client := New(server.URL, "fake-token")
		conn, err := client.GetConnection("azure-bearer", "monitoring")
		Expect(err).ToNot(HaveOccurred())
		Expect(conn).ToNot(BeNil())
		Expect(conn.Name).To(Equal("azure-bearer"))
	})
})

var _ = ginkgo.Describe("TestConnection HTML detection", func() {
	ginkgo.It("returns ErrHTMLResponse on HTML error page (405 from frontend proxy)", func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusMethodNotAllowed)
			_, _ = w.Write([]byte(`<!DOCTYPE html><html><title>405</title></html>`))
		}))
		defer server.Close()

		client := New(server.URL, "fake-token")
		_, err := client.TestConnection("00000000-0000-0000-0000-000000000000")
		Expect(errors.Is(err, ErrHTMLResponse)).To(BeTrue(), "got: %v", err)
	})
})

var _ = ginkgo.Describe("Plugin operation server errors", func() {
	ginkgo.It("returns structured oops server errors", func() {
		payload := `{
			"code": "HANDLER_ERROR",
			"context": {"name": "", "namespace": "", "user": "Admin"},
			"error": "config_item_id is required",
			"stacktrace": "Oops: config_item_id is required\n  --- at controller.go:165 InvokeOperation()",
			"time": "2026-05-07T10:20:07.73204Z",
			"trace": "01KR0Z7BNP92W83AEQ1MMATATA"
		}`
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			Expect(r.URL.Path).To(Equal("/api/plugins/arthas/operations/session"))
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(payload))
		}))
		defer server.Close()

		body, status, err := New(server.URL, "fake-token").DispatchPluginOperation(context.Background(), "arthas", "session", []byte(`{}`), "")

		Expect(status).To(Equal(http.StatusInternalServerError))
		Expect(body).To(MatchJSON(payload))
		var serverErr *ServerError
		Expect(errors.As(err, &serverErr)).To(BeTrue(), "got: %v", err)
		Expect(serverErr.StatusCode).To(Equal(http.StatusInternalServerError))
		Expect(serverErr.Code).To(Equal("HANDLER_ERROR"))
		Expect(serverErr.Message).To(Equal("config_item_id is required"))
		Expect(serverErr.Trace).To(Equal("01KR0Z7BNP92W83AEQ1MMATATA"))
		Expect(serverErr.Time).To(Equal("2026-05-07T10:20:07.73204Z"))
		Expect(serverErr.Context).To(HaveKeyWithValue("user", "Admin"))
		Expect(serverErr.Stacktrace).To(ContainSubstring("controller.go:165"))
		Expect(err.Error()).To(Equal("server 500: config_item_id is required"))
	})

	ginkgo.It("preserves non-oops JSON server errors with a useful fallback", func() {
		payload := `{"message":"bad request"}`
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(payload))
		}))
		defer server.Close()

		body, status, err := New(server.URL, "fake-token").DispatchPluginOperation(context.Background(), "arthas", "session", []byte(`{}`), "")

		Expect(status).To(Equal(http.StatusBadRequest))
		Expect(body).To(MatchJSON(payload))
		var serverErr *ServerError
		Expect(errors.As(err, &serverErr)).To(BeTrue(), "got: %v", err)
		Expect(serverErr.Message).To(Equal("bad request"))
		Expect(err.Error()).To(Equal("server 400: bad request"))
	})

	ginkgo.It("keeps returning ErrHTMLResponse for HTML server errors", func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`<!DOCTYPE html><html><title>500</title></html>`))
		}))
		defer server.Close()

		_, status, err := New(server.URL, "fake-token").DispatchPluginOperation(context.Background(), "arthas", "session", []byte(`{}`), "")

		Expect(status).To(Equal(http.StatusInternalServerError))
		Expect(errors.Is(err, ErrHTMLResponse)).To(BeTrue(), "got: %v", err)
	})
})

var _ = ginkgo.Describe("Playbook client", func() {
	ginkgo.It("lists playbooks with target filters", func() {
		playbookID := uuid.New()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			Expect(r.URL.Path).To(Equal("/playbook/list"))
			Expect(r.URL.Query().Get("config_id")).To(Equal("config-1"))
			Expect(r.Header.Get("Authorization")).To(Equal("Bearer fake-token"))
			w.Header().Set("Content-Type", "application/json")
			Expect(json.NewEncoder(w).Encode([]icapi.PlaybookListItem{{
				ID:        playbookID,
				Namespace: "default",
				Name:      "restart",
			}})).To(Succeed())
		}))
		defer server.Close()

		playbooks, err := New(server.URL, "fake-token").ListPlaybooks(PlaybookListOptions{ConfigID: "config-1"})
		Expect(err).ToNot(HaveOccurred())
		Expect(playbooks).To(HaveLen(1))
		Expect(playbooks[0].ID).To(Equal(playbookID))
	})

	ginkgo.It("runs a playbook and posts parameters", func() {
		playbookID := uuid.New()
		configID := uuid.New()
		runID := uuid.New()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			Expect(r.URL.Path).To(Equal("/playbook/run"))
			body, err := io.ReadAll(r.Body)
			Expect(err).ToNot(HaveOccurred())
			var params PlaybookRunParams
			Expect(json.Unmarshal(body, &params)).To(Succeed())
			Expect(params.ID).To(Equal(playbookID))
			Expect(params.ConfigID).To(Equal(&configID))
			Expect(params.Params).To(HaveKeyWithValue("name", "api"))
			w.Header().Set("Content-Type", "application/json")
			Expect(json.NewEncoder(w).Encode(PlaybookRunResponse{
				RunID:    runID.String(),
				StartsAt: "2026-04-29T17:00:00Z",
			})).To(Succeed())
		}))
		defer server.Close()

		response, err := New(server.URL, "fake-token").RunPlaybook(PlaybookRunParams{
			ID:       playbookID,
			ConfigID: &configID,
			Params:   map[string]string{"name": "api"},
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(response.RunID).To(Equal(runID.String()))
	})

	ginkgo.It("gets playbook run status summaries", func() {
		runID := uuid.New()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			Expect(r.URL.Path).To(Equal("/playbook/run/" + runID.String() + "/status"))
			w.Header().Set("Content-Type", "application/json")
			Expect(json.NewEncoder(w).Encode(PlaybookSummary{
				Run: models.PlaybookRun{
					ID:     runID,
					Status: models.PlaybookRunStatusCompleted,
				},
				Actions: []models.PlaybookRunAction{{
					ID:     uuid.New(),
					Name:   "echo",
					Status: models.PlaybookActionStatusCompleted,
				}},
			})).To(Succeed())
		}))
		defer server.Close()

		summary, err := New(server.URL, "fake-token").GetPlaybookRunStatus(runID.String())
		Expect(err).ToNot(HaveOccurred())
		Expect(summary.Run.Status).To(Equal(models.PlaybookRunStatusCompleted))
		Expect(summary.Actions).To(HaveLen(1))
	})
})
