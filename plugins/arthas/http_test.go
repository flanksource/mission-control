package main

import (
	"io"
	"net/http"
	"strings"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("Arthas proxy rewriting", func() {
	ginkgo.It("rewrites root absolute assets in HTML", func() {
		resp := &http.Response{
			Header: http.Header{"Content-Type": []string{"text/html"}},
			Body:   io.NopCloser(strings.NewReader(`<html><head></head><script src="/static/js/main.js"></script></html>`)),
		}
		Expect(rewriteArthasResponse(resp, "/proxy/s1/")).To(Succeed())
		body, err := io.ReadAll(resp.Body)
		Expect(err).ToNot(HaveOccurred())
		Expect(string(body)).To(ContainSubstring(`src="/proxy/s1/static/js/main.js"`))
		Expect(string(body)).To(ContainSubstring("window.WebSocket"))
	})

	ginkgo.It("rewrites assets through the host UI prefix when present", func() {
		resp := &http.Response{
			Header: http.Header{"Content-Type": []string{"text/html"}},
			Body:   io.NopCloser(strings.NewReader(`<html><head></head><link href="/static/main.css"><form action="/api"><script src="/static/js/main.js"></script></html>`)),
		}
		basePrefix := "/api/plugins/arthas/ui/proxy/s1/"
		Expect(rewriteArthasResponse(resp, basePrefix)).To(Succeed())
		body, err := io.ReadAll(resp.Body)
		Expect(err).ToNot(HaveOccurred())
		got := string(body)
		Expect(got).To(ContainSubstring(`<base href="/api/plugins/arthas/ui/proxy/s1/">`))
		Expect(got).To(ContainSubstring(`src="/api/plugins/arthas/ui/proxy/s1/static/js/main.js"`))
		Expect(got).To(ContainSubstring(`href="/api/plugins/arthas/ui/proxy/s1/static/main.css"`))
		Expect(got).To(ContainSubstring(`action="/api/plugins/arthas/ui/proxy/s1/api"`))
		Expect(got).To(ContainSubstring(`proxyBase = "/api/plugins/arthas/ui/proxy/s1/"`))
	})

	ginkgo.It("rewrites JS root-relative service paths via the host prefix", func() {
		resp := &http.Response{
			Header: http.Header{"Content-Type": []string{"application/javascript"}},
			Body:   io.NopCloser(strings.NewReader(`fetch("/api"); new WebSocket("/ws"); var u = "/static/js/extra.js"; var v = '/static/img/logo.png';`)),
		}
		Expect(rewriteArthasResponse(resp, "/api/plugins/arthas/ui/proxy/s1/")).To(Succeed())
		body, err := io.ReadAll(resp.Body)
		Expect(err).ToNot(HaveOccurred())
		got := string(body)
		Expect(got).To(ContainSubstring(`fetch("/api/plugins/arthas/ui/proxy/s1/api")`))
		Expect(got).To(ContainSubstring(`new WebSocket("/api/plugins/arthas/ui/proxy/s1/ws")`))
		Expect(got).To(ContainSubstring(`"/api/plugins/arthas/ui/proxy/s1/static/js/extra.js"`))
		Expect(got).To(ContainSubstring(`'/api/plugins/arthas/ui/proxy/s1/static/img/logo.png'`))
	})

	ginkgo.It("rewrites CSS urls and redirect locations", func() {
		resp := &http.Response{
			Header: http.Header{
				"Content-Type": []string{"text/css"},
				"Location":     []string{"/api"},
			},
			Body: io.NopCloser(strings.NewReader(`.logo{background:url(/static/png/arthas.png)}`)),
		}
		Expect(rewriteArthasResponse(resp, "/api/plugins/arthas/ui/proxy/s1/")).To(Succeed())
		Expect(resp.Header.Get("Location")).To(Equal("/api/plugins/arthas/ui/proxy/s1/api"))
		body, err := io.ReadAll(resp.Body)
		Expect(err).ToNot(HaveOccurred())
		Expect(string(body)).To(ContainSubstring(`url(/api/plugins/arthas/ui/proxy/s1/static/png/arthas.png)`))
	})
})
