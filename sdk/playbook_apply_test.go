package sdk

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
		Spec:      json.RawMessage(`{"actions":[{"name":"echo","exec":{"script":"echo ok"}}]}`),
	}

	ginkgo.It("creates API-owned playbooks", func() {
		id := uuid.New()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			Expect(r.Method).To(Equal(http.MethodPost))
			Expect(r.URL.Path).To(Equal("/playbook/apply"))
			var body map[string]any
			Expect(json.NewDecoder(r.Body).Decode(&body)).To(Succeed())
			Expect(body).To(HaveKeyWithValue("namespace", "default"))
			Expect(body["spec"]).To(HaveKey("actions"))
			w.Header().Set("Content-Type", "application/json")
			Expect(json.NewEncoder(w).Encode(clientapi.PlaybookApplyResponse{
				Playbook: clientapi.Playbook{ID: id, Namespace: "default", Name: "restart", Source: clientapi.SourceUI},
				Created:  true,
			})).To(Succeed())
		}))
		defer server.Close()

		result, err := New(server.URL, "token").ApplyPlaybook(context.Background(), params)

		Expect(err).ToNot(HaveOccurred())
		Expect(result.Created).To(BeTrue())
		Expect(result.Playbook.ID).To(Equal(id))

	})

	ginkgo.It("updates UI-created playbooks without changing their source", func() {
		id := uuid.New()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			Expect(r.Method).To(Equal(http.MethodPost))
			Expect(r.URL.Path).To(Equal("/playbook/apply"))
			Expect(json.NewEncoder(w).Encode(clientapi.PlaybookApplyResponse{
				Playbook: clientapi.Playbook{ID: id, Namespace: "default", Name: "restart", Source: clientapi.SourceUI},
			})).To(Succeed())
		}))
		defer server.Close()

		result, err := New(server.URL, "token").ApplyPlaybook(context.Background(), params)

		Expect(err).ToNot(HaveOccurred())
		Expect(result.Created).To(BeFalse())
		Expect(result.Playbook.Source).To(Equal(clientapi.SourceUI))
	})

	ginkgo.It("does not update externally managed playbooks", func() {
		id := uuid.New()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"error":"not created through the API"}`))
		}))
		defer server.Close()

		_, err := New(server.URL, "token").ApplyPlaybook(context.Background(), params)

		Expect(err).To(MatchError(ContainSubstring("not created through the API")))
		_ = id
	})
})
