package sdk

import (
	"context"
	"encoding/json"
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
