package main

import (
	"net/http"
	"net/http/httptest"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("http", func() {
	ginkgo.It("exports buffered events as an attachment", func() {
		plugin := newPlugin()
		gadget, ok := gadgetByID("trace_exec", defaultIGTag)
		Expect(ok).To(BeTrue())
		session, _ := newTraceSession(gadget, TraceTarget{Namespace: "default", Pod: "pod"}, nil, TraceDiagnostics{}, 10)
		session.AddEvent(TraceEvent{Raw: `{"comm":"sh"}`})
		plugin.sessions.Add(session)

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/sessions/"+session.ID+"/export", nil)
		plugin.HTTPHandler().ServeHTTP(rec, req)

		Expect(rec.Code).To(Equal(http.StatusOK))
		Expect(rec.Header().Get("Content-Type")).To(Equal("application/x-ndjson"))
		Expect(rec.Header().Get("Content-Disposition")).To(ContainSubstring(session.ID + ".ndjson"))
		Expect(rec.Body.String()).To(ContainSubstring(`"raw":"{\"comm\":\"sh\"}"`))
	})
})
