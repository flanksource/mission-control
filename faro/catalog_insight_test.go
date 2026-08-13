package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/flanksource/clicky"
	"github.com/flanksource/clicky/api"
	"github.com/flanksource/duty/query"
	"github.com/flanksource/incident-commander/sdk"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// ids returns count sequential insight ids, so a paging fixture can be described by its
// size rather than by a literal list.
func ids(start, count int) []string {
	out := make([]string, count)
	for i := range out {
		out[i] = fmt.Sprintf("00000000-0000-0000-0000-%012d", start+i)
	}
	return out
}

func distinctIDs(items []catalogInsightSearchHit) map[string]struct{} {
	unique := make(map[string]struct{}, len(items))
	for _, item := range items {
		unique[item.ID] = struct{}{}
	}
	return unique
}

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

				var got clientapi.SearchResourcesRequest
				Expect(json.NewDecoder(r.Body).Decode(&got)).To(Succeed())
				// One past the caller's 25: the extra row is what tells "exactly 25
				// matched" apart from "25 matched and more remain".
				Expect(got.Limit).To(Equal(26))
				Expect(got.Timestamps).To(BeTrue())
				Expect(got.ConfigAnalysis).To(HaveLen(1))
				Expect(got.ConfigAnalysis[0].Search).To(Equal("severity=high type=security sort=id offset=0"))
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
		Expect(full).To(BeAssignableToTypeOf([]catalogInsightDetailView{}))
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
		// A ten-column table is fitted to the terminal, so without pinning a width this
		// asserts on whatever size the test happens to run under and truncates the cells
		// mid-value. The subject here is which columns faro maps, not how narrow a
		// terminal wraps them.
		api.SetTerminalWidth(240)
		defer api.SetTerminalWidth(0)
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

	ginkgo.It("asks for a full page when no limit bounds the search", func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var got clientapi.SearchResourcesRequest
			Expect(json.NewDecoder(r.Body).Decode(&got)).To(Succeed())
			Expect(got.Limit).To(Equal(query.MaxSearchResourcesLimit))
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"config_analysis":[]}`))
		}))
		defer server.Close()
		storeRemoteContext(server.URL)

		result, err := remoteSearchInsights("severity=high", "all", insightSearchUnlimited)
		Expect(err).ToNot(HaveOccurred())
		Expect(result.Limited).To(BeFalse())
	})

	// insightPageServer serves `pages` of search hits keyed by the offset the request
	// carries, hydrating whatever ids it handed out. It records every search expression it
	// saw so a test can assert the paging walk itself.
	insightPageServer := func(pages map[int][]string, seen *[]string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch r.URL.Path {
			case "/resources/search":
				var got query.SearchResourcesRequest
				Expect(json.NewDecoder(r.Body).Decode(&got)).To(Succeed())
				search := got.ConfigAnalysis[0].Search
				*seen = append(*seen, search)
				offset := 0
				for candidate := range pages {
					if strings.HasSuffix(search, fmt.Sprintf("offset=%d", candidate)) {
						offset = candidate
					}
				}
				hits := make([]string, 0, len(pages[offset]))
				for _, id := range pages[offset] {
					hits = append(hits, fmt.Sprintf(`{"id":%q,"type":"security","status":"open"}`, id))
				}
				_, _ = w.Write([]byte(`{"config_analysis":[` + strings.Join(hits, ",") + `]}`))
			case "/db/config_analysis":
				details := make([]string, 0)
				for _, page := range pages {
					for _, id := range page {
						details = append(details, fmt.Sprintf(`{"id":%q,"analysis_type":"security","status":"open"}`, id))
					}
				}
				_, _ = w.Write([]byte(`[` + strings.Join(details, ",") + `]`))
			default:
				ginkgo.Fail("unexpected request: " + r.URL.Path)
			}
		}))
	}

	ginkgo.It("pages until a short page and walks the offsets in order", func() {
		var seen []string
		server := insightPageServer(map[int][]string{
			0:                                 ids(0, query.MaxSearchResourcesLimit),
			query.MaxSearchResourcesLimit:     ids(query.MaxSearchResourcesLimit, query.MaxSearchResourcesLimit),
			2 * query.MaxSearchResourcesLimit: ids(2*query.MaxSearchResourcesLimit, 5),
		}, &seen)
		defer server.Close()
		storeRemoteContext(server.URL)

		result, err := remoteSearchInsights("status=resolved", "all", insightSearchUnlimited)

		Expect(err).ToNot(HaveOccurred())
		Expect(result.Items).To(HaveLen(2*query.MaxSearchResourcesLimit + 5))
		Expect(result.Limited).To(BeFalse())
		Expect(seen).To(Equal([]string{
			"status=resolved sort=id offset=0",
			fmt.Sprintf("status=resolved sort=id offset=%d", query.MaxSearchResourcesLimit),
			fmt.Sprintf("status=resolved sort=id offset=%d", 2*query.MaxSearchResourcesLimit),
		}))
	})

	// Measured against a live catalog: offset paging over the search view repeats the row
	// on every page boundary, because the view carries no ORDER BY of its own. Paging must
	// absorb that rather than hand the caller a duplicate.
	ginkgo.It("returns a row once when the server repeats it across a page boundary", func() {
		var seen []string
		boundary := "00000000-0000-0000-0000-0000000000ff"
		server := insightPageServer(map[int][]string{
			0:                             append(ids(0, query.MaxSearchResourcesLimit-1), boundary),
			query.MaxSearchResourcesLimit: {boundary, "00000000-0000-0000-0000-000000000fff"},
		}, &seen)
		defer server.Close()
		storeRemoteContext(server.URL)

		result, err := remoteSearchInsights("status=resolved", "all", insightSearchUnlimited)

		Expect(err).ToNot(HaveOccurred())
		Expect(result.Items).To(HaveLen(query.MaxSearchResourcesLimit + 1))
		Expect(distinctIDs(result.Items)).To(HaveLen(query.MaxSearchResourcesLimit + 1))
	})

	ginkgo.It("stops at an explicit limit and reports the result as limited", func() {
		var seen []string
		server := insightPageServer(map[int][]string{0: ids(0, 3)}, &seen)
		defer server.Close()
		storeRemoteContext(server.URL)

		result, err := remoteSearchInsights("status=open", "all", 2)

		Expect(err).ToNot(HaveOccurred())
		Expect(result.Items).To(HaveLen(2))
		Expect(result.Limited).To(BeTrue())
		Expect(seen).To(HaveLen(1))
	})

	ginkgo.It("rejects a search expression that pages itself", func() {
		for search, key := range map[string]string{
			"status=open offset=10": "offset",
			"status=open sort=id":   "sort",
		} {
			_, err := remoteSearchInsights(search, "all", insightSearchUnlimited)
			Expect(err).To(MatchError(ContainSubstring("sets " + key)))
		}
	})

	// The guard reads whole tokens, so a value that merely spells one of the reserved keys
	// inside itself is an ordinary search, not a caller rolling its own paging.
	ginkgo.It("accepts a search whose value contains a reserved key as a substring", func() {
		var seen []string
		server := insightPageServer(map[int][]string{0: ids(0, 1)}, &seen)
		defer server.Close()
		storeRemoteContext(server.URL)

		_, err := remoteSearchInsights("name=resort=weekly", "all", insightSearchUnlimited)

		Expect(err).ToNot(HaveOccurred())
		Expect(seen).To(Equal([]string{"name=resort=weekly sort=id offset=0"}))
	})

	// A result that exactly fills the limit is complete, not truncated: reporting it as
	// limited told the user to raise a limit that was already big enough, and failed a
	// projection whose source matched its limit exactly.
	ginkgo.It("reports a result that exactly fills the limit as complete", func() {
		var seen []string
		server := insightPageServer(map[int][]string{0: ids(0, 2)}, &seen)
		defer server.Close()
		storeRemoteContext(server.URL)

		result, err := remoteSearchInsights("status=open", "all", 2)

		Expect(err).ToNot(HaveOccurred())
		Expect(result.Items).To(HaveLen(2))
		Expect(result.Limited).To(BeFalse())
		Expect(result.TotalAtLeast).To(Equal(2))
	})

	ginkgo.It("counts one past the limit when more remain", func() {
		var seen []string
		server := insightPageServer(map[int][]string{0: ids(0, 3)}, &seen)
		defer server.Close()
		storeRemoteContext(server.URL)

		result, err := remoteSearchInsights("status=open", "all", 2)

		Expect(err).ToNot(HaveOccurred())
		Expect(result.Items).To(HaveLen(2))
		Expect(result.Limited).To(BeTrue())
		Expect(result.TotalAtLeast).To(Equal(3))
	})

	// A server that ignores offset serves page 0 forever. Every row after the first page is
	// a duplicate, so without a guard the loop never terminates.
	ginkgo.It("fails rather than looping when the server ignores offset", func() {
		var seen []string
		page := ids(0, query.MaxSearchResourcesLimit)
		server := insightPageServer(map[int][]string{
			0:                                 page,
			query.MaxSearchResourcesLimit:     page,
			2 * query.MaxSearchResourcesLimit: page,
		}, &seen)
		defer server.Close()
		storeRemoteContext(server.URL)

		_, err := remoteSearchInsights("status=open", "all", insightSearchUnlimited)

		Expect(err).To(MatchError(ContainSubstring("ignoring offset")))
		Expect(seen).To(HaveLen(2))
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
		insight := result.(*catalogInsightDetailView)
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
