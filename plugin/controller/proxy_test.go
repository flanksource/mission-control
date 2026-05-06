package controller

import (
	"net/http"

	"github.com/flanksource/duty/models"
	"github.com/google/uuid"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("plugin UI proxy auth levels", func() {
	ginkgo.It("requires update for unsafe methods and websocket upgrades", func() {
		Expect(methodRequiresPluginUpdate(mustRequest(http.MethodGet, ""))).To(BeFalse())
		Expect(methodRequiresPluginUpdate(mustRequest(http.MethodPost, ""))).To(BeTrue())
		Expect(methodRequiresPluginUpdate(mustRequest(http.MethodGet, "websocket"))).To(BeTrue())
	})
})

var _ = ginkgo.Describe("rewriteProxiedRequest", func() {
	ginkgo.It("strips the host UI prefix and forwards it to the plugin", func() {
		r, _ := http.NewRequest(http.MethodGet, "http://example/api/plugins/arthas/ui/proxy/abc/index.html", nil)
		rewriteProxiedRequest(r, "/api/plugins/arthas/ui", nil, "")
		Expect(r.URL.Path).To(Equal("/proxy/abc/index.html"))
		Expect(r.Header.Get("X-Forwarded-Prefix")).To(Equal("/api/plugins/arthas/ui"))
	})

	ginkgo.It("normalizes a stripped-empty path to /", func() {
		r, _ := http.NewRequest(http.MethodGet, "http://example/api/plugins/arthas/ui", nil)
		rewriteProxiedRequest(r, "/api/plugins/arthas/ui", nil, "")
		Expect(r.URL.Path).To(Equal("/"))
	})

	ginkgo.It("forwards caller identity and config id when supplied", func() {
		r, _ := http.NewRequest(http.MethodGet, "http://example/api/plugins/arthas/ui/", nil)
		user := &models.Person{ID: uuid.New(), Email: "alice@example.com"}
		rewriteProxiedRequest(r, "/api/plugins/arthas/ui", user, "cfg-123")
		Expect(r.Header.Get("X-Mission-Control-User")).To(Equal(user.ID.String()))
		Expect(r.Header.Get("X-Mission-Control-User-Email")).To(Equal("alice@example.com"))
		Expect(r.Header.Get("X-Mission-Control-Config-Id")).To(Equal("cfg-123"))
	})
})

func mustRequest(method, upgrade string) *http.Request {
	r, err := http.NewRequest(method, "/api/plugins/arthas/ui", nil)
	Expect(err).ToNot(HaveOccurred())
	if upgrade != "" {
		r.Header.Set("Upgrade", upgrade)
	}
	return r
}
