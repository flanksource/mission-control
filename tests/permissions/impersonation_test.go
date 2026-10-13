// ABOUTME: E2E tests for scope impersonation via X-Flanksource-Scope header.
// ABOUTME: Verifies the full middleware chain: header parsing, RLS override, and intersection.
package permissions_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"

	icrbac "github.com/flanksource/incident-commander/rbac"

	httpClient "github.com/flanksource/commons/http"
	"github.com/flanksource/commons/properties"
	"github.com/flanksource/duty/context"
	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/flanksource/duty/rls"
	"github.com/flanksource/duty/tests/setup"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sTypes "k8s.io/apimachinery/pkg/types"

	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/auth"
	"github.com/flanksource/incident-commander/db"
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
		server      *httptest.Server
		adminUser   *models.Person
		guestUser   *models.Person
		oldAuthMode string
		scopeIDs    = map[string]string{}
	)

	BeforeAll(func() {
		oldAuthMode = vars.AuthMode
		err := icrbac.Init(DefaultContext)
		Expect(err).ToNot(HaveOccurred())

		adminUser = newAdmin("Impersonation Admin", "impersonation-admin@test.com")
		guestUser = setup.CreateUserWithRole(DefaultContext, "Impersonation Guest", "impersonation-guest@test.com")

		// Give the guest user read on the backend and frontend Scopes
		for _, ns := range []string{"backend", "frontend"} {
			scope := &v1.Scope{
				ObjectMeta: metav1.ObjectMeta{Name: "impersonation-" + ns, Namespace: "default", UID: k8sTypes.UID(uuid.NewString())},
				Spec:       v1.ScopeSpec{Targets: []v1.ScopeTarget{{Config: &v1.ScopeConfigSelector{TagSelector: "namespace=" + ns}}}},
			}
			Expect(db.PersistScopeFromCRD(DefaultContext, scope)).To(Succeed())
			scopeIDs[ns] = string(scope.UID)
		}

		guestPerm := &models.Permission{
			Name:           "impersonation-test-perm",
			Namespace:      "default",
			Action:         policy.ActionRead,
			Subject:        guestUser.ID.String(),
			SubjectType:    models.PermissionSubjectTypePerson,
			ObjectSelector: []byte(`{"scopes":[{"namespace":"default","name":"impersonation-backend"},{"namespace":"default","name":"impersonation-frontend"}]}`),
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

		vars.AuthMode = oldAuthMode

		// Clean up
		DefaultContext.DB().Where("name = ?", "impersonation-test-perm").Delete(&models.Permission{})
		DefaultContext.DB().Where("name LIKE ?", "impersonation-%").Delete(&models.Scope{})
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
		It("admin should list rows through their grants, like any subject", func() {
			payload, status := getRLSPayload(adminUser.Email)
			Expect(status).To(Equal(http.StatusOK))
			Expect(payload.Disable).To(BeFalse())
			Expect(payload.Config).ToNot(BeNil())
			Expect(payload.Config.Any).ToNot(BeEmpty())
		})

		It("guest should have their real RLS payload", func() {
			payload, status := getRLSPayload(guestUser.Email)
			Expect(status).To(Equal(http.StatusOK))
			Expect(payload.Disable).To(BeFalse())
			Expect(payload.Config.Any).To(ConsistOf(rls.Grant{Scope: scopeIDs["backend"]}, rls.Grant{Scope: scopeIDs["frontend"]}))
		})
	})

	header := func(ids ...string) string {
		raw, err := json.Marshal(ids)
		Expect(err).ToNot(HaveOccurred())
		return string(raw)
	}

	// shippedScope is the id of the shipped Scope the admin Role reads a type through
	shippedScope := func(name string) string {
		GinkgoHelper()
		var scope models.Scope
		Expect(DefaultContext.DB().Where("namespace = ? AND name = ? AND deleted_at IS NULL", adapter.ShippedNamespace, name).First(&scope).Error).To(Succeed())
		return scope.ID.String()
	}

	Context("admin with impersonation header", func() {
		It("should narrow the admin's grants to the named Scopes", func() {
			payload, status := getRLSPayload(adminUser.Email, header(scopeIDs["backend"]))
			Expect(status).To(Equal(http.StatusOK))
			Expect(payload.Disable).To(BeFalse())
			Expect(payload.Config.Any).To(Equal([]rls.Grant{{Scope: shippedScope("all-configs"), Impersonated: []string{scopeIDs["backend"]}}}))
			Expect(payload.Component.Any).To(Equal([]rls.Grant{{Scope: shippedScope("all-components"), Impersonated: []string{scopeIDs["backend"]}}}))
		})

		It("should grant the rows of any of several named Scopes", func() {
			payload, status := getRLSPayload(adminUser.Email, header(scopeIDs["backend"], scopeIDs["frontend"]))
			Expect(status).To(Equal(http.StatusOK))
			Expect(payload.Config.Any).To(ConsistOf(
				rls.Grant{Scope: shippedScope("all-configs"), Impersonated: []string{scopeIDs["backend"]}},
				rls.Grant{Scope: shippedScope("all-configs"), Impersonated: []string{scopeIDs["frontend"]}},
			))
		})

		It("should restrict admin to nothing with an empty list", func() {
			payload, status := getRLSPayload(adminUser.Email, `[]`)
			Expect(status).To(Equal(http.StatusOK))
			Expect(payload.Disable).To(BeFalse())
			Expect(payload.Config).To(BeNil())
			Expect(payload.Component).To(BeNil())
		})
	})

	Context("guest with impersonation header", func() {
		It("should narrow every grant of the real payload", func() {
			payload, status := getRLSPayload(guestUser.Email, header(scopeIDs["backend"]))
			Expect(status).To(Equal(http.StatusOK))
			Expect(payload.Disable).To(BeFalse())
			Expect(payload.Config.Any).To(ConsistOf(
				rls.Grant{Scope: scopeIDs["backend"]},
				rls.Grant{Scope: scopeIDs["frontend"], Impersonated: []string{scopeIDs["backend"]}},
			))
		})
	})

	Context("feature flag disabled", func() {
		It("should ignore the header when auth.impersonation is off", func() {
			properties.Set("auth.impersonation", "false")
			defer properties.Set("auth.impersonation", "")

			payload, status := getRLSPayload(adminUser.Email, header(scopeIDs["backend"]))
			Expect(status).To(Equal(http.StatusOK))
			// Should return the real payload, not the impersonated one
			Expect(payload.Disable).To(BeFalse())
			Expect(payload.Config.Any).To(Equal([]rls.Grant{{Scope: shippedScope("all-configs")}}))
		})
	})

	Context("invalid header", func() {
		It("should return 400 for malformed JSON", func() {
			_, status := getRLSPayload(adminUser.Email, `{not json}`)
			Expect(status).To(Equal(http.StatusBadRequest))
		})

		It("should return 400 for the former payload format", func() {
			_, status := getRLSPayload(adminUser.Email, `{"config":[{"tags":{"team":"platform"}}]}`)
			Expect(status).To(Equal(http.StatusBadRequest))
		})
	})
})
