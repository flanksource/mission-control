package permissions_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"

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

	"github.com/flanksource/incident-commander/agent"
	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/application"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/notification"
	mcRBAC "github.com/flanksource/incident-commander/rbac"
	"github.com/flanksource/incident-commander/rbac/adapter"
)

// serveAs serves a request made by the person, and returns its status.
func serveAs(e *echo.Echo, person *models.Person, method, path string) int {
	GinkgoHelper()
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	req = req.WithContext(DefaultContext.WithUser(person))
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec.Code
}

// authorized reports whether a status is one of a request that passed authorization:
// the handlers are called without a valid body or with names that don't exist.
func authorized(code int) bool {
	return lo.Contains([]int{http.StatusOK, http.StatusCreated, http.StatusBadRequest, http.StatusNotFound}, code)
}

// wholeTypeRoutes serves the routes of the whole-type resources, with /db answered by a stub after its middleware.
func wholeTypeRoutes() *echo.Echo {
	e := echo.New()
	mcRBAC.RegisterRoutes(e)
	application.RegisterRoutes(e)
	notification.RegisterRoutes(e)
	agent.RegisterRoutes(e)
	e.Any("/db/*", func(c echo.Context) error { return c.NoContent(http.StatusOK) }, mcRBAC.DbMiddleware())
	return e
}

// bindWholeType creates a Scope of all resources of the type, and a Role with a rule per action on it,
// and binds the subjects to the Role.
func bindWholeType(namespace, name, resource string, deny bool, subjects v1.RoleBindingSubjects, actions ...string) {
	GinkgoHelper()
	uid := func() k8sTypes.UID { return k8sTypes.UID(uuid.NewString()) }

	var target v1.ScopeTarget
	Expect(json.Unmarshal(fmt.Appendf(nil, `{%q: {"name": "*"}}`, resource), &target)).To(Succeed())
	Expect(db.PersistScopeFromCRD(DefaultContext, &v1.Scope{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, UID: uid()},
		Spec:       v1.ScopeSpec{Targets: []v1.ScopeTarget{target}},
	})).To(Succeed())

	rules := lo.Map(actions, func(action string, _ int) v1.RoleRule {
		return v1.RoleRule{Name: action, Action: action, Resource: &v1.ScopeReference{ScopeRef: name}, Deny: deny}
	})
	Expect(db.PersistRoleFromCRD(DefaultContext, &v1.Role{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, UID: uid()},
		Spec:       v1.RoleSpec{Rules: rules},
	})).To(Succeed())

	Expect(db.PersistRoleBindingFromCRD(DefaultContext, &v1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, UID: uid()},
		Spec:       v1.RoleBindingSpec{Role: name, Subjects: subjects},
	})).To(Succeed())
}

// boundPeople selects the people by email.
func boundPeople(people ...*models.Person) v1.RoleBindingSubjects {
	return v1.RoleBindingSubjects{PermissionGroupSubjects: v1.PermissionGroupSubjects{
		People: lo.Map(people, func(p *models.Person, _ int) string { return p.Email }),
	}}
}

// deleteNamespace deletes the RBAC objects of the namespace, and reloads the policy.
func deleteNamespace(namespace string) {
	GinkgoHelper()
	Expect(DefaultContext.DB().Where("namespace = ?", namespace).Delete(&models.RoleBinding{}).Error).To(Succeed())
	Expect(DefaultContext.DB().Where("namespace = ?", namespace).Delete(&models.Role{}).Error).To(Succeed())
	Expect(DefaultContext.DB().Where("namespace = ?", namespace).Delete(&models.Scope{}).Error).To(Succeed())
	Expect(rbac.ReloadPolicy()).To(Succeed())
}

