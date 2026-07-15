package sdk

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"

	"github.com/flanksource/duty/query"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("catalog client", func() {
	ginkgo.It("posts a search request and decodes configs", func() {
		var gotBody query.SearchResourcesRequest
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			Expect(r.Method).To(Equal(http.MethodPost))
			Expect(r.URL.Path).To(Equal("/resources/search"))
			Expect(json.NewDecoder(r.Body).Decode(&gotBody)).To(Succeed())
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"configs":[{"id":"c1","name":"api","type":"Kubernetes::Pod"}]}`))
		}))
		defer server.Close()

		resp, err := New(server.URL, "tok").SearchCatalog(context.Background(), query.SearchResourcesRequest{Limit: 5})
		Expect(err).ToNot(HaveOccurred())
		Expect(gotBody.Limit).To(Equal(5))
		Expect(resp.Configs).To(HaveLen(1))
		Expect(resp.Configs[0].Name).To(Equal("api"))
	})

	ginkgo.It("gets a single catalog item by id", func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			Expect(r.Method).To(Equal(http.MethodGet))
			Expect(r.URL.Path).To(Equal("/resources/abc"))
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"3a96d327-2a6b-4a3a-9b2a-1f0f6b6b6b6b","name":"my-config"}`))
		}))
		defer server.Close()

		item, err := New(server.URL, "tok").GetCatalogItem(context.Background(), "abc")
		Expect(err).ToNot(HaveOccurred())
		Expect(item.Name).ToNot(BeNil())
		Expect(*item.Name).To(Equal("my-config"))
	})

	ginkgo.It("gets full catalog change details from PostgREST", func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			Expect(r.Method).To(Equal(http.MethodGet))
			Expect(r.URL.Path).To(Equal("/db/config_changes"))
			Expect(r.URL.Query().Get("id")).To(Equal("eq.521bae33-e4c3-42eb-a9c5-071ab92940b5"))
			Expect(r.URL.Query().Get("select")).To(Equal(catalogChangeDetailSelect))
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"id":"521bae33-e4c3-42eb-a9c5-071ab92940b5","config_id":"21e7586d-31fb-453c-a205-d73dc6b58eaa","change_type":"Failed","created_at":"2026-06-24T16:41:38Z","source":"kubernetes/","details":{"reason":"Failed"},"config":{"id":"21e7586d-31fb-453c-a205-d73dc6b58eaa","name":"opensearch-fail","type":"MissionControl::Canary","config_class":"Canary"},"artifacts":[]}]`))
		}))
		defer server.Close()

		change, err := New(server.URL, "tok").GetCatalogChange(context.Background(), "521bae33-e4c3-42eb-a9c5-071ab92940b5")
		Expect(err).ToNot(HaveOccurred())
		Expect(change.ChangeType).To(Equal("Failed"))
		Expect(change.Details).To(HaveKeyWithValue("reason", "Failed"))
		Expect(change.Config).ToNot(BeNil())
		Expect(change.Config.Name).To(Equal("opensearch-fail"))
	})

	ginkgo.It("gets full catalog insight details from PostgREST", func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			Expect(r.Method).To(Equal(http.MethodGet))
			Expect(r.URL.Path).To(Equal("/db/config_analysis"))
			Expect(r.URL.Query().Get("id")).To(Equal("eq.521bae33-e4c3-42eb-a9c5-071ab92940b5"))
			Expect(r.URL.Query().Get("select")).To(Equal(catalogInsightDetailSelect))
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"id":"521bae33-e4c3-42eb-a9c5-071ab92940b5","config_id":"21e7586d-31fb-453c-a205-d73dc6b58eaa","analyzer":"no-public-ip","message":"instance has public ip","summary":"public ip","status":"open","severity":"high","analysis_type":"security","analysis":{"rule":"R1"},"config":{"id":"21e7586d-31fb-453c-a205-d73dc6b58eaa","name":"prod-instance","type":"AWS::EC2::Instance","config_class":"EC2"}}]`))
		}))
		defer server.Close()

		insight, err := New(server.URL, "tok").GetCatalogInsight(context.Background(), "521bae33-e4c3-42eb-a9c5-071ab92940b5")
		Expect(err).ToNot(HaveOccurred())
		Expect(insight.Analyzer).To(Equal("no-public-ip"))
		Expect(insight.Analysis).To(HaveKeyWithValue("rule", "R1"))
		Expect(insight.Config).ToNot(BeNil())
		Expect(insight.Config.Name).To(Equal("prod-instance"))
	})

	ginkgo.It("returns not found for an empty catalog change response", func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[]`))
		}))
		defer server.Close()

		_, err := New(server.URL, "tok").GetCatalogChange(context.Background(), "missing")
		Expect(errors.Is(err, ErrNotFound)).To(BeTrue())
	})

	ginkgo.It("fetches relationships and exposes both tree directions", func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			Expect(r.URL.Path).To(Equal("/catalog/root-id/relationships"))
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"00000000-0000-0000-0000-000000000000","outgoing":{"id":"00000000-0000-0000-0000-000000000000","name":"root"}}`))
		}))
		defer server.Close()

		rels, err := New(server.URL, "tok").GetCatalogRelationships(context.Background(), "root-id")
		Expect(err).ToNot(HaveOccurred())
		Expect(rels.Outgoing).ToNot(BeNil())
		Expect(rels.Outgoing.Name).ToNot(BeNil())
		Expect(*rels.Outgoing.Name).To(Equal("root"))
	})

	ginkgo.It("returns ErrHTMLResponse when the frontend answers a catalog search", func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<!DOCTYPE html><html></html>"))
		}))
		defer server.Close()

		_, err := New(server.URL, "tok").SearchCatalog(context.Background(), query.SearchResourcesRequest{})
		Expect(err).To(MatchError(ErrHTMLResponse))
	})
})
