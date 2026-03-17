package views

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	v1 "github.com/flanksource/incident-commander/api/v1"
)

var _ = ginkgo.Describe("renderFacetHTTP", func() {
	var (
		server   *httptest.Server
		pdfBytes = []byte("%PDF-1.4 test content")
	)

	ginkgo.BeforeEach(func() {
		mux := http.NewServeMux()
		mux.HandleFunc("/render", func(w http.ResponseWriter, r *http.Request) {
			Expect(r.Method).To(Equal("POST"))
			Expect(r.Header.Get("X-API-Key")).To(Equal("test-token"))
			Expect(r.Header.Get("Content-Type")).To(Equal("application/gzip"))
			Expect(r.URL.Query().Get("format")).To(Equal("pdf"))
			Expect(r.URL.Query().Get("entryFile")).To(Equal("ViewReport.tsx"))

			dataHeader := r.Header.Get("X-Facet-Data")
			Expect(dataHeader).ToNot(BeEmpty())
			decoded, err := base64.StdEncoding.DecodeString(dataHeader)
			Expect(err).ToNot(HaveOccurred())
			var data map[string]any
			Expect(json.Unmarshal(decoded, &data)).To(Succeed())
			Expect(data).To(HaveKey("key"))

			body, err := io.ReadAll(r.Body)
			Expect(err).ToNot(HaveOccurred())
			Expect(len(body)).To(BeNumerically(">", 0))

			w.Header().Set("Content-Type", "application/json")
			Expect(json.NewEncoder(w).Encode(map[string]string{"url": "/results/abc123"})).To(Succeed())
		})
		mux.HandleFunc("/results/abc123", func(w http.ResponseWriter, r *http.Request) {
			Expect(r.Method).To(Equal("GET"))
			w.Header().Set("Content-Type", "application/pdf")
			_, err := w.Write(pdfBytes)
			Expect(err).ToNot(HaveOccurred())
		})
		server = httptest.NewServer(mux)
	})

	ginkgo.AfterEach(func() {
		server.Close()
	})

	ginkgo.It("fetches PDF via two-step render+download", func() {
		opts := &v1.FacetOptions{
			URL: server.URL,
			PDFOptions: &v1.FacetPDFOptions{
				PageSize: "A4",
			},
		}

		result, err := renderFacetHTTP(DefaultContext, server.URL, "test-token", map[string]string{"key": "value"}, "pdf", opts)
		Expect(err).ToNot(HaveOccurred())
		Expect(result).To(Equal(pdfBytes))
	})

	ginkgo.It("returns HTML directly without two-step fetch", func() {
		htmlBytes := []byte("<html><body>Report</body></html>")
		htmlServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			Expect(r.URL.Query().Get("format")).To(Equal("html"))
			w.Header().Set("Content-Type", "text/html")
			_, err := w.Write(htmlBytes)
			Expect(err).ToNot(HaveOccurred())
		}))
		defer htmlServer.Close()

		result, err := renderFacetHTTP(DefaultContext, htmlServer.URL, "", map[string]string{"key": "value"}, "html", nil)
		Expect(err).ToNot(HaveOccurred())
		Expect(result).To(Equal(htmlBytes))
	})
})
