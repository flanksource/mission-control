package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"

	"github.com/flanksource/duty/query"
	"github.com/flanksource/incident-commander/clientcmd"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("faro catalog API paths", func() {
	ginkgo.BeforeEach(func() {
		dir := ginkgo.GinkgoT().TempDir()
		ginkgo.GinkgoT().Setenv("HOME", dir)
		ginkgo.GinkgoT().Setenv("XDG_CONFIG_HOME", dir)
	})

	ginkgo.It("uses direct backend paths when the context server is a backend URL", func() {
		server := catalogSearchServer("/resources/search")
		defer server.Close()
		storeRemoteContext(server.URL)

		items, err := remoteList(catalogListOpts{Limit: 5})

		Expect(err).ToNot(HaveOccurred())
		Expect(items).To(HaveLen(1))
		Expect(*items[0].Name).To(Equal("api"))
	})

	ginkgo.It("uses /api paths when the context server is a resolved frontend API URL", func() {
		server := catalogSearchServer("/api/resources/search")
		defer server.Close()
		storeRemoteContext(server.URL + "/api")

		items, err := remoteList(catalogListOpts{Limit: 5})

		Expect(err).ToNot(HaveOccurred())
		Expect(items).To(HaveLen(1))
		Expect(*items[0].Name).To(Equal("api"))
	})
})

func catalogSearchServer(expectedPath string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		Expect(r.Method).To(Equal(http.MethodPost))
		Expect(r.URL.Path).To(Equal(expectedPath))
		var got query.SearchResourcesRequest
		Expect(json.NewDecoder(r.Body).Decode(&got)).To(Succeed())
		Expect(got.Limit).To(Equal(5))
		Expect(got.Fields).To(ContainElements("created_at", "inserted_at", "updated_at"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"configs":[{"id":"00000000-0000-0000-0000-000000000001","name":"api","type":"Kubernetes::Pod"}]}`))
	}))
}

func storeRemoteContext(serverURL string) {
	Expect(clientcmd.SaveConfig(&clientcmd.MCConfig{
		CurrentContext: "test",
		Contexts: []clientcmd.MCContext{{
			Name:   "test",
			Server: serverURL,
			Token:  "token",
		}},
	})).To(Succeed())
}
