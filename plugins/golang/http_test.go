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
		sess := NewSession("default", "pod", "app", "app-0", "app", nil)
		p.sessions.Add(sess)
		req := httptest.NewRequest(http.MethodGet, "/profiles/"+sess.ID+"/unknown", nil)
		rec := httptest.NewRecorder()

		p.HTTPHandler().ServeHTTP(rec, req)

		Expect(rec.Code).To(Equal(http.StatusBadRequest))
	})

	ginkgo.It("serves completed profile runs from the registry", func() {
		p := newPlugin()
		sess := NewSession("default", "pod", "app", "app-0", "app", nil)
		p.sessions.Add(sess)
		run, _ := NewProfileRun(sess.ID, "heap", "pprof", 30)
		run.MarkDone([]byte("profile-bytes"), "pprof", nil)
		p.profiles.Add(run)

		req := httptest.NewRequest(http.MethodGet, "/profiles/"+sess.ID+"/"+run.ID, nil)
		rec := httptest.NewRecorder()

		p.HTTPHandler().ServeHTTP(rec, req)

		Expect(rec.Code).To(Equal(http.StatusOK))
		Expect(rec.Body.String()).To(Equal("profile-bytes"))
		Expect(rec.Header().Get("X-Diagnostics-Source")).To(Equal("pprof"))
		Expect(rec.Header().Get("Content-Disposition")).To(ContainSubstring(pluginName))
	})

	ginkgo.It("does not download running profile runs", func() {
		p := newPlugin()
		sess := NewSession("default", "pod", "app", "app-0", "app", nil)
		p.sessions.Add(sess)
		run, _ := NewProfileRun(sess.ID, "cpu", "auto", 30)
		p.profiles.Add(run)

		req := httptest.NewRequest(http.MethodGet, "/profiles/"+sess.ID+"/"+run.ID, nil)
		rec := httptest.NewRecorder()

		p.HTTPHandler().ServeHTTP(rec, req)

		Expect(rec.Code).To(Equal(http.StatusConflict))
		Expect(rec.Body.String()).To(ContainSubstring("not completed"))
	})
})
