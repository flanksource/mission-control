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

			var got query.SearchResourcesRequest
			Expect(json.NewDecoder(r.Body).Decode(&got)).To(Succeed())
			Expect(got.Limit).To(Equal(25))
			Expect(got.Configs).To(HaveLen(1))
			Expect(got.Configs[0].Search).To(Equal("tags.cluster=beta-cluster type=pod mission-control"))
			Expect(got.Configs[0].Agent).To(Equal("all"))

			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"configs":[{"id":"00000000-0000-0000-0000-000000000001","name":"api","type":"Kubernetes::Pod","health":"healthy","status":"Running"}]}`))
		}))
		defer server.Close()
		storeRemoteContext(server.URL)

		items, err := remoteSearch("tags.cluster=beta-cluster type=pod mission-control", "all", 25)

		Expect(err).ToNot(HaveOccurred())
		Expect(items).To(HaveLen(1))
		Expect(*items[0].Name).To(Equal("api"))
		Expect(*items[0].Type).To(Equal("Kubernetes::Pod"))
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

		_, err := remoteSearch("type=Kubernetes::Pod", "all", 0)
		Expect(err).ToNot(HaveOccurred())
	})
})
