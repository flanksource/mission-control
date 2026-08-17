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

var _ = ginkgo.Describe("ListPlaybookRuns", func() {
	ginkgo.It("lists top-level runs with playbook and status filters", func() {
		playbookID := uuid.New()
		runID := uuid.New()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			Expect(r.Method).To(Equal(http.MethodGet))
			Expect(r.URL.Path).To(Equal("/db/playbook_runs"))
			Expect(r.URL.Query().Get("playbook_id")).To(Equal("eq." + playbookID.String()))
			Expect(r.URL.Query().Get("status")).To(Equal("in.(failed,timed_out)"))
			Expect(r.URL.Query().Get("parent_id")).To(Equal("is.null"))
			Expect(r.URL.Query().Get("order")).To(Equal("created_at.desc"))
			Expect(r.URL.Query().Get("limit")).To(Equal("5"))
			w.Header().Set("Content-Type", "application/json")
			Expect(json.NewEncoder(w).Encode([]clientapi.PlaybookRun{{
				ID: runID, PlaybookID: playbookID, Status: clientapi.PlaybookRunStatusFailed,
			}})).To(Succeed())
		}))
		defer server.Close()

		runs, err := New(server.URL, "token").ListPlaybookRuns(context.Background(), PlaybookRunListOptions{
			PlaybookID: &playbookID,
			Statuses: []clientapi.PlaybookRunStatus{
				clientapi.PlaybookRunStatusFailed,
				clientapi.PlaybookRunStatusTimedOut,
			},
			Limit: 5,
		})

		Expect(err).ToNot(HaveOccurred())
		Expect(runs).To(HaveLen(1))
		Expect(runs[0].ID).To(Equal(runID))
	})

	ginkgo.It("defaults the result limit", func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			Expect(r.URL.Query().Get("limit")).To(Equal("20"))
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[]`))
		}))
		defer server.Close()

		runs, err := New(server.URL, "token").ListPlaybookRuns(context.Background(), PlaybookRunListOptions{})

		Expect(err).ToNot(HaveOccurred())
		Expect(runs).To(BeEmpty())
	})
})
