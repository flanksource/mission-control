// ABOUTME: E2E tests for scope impersonation via X-Flanksource-Scope header.
// ABOUTME: Verifies the full middleware chain: header parsing, RLS override, and intersection.
package permissions_test

import (
	"net/http"
	"net/http/httptest"

	httpClient "github.com/flanksource/commons/http"
	"github.com/flanksource/commons/properties"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/flanksource/duty/rls"
	"github.com/flanksource/duty/tests/setup"
	"github.com/labstack/echo/v4"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/flanksource/incident-commander/auth"
	echoSrv "github.com/flanksource/incident-commander/echo"
	"github.com/flanksource/incident-commander/rbac/adapter"
	"github.com/flanksource/incident-commander/vars"
)

// rlsPayloadHandler returns the effective RLS payload as JSON for testing.
func rlsPayloadHandler(c echo.Context) error {
	ctx := c.Request().Context().(context.Context)
	payload, err := auth.GetRLSPayload(ctx)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, payload)
}

var _ = Describe("Scope Impersonation E2E", Ordered, func() {
	var (
		server    *httptest.Server
		adminUser *models.Person
		guestUser *models.Person
	)

	BeforeAll(func() {
		err := rbac.Init(DefaultContext, []string{"admin"}, adapter.NewPermissionAdapter)
		Expect(err).ToNot(HaveOccurred())

		adminUser = setup.CreateUserWithRole(DefaultContext, "Impersonation Admin", "impersonation-admin@test.com", policy.RoleAdmin)
		guestUser = setup.CreateUserWithRole(DefaultContext, "Impersonation Guest", "impersonation-guest@test.com", policy.RoleGuest)

		// Give the guest user config scope permissions for backend and frontend namespaces
		guestPerm := &models.Permission{
			Name:           "impersonation-test-perm",
			Namespace:      "default",
			Action:         policy.ActionRead,
			Subject:        guestUser.ID.String(),
			SubjectType:    models.PermissionSubjectTypePerson,
			ObjectSelector: []byte(`{"configs":[{"tagSelector":"namespace=backend"},{"tagSelector":"namespace=frontend"}]}`),
		}
		err = DefaultContext.DB().Create(guestPerm).Error
		Expect(err).ToNot(HaveOccurred())

		Expect(rbac.ReloadPolicy()).ToNot(HaveOccurred())

		vars.AuthMode = ""

		e := echoSrv.New(DefaultContext)
		e.Use(auth.MockAuthMiddleware)
		e.GET("/test/rls-payload", rlsPayloadHandler, echoSrv.RLSMiddleware)

		server = httptest.NewServer(e)
	})

	AfterAll(func() {
		if server != nil {
			server.Close()
		}

		// Clean up
		DefaultContext.DB().Where("name = ?", "impersonation-test-perm").Delete(&models.Permission{})
		DefaultContext.DB().Delete(adminUser)
		DefaultContext.DB().Delete(guestUser)
	})

	getRLSPayload := func(user string, scopeHeader ...string) (*rls.Payload, int) {
		client := httpClient.NewClient().BaseURL(server.URL).Auth(user, user)

		req := client.R(DefaultContext)
		if len(scopeHeader) > 0 && scopeHeader[0] != "" {
			req = req.Header(auth.HeaderFlanksourceScope, scopeHeader[0])
		}

		resp, err := req.Do("GET", "/test/rls-payload")
		Expect(err).ToNot(HaveOccurred())

		if resp.StatusCode != http.StatusOK {
			return nil, resp.StatusCode
		}

		var payload rls.Payload
		err = resp.Into(&payload)
		Expect(err).ToNot(HaveOccurred())

		return &payload, resp.StatusCode
	}

	Context("without impersonation header", func() {
		It("admin should have RLS disabled", func() {
			payload, status := getRLSPayload(adminUser.Email)
			Expect(status).To(Equal(http.StatusOK))
			Expect(payload.Disable).To(BeTrue())
		})

		It("guest should have their real RLS payload", func() {
			payload, status := getRLSPayload(guestUser.Email)
			Expect(status).To(Equal(http.StatusOK))
			Expect(payload.Disable).To(BeFalse())
			Expect(payload.Config).To(HaveLen(2))
		})
	})

	Context("admin with impersonation header", func() {
		It("should use the impersonated payload directly", func() {
			scope := `{"config":[{"tags":{"team":"platform"}}]}`
			payload, status := getRLSPayload(adminUser.Email, scope)
			Expect(status).To(Equal(http.StatusOK))
			Expect(payload.Disable).To(BeFalse())
			Expect(payload.Config).To(HaveLen(1))
			Expect(payload.Config[0].Tags).To(Equal(map[string]string{"team": "platform"}))
		})

		It("should restrict admin to nothing with empty payload", func() {
			scope := `{}`
			payload, status := getRLSPayload(adminUser.Email, scope)
			Expect(status).To(Equal(http.StatusOK))
			Expect(payload.Disable).To(BeFalse())
			Expect(payload.Config).To(BeEmpty())
			Expect(payload.Component).To(BeEmpty())
		})
	})

	Context("guest with impersonation header", func() {
		It("should intersect with real payload — matching scope kept", func() {
			// Guest has backend + frontend. Impersonate only backend.
			scope := `{"config":[{"tags":{"namespace":"backend"}}]}`
			payload, status := getRLSPayload(guestUser.Email, scope)
			Expect(status).To(Equal(http.StatusOK))
			Expect(payload.Disable).To(BeFalse())
			Expect(payload.Config).To(HaveLen(1))
			Expect(payload.Config[0].Tags).To(Equal(map[string]string{"namespace": "backend"}))
		})

		It("should intersect with real payload — no overlap produces empty", func() {
			// Guest has backend + frontend. Impersonate kube-system (not in real).
			scope := `{"config":[{"tags":{"namespace":"kube-system"}}]}`
			payload, status := getRLSPayload(guestUser.Email, scope)
			Expect(status).To(Equal(http.StatusOK))
			Expect(payload.Config).To(BeEmpty())
		})

		It("should intersect with real payload — narrowing with extra tags", func() {
			// Guest has namespace=backend. Impersonate namespace=backend + env=prod.
			// Intersection: both tag sets merge (no conflict).
			scope := `{"config":[{"tags":{"namespace":"backend","env":"prod"}}]}`
			payload, status := getRLSPayload(guestUser.Email, scope)
			Expect(status).To(Equal(http.StatusOK))
			Expect(payload.Config).To(HaveLen(1))
			Expect(payload.Config[0].Tags).To(Equal(map[string]string{"namespace": "backend", "env": "prod"}))
		})
	})

	Context("feature flag disabled", func() {
		It("should ignore the header when auth.impersonation is off", func() {
			properties.Set("auth.impersonation", "false")
			defer properties.Set("auth.impersonation", "")

			scope := `{"config":[{"tags":{"team":"platform"}}]}`
			payload, status := getRLSPayload(adminUser.Email, scope)
			Expect(status).To(Equal(http.StatusOK))
			// Should return the real payload (admin = disabled), not the impersonated one
			Expect(payload.Disable).To(BeTrue())
		})
	})

	Context("invalid header", func() {
		It("should return 400 for malformed JSON", func() {
			_, status := getRLSPayload(adminUser.Email, `{not json}`)
			Expect(status).To(Equal(http.StatusBadRequest))
		})
	})
})
