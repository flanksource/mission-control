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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sTypes "k8s.io/apimachinery/pkg/types"

	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/auth"
	"github.com/flanksource/incident-commander/db"
	echoSrv "github.com/flanksource/incident-commander/echo"
	mcRBAC "github.com/flanksource/incident-commander/rbac"
	"github.com/flanksource/incident-commander/rbac/adapter"
)

var _ = Describe("person:manage", Ordered, func() {
	const namespace = "person-manage-test"

	var manager, reader, admin, guest *models.Person
	var e *echo.Echo

	uid := func() k8sTypes.UID { return k8sTypes.UID(uuid.NewString()) }

	// Every route that invites, disables, re-enables, changes or deletes an account
	target := uuid.NewString()
	routes := []struct{ method, path, body string }{
		{http.MethodPost, "/auth/invite_user", `{"email":"person-manage-invitee@test.com","teams":["person-manage-team"]}`},
		{http.MethodPost, "/auth/" + target + "/update_state", `{"id":"` + target + `","state":"inactive"}`},
		{http.MethodPost, "/auth/" + target + "/properties", `{}`},
		{http.MethodPost, "/people/update", `{"id":"` + target + `"}`},
		{http.MethodDelete, "/people/" + target, ``},
		{http.MethodPost, "/rbac/" + target + "/update_role", `{"roles":[]}`},
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
		Expect(rbac.Init(DefaultContext, []string{"admin"}, adapter.NewPermissionAdapter)).To(Succeed())

		manager = setup.CreateUserWithRole(DefaultContext, "Person Manager", "person-manage-manager@test.com", policy.RoleGuest)
		reader = setup.CreateUserWithRole(DefaultContext, "Person Reader", "person-manage-reader@test.com", policy.RoleGuest)
		guest = setup.CreateUserWithRole(DefaultContext, "Person Guest", "person-manage-guest@test.com", policy.RoleGuest)
		admin = setup.CreateUserWithRole(DefaultContext, "Person Admin", "person-manage-admin@test.com", policy.RoleAdmin)

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
		for _, p := range []*models.Person{manager, reader, guest, admin} {
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

		It("lets the built-in admin call "+name, func() {
			rec := request(admin, route.method, route.path, route.body)
			Expect(rec.Code).ToNot(Equal(http.StatusForbidden), rec.Body.String())
			Expect(rec.Header().Get("X-Rbac-Action")).To(BeEmpty())
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

			It("lets the built-in admin write "+table, func() {
				for _, method := range writes {
					Expect(dbWrite(admin, method, table)).To(Equal(http.StatusOK), method)
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

	It("doesn't grant read on people", func() {
		Expect(rbac.Check(DefaultContext, manager.ID.String(), policy.ObjectPeople, policy.ActionRead)).To(BeFalse())
	})
})
