package sdk

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"

	"github.com/flanksource/duty/models"
	"github.com/google/uuid"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("ApplyPlaybook", func() {
	params := PlaybookApplyParams{
		Namespace:   "default",
		Name:        "restart",
		Title:       "Restart",
		Description: "Restart a workload",
		Category:    "Kubernetes",
		Spec:        json.RawMessage(`{"actions":[{"name":"echo","exec":{"script":"echo ok"}}]}`),
	}

	ginkgo.It("creates API-owned playbooks", func() {
		id := uuid.New()
		requestCount := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestCount++
			switch requestCount {
			case 1:
				Expect(r.Method).To(Equal(http.MethodGet))
				Expect(r.URL.Path).To(Equal("/db/playbooks"))
				Expect(r.URL.Query().Get("namespace")).To(Equal("eq.default"))
				Expect(r.URL.Query().Get("name")).To(Equal("eq.restart"))
				_, _ = w.Write([]byte(`[]`))
			case 2:
				Expect(r.Method).To(Equal(http.MethodPost))
				var body map[string]any
				Expect(json.NewDecoder(r.Body).Decode(&body)).To(Succeed())
				Expect(body).To(HaveKeyWithValue("source", models.SourceUI))
				Expect(body["spec"]).To(HaveKey("actions"))
				w.Header().Set("Content-Type", "application/json")
				Expect(json.NewEncoder(w).Encode([]models.Playbook{{
					ID: id, Namespace: "default", Name: "restart", Source: models.SourceUI,
				}})).To(Succeed())
			default:
				ginkgo.Fail("unexpected request")
			}
		}))
		defer server.Close()

		result, err := New(server.URL, "token").ApplyPlaybook(context.Background(), params)

		Expect(err).ToNot(HaveOccurred())
		Expect(result.Created).To(BeTrue())
		Expect(result.Playbook.ID).To(Equal(id))
		Expect(requestCount).To(Equal(2))
	})

	ginkgo.It("updates UI-created playbooks without changing their source", func() {
		id := uuid.New()
		requestCount := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestCount++
			w.Header().Set("Content-Type", "application/json")
			if requestCount == 1 {
				Expect(json.NewEncoder(w).Encode([]models.Playbook{{
					ID: id, Namespace: "default", Name: "restart", Source: models.SourceUI,
				}})).To(Succeed())
				return
			}

			Expect(r.Method).To(Equal(http.MethodPatch))
			Expect(r.URL.Query().Get("id")).To(Equal("eq." + id.String()))
			var body map[string]any
			Expect(json.NewDecoder(r.Body).Decode(&body)).To(Succeed())
			Expect(body).ToNot(HaveKey("source"))
			Expect(json.NewEncoder(w).Encode([]models.Playbook{{
				ID: id, Namespace: "default", Name: "restart", Source: models.SourceUI,
			}})).To(Succeed())
		}))
		defer server.Close()

		result, err := New(server.URL, "token").ApplyPlaybook(context.Background(), params)

		Expect(err).ToNot(HaveOccurred())
		Expect(result.Created).To(BeFalse())
		Expect(result.Playbook.Source).To(Equal(models.SourceUI))
		Expect(requestCount).To(Equal(2))
	})

	ginkgo.It("does not update externally managed playbooks", func() {
		id := uuid.New()
		requestCount := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			requestCount++
			w.Header().Set("Content-Type", "application/json")
			Expect(json.NewEncoder(w).Encode([]models.Playbook{{
				ID: id, Namespace: "default", Name: "restart", Source: models.SourceConfigFile,
			}})).To(Succeed())
		}))
		defer server.Close()

		_, err := New(server.URL, "token").ApplyPlaybook(context.Background(), params)

		Expect(err).To(MatchError(ContainSubstring("not created through the API")))
		Expect(requestCount).To(Equal(1))
	})
})
