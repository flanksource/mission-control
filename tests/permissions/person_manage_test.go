package permissions_test

import (
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/flanksource/duty/tests/setup"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sTypes "k8s.io/apimachinery/pkg/types"

	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/auth"
	"github.com/flanksource/incident-commander/db"
	echoSrv "github.com/flanksource/incident-commander/echo"
	mcRBAC "github.com/flanksource/incident-commander/rbac"
	"github.com/flanksource/incident-commander/rbac/adapter"
	"github.com/flanksource/incident-commander/vars"
)

var _ = Describe("person:manage", Ordered, func() {
	const namespace = "person-manage-test"

	var manager, reader, legacyAdmin, guest *models.Person
	var e *echo.Echo

	uid := func() k8sTypes.UID { return k8sTypes.UID(uuid.NewString()) }

	// Every route that invites, disables, re-enables, changes or deletes an account, with what it checked before
	// person:manage existed
	target := uuid.NewString()
	routes := []struct {
		method, path, body string
		legacy             objectAction
	}{
		{http.MethodPost, "/auth/invite_user", `{"email":"person-manage-invitee@test.com","teams":["person-manage-team"]}`, objectAction{Object: policy.ObjectAuth, Action: policy.ActionUpdate}},
		{http.MethodPost, "/auth/" + target + "/update_state", `{"id":"` + target + `","state":"inactive"}`, objectAction{Object: policy.ObjectAuth, Action: policy.ActionUpdate}},
		{http.MethodPost, "/auth/" + target + "/properties", `{}`, objectAction{Object: policy.ObjectAuth, Action: policy.ActionUpdate}},
		{http.MethodPost, "/people/update", `{"id":"` + target + `"}`, objectAction{Object: policy.ObjectPeople, Action: policy.ActionUpdate}},
		{http.MethodDelete, "/people/" + target, ``, objectAction{Object: policy.ObjectPeople, Action: policy.ActionDelete}},
	}

	// request makes the request as the person, and returns the response
	request := func(person *models.Person, method, path, body string) *httptest.ResponseRecorder {
		GinkgoHelper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		req = req.WithContext(DefaultContext.WithUser(person))
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		return rec
	}

	// dbWrite reports the status of a write through /db, made as the person, with the handler stubbed
	dbWrite := func(person *models.Person, method, table string) int {
		GinkgoHelper()
		req := httptest.NewRequest(method, "/db/"+table, nil)
		req = req.WithContext(DefaultContext.WithUser(person))
		rec := httptest.NewRecorder()
		handler := mcRBAC.DbMiddleware()(func(c echo.Context) error { return c.NoContent(http.StatusOK) })
		Expect(handler(echo.New().NewContext(req, rec))).To(Succeed())
		return rec.Code
	}

	BeforeAll(func() {
		Expect(mcRBAC.Init(DefaultContext)).To(Succeed())

		manager = setup.CreateUserWithRole(DefaultContext, "Person Manager", "person-manage-manager@test.com")
		reader = setup.CreateUserWithRole(DefaultContext, "Person Reader", "person-manage-reader@test.com")
		guest = setup.CreateUserWithRole(DefaultContext, "Person Guest", "person-manage-guest@test.com")

		// Assigned the built-in admin role, and holding what each route checked before person:manage, directly
		legacyAdmin = setup.CreateUserWithRole(DefaultContext, "Person Legacy Admin", "person-manage-legacy-admin@test.com", policy.RoleAdmin)
		for _, grant := range append(lo.Map(routes, func(r struct {
			method, path, body string
			legacy             objectAction
		}, _ int) objectAction {
			return r.legacy
		}), objectAction{Object: policy.ObjectRBAC, Action: policy.ActionUpdate}) {
			_, err := rbac.Enforcer().AddPolicy(legacyAdmin.ID.String(), grant.Object, grant.Action, "allow", "", "legacy")
			Expect(err).ToNot(HaveOccurred())
		}
		// and what /db checked on the account tables
		for _, object := range []string{policy.ObjectPeople, policy.ObjectDatabasePublic, policy.ObjectAuth} {
			for _, action := range []string{policy.ActionCreate, policy.ActionUpdate, policy.ActionDelete} {
				_, err := rbac.Enforcer().AddPolicy(legacyAdmin.ID.String(), object, action, "allow", "", "legacy")
				Expect(err).ToNot(HaveOccurred())
			}
		}

		Expect(db.PersistScopeFromCRD(DefaultContext, &v1.Scope{
			ObjectMeta: metav1.ObjectMeta{Name: "all-people", Namespace: namespace, UID: uid()},
			Spec:       v1.ScopeSpec{Targets: []v1.ScopeTarget{{Person: &v1.ScopeWholeTypeSelector{Name: "*"}}}},
		})).To(Succeed())

		for name, rule := range map[string]v1.RoleRule{
			"manage-people": {Name: "manage", Action: adapter.ActionPersonManage},
			"read-people":   {Name: "read", Action: policy.ActionRead, Resource: &v1.ScopeReference{ScopeRef: "all-people"}},
		} {
			Expect(db.PersistRoleFromCRD(DefaultContext, &v1.Role{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, UID: uid()},
				Spec:       v1.RoleSpec{Rules: []v1.RoleRule{rule}},
			})).To(Succeed())
		}

		for role, person := range map[string]*models.Person{"manage-people": manager, "read-people": reader} {
			Expect(db.PersistRoleBindingFromCRD(DefaultContext, &v1.RoleBinding{
				ObjectMeta: metav1.ObjectMeta{Name: role, Namespace: namespace, UID: uid()},
				Spec: v1.RoleBindingSpec{Role: role, Subjects: v1.RoleBindingSubjects{
					PermissionGroupSubjects: v1.PermissionGroupSubjects{People: []string{person.Email}},
				}},
			})).To(Succeed())
		}

		Expect(rbac.ReloadPolicy()).To(Succeed())

		e = echo.New()
		auth.RegisterRoutes(e)
		auth.RegisterKratosRoutes(e, auth.NewKratosHandler())
		echoSrv.RegisterPeopleRoutes(e, nil)
		mcRBAC.RegisterRoutes(e)
	})

	AfterAll(func() {
		Expect(DefaultContext.DB().Where("namespace = ?", namespace).Delete(&models.RoleBinding{}).Error).To(Succeed())
		Expect(DefaultContext.DB().Where("namespace = ?", namespace).Delete(&models.Role{}).Error).To(Succeed())
		Expect(DefaultContext.DB().Where("namespace = ?", namespace).Delete(&models.Scope{}).Error).To(Succeed())
		Expect(DefaultContext.DB().Where("email = ?", "person-manage-invitee@test.com").Delete(&models.Invite{}).Error).To(Succeed())
		_, err := rbac.Enforcer().DeletePermissionsForUser(legacyAdmin.ID.String())
		Expect(err).ToNot(HaveOccurred())
		for _, p := range []*models.Person{manager, reader, guest, legacyAdmin} {
			Expect(DefaultContext.DB().Delete(p).Error).To(Succeed())
		}
		Expect(rbac.ReloadPolicy()).To(Succeed())
	})

	for _, route := range routes {
		name := route.method + " " + strings.ReplaceAll(route.path, target, ":id")

		It("lets a subject with only person:manage call "+name, func() {
			rec := request(manager, route.method, route.path, route.body)
			Expect(rec.Code).ToNot(Equal(http.StatusForbidden), rec.Body.String())
			Expect(rec.Header().Get("X-Rbac-Action")).To(BeEmpty())
		})

		It("refuses "+name+" to a subject with only what it checked before person:manage, and the built-in admin role", func() {
			Expect(rbac.Check(DefaultContext, legacyAdmin.ID.String(), route.legacy.Object, route.legacy.Action)).To(BeTrue())
			rec := request(legacyAdmin, route.method, route.path, route.body)
			Expect(rec.Code).To(Equal(http.StatusForbidden))
			Expect(rec.Header().Get("X-Rbac-Action")).To(Equal(adapter.ActionPersonManage))
		})

		It("refuses "+name+" to a subject with read on people", func() {
			rec := request(reader, route.method, route.path, route.body)
			Expect(rec.Code).To(Equal(http.StatusForbidden))
			Expect(rec.Header().Get("X-Rbac-Action")).To(Equal(adapter.ActionPersonManage))
		})

		It("refuses "+name+" to a guest", func() {
			Expect(request(guest, route.method, route.path, route.body).Code).To(Equal(http.StatusForbidden))
		})
	}

	Describe("through /db", func() {
		writes := []string{http.MethodPost, http.MethodPatch, http.MethodDelete}

		for _, table := range []string{"people", "identities"} {
			It("lets a subject with only person:manage write "+table, func() {
				for _, method := range writes {
					Expect(dbWrite(manager, method, table)).To(Equal(http.StatusOK), method)
				}
			})

			It("refuses writing "+table+" to a subject with only the built-in admin role, and the table's own object", func() {
				for _, method := range writes {
					Expect(dbWrite(legacyAdmin, method, table)).To(Equal(http.StatusForbidden), method)
				}
			})

			It("refuses writing "+table+" to a subject with read on people", func() {
				for _, method := range writes {
					Expect(dbWrite(reader, method, table)).To(Equal(http.StatusForbidden), method)
				}
			})
		}

		It("still lets a subject with read on people read them", func() {
			Expect(dbWrite(reader, http.MethodGet, "people")).To(Equal(http.StatusOK))
		})
	})

	It("no longer assigns roles through POST /rbac/:id/update_role", func() {
		Expect(request(manager, http.MethodPost, "/rbac/"+target+"/update_role", `{"roles":["admin"]}`).Code).To(Equal(http.StatusNotFound))
	})

	It("refuses a role through POST /people/update", func() {
		previous := vars.AuthMode
		vars.AuthMode = auth.Kratos
		DeferCleanup(func() { vars.AuthMode = previous })
		Expect(request(manager, http.MethodPost, "/people/update", `{"id":"`+target+`","role":"admin"}`).Code).To(Equal(http.StatusBadRequest))
	})

	It("doesn't grant read on people", func() {
		Expect(rbac.Check(DefaultContext, manager.ID.String(), policy.ObjectPeople, policy.ActionRead)).To(BeFalse())
	})
})
