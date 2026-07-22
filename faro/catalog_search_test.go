package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"

	"github.com/flanksource/duty/query"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("faro catalog search", func() {
	ginkgo.BeforeEach(func() {
		dir := ginkgo.GinkgoT().TempDir()
		ginkgo.GinkgoT().Setenv("HOME", dir)
		ginkgo.GinkgoT().Setenv("XDG_CONFIG_HOME", dir)
	})

	ginkgo.It("forwards the grammar query, agent and limit to /resources/search", func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			Expect(r.Method).To(Equal(http.MethodPost))
			Expect(r.URL.Path).To(Equal("/resources/search"))
			w.Header().Set("Content-Type", "application/json")

			var got query.SearchResourcesRequest
			Expect(json.NewDecoder(r.Body).Decode(&got)).To(Succeed())
			Expect(got.Limit).To(Equal(25))
			Expect(got.Timestamps).To(BeTrue())
			Expect(got.Configs).To(HaveLen(1))
			Expect(got.Configs[0].Search).To(Equal("tags.cluster=beta-cluster type=pod mission-control"))
			Expect(got.Configs[0].Agent).To(Equal("all"))

			_, _ = w.Write([]byte(`{"configs":[{"id":"00000000-0000-0000-0000-000000000001","name":"api","type":"Kubernetes::Pod","health":"healthy","status":"Running","created_at":"2026-06-24T16:41:38Z","updated_at":"2026-06-24T16:43:38Z","deleted_at":"2026-06-24T16:44:38Z"}]}`))
		}))
		defer server.Close()
		storeRemoteContext(server.URL)

		items, err := remoteSearch("tags.cluster=beta-cluster type=pod mission-control", "all", 25, false)

		Expect(err).ToNot(HaveOccurred())
		Expect(items).To(HaveLen(1))
		Expect(*items[0].Name).To(Equal("api"))
		Expect(*items[0].Type).To(Equal("Kubernetes::Pod"))
		Expect(items[0].CreatedAt.IsZero()).To(BeFalse())
		Expect(items[0].UpdatedAt).ToNot(BeNil())
		Expect(items[0].DeletedAt).ToNot(BeNil())
	})

	ginkgo.It("defaults an empty limit to 100", func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var got query.SearchResourcesRequest
			Expect(json.NewDecoder(r.Body).Decode(&got)).To(Succeed())
			Expect(got.Limit).To(Equal(100))
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"configs":[]}`))
		}))
		defer server.Close()
		storeRemoteContext(server.URL)

		_, err := remoteSearch("type=Kubernetes::Pod", "all", 0, false)
		Expect(err).ToNot(HaveOccurred())
	})

	ginkgo.It("hydrates complete catalog items when full is requested", func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch r.URL.Path {
			case "/resources/search":
				_, _ = w.Write([]byte(`{"configs":[{"id":"00000000-0000-0000-0000-000000000001","name":"api","type":"Kubernetes::Pod"}]}`))
			case "/db/config_items":
				Expect(r.URL.Query().Get("id")).To(Equal("in.(00000000-0000-0000-0000-000000000001)"))
				_, _ = w.Write([]byte(`[{"id":"00000000-0000-0000-0000-000000000001","name":"api","type":"Kubernetes::Pod","config_class":"Pod","description":"full record","config":{"kind":"Pod"},"properties":[{"name":"namespace","text":"production"}]}]`))
			default:
				ginkgo.Fail("unexpected request: " + r.URL.Path)
			}
		}))
		defer server.Close()
		storeRemoteContext(server.URL)

		items, err := remoteSearch("type=Kubernetes::Pod", "all", 25, true)

		Expect(err).ToNot(HaveOccurred())
		Expect(items).To(HaveLen(1))
		Expect(items[0].Description).ToNot(BeNil())
		Expect(*items[0].Description).To(Equal("full record"))
		Expect(items[0].Properties).ToNot(BeNil())
		Expect(*items[0].Properties).To(HaveLen(1))
	})

	ginkgo.It("falls back to lightweight results when an item vanishes during hydration", func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch r.URL.Path {
			case "/resources/search":
				_, _ = w.Write([]byte(`{"configs":[{"id":"00000000-0000-0000-0000-000000000001","name":"vanished","type":"Kubernetes::Pod"},{"id":"00000000-0000-0000-0000-000000000002","name":"api","type":"Kubernetes::Pod"}]}`))
			case "/db/config_items":
				_, _ = w.Write([]byte(`[{"id":"00000000-0000-0000-0000-000000000002","name":"api","type":"Kubernetes::Pod","config_class":"Pod","description":"full record"}]`))
			default:
				ginkgo.Fail("unexpected request: " + r.URL.Path)
			}
		}))
		defer server.Close()
		storeRemoteContext(server.URL)

		items, err := remoteSearch("type=Kubernetes::Pod", "all", 25, true)

		Expect(err).ToNot(HaveOccurred())
		Expect(items).To(HaveLen(2))
		Expect(items[0].ID.String()).To(Equal("00000000-0000-0000-0000-000000000001"))
		Expect(*items[0].Name).To(Equal("vanished"))
		Expect(items[0].Description).To(BeNil())
		Expect(items[1].ID.String()).To(Equal("00000000-0000-0000-0000-000000000002"))
		Expect(items[1].Description).ToNot(BeNil())
		Expect(*items[1].Description).To(Equal("full record"))
	})
})
