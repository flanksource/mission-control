package permissions_test

import (
	"net/http"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/flanksource/duty/tests/setup"
	"github.com/labstack/echo/v4"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	mcRBAC "github.com/flanksource/incident-commander/rbac"
	"github.com/flanksource/incident-commander/rbac/adapter"
)

var _ = Describe("A person no RoleBinding selects", Ordered, func() {
	var (
		e      *echo.Echo
		nobody *models.Person
	)

	BeforeAll(func() {
		Expect(mcRBAC.Init(DefaultContext)).To(Succeed())
		nobody = setup.CreateUserWithRole(DefaultContext, "No Binding", "no-binding@test.com")
		e = wholeTypeRoutes()
	})

	AfterAll(func() {
		Expect(DefaultContext.DB().Delete(nobody).Error).To(Succeed())
	})

	It("is granted no object, action or resource", func() {
		ctx := DefaultContext.WithUser(nobody)
		objects := append(policy.AllObjects, adapter.ObjectChecks, adapter.ObjectNotificationSilences, adapter.ObjectTeams,
			adapter.ObjectScopes, adapter.ObjectRoles, adapter.ObjectRoleBindings, adapter.ObjectScrapers, "*")
		for _, object := range objects {
			for _, action := range append(adapter.Actions(), "*") {
				Expect(mcRBAC.CheckContext(ctx, object, action)).To(BeFalse(), "%s:%s", object, action)
			}
		}
		Expect(mcRBAC.AccessSummary(ctx)).To(HaveEach(HaveEach(Equal(mcRBAC.AccessNone))))
	})

	It("reads nothing through /db but feature flags", func() {
		Expect(serveAs(e, nobody, http.MethodGet, "/db/feature_flags")).To(Equal(http.StatusOK))

		for _, table := range []string{"config_items", "components", "checks", "canaries", "playbooks", "people", "teams",
			"notifications", "agents", "connection_details", "scopes", "roles", "role_bindings", "config_types", "job_history"} {
			Expect(serveAs(e, nobody, http.MethodGet, "/db/"+table)).To(Equal(http.StatusForbidden), table)
		}
	})
})
