package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/flanksource/clicky"
	"github.com/flanksource/duty/query"
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
				_, _ = w.Write([]byte(`[{"id":"0274d556-6257-426a-b651-0a9bc35c26d8","config_id":"21e7586d-31fb-453c-a205-d73dc6b58eaa","analyzer":"no-public-ip","message":"instance has public ip","summary":"public ip","status":"open","severity":"high","analysis_type":"security","analysis":{"rule":"R1"},"properties":[{"name":"port","value":443}],"config":{"id":"21e7586d-31fb-453c-a205-d73dc6b58eaa","name":"prod-instance","type":"AWS::EC2::Instance"}}]`))
			default:
				ginkgo.Fail("unexpected request: " + r.URL.Path)
			}
		}))
		defer server.Close()
		storeRemoteContext(server.URL)

		result, err := remoteSearchInsights("severity=high type=security", "all", 25)

		Expect(err).ToNot(HaveOccurred())
		Expect(result.Items).To(HaveLen(1))
		Expect(result.Items[0].ID).To(Equal("0274d556-6257-426a-b651-0a9bc35c26d8"))
		Expect(result.Items[0].InsightType).To(Equal("security"))
		Expect(result.Items[0].Severity).ToNot(BeNil())
		Expect(*result.Items[0].Severity).To(Equal("high"))
		Expect(result.Items[0].FirstObserved).ToNot(BeNil())
		Expect(*result.Items[0].FirstObserved).To(Equal(time.Date(2026, 6, 24, 16, 41, 38, 0, time.UTC)))
		Expect(result.Items[0].LastObserved).ToNot(BeNil())
		Expect(*result.Items[0].LastObserved).To(Equal(time.Date(2026, 6, 25, 10, 0, 0, 0, time.UTC)))
		Expect(result.Details).To(HaveLen(1))
		Expect(result.Details[0].Message).To(Equal("instance has public ip"))
		Expect(result.Details[0].Analysis).To(HaveKeyWithValue("rule", "R1"))

		compact := catalogInsightSearchOutput(result, false)
		Expect(compact).To(BeAssignableToTypeOf([]catalogInsightSearchHit{}))
		full := catalogInsightSearchOutput(result, true)
		Expect(full).To(BeAssignableToTypeOf([]sdk.CatalogInsightDetail{}))
		compactJSON, err := json.Marshal(compact)
		Expect(err).ToNot(HaveOccurred())
		Expect(string(compactJSON)).ToNot(ContainSubstring(`"message"`))
		fullJSON, err := json.Marshal(full)
		Expect(err).ToNot(HaveOccurred())
		Expect(string(fullJSON)).To(And(
			ContainSubstring(`"message":"instance has public ip"`),
			ContainSubstring(`"analysis":{"rule":"R1"}`),
			ContainSubstring(`"properties":[{"name":"port"`),
		))

		row := result.Items[0]
		Expect(row.Columns()).To(HaveLen(10))
		Expect(row.Row()).To(And(
			HaveKeyWithValue("ConfigID", "21e7586d-31fb-453c-a205-d73dc6b58eaa"),
			HaveKeyWithValue("ConfigName", "prod-instance"),
			HaveKeyWithValue("ConfigType", "AWS::EC2::Instance"),
			HaveKey("Name"),
			HaveKey("Summary"),
			HaveKey("InsightType"),
			HaveKey("Status"),
			HaveKey("Severity"),
			HaveKey("LastObserved"),
		))
		rendered, err := clicky.Format([]catalogInsightSearchHit{row}, clicky.FormatOptions{Pretty: true, NoColor: true})
		Expect(err).ToNot(HaveOccurred())
		Expect(rendered).To(And(
			ContainSubstring("Config ID"),
			ContainSubstring("Config Name"),
			ContainSubstring("Config Type"),
			ContainSubstring("prod-instance"),
		))
	})

	formats := []struct {
		name string
		opts clicky.FormatOptions
	}{
		{name: "pretty", opts: clicky.FormatOptions{Pretty: true, NoColor: true}},
		{name: "CSV", opts: clicky.FormatOptions{CSV: true}},
		{name: "Markdown", opts: clicky.FormatOptions{Markdown: true}},
		{name: "HTML", opts: clicky.FormatOptions{HTML: true}},
	}
	for _, format := range formats {
		ginkgo.It("uses the same insight columns for "+format.name, func() {
			severity := "high"
			firstObserved := time.Date(1999, 1, 1, 0, 0, 0, 0, time.UTC)
			lastObserved := time.Date(2031, 1, 1, 0, 0, 0, 0, time.UTC)
			hit := catalogInsightSearchHit{
				ID:            "id",
				Name:          "name",
				Summary:       "summary",
				InsightType:   "kind",
				Status:        "open",
				Severity:      &severity,
				IssueIDs:      []string{"hidden-issue"},
				FirstObserved: &firstObserved,
				LastObserved:  &lastObserved,
				Config: &catalogInsightConfig{
					ID:   "cid",
					Name: "cfg",
					Type: "type",
				},
			}

			columns := hit.Columns()
			labels := make([]string, len(columns))
			for i, column := range columns {
				labels[i] = column.DisplayLabel()
			}
			Expect(labels).To(Equal([]string{"Id", "Config ID", "Config Name", "Config Type", "Name", "Summary", "Insight Type", "Status", "Severity", "Last Observed"}))

			rendered, err := clicky.Format([]catalogInsightSearchHit{hit}, format.opts)
			Expect(err).ToNot(HaveOccurred())
			for _, value := range []string{"cid", "cfg", "type", "name", "summary", "kind", "open", "high", "2031"} {
				Expect(rendered).To(ContainSubstring(value))
			}
			Expect(rendered).ToNot(ContainSubstring("hidden-issue"))
			Expect(rendered).ToNot(ContainSubstring("1999"))
		})
	}

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

	ginkgo.It("registers full on insight search entry points and resolves list as an alias", func() {
		Expect(CatalogInsight.Flags().Lookup("full")).ToNot(BeNil())
		Expect(CatalogInsightSearch.Flags().Lookup("full")).ToNot(BeNil())
		Expect(CatalogInsightGet.Flags().Lookup("full")).To(BeNil())

		command, args, err := CatalogInsight.Find([]string{"list", "severity=critical"})
		Expect(err).ToNot(HaveOccurred())
		Expect(command).To(BeIdenticalTo(CatalogInsightSearch))
		Expect(args).To(Equal([]string{"severity=critical"}))
		Expect(catalogInsightSearchQuery(nil)).To(Equal("status=open"))
	})
})
