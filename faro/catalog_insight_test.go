package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"

	"github.com/flanksource/incident-commander/sdk"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("faro catalog insights", func() {
	ginkgo.BeforeEach(func() {
		dir := ginkgo.GinkgoT().TempDir()
		ginkgo.GinkgoT().Setenv("HOME", dir)
		ginkgo.GinkgoT().Setenv("XDG_CONFIG_HOME", dir)
	})

	ginkgo.It("forwards insight search grammar and limit to /resources/search", func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			Expect(r.Method).To(Equal(http.MethodPost))
			Expect(r.URL.Path).To(Equal("/resources/search"))

			var got sdk.CatalogInsightSearchRequest
			Expect(json.NewDecoder(r.Body).Decode(&got)).To(Succeed())
			Expect(got.Limit).To(Equal(25))
			Expect(got.ConfigAnalysis).To(HaveLen(1))
			Expect(got.ConfigAnalysis[0].Search).To(Equal("severity=high type=security"))

			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"config_analysis":[{"id":"0274d556-6257-426a-b651-0a9bc35c26d8","name":"no-public-ip","type":"security","status":"open","severity":"high"}]}`))
		}))
		defer server.Close()
		storeRemoteContext(server.URL)

		items, err := remoteSearchInsights("severity=high type=security", 25)

		Expect(err).ToNot(HaveOccurred())
		Expect(items).To(HaveLen(1))
		Expect(items[0].ID).To(Equal("0274d556-6257-426a-b651-0a9bc35c26d8"))
		Expect(items[0].InsightType).To(Equal("security"))
		Expect(items[0].Severity).ToNot(BeNil())
		Expect(*items[0].Severity).To(Equal("high"))
	})

	ginkgo.It("defaults insight search empty limit to 100", func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var got sdk.CatalogInsightSearchRequest
			Expect(json.NewDecoder(r.Body).Decode(&got)).To(Succeed())
			Expect(got.Limit).To(Equal(100))
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"config_analysis":[]}`))
		}))
		defer server.Close()
		storeRemoteContext(server.URL)

		_, err := remoteSearchInsights("severity=high", 0)
		Expect(err).ToNot(HaveOccurred())
	})

	ginkgo.It("gets full insight details from the PostgREST endpoint", func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			Expect(r.Method).To(Equal(http.MethodGet))
			Expect(r.URL.Path).To(Equal("/api/db/config_analysis"))
			Expect(r.URL.Query().Get("id")).To(Equal("eq.521bae33-e4c3-42eb-a9c5-071ab92940b5"))
			Expect(r.URL.Query().Get("select")).To(ContainSubstring("analysis,properties"))
			Expect(r.URL.Query().Get("select")).To(ContainSubstring("config:configs"))

			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"id":"521bae33-e4c3-42eb-a9c5-071ab92940b5","config_id":"21e7586d-31fb-453c-a205-d73dc6b58eaa","analyzer":"no-public-ip","message":"instance has public ip","summary":"public ip","status":"open","severity":"high","analysis_type":"security","analysis":{"rule":"R1"},"config":{"id":"21e7586d-31fb-453c-a205-d73dc6b58eaa","name":"prod-instance","type":"AWS::EC2::Instance","config_class":"EC2"}}]`))
		}))
		defer server.Close()
		storeRemoteContext(server.URL + "/api")

		result, err := remoteGetInsight("521bae33-e4c3-42eb-a9c5-071ab92940b5")

		Expect(err).ToNot(HaveOccurred())
		insight := result.(*sdk.CatalogInsightDetail)
		Expect(insight.Analyzer).To(Equal("no-public-ip"))
		Expect(insight.Config).ToNot(BeNil())
		Expect(insight.Config.ConfigClass).To(Equal("EC2"))
	})
})
