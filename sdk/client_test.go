package sdk

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestSDK(t *testing.T) {
	RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "SDK")
}

var _ = ginkgo.Describe("GetConnection HTML detection", func() {
	ginkgo.It("returns ErrHTMLResponse when server returns HTML with 200 OK", func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`<!DOCTYPE html><html><body>Frontend</body></html>`))
		}))
		defer server.Close()

		client := New(server.URL, "fake-token")
		_, err := client.GetConnection("any", "default")
		Expect(errors.Is(err, ErrHTMLResponse)).To(BeTrue(), "got: %v", err)
	})

	ginkgo.It("returns ErrHTMLResponse when body starts with '<' even without HTML content-type", func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`<html>no content type header</html>`))
		}))
		defer server.Close()

		client := New(server.URL, "fake-token")
		_, err := client.GetConnection("any", "default")
		Expect(errors.Is(err, ErrHTMLResponse)).To(BeTrue(), "got: %v", err)
	})

	ginkgo.It("decodes JSON successfully when server returns valid JSON", func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[{"name":"azure-bearer","namespace":"monitoring","type":"http"}]`))
		}))
		defer server.Close()

		client := New(server.URL, "fake-token")
		conn, err := client.GetConnection("azure-bearer", "monitoring")
		Expect(err).ToNot(HaveOccurred())
		Expect(conn).ToNot(BeNil())
		Expect(conn.Name).To(Equal("azure-bearer"))
	})
})

var _ = ginkgo.Describe("TestConnection HTML detection", func() {
	ginkgo.It("returns ErrHTMLResponse on HTML error page (405 from frontend proxy)", func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusMethodNotAllowed)
			_, _ = w.Write([]byte(`<!DOCTYPE html><html><title>405</title></html>`))
		}))
		defer server.Close()

		client := New(server.URL, "fake-token")
		_, err := client.TestConnection("00000000-0000-0000-0000-000000000000")
		Expect(errors.Is(err, ErrHTMLResponse)).To(BeTrue(), "got: %v", err)
	})
})
