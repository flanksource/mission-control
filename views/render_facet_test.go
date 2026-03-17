package views

import (
	"encoding/json"
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

			var body map[string]any
			Expect(json.NewDecoder(r.Body).Decode(&body)).To(Succeed())
			Expect(body["template"]).To(Equal("ViewReport.tsx"))
			Expect(body["format"]).To(Equal("pdf"))

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
			w.Header().Set("Content-Type", "text/html")
			_, err := w.Write(htmlBytes)
			Expect(err).ToNot(HaveOccurred())
		}))
		defer htmlServer.Close()

		result, err := renderFacetHTTP(DefaultContext, htmlServer.URL, "", map[string]string{"key": "value"}, "html", nil)
		Expect(err).ToNot(HaveOccurred())
		Expect(result).To(Equal(htmlBytes))
	})

	ginkgo.It("sends timestamp URL in signature when configured", func() {
		var receivedBody map[string]any
		tsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/render" {
				Expect(json.NewDecoder(r.Body).Decode(&receivedBody)).To(Succeed())
				w.Header().Set("Content-Type", "application/json")
				Expect(json.NewEncoder(w).Encode(map[string]string{"url": "/results/ts123"})).To(Succeed())
				return
			}
			_, err := w.Write(pdfBytes)
			Expect(err).ToNot(HaveOccurred())
		}))
		defer tsServer.Close()

		opts := &v1.FacetOptions{
			TimestampURL: "http://timestamp.example.com",
		}

		_, err := renderFacetHTTP(DefaultContext, tsServer.URL, "", map[string]string{}, "pdf", opts)
		Expect(err).ToNot(HaveOccurred())

		sig, ok := receivedBody["signature"].(map[string]any)
		Expect(ok).To(BeTrue())
		Expect(sig["timestampUrl"]).To(Equal("http://timestamp.example.com"))
	})
})
