package sdk

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/flanksource/incident-commander/clientapi"
	"github.com/google/uuid"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("DeletePlaybook", func() {
	ginkgo.It("soft-deletes a playbook by exact ID", func() {
		id := uuid.New()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			Expect(r.Method).To(Equal(http.MethodPatch))
			Expect(r.URL.Path).To(Equal("/db/playbooks"))
			Expect(r.URL.Query().Get("id")).To(Equal("eq." + id.String()))
			Expect(r.Header.Get("Prefer")).To(Equal("return=representation"))
			var body map[string]any
			Expect(json.NewDecoder(r.Body).Decode(&body)).To(Succeed())
			deletedAt, err := time.Parse(time.RFC3339Nano, body["deleted_at"].(string))
			Expect(err).ToNot(HaveOccurred())
			Expect(deletedAt).To(BeTemporally("~", time.Now(), time.Second))
			w.Header().Set("Content-Type", "application/json")
			Expect(json.NewEncoder(w).Encode([]clientapi.Playbook{{ID: id, Source: clientapi.SourceUI}})).To(Succeed())
		}))
		defer server.Close()

		playbook, err := New(server.URL, "token").DeletePlaybook(context.Background(), id)

		Expect(err).ToNot(HaveOccurred())
		Expect(playbook.ID).To(Equal(id))
	})
})
