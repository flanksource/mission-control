package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/flanksource/duty/query"
	"github.com/flanksource/incident-commander/sdk"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("faro catalog change", func() {
	ginkgo.BeforeEach(func() {
		dir := ginkgo.GinkgoT().TempDir()
		ginkgo.GinkgoT().Setenv("HOME", dir)
		ginkgo.GinkgoT().Setenv("XDG_CONFIG_HOME", dir)
	})

	ginkgo.It("forwards change search grammar and limit to /resources/search", func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			Expect(r.Method).To(Equal(http.MethodPost))
			Expect(r.URL.Path).To(Equal("/resources/search"))

			var got query.SearchResourcesRequest
			Expect(json.NewDecoder(r.Body).Decode(&got)).To(Succeed())
			Expect(got.Limit).To(Equal(25))
			Expect(got.Timestamps).To(BeTrue())
			Expect(got.ConfigChanges).To(HaveLen(1))
			Expect(got.ConfigChanges[0].Search).To(Equal("change_type=diff type=deployment"))
			Expect(got.ConfigChanges[0].Agent).To(BeEmpty())

			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"config_changes":[{"id":"0274d556-6257-426a-b651-0a9bc35c26d8","agent":"00000000-0000-0000-0000-000000000000","name":"status","namespace":"kube-system","type":"diff","created_at":"2026-06-24T16:41:38Z"}]}`))
		}))
		defer server.Close()
		storeRemoteContext(server.URL)

		items, err := remoteSearchChanges("change_type=diff type=deployment", 25)

		Expect(err).ToNot(HaveOccurred())
		Expect(items).To(HaveLen(1))
		Expect(items[0].ID).To(Equal("0274d556-6257-426a-b651-0a9bc35c26d8"))
		Expect(items[0].ChangeType).To(Equal("diff"))
		Expect(items[0].CreatedAt).ToNot(BeNil())
		Expect(items[0].CreatedAt.UTC()).To(Equal(time.Date(2026, 6, 24, 16, 41, 38, 0, time.UTC)))
	})

	ginkgo.It("defaults change search empty limit to 100", func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var got query.SearchResourcesRequest
			Expect(json.NewDecoder(r.Body).Decode(&got)).To(Succeed())
			Expect(got.Limit).To(Equal(100))
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"config_changes":[]}`))
		}))
		defer server.Close()
		storeRemoteContext(server.URL)

		_, err := remoteSearchChanges("change_type=diff", 0)
		Expect(err).ToNot(HaveOccurred())
	})

	ginkgo.It("gets full change details from the PostgREST endpoint", func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			Expect(r.Method).To(Equal(http.MethodGet))
			Expect(r.URL.Path).To(Equal("/api/db/config_changes"))
			Expect(r.URL.Query().Get("id")).To(Equal("eq.521bae33-e4c3-42eb-a9c5-071ab92940b5"))
			Expect(r.URL.Query().Get("select")).To(ContainSubstring("diff,details,patches"))
			Expect(r.URL.Query().Get("select")).To(ContainSubstring("config:configs"))

			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"id":"521bae33-e4c3-42eb-a9c5-071ab92940b5","config_id":"21e7586d-31fb-453c-a205-d73dc6b58eaa","change_type":"Failed","created_at":"2026-06-24T16:41:38Z","source":"kubernetes/","details":{"reason":"Failed"},"config":{"id":"21e7586d-31fb-453c-a205-d73dc6b58eaa","name":"opensearch-fail","type":"MissionControl::Canary","config_class":"Canary"},"artifacts":[]}]`))
		}))
		defer server.Close()
		storeRemoteContext(server.URL + "/api")

		result, err := remoteGetChange("521bae33-e4c3-42eb-a9c5-071ab92940b5")

		Expect(err).ToNot(HaveOccurred())
		change := result.(*sdk.CatalogChangeDetail)
		Expect(change.ChangeType).To(Equal("Failed"))
		Expect(change.Config).ToNot(BeNil())
		Expect(change.Config.ConfigClass).To(Equal("Canary"))
	})
})
