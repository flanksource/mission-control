package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"

	"github.com/flanksource/incident-commander/clientapi"
	"github.com/google/uuid"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("ApplyPlaybook", func() {
	params := PlaybookApplyParams{
		Namespace: "default",
		Name:      "restart",
		Spec:      json.RawMessage(`{"title":"Restart","icon":"restart","description":"Restart a workload","category":"Kubernetes","actions":[{"name":"echo","exec":{"script":"echo ok"}}]}`),
	}

	ginkgo.It("creates API-owned playbooks", func() {
		id := uuid.New()
		requestCount := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestCount++
			w.Header().Set("Content-Type", "application/json")
			switch requestCount {
			case 1:
				Expect(r.Method).To(Equal(http.MethodGet))
				Expect(r.URL.Path).To(Equal("/db/playbooks"))
				Expect(r.URL.Query().Get("namespace")).To(Equal("eq.default"))
				Expect(r.URL.Query().Get("name")).To(Equal("eq.restart"))
				Expect(r.URL.Query().Get("deleted_at")).To(Equal("is.null"))
				Expect(r.URL.Query().Get("select")).To(Equal("*"))
				_, _ = w.Write([]byte(`[]`))
			case 2:
				Expect(r.Method).To(Equal(http.MethodPost))
				Expect(r.URL.Path).To(Equal("/db/playbooks"))
				Expect(r.Header.Get("Prefer")).To(Equal("return=representation"))
				var body map[string]any
				Expect(json.NewDecoder(r.Body).Decode(&body)).To(Succeed())
				Expect(body).To(HaveKeyWithValue("source", clientapi.SourceUI))
				Expect(body).To(HaveKeyWithValue("title", "Restart"))
				Expect(body).To(HaveKeyWithValue("icon", "restart"))
				Expect(body).To(HaveKeyWithValue("description", "Restart a workload"))
				Expect(body).To(HaveKeyWithValue("category", "Kubernetes"))
				Expect(body["spec"]).To(HaveKey("actions"))
				Expect(json.NewEncoder(w).Encode([]clientapi.Playbook{{
					ID: id, Namespace: "default", Name: "restart", Title: "Restart", Source: clientapi.SourceUI,
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
		Expect(result.Playbook.Source).To(Equal(clientapi.SourceUI))
		Expect(requestCount).To(Equal(2))
	})

	ginkgo.It("normalizes the namespace before reads and writes", func() {
		requestCount := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestCount++
			w.Header().Set("Content-Type", "application/json")
			switch requestCount {
			case 1:
				Expect(r.URL.Query().Get("namespace")).To(Equal("eq.default"))
				_, _ = w.Write([]byte(`[]`))
			case 2:
				var body map[string]any
				Expect(json.NewDecoder(r.Body).Decode(&body)).To(Succeed())
				Expect(body).To(HaveKeyWithValue("namespace", "default"))
				Expect(json.NewEncoder(w).Encode([]clientapi.Playbook{{
					ID: uuid.New(), Namespace: "default", Name: "restart", Source: clientapi.SourceUI,
				}})).To(Succeed())
			default:
				ginkgo.Fail("unexpected request")
			}
		}))
		defer server.Close()

		normalized := params
		normalized.Namespace = ""
		_, err := New(server.URL, "token").ApplyPlaybook(context.Background(), normalized)

		Expect(err).ToNot(HaveOccurred())
		Expect(requestCount).To(Equal(2))
	})

	ginkgo.It("rejects an empty name before making a request", func() {
		requestCount := 0
		server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			requestCount++
		}))
		defer server.Close()

		invalid := params
		invalid.Name = ""
		_, err := New(server.URL, "token").ApplyPlaybook(context.Background(), invalid)

		Expect(err).To(MatchError("playbook name is required"))
		Expect(requestCount).To(BeZero())
	})

	ginkgo.It("updates UI-created playbooks without changing their source", func() {
		id := uuid.New()
		requestCount := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestCount++
			w.Header().Set("Content-Type", "application/json")
			switch requestCount {
			case 1:
				Expect(r.Method).To(Equal(http.MethodGet))
				Expect(r.URL.Path).To(Equal("/db/playbooks"))
				Expect(json.NewEncoder(w).Encode([]clientapi.Playbook{{
					ID: id, Namespace: "default", Name: "restart", Source: clientapi.SourceUI,
				}})).To(Succeed())
			case 2:
				Expect(r.Method).To(Equal(http.MethodPatch))
				Expect(r.URL.Path).To(Equal("/db/playbooks"))
				Expect(r.URL.Query().Get("id")).To(Equal("eq." + id.String()))
				Expect(r.Header.Get("Prefer")).To(Equal("return=representation"))
				var body map[string]any
				Expect(json.NewDecoder(r.Body).Decode(&body)).To(Succeed())
				Expect(body).ToNot(HaveKey("source"))
				Expect(body).To(HaveKeyWithValue("title", "Restart"))
				Expect(json.NewEncoder(w).Encode([]clientapi.Playbook{{
					ID: id, Namespace: "default", Name: "restart", Title: "Restart", Source: clientapi.SourceUI,
				}})).To(Succeed())
			default:
				ginkgo.Fail("unexpected request")
			}
		}))
		defer server.Close()

		result, err := New(server.URL, "token").ApplyPlaybook(context.Background(), params)

		Expect(err).ToNot(HaveOccurred())
		Expect(result.Created).To(BeFalse())
		Expect(result.Playbook.ID).To(Equal(id))
		Expect(result.Playbook.Source).To(Equal(clientapi.SourceUI))
		Expect(requestCount).To(Equal(2))
	})

	ginkgo.It("does not update externally managed playbooks", func() {
		requestCount := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestCount++
			w.Header().Set("Content-Type", "application/json")
			switch requestCount {
			case 1:
				Expect(r.URL.Path).To(Equal("/db/playbooks"))
				Expect(json.NewEncoder(w).Encode([]clientapi.Playbook{{
					ID: uuid.New(), Namespace: "default", Name: "restart", Source: "ConfigFile",
				}})).To(Succeed())
			default:
				ginkgo.Fail("unexpected write request")
			}
		}))
		defer server.Close()

		_, err := New(server.URL, "token").ApplyPlaybook(context.Background(), params)

		Expect(err).To(MatchError(ContainSubstring("was not created through the API")))
		Expect(requestCount).To(Equal(1))
	})
})