var _ = Describe("Routes of whole-type resources", Ordered, func() {
	const namespace = "whole-type-routes"

	type route struct {
		resource     string
		method, path string
		write        bool

		// legacy is what the route checked before its type had an object of its own
		legacy mcRBAC.Grant
	}

	read := func(object string) mcRBAC.Grant { return mcRBAC.Grant{Object: object, Action: policy.ActionRead} }
	grant := func(object, action string) mcRBAC.Grant { return mcRBAC.Grant{Object: object, Action: action} }

	routes := []route{
		{v1.ResourceApplication, http.MethodGet, "/application/default/missing", false, read(policy.ObjectCatalog)},
		{v1.ResourceApplication, http.MethodGet, "/db/applications", false, read(policy.ObjectApplication)},
		{v1.ResourceApplication, http.MethodPost, "/db/applications", true, grant(policy.ObjectApplication, policy.ActionCreate)},

		{v1.ResourceNotification, http.MethodGet, "/db/notifications", false, read(policy.ObjectNotification)},
		{v1.ResourceNotification, http.MethodGet, "/db/notifications_summary", false, read(policy.ObjectMonitor)},
		{v1.ResourceNotification, http.MethodPatch, "/db/notifications", true, grant(policy.ObjectNotification, policy.ActionUpdate)},

		{v1.ResourceNotificationSilence, http.MethodGet, "/notification/silence_preview", false, read(policy.ObjectNotification)},
		{v1.ResourceNotificationSilence, http.MethodGet, "/db/notification_silences", false, read(policy.ObjectNotification)},
		{v1.ResourceNotificationSilence, http.MethodPost, "/notification/silence", true, grant(policy.ObjectNotification, policy.ActionCreate)},
		{v1.ResourceNotificationSilence, http.MethodPatch, "/db/notification_silences", true, grant(policy.ObjectNotification, policy.ActionUpdate)},

		{v1.ResourceAgent, http.MethodGet, "/db/agents", false, read(policy.ObjectDatabasePublic)},
		{v1.ResourceAgent, http.MethodGet, "/db/agents_summary", false, read(policy.ObjectMonitor)},
		{v1.ResourceAgent, http.MethodPost, "/agent/generate", true, grant(policy.ObjectAgent, policy.ActionUpdate)},
		{v1.ResourceAgent, http.MethodPost, "/agent/token", true, grant(policy.ObjectAgent, policy.ActionUpdate)},

		{v1.ResourcePerson, http.MethodGet, "/db/people", false, read(policy.ObjectPeople)},

		{v1.ResourceTeam, http.MethodGet, "/db/teams", false, read(policy.ObjectDatabasePublic)},
		{v1.ResourceTeam, http.MethodGet, "/db/team_members", false, read(policy.ObjectDatabasePublic)},
		{v1.ResourceTeam, http.MethodDelete, "/db/teams?id=eq." + uuid.NewString(), true, grant(policy.ObjectDatabasePublic, policy.ActionDelete)},
		{v1.ResourceTeam, http.MethodPost, "/db/team_members", true, grant(policy.ObjectDatabasePublic, policy.ActionCreate)},

		{v1.ResourceScope, http.MethodGet, "/rbac/scopes", false, read(policy.ObjectRBAC)},
		{v1.ResourceScope, http.MethodGet, "/db/scopes", false, read(policy.ObjectRBAC)},
		{v1.ResourceScope, http.MethodGet, "/db/scope_targets", false, read(policy.ObjectRBAC)},
		{v1.ResourceScope, http.MethodPost, "/rbac/scopes", true, grant(policy.ObjectRBAC, policy.ActionCreate)},

		{v1.ResourceRole, http.MethodGet, "/rbac/roles", false, read(policy.ObjectRBAC)},
		{v1.ResourceRole, http.MethodGet, "/db/roles", false, read(policy.ObjectRBAC)},
		{v1.ResourceRole, http.MethodPost, "/rbac/roles", true, grant(policy.ObjectRBAC, policy.ActionCreate)},

		{v1.ResourceRoleBinding, http.MethodGet, "/rbac/role-bindings", false, read(policy.ObjectRBAC)},
		{v1.ResourceRoleBinding, http.MethodGet, "/db/role_bindings", false, read(policy.ObjectRBAC)},
		{v1.ResourceRoleBinding, http.MethodPost, "/rbac/role-bindings", true, grant(policy.ObjectRBAC, policy.ActionCreate)},
	}

	var (
		e                                    *echo.Echo
		configReader, viewer, editor, nobody *models.Person
		readers                              = map[string]*models.Person{}
		writers                              = map[string]*models.Person{}
		persons                              []*models.Person
	)

	passes := func(person *models.Person, r route) bool {
		GinkgoHelper()
		return authorized(serveAs(e, person, r.method, r.path))
	}

	bind := func(person *models.Person, name, resource string, actions ...string) {
		GinkgoHelper()
		bindWholeType(namespace, name, resource, false, boundPeople(person), actions...)
	}

	person := func(name, role string) *models.Person {
		GinkgoHelper()
		p := setup.CreateUserWithRole(DefaultContext, "Whole Type "+name, "whole-type-"+name+"@test.com", role)
		persons = append(persons, p)
		return p
	}

	BeforeAll(func() {
		Expect(rbac.Init(DefaultContext, []string{"admin"}, adapter.NewPermissionAdapter)).To(Succeed())

		for _, resource := range v1.WholeTypeResources {
			readers[resource] = person("reader-"+resource, policy.RoleGuest)
			bind(readers[resource], "read-"+lo.KebabCase(resource), resource, policy.ActionRead)

			if resource != v1.ResourcePerson {
				writers[resource] = person("writer-"+resource, policy.RoleGuest)
				bind(writers[resource], "write-"+lo.KebabCase(resource), resource, policy.ActionCreate, policy.ActionUpdate, policy.ActionDelete)
			}
		}

		configReader = person("config-reader", policy.RoleGuest)
		bind(configReader, "read-configs", policy.ResourceConfig, policy.ActionRead)

		viewer = person("viewer", policy.RoleViewer)
		editor = person("editor", policy.RoleEditor)
		nobody = person("nobody", policy.RoleGuest)

		Expect(rbac.ReloadPolicy()).To(Succeed())

		e = wholeTypeRoutes()
	})

	AfterAll(func() {
		for _, p := range persons {
			Expect(DefaultContext.DB().Delete(p).Error).To(Succeed())
		}
		deleteNamespace(namespace)
	})

	for _, resource := range v1.WholeTypeResources {
		own := lo.Filter(routes, func(r route, _ int) bool { return r.resource == resource })
		others := lo.Filter(routes, func(r route, _ int) bool { return r.resource != resource })

		It("lets a subject reading only "+resource+" read it, and nothing else", func() {
			for _, r := range own {
				Expect(passes(readers[resource], r)).To(Equal(!r.write), r.method+" "+r.path)
			}
			for _, r := range others {
				Expect(passes(readers[resource], r)).To(BeFalse(), r.method+" "+r.path)
			}
		})

		if resource == v1.ResourcePerson {
			continue
		}

		It("lets a subject writing only "+resource+" write it, and nothing else", func() {
			for _, r := range own {
				Expect(passes(writers[resource], r)).To(Equal(r.write), r.method+" "+r.path)
			}
			for _, r := range others {
				Expect(passes(writers[resource], r)).To(BeFalse(), r.method+" "+r.path)
			}
		})
	}

	It("doesn't let a Role's grant on another type through the routes' legacy objects", func() {
		Expect(passes(configReader, routes[0])).To(BeFalse(), "reading configs grants catalog, which /application checked")
		for _, r := range routes {
			Expect(passes(configReader, r)).To(BeFalse(), r.method+" "+r.path)
		}
	})

	It("refuses a subject without grants", func() {
		for _, r := range routes {
			Expect(passes(nobody, r)).To(BeFalse(), r.method+" "+r.path)
		}
	})

	for _, builtIn := range []string{policy.RoleViewer, policy.RoleEditor} {
		It("keeps the access a built-in "+builtIn+" had", func() {
			p := lo.Ternary(builtIn == policy.RoleViewer, viewer, editor)
			for _, r := range routes {
				before := rbac.Check(DefaultContext, p.ID.String(), r.legacy.Object, r.legacy.Action)
				if before {
					Expect(passes(p, r)).To(BeTrue(), r.method+" "+r.path)
				}
			}
		})
	}

	It("lets a built-in viewer read applications, agents, people and teams, and nothing that configures access", func() {
		allowed := map[string]bool{"/application/default/missing": true, "/db/applications": true, "/db/agents": true,
			"/db/people": true, "/db/teams": true, "/db/team_members": true}
		for _, r := range routes {
			Expect(passes(viewer, r)).To(Equal(allowed[r.path] && r.method == http.MethodGet), r.method+" "+r.path)
		}
	})

	It("lets a built-in editor also change notifications, their silences and applications", func() {
		for _, r := range routes {
			switch r.resource {
			case v1.ResourceNotification, v1.ResourceNotificationSilence, v1.ResourceApplication:
				Expect(passes(editor, r)).To(BeTrue(), r.method+" "+r.path)
			case v1.ResourceScope, v1.ResourceRole, v1.ResourceRoleBinding:
				Expect(passes(editor, r)).To(BeFalse(), r.method+" "+r.path)
			}
		}
	})
})
