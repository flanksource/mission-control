package views

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"

	"github.com/flanksource/commons/properties"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/types"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/report"
)

// maxMultipartMemory is the max memory used when parsing multipart form data in tests (32MB).
const maxMultipartMemory = 32 << 20

var _ = ginkgo.Describe("ResolveServer with a facet connection", func() {
	var facetConn models.Connection

	ginkgo.BeforeEach(func() {
		facetConn = models.Connection{
			Name:       "test-facet-connection",
			Namespace:  "default",
			Type:       models.ConnectionTypeFacet,
			URL:        "http://facet.test",
			Password:   "facet-token",
			Properties: types.JSONStringMap{"timestampUrl": "http://tsa.test"},
			Source:     models.SourceUI,
		}
		Expect(DefaultContext.DB().Save(&facetConn).Error).ToNot(HaveOccurred())

		ginkgo.DeferCleanup(func() {
			Expect(DefaultContext.DB().Delete(&facetConn).Error).ToNot(HaveOccurred())
			properties.Global.Set(report.PropertyConnection, "")
			DefaultContext.ClearCache()
		})
	})

	ginkgo.It("resolves the url, token and timestamp url from the connection", func() {
		server, err := report.ResolveServer(DefaultContext, &v1.FacetOptions{Connection: "connection://default/test-facet-connection"})
		Expect(err).ToNot(HaveOccurred())
		Expect(server.BaseURL).To(Equal("http://facet.test"))
		Expect(server.Token).To(Equal("facet-token"))
		Expect(server.TimestampURL).To(Equal("http://tsa.test"))
	})

	ginkgo.It("resolves the connection named by the facet.connection property", func() {
		properties.Global.Set(report.PropertyConnection, "connection://default/test-facet-connection")
		DefaultContext.ClearCache()

		server, err := report.ResolveServer(DefaultContext, nil)
		Expect(err).ToNot(HaveOccurred())
		Expect(server.BaseURL).To(Equal("http://facet.test"))
		Expect(server.Token).To(Equal("facet-token"))
	})

	ginkgo.It("rejects a connection that is not a facet connection", func() {
		_, err := report.ResolveServer(DefaultContext, &v1.FacetOptions{Connection: connectionName})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("expected"))
	})
})

var _ = ginkgo.Describe("RenderHTTP", func() {
	var (
		server   *httptest.Server
		pdfBytes = []byte("%PDF-1.4 test content")
	)

	ginkgo.BeforeEach(func() {
		mux := http.NewServeMux()
		mux.HandleFunc("/render", func(w http.ResponseWriter, r *http.Request) {
			Expect(r.Method).To(Equal("POST"))
			Expect(r.Header.Get("X-API-Key")).To(Equal("test-token"))
			Expect(r.Header.Get("Content-Type")).To(ContainSubstring("multipart/form-data"))

			Expect(r.ParseMultipartForm(maxMultipartMemory)).To(Succeed())

			archiveFile, _, err := r.FormFile("archive")
			Expect(err).ToNot(HaveOccurred())
			archiveBytes, err := io.ReadAll(archiveFile)
			Expect(err).ToNot(HaveOccurred())
			Expect(len(archiveBytes)).To(BeNumerically(">", 0))

			dataStr := r.FormValue("data")
			Expect(dataStr).ToNot(BeEmpty())
			var data map[string]any
			Expect(json.Unmarshal([]byte(dataStr), &data)).To(Succeed())
			Expect(data).To(HaveKey("key"))

			optionsStr := r.FormValue("options")
			Expect(optionsStr).ToNot(BeEmpty())
			var options map[string]any
			Expect(json.Unmarshal([]byte(optionsStr), &options)).To(Succeed())
			Expect(options["format"]).To(Equal("pdf"))
			Expect(options["entryFile"]).To(Equal("ViewReport.tsx"))

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
		result, err := report.RenderHTTP(DefaultContext, server.URL, "test-token", map[string]string{"key": "value"}, "pdf", "ViewReport.tsx")
		Expect(err).ToNot(HaveOccurred())
		Expect(result).To(Equal(pdfBytes))
	})

	ginkgo.It("returns HTML directly without two-step fetch", func() {
		htmlBytes := []byte("<html><body>Report</body></html>")
		htmlServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			Expect(r.ParseMultipartForm(maxMultipartMemory)).To(Succeed())
			optionsStr := r.FormValue("options")
			var options map[string]any
			Expect(json.Unmarshal([]byte(optionsStr), &options)).To(Succeed())
			Expect(options["format"]).To(Equal("html"))
			w.Header().Set("Content-Type", "text/html")
			_, err := w.Write(htmlBytes)
			Expect(err).ToNot(HaveOccurred())
		}))
		defer htmlServer.Close()

		result, err := report.RenderHTTP(DefaultContext, htmlServer.URL, "", map[string]string{"key": "value"}, "html", "ViewReport.tsx")
		Expect(err).ToNot(HaveOccurred())
		Expect(result).To(Equal(htmlBytes))
	})
})
