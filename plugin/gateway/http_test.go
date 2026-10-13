package gateway

import (
	"net/http"
	"net/http/httptest"

	"github.com/labstack/echo/v4"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/samber/lo"
)

var _ = ginkgo.Describe("plugin HTTP API", func() {
	ginkgo.It("has no route to upgrade a plugin", func() {
		e := echo.New()
		RegisterRoutes(e)

		Expect(lo.Map(e.Routes(), func(r *echo.Route, _ int) string { return r.Method + " " + r.Path })).
			ToNot(ContainElement(ContainSubstring("/upgrade")))

		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/plugins/kubernetes-logs/upgrade", nil))
		Expect(rec.Code).To(Or(Equal(http.StatusNotFound), Equal(http.StatusMethodNotAllowed)))
	})
})
