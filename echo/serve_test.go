package echo

import (
	"net/http"
	"net/http/httptest"
	"testing"

	echov4 "github.com/labstack/echo/v4"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestPostgrestInterceptor(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Postgrest Interceptor")
}

var _ = Describe("postgrestInterceptor", func() {
	var e *echov4.Echo
	var capturedRequest *http.Request

	BeforeEach(func() {
		e = echov4.New()
		capturedRequest = nil

		// Add a handler that captures the modified request
		e.Any("/db/*", func(c echov4.Context) error {
			capturedRequest = c.Request()
			return c.String(http.StatusOK, "OK")
		}, postgrestInterceptor)
	})

	It("should add name filter for playbook_names GET requests", func() {
		req := httptest.NewRequest(http.MethodGet, "/db/playbook_names", nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		Expect(rec.Code).To(Equal(http.StatusOK))
		Expect(capturedRequest).NotTo(BeNil())
		Expect(capturedRequest.URL.Query().Get("name")).To(Equal("eq.bad-exec"))
	})

	It("should preserve existing query parameters for playbook_names", func() {
		req := httptest.NewRequest(http.MethodGet, "/db/playbook_names?select=id,name&limit=10", nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		Expect(rec.Code).To(Equal(http.StatusOK))
		Expect(capturedRequest).NotTo(BeNil())
		query := capturedRequest.URL.Query()
		Expect(query.Get("name")).To(Equal("eq.bad-exec"))
		Expect(query.Get("select")).To(Equal("id,name"))
		Expect(query.Get("limit")).To(Equal("10"))
	})

	It("should not add filter for non-GET requests to playbook_names", func() {
		req := httptest.NewRequest(http.MethodPost, "/db/playbook_names", nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		Expect(rec.Code).To(Equal(http.StatusOK))
		Expect(capturedRequest).NotTo(BeNil())
		Expect(capturedRequest.URL.Query().Get("name")).To(BeEmpty())
	})

	It("should not add filter for other views", func() {
		req := httptest.NewRequest(http.MethodGet, "/db/playbooks", nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		Expect(rec.Code).To(Equal(http.StatusOK))
		Expect(capturedRequest).NotTo(BeNil())
		Expect(capturedRequest.URL.Query().Get("name")).To(BeEmpty())
	})
})