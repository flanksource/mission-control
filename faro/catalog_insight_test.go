package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/flanksource/duty/query"
	"github.com/flanksource/incident-commander/sdk"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spf13/cobra"
)

var _ = ginkgo.Describe("faro catalog insights", func() {
	ginkgo.BeforeEach(func() {
		dir := ginkgo.GinkgoT().TempDir()
		ginkgo.GinkgoT().Setenv("HOME", dir)
		ginkgo.GinkgoT().Setenv("XDG_CONFIG_HOME", dir)
	})

	ginkgo.It("forwards insight search grammar and limit to /resources/search", func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch r.URL.Path {
			case "/resources/search":
				Expect(r.Method).To(Equal(http.MethodPost))

				var got query.SearchResourcesRequest
				Expect(json.NewDecoder(r.Body).Decode(&got)).To(Succeed())
				Expect(got.Limit).To(Equal(26))
				Expect(got.Timestamps).To(BeTrue())
				Expect(got.ConfigAnalysis).To(HaveLen(1))
				Expect(got.ConfigAnalysis[0].Search).To(Equal("severity=high type=security"))
				Expect(got.ConfigAnalysis[0].Agent).To(Equal("all"))

				_, _ = w.Write([]byte(`{"config_analysis":[{"id":"0274d556-6257-426a-b651-0a9bc35c26d8","name":"no-public-ip","type":"security","status":"open","severity":"high","created_at":"2026-06-24T16:41:38Z","updated_at":"2026-06-25T10:00:00Z"}]}`))
			case "/db/config_analysis":
				Expect(r.Method).To(Equal(http.MethodGet))
				Expect(r.URL.Query().Get("id")).To(Equal("in.(0274d556-6257-426a-b651-0a9bc35c26d8)"))
				Expect(r.URL.Query().Get("select")).To(ContainSubstring("config:configs(id,name,type,config_class)"))
				Expect(r.URL.Query().Get("select")).To(ContainSubstring("evidences(hypothesis:hypotheses(incident:incidents(incident_id)))"))
				_, _ = w.Write([]byte(`[{"id":"0274d556-6257-426a-b651-0a9bc35c26d8","summary":"Public IP exposed","config":{"id":"21e7586d-31fb-453c-a205-d73dc6b58eaa","name":"prod-instance","type":"AWS::EC2::Instance"},"evidences":[{"hypothesis":{"incident":{"incident_id":"INC-42"}}},{"hypothesis":{"incident":{"incident_id":"INC-42"}}},{"hypothesis":{"incident":{"incident_id":"INC-7"}}}]}]`))
			default:
				ginkgo.Fail("unexpected request: " + r.URL.Path)
			}
		}))
		defer server.Close()
		storeRemoteContext(server.URL)

		result, err := remoteSearchInsights("severity=high type=security", "all", 25)

		Expect(err).ToNot(HaveOccurred())
		Expect(result.Limited).To(BeFalse())
		Expect(result.TotalAtLeast).To(Equal(1))
		items := result.Items
		Expect(items).To(HaveLen(1))
		Expect(items[0].ID).To(Equal("0274d556-6257-426a-b651-0a9bc35c26d8"))
		Expect(items[0].InsightType).To(Equal("security"))
		Expect(items[0].Severity).ToNot(BeNil())
		Expect(*items[0].Severity).To(Equal("high"))
		Expect(items[0].CreatedAt).ToNot(BeNil())
		Expect(*items[0].CreatedAt).To(Equal(time.Date(2026, 6, 24, 16, 41, 38, 0, time.UTC)))
		Expect(items[0].UpdatedAt).ToNot(BeNil())
		Expect(*items[0].UpdatedAt).To(Equal(time.Date(2026, 6, 25, 10, 0, 0, 0, time.UTC)))
		Expect(items[0].Summary).To(Equal("Public IP exposed"))
		Expect(items[0].Config).ToNot(BeNil())
		Expect(items[0].Config.ID).To(Equal("21e7586d-31fb-453c-a205-d73dc6b58eaa"))
		Expect(items[0].Config.Name).To(Equal("prod-instance"))
		Expect(items[0].Config.Type).To(Equal("AWS::EC2::Instance"))
		Expect(items[0].IssueIDs).To(Equal([]string{"INC-42", "INC-7"}))

		row := items[0].Row()
		Expect(row).To(HaveKeyWithValue("ConfigID", "21e7586d-31fb-453c-a205-d73dc6b58eaa"))
		Expect(row).To(HaveKeyWithValue("ConfigName", "prod-instance"))
		Expect(row).To(HaveKeyWithValue("ConfigType", "AWS::EC2::Instance"))
		Expect(row).To(HaveKeyWithValue("Summary", "Public IP exposed"))
		Expect(row).To(HaveKeyWithValue("IssueIDs", "INC-42, INC-7"))

		payload, err := json.Marshal(items[0])
		Expect(err).ToNot(HaveOccurred())
		Expect(payload).To(MatchJSON(`{
			"id": "0274d556-6257-426a-b651-0a9bc35c26d8",
			"name": "no-public-ip",
			"insight_type": "security",
			"status": "open",
			"severity": "high",
			"summary": "Public IP exposed",
			"config": {
				"id": "21e7586d-31fb-453c-a205-d73dc6b58eaa",
				"name": "prod-instance",
				"type": "AWS::EC2::Instance"
			},
			"issue_ids": ["INC-42", "INC-7"],
			"created_at": "2026-06-24T16:41:38Z",
			"updated_at": "2026-06-25T10:00:00Z"
		}`))
	})

	ginkgo.It("defaults insight search empty limit to 100", func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var got query.SearchResourcesRequest
			Expect(json.NewDecoder(r.Body).Decode(&got)).To(Succeed())
			Expect(got.Limit).To(Equal(101))
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"config_analysis":[]}`))
		}))
		defer server.Close()
		storeRemoteContext(server.URL)

		_, err := remoteSearchInsights("severity=high", "all", 0)
		Expect(err).ToNot(HaveOccurred())
	})

	ginkgo.It("uses an open-status query when the query argument is omitted", func() {
		Expect(CatalogInsight.Runnable()).To(BeTrue())
		Expect(CatalogInsight.Args(CatalogInsight, nil)).To(Succeed())
		Expect(CatalogInsightSearch.Args(CatalogInsightSearch, nil)).To(Succeed())
		Expect(catalogInsightSearchQuery(nil)).To(Equal("status=open"))
		Expect(catalogInsightSearchQuery([]string{"severity=high", "type=security"})).To(Equal("severity=high type=security"))
	})

	ginkgo.It("detects and truncates limited insight results", func() {
		const firstID = "0274d556-6257-426a-b651-0a9bc35c26d8"
		const secondID = "1274d556-6257-426a-b651-0a9bc35c26d8"
		const thirdID = "2274d556-6257-426a-b651-0a9bc35c26d8"
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch r.URL.Path {
			case "/resources/search":
				var got query.SearchResourcesRequest
				Expect(json.NewDecoder(r.Body).Decode(&got)).To(Succeed())
				Expect(got.Limit).To(Equal(3))
				_, _ = w.Write([]byte(`{"config_analysis":[{"id":"` + firstID + `"},{"id":"` + secondID + `"},{"id":"` + thirdID + `"}]}`))
			case "/db/config_analysis":
				Expect(r.URL.Query().Get("id")).To(Equal("in.(" + firstID + "," + secondID + ")"))
				_, _ = w.Write([]byte(`[{"id":"` + firstID + `"},{"id":"` + secondID + `"}]`))
			default:
				ginkgo.Fail("unexpected request: " + r.URL.Path)
			}
		}))
		defer server.Close()
		storeRemoteContext(server.URL)

		result, err := remoteSearchInsights("status=open", "all", 2)

		Expect(err).ToNot(HaveOccurred())
		Expect(result.Items).To(HaveLen(2))
		Expect(result.Limited).To(BeTrue())
		Expect(result.TotalAtLeast).To(Equal(3))

		var stderr bytes.Buffer
		cmd := &cobra.Command{}
		cmd.SetErr(&stderr)
		printCatalogInsightLimitWarning(cmd, result)
		Expect(stderr.String()).To(Equal("Warning: showing 2 of at least 3 total insights; increase --limit to return more.\n"))
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
