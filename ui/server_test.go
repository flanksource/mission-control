package ui

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"

	echov4 "github.com/labstack/echo/v4"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("UI routes", func() {
	ginkgo.It("redirects home to /ui in UI mode", func() {
		e := echov4.New()
		RegisterRoutes(e, Options{})

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		Expect(rec.Code).To(Equal(http.StatusFound))
		Expect(rec.Header().Get("Location")).To(Equal("/ui"))
	})

	ginkgo.It("serves the embedded shell without a dev proxy", func() {
		e := echov4.New()
		RegisterRoutes(e, Options{})

		req := httptest.NewRequest(http.MethodGet, "/ui", nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		Expect(rec.Code).To(Equal(http.StatusOK))
		Expect(rec.Body.String()).To(ContainSubstring("Mission Control"))
	})

	ginkgo.It("serves UI static assets", func() {
		e := echov4.New()
		RegisterRoutes(e, Options{})

		for _, path := range []string{"/ui/logo.svg", "/ui/favicon.svg"} {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)

			Expect(rec.Code).To(Equal(http.StatusOK))
			Expect(rec.Header().Get("Content-Type")).To(ContainSubstring("image/svg+xml"))
			Expect(rec.Body.String()).To(ContainSubstring("<svg"))
		}
	})

	ginkgo.It("proxies UI paths in dev mode but keeps openapi and static assets local", func() {
		var upstreamHits atomic.Int32
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			upstreamHits.Add(1)
			Expect(r.URL.Path).To(Equal("/ui/configs/123"))
			_, _ = w.Write([]byte("proxied-vite"))
		}))
		defer upstream.Close()

		e := echov4.New()
		RegisterRoutes(e, Options{DevProxyTarget: upstream.URL})

		req := httptest.NewRequest(http.MethodGet, "/ui/configs/123", nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		Expect(rec.Code).To(Equal(http.StatusOK))
		Expect(rec.Body.String()).To(Equal("proxied-vite"))
		Expect(upstreamHits.Load()).To(Equal(int32(1)))

		req = httptest.NewRequest(http.MethodGet, "/ui/openapi.json", nil)
		rec = httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		Expect(rec.Code).To(Equal(http.StatusOK))
		Expect(rec.Body.String()).NotTo(Equal("proxied-vite"))
		Expect(upstreamHits.Load()).To(Equal(int32(1)))

		req = httptest.NewRequest(http.MethodGet, "/ui/favicon.svg", nil)
		rec = httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		Expect(rec.Code).To(Equal(http.StatusOK))
		Expect(rec.Header().Get("Content-Type")).To(ContainSubstring("image/svg+xml"))
		Expect(upstreamHits.Load()).To(Equal(int32(1)))
	})
})
