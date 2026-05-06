package main

import (
	"net/http"
	"net/http/httptest"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("HTTP handler", func() {
	ginkgo.It("returns a useful error for missing pprof session", func() {
		p := newPlugin()
		req := httptest.NewRequest(http.MethodGet, "/pprof/missing/", nil)
		rec := httptest.NewRecorder()

		p.HTTPHandler().ServeHTTP(rec, req)

		Expect(rec.Code).To(Equal(http.StatusNotFound))
		Expect(rec.Body.String()).To(ContainSubstring("session not found"))
	})

	ginkgo.It("validates profile paths", func() {
		p := newPlugin()
		req := httptest.NewRequest(http.MethodGet, "/profiles/s1/unknown", nil)
		rec := httptest.NewRecorder()

		p.HTTPHandler().ServeHTTP(rec, req)

		Expect(rec.Code).To(Equal(http.StatusBadRequest))
	})
})
