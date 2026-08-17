package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/flanksource/incident-commander/clientapi"
	sdk "github.com/flanksource/incident-commander/sdk/client"
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

			var got clientapi.SearchResourcesRequest
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
			var got clientapi.SearchResourcesRequest
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

	ginkgo.It("fetches config-scoped downstream changes including soft relationships", func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			Expect(r.Method).To(Equal(http.MethodPost))
			Expect(r.URL.Path).To(Equal("/catalog/changes"))

			var got clientapi.CatalogChangesSearchRequest
			Expect(json.NewDecoder(r.Body).Decode(&got)).To(Succeed())
			Expect(got.CatalogID).To(Equal("03e294e4-a297-5047-5325-3041303b1ce0"))
			Expect(got.Recursive).To(Equal(clientapi.CatalogChangeRecursiveDownstream))
			Expect(got.Depth).To(Equal(5))
			Expect(got.Soft).To(BeTrue())
			Expect(got.PageSize).To(Equal(25))
			Expect(got.SortBy).To(Equal("-created_at"))

			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"total":1,"changes":[{"id":"0274d556-6257-426a-b651-0a9bc35c26d8","config_id":"21e7586d-31fb-453c-a205-d73dc6b58eaa","agent_id":"00000000-0000-0000-0000-000000000000","name":"api","type":"Kubernetes::Deployment","tags":{"namespace":"production"},"change_type":"diff","created_at":"2026-06-24T16:41:38Z"}]}`))
		}))
		defer server.Close()
		storeRemoteContext(server.URL)

		items, err := remoteSearchRelatedChanges(catalogChangeSearchOptions{
			ConfigID: "03e294e4-a297-5047-5325-3041303b1ce0",
			Related:  clientapi.CatalogChangeRecursiveDownstream,
			Depth:    5,
			Soft:     true,
			Limit:    25,
		})

		Expect(err).ToNot(HaveOccurred())
		Expect(items).To(HaveLen(1))
		Expect(items[0].ConfigID).To(Equal("21e7586d-31fb-453c-a205-d73dc6b58eaa"))
		Expect(items[0].Name).To(Equal("api"))
		Expect(items[0].Namespace).To(Equal("production"))
		Expect(items[0].ConfigType).To(Equal("Kubernetes::Deployment"))
		Expect(items[0].ChangeType).To(Equal("diff"))
	})

	validationTests := []struct {
		name        string
		searchQuery string
		opts        catalogChangeSearchOptions
		errorText   string
	}{
		{name: "requires a query or config", errorText: "query or --config is required"},
		{
			name:        "rejects query with config",
			searchQuery: "change_type=diff",
			opts:        catalogChangeSearchOptions{ConfigID: "03e294e4-a297-5047-5325-3041303b1ce0", Depth: 5, Related: clientapi.CatalogChangeRecursiveNone},
			errorText:   "cannot be used together",
		},
		{
			name:      "rejects relationship flags without config",
			opts:      catalogChangeSearchOptions{Depth: 5, Related: clientapi.CatalogChangeRecursiveDownstream, RelatedSet: true},
			errorText: "require --config",
		},
		{
			name:      "validates config UUID",
			opts:      catalogChangeSearchOptions{ConfigID: "not-a-uuid", Depth: 5, Related: clientapi.CatalogChangeRecursiveNone},
			errorText: "invalid --config UUID",
		},
		{
			name:      "validates related direction",
			opts:      catalogChangeSearchOptions{ConfigID: "03e294e4-a297-5047-5325-3041303b1ce0", Depth: 5, Related: "sideways"},
			errorText: "--related must be one of",
		},
		{
			name:      "validates depth",
			opts:      catalogChangeSearchOptions{ConfigID: "03e294e4-a297-5047-5325-3041303b1ce0", Related: clientapi.CatalogChangeRecursiveDownstream},
			errorText: "--depth must be greater than zero",
		},
		{
			name:      "rejects soft with no relationship traversal",
			opts:      catalogChangeSearchOptions{ConfigID: "03e294e4-a297-5047-5325-3041303b1ce0", Depth: 5, Related: clientapi.CatalogChangeRecursiveNone, Soft: true, SoftSet: true},
			errorText: "require --related",
		},
		{
			name:        "accepts a global query",
			searchQuery: "change_type=diff",
			opts:        catalogChangeSearchOptions{Depth: 5, Related: clientapi.CatalogChangeRecursiveNone},
		},
		{
			name: "accepts config relationship traversal",
			opts: catalogChangeSearchOptions{
				ConfigID: "03e294e4-a297-5047-5325-3041303b1ce0",
				Depth:    5,
				Related:  clientapi.CatalogChangeRecursiveAll,
				Soft:     true,
				SoftSet:  true,
			},
		},
	}

	for _, tt := range validationTests {
		ginkgo.It(tt.name, func() {
			err := validateCatalogChangeSearch(tt.searchQuery, tt.opts)
			if tt.errorText == "" {
				Expect(err).ToNot(HaveOccurred())
			} else {
				Expect(err).To(MatchError(ContainSubstring(tt.errorText)))
			}
		})
	}

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
