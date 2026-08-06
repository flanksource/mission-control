package report

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/flanksource/commons/properties"
	"github.com/flanksource/duty/context"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	v1 "github.com/flanksource/incident-commander/api/v1"
)

// setProperty sets a global property for the duration of the spec and clears
// the duty property cache so the change is visible to ctx.Properties().
func setProperty(ctx context.Context, key, value string) {
	properties.Global.Set(key, value)
	ctx.ClearCache()
	ginkgo.DeferCleanup(func() {
		properties.Global.Set(key, "")
		ctx.ClearCache()
	})
}

var _ = ginkgo.Describe("ResolveServer", func() {
	var ctx context.Context

	ginkgo.BeforeEach(func() {
		ctx = context.New()
		ctx.ClearCache()
	})

	ginkgo.It("returns an unconfigured server when nothing is set", func() {
		server, err := ResolveServer(ctx, nil)
		Expect(err).ToNot(HaveOccurred())
		Expect(server.Configured()).To(BeFalse())
		Expect(server.BaseURL).To(BeEmpty())
	})

	ginkgo.It("uses the url from the options", func() {
		server, err := ResolveServer(ctx, &v1.FacetOptions{URL: "http://facet.local"})
		Expect(err).ToNot(HaveOccurred())
		Expect(server.Configured()).To(BeTrue())
		Expect(server.BaseURL).To(Equal("http://facet.local"))
	})

	ginkgo.It("falls back to the facet.url property", func() {
		setProperty(ctx, PropertyURL, "http://facet.property")

		server, err := ResolveServer(ctx, nil)
		Expect(err).ToNot(HaveOccurred())
		Expect(server.BaseURL).To(Equal("http://facet.property"))
	})

	ginkgo.It("prefers the options url over the facet.url property", func() {
		setProperty(ctx, PropertyURL, "http://facet.property")

		server, err := ResolveServer(ctx, &v1.FacetOptions{URL: "http://facet.explicit"})
		Expect(err).ToNot(HaveOccurred())
		Expect(server.BaseURL).To(Equal("http://facet.explicit"))
	})

	ginkgo.It("carries the timestamp url from the options", func() {
		setProperty(ctx, PropertyURL, "http://facet.property")

		opts := &v1.FacetOptions{}
		opts.TimestampURL = "http://tsa.local"

		server, err := ResolveServer(ctx, opts)
		Expect(err).ToNot(HaveOccurred())
		Expect(server.TimestampURL).To(Equal("http://tsa.local"))
	})

	ginkgo.It("parses the render timeout from the options", func() {
		server, err := ResolveServer(ctx, &v1.FacetOptions{URL: "http://facet.local", Timeout: "5m"})
		Expect(err).ToNot(HaveOccurred())
		Expect(server.Timeout).To(Equal(5 * time.Minute))
	})

	ginkgo.It("leaves the render timeout to the facet server when the options don't set one", func() {
		server, err := ResolveServer(ctx, &v1.FacetOptions{URL: "http://facet.local"})
		Expect(err).ToNot(HaveOccurred())
		Expect(server.Timeout).To(BeZero())
	})

	ginkgo.It("fails on an invalid render timeout", func() {
		_, err := ResolveServer(ctx, &v1.FacetOptions{URL: "http://facet.local", Timeout: "soon"})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("soon"))
	})
})

var _ = ginkgo.Describe("Render", func() {
	var (
		ctx        context.Context
		server     *httptest.Server
		gotAPIKey  string
		gotOptions map[string]any
		rendered   = []byte("<html><body>Rendered</body></html>")
	)

	ginkgo.BeforeEach(func() {
		ctx = context.New()
		ctx.ClearCache()
		gotAPIKey = ""
		gotOptions = nil

		server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotAPIKey = r.Header.Get("X-API-Key")
			Expect(r.ParseMultipartForm(32 << 20)).To(Succeed())

			var options map[string]any
			Expect(json.Unmarshal([]byte(r.FormValue("options")), &options)).To(Succeed())
			Expect(options["entryFile"]).To(Equal("CatalogReport.tsx"))
			gotOptions = options

			w.Header().Set("Content-Type", "text/html")
			_, err := w.Write(rendered)
			Expect(err).ToNot(HaveOccurred())
		}))
		ginkgo.DeferCleanup(server.Close)
	})

	ginkgo.It("renders via the server configured in the facet.url property", func() {
		setProperty(ctx, PropertyURL, server.URL)

		result, err := Render(ctx, map[string]string{"key": "value"}, "html", "CatalogReport.tsx", "", nil)
		Expect(err).ToNot(HaveOccurred())
		Expect(result.Data).To(Equal(rendered))
	})

	ginkgo.It("renders via the server given in the options", func() {
		result, err := Render(ctx, map[string]string{"key": "value"}, "html", "CatalogReport.tsx", "", &v1.FacetOptions{URL: server.URL})
		Expect(err).ToNot(HaveOccurred())
		Expect(result.Data).To(Equal(rendered))
	})

	ginkgo.It("sends the api key to the configured server", func() {
		result, err := RenderWith(ctx, map[string]string{"key": "value"}, "html", "CatalogReport.tsx", "",
			Server{BaseURL: server.URL, Token: "secret-token"})
		Expect(err).ToNot(HaveOccurred())
		Expect(result.Data).To(Equal(rendered))
		Expect(gotAPIKey).To(Equal("secret-token"))
	})

	ginkgo.It("sends no api key when the server has no token", func() {
		result, err := RenderWith(ctx, map[string]string{"key": "value"}, "html", "CatalogReport.tsx", "",
			Server{BaseURL: server.URL})
		Expect(err).ToNot(HaveOccurred())
		Expect(result.Data).To(Equal(rendered))
		Expect(gotAPIKey).To(BeEmpty())
	})

	ginkgo.It("sends the render timeout in milliseconds", func() {
		_, err := RenderWith(ctx, map[string]string{"key": "value"}, "html", "CatalogReport.tsx", "",
			Server{BaseURL: server.URL, Timeout: 90 * time.Second})
		Expect(err).ToNot(HaveOccurred())
		Expect(gotOptions["timeout"]).To(BeEquivalentTo(90_000))
	})

	ginkgo.It("sends no render timeout when none is configured", func() {
		_, err := RenderWith(ctx, map[string]string{"key": "value"}, "html", "CatalogReport.tsx", "",
			Server{BaseURL: server.URL})
		Expect(err).ToNot(HaveOccurred())
		Expect(gotOptions).ToNot(HaveKey("timeout"))
	})
})
