package permissions_test

import (
	"net/http"

	"github.com/flanksource/duty/models"
	dutyRBAC "github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/flanksource/duty/tests/setup"
	"github.com/flanksource/duty/types"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sTypes "k8s.io/apimachinery/pkg/types"

	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/db"
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

	It("reads nothing through /db but properties", func() {
		Expect(serveAs(e, nobody, http.MethodGet, "/db/properties")).To(Equal(http.StatusOK))

		for _, table := range []string{"config_items", "components", "checks", "canaries", "playbooks", "people", "teams",
			"notifications", "agents", "connection_details", "scopes", "roles", "role_bindings", "config_types", "job_history"} {
			Expect(serveAs(e, nobody, http.MethodGet, "/db/"+table)).To(Equal(http.StatusForbidden), table)
		}
	})

	It("isn't selected by a PermissionGroup's retired people wildcard, whatever its name", func() {
		star := setup.CreateUserWithRole(DefaultContext, "*", "no-binding-star@test.com")
		DeferCleanup(func() { Expect(DefaultContext.DB().Delete(star).Error).To(Succeed()) })

		group := &v1.PermissionGroup{
			ObjectMeta: metav1.ObjectMeta{Name: "no-binding-everyone", Namespace: "default", UID: k8sTypes.UID(uuid.NewString())},
			Spec:       v1.PermissionGroupSpec{PermissionGroupSubjects: v1.PermissionGroupSubjects{People: []string{"*"}}},
		}
		Expect(db.PersistPermissionGroupFromCRD(DefaultContext, group)).To(Succeed())
		permission := &v1.Permission{
			ObjectMeta: metav1.ObjectMeta{Name: "no-binding-everyone-reads", Namespace: "default", UID: k8sTypes.UID(uuid.NewString())},
			Spec: v1.PermissionSpec{
				Subject: v1.PermissionSubject{Group: group.Name},
				Actions: []string{policy.ActionRead},
				Object:  v1.PermissionObject{Selectors: dutyRBAC.Selectors{Configs: []types.ResourceSelector{{Name: "*"}}}},
			},
		}
		Expect(db.PersistPermissionFromCRD(DefaultContext, permission)).To(Succeed())
		DeferCleanup(func() {
			Expect(DefaultContext.DB().Delete(&models.Permission{}, "id = ?", string(permission.UID)).Error).To(Succeed())
			Expect(DefaultContext.DB().Delete(&models.PermissionGroup{}, "id = ?", string(group.UID)).Error).To(Succeed())
			Expect(dutyRBAC.ReloadPolicy()).To(Succeed())
		})
		Expect(dutyRBAC.ReloadPolicy()).To(Succeed())

		for _, person := range []*models.Person{nobody, star} {
			Expect(mcRBAC.CheckContext(DefaultContext.WithUser(person), policy.ObjectCatalog, policy.ActionRead)).To(BeFalse(), person.Name)
		}
	})
})
