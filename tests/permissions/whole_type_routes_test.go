package permissions_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/flanksource/duty/context"
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
	"github.com/flanksource/incident-commander/auth"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/notification"
	mcRBAC "github.com/flanksource/incident-commander/rbac"
	"github.com/flanksource/incident-commander/rbac/adapter"
	"github.com/flanksource/incident-commander/rbac/rbactest"
	"github.com/flanksource/incident-commander/rbac/shipped"
)

// objectAction is an action on an RBAC object.
type objectAction struct {
	Object string
	Action string
}

// serveAs serves a request made by the person, and returns its status.
func serveAs(e *echo.Echo, person *models.Person, method, path string) int {
	GinkgoHelper()
	return serveWith(e, DefaultContext.WithUser(person), method, path)
}

func serveWith(e *echo.Echo, ctx context.Context, method, path string) int {
	GinkgoHelper()
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	req = req.WithContext(ctx)
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
func bindWholeType(namespace, name, resource string, subjects v1.RoleBindingSubjects, actions ...string) {
	GinkgoHelper()
	uid := func() k8sTypes.UID { return k8sTypes.UID(uuid.NewString()) }

	var target v1.ScopeTarget
	Expect(json.Unmarshal(fmt.Appendf(nil, `{%q: {"name": "*"}}`, resource), &target)).To(Succeed())
	Expect(db.PersistScopeFromCRD(DefaultContext, &v1.Scope{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, UID: uid()},
		Spec:       v1.ScopeSpec{Targets: []v1.ScopeTarget{target}},
	})).To(Succeed())

	rules := lo.Map(actions, func(action string, _ int) v1.RoleRule {
		return v1.RoleRule{Name: action, Action: action, Resource: &v1.ScopeReference{ScopeRef: name}}
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

// newAdmin creates a person in the shipped admins team, which is bound to the shipped admin Role.
func newAdmin(name, email string) *models.Person {
	GinkgoHelper()
	p := setup.CreateUserWithRole(DefaultContext, name, email)
	Expect(rbactest.MakeAdmins(DefaultContext, p.ID)).To(Succeed())
	auth.FlushTokenCache()
	return p
}

// newViewer creates a person bound to the shipped viewer Role.
func newViewer(name, email string) *models.Person {
	GinkgoHelper()
	p := setup.CreateUserWithRole(DefaultContext, name, email)
	Expect(shipped.Sync(DefaultContext)).To(Succeed())
	binding := &v1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "viewer-" + p.ID.String(), Namespace: "default", UID: k8sTypes.UID(uuid.NewString())},
		Spec:       v1.RoleBindingSpec{Role: adapter.ShippedNamespace + "/" + shipped.RoleViewer, Subjects: boundPeople(p)},
	}
	Expect(db.PersistRoleBindingFromCRD(DefaultContext, binding)).To(Succeed())
	Expect(rbac.ReloadPolicy()).To(Succeed())
	auth.FlushTokenCache()
	DeferCleanup(func() {
		Expect(DefaultContext.DB().Where("id = ?", binding.UID).Delete(&models.RoleBinding{}).Error).To(Succeed())
		Expect(rbac.ReloadPolicy()).To(Succeed())
	})
	return p
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

		// legacy is what the route checked before its type had an object of its own, which built-in roles granted
		legacy objectAction
	}

	// objects are the objects each type is checked on
	objects := map[string]string{
		v1.ResourceApplication:         policy.ObjectApplication,
		v1.ResourceNotification:        policy.ObjectNotification,
		v1.ResourceNotificationSilence: adapter.ObjectNotificationSilences,
		v1.ResourceAgent:               policy.ObjectAgent,
		v1.ResourcePerson:              policy.ObjectPeople,
		v1.ResourceTeam:                adapter.ObjectTeams,
		v1.ResourceScope:               adapter.ObjectScopes,
		v1.ResourceRole:                adapter.ObjectRoles,
		v1.ResourceRoleBinding:         adapter.ObjectRoleBindings,
	}

	read := func(object string) objectAction { return objectAction{Object: object, Action: policy.ActionRead} }
	grant := func(object, action string) objectAction { return objectAction{Object: object, Action: action} }

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
		{v1.ResourceAgent, http.MethodGet, "/agent/" + uuid.NewString() + "/tokens", false, read(policy.ObjectAgent)},
		{v1.ResourceAgent, http.MethodDelete, "/agent/" + uuid.NewString() + "/tokens/" + uuid.NewString(), true, grant(policy.ObjectAgent, policy.ActionUpdate)},

		{v1.ResourcePerson, http.MethodGet, "/db/people", false, read(policy.ObjectPeople)},
		{v1.ResourcePerson, http.MethodGet, "/db/person_analytics", false, read(policy.ObjectPeople)},

		{v1.ResourceTeam, http.MethodGet, "/db/teams", false, read(policy.ObjectDatabasePublic)},
		{v1.ResourceTeam, http.MethodGet, "/db/teams_with_status", false, read(policy.ObjectDatabasePublic)},
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
		e                    *echo.Echo
		configReader, nobody *models.Person
		readers              = map[string]*models.Person{}
		writers              = map[string]*models.Person{}
		persons              []*models.Person
	)

	passes := func(person *models.Person, r route) bool {
		GinkgoHelper()
		return authorized(serveAs(e, person, r.method, r.path))
	}

	bind := func(person *models.Person, name, resource string, actions ...string) {
		GinkgoHelper()
		bindWholeType(namespace, name, resource, boundPeople(person), actions...)
	}

	person := func(name string, roles ...string) *models.Person {
		GinkgoHelper()
		p := setup.CreateUserWithRole(DefaultContext, "Whole Type "+name, "whole-type-"+name+"@test.com", roles...)
		persons = append(persons, p)
		return p
	}

	BeforeAll(func() {
		Expect(mcRBAC.Init(DefaultContext)).To(Succeed())

		for _, resource := range v1.WholeTypeResources {
			readers[resource] = person("reader-" + resource)
			bind(readers[resource], "read-"+lo.KebabCase(resource), resource, policy.ActionRead)

			if resource != v1.ResourcePerson {
				writers[resource] = person("writer-" + resource)
				bind(writers[resource], "write-"+lo.KebabCase(resource), resource, policy.ActionCreate, policy.ActionUpdate, policy.ActionDelete)
			}
		}

		configReader = person("config-reader")
		bind(configReader, "read-configs", policy.ResourceConfig, policy.ActionRead)

		nobody = person("nobody")

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

	It("lets any signed-in person record their own analytics through POST /db/person_analytics, and only person:manage another's", func() {
		recordAnalytics := func(by *models.Person, of ...uuid.UUID) *httptest.ResponseRecorder {
			GinkgoHelper()
			rows := lo.Map(of, func(id uuid.UUID, _ int) map[string]string {
				return map[string]string{"person_id": id.String(), "key": "playbooks.run"}
			})
			body, err := json.Marshal(rows)
			Expect(err).ToNot(HaveOccurred())
			req := httptest.NewRequest(http.MethodPost, "/db/person_analytics", bytes.NewReader(body))
			req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req.WithContext(DefaultContext.WithUser(by)))
			return rec
		}

		Expect(recordAnalytics(nobody, nobody.ID).Code).To(Equal(http.StatusOK))

		for _, of := range [][]uuid.UUID{{configReader.ID}, {nobody.ID, configReader.ID}} {
			rec := recordAnalytics(nobody, of...)
			Expect(rec.Code).To(Equal(http.StatusForbidden), "%v", of)
			Expect(rec.Header().Get("X-Rbac-Action")).To(Equal(adapter.ActionPersonManage))
		}

		manager := person("analytics-manager")
		_, err := rbac.Enforcer().AddPolicy(manager.ID.String(), policy.ObjectPeople, adapter.ActionPersonManage, "allow", "", "test")
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() {
			_, err := rbac.Enforcer().DeletePermissionsForUser(manager.ID.String())
			Expect(err).ToNot(HaveOccurred())
		})
		Expect(recordAnalytics(manager, configReader.ID).Code).To(Equal(http.StatusOK))
	})

	// checked is the object and action a route checks now
	checked := func(r route) objectAction {
		switch {
		case r.path == "/agent/token", strings.HasPrefix(r.path, "/agent/") && r.method == http.MethodDelete:
			return objectAction{objects[r.resource], policy.ActionUpdate}
		case strings.HasSuffix(strings.Split(r.path, "?")[0], "/team_members") && r.write:
			return objectAction{objects[r.resource], policy.ActionUpdate}
		}
		return objectAction{objects[r.resource], map[string]string{
			http.MethodGet: policy.ActionRead, http.MethodPost: policy.ActionCreate,
			http.MethodPatch: policy.ActionUpdate, http.MethodDelete: policy.ActionDelete,
		}[r.method]}
	}

	for _, r := range routes {
		if r.legacy == checked(r) {
			continue
		}

		It("refuses "+r.method+" "+r.path+" to a subject with only "+r.legacy.Object+":"+r.legacy.Action+" and the built-in roles", func() {
			holder := person("legacy-"+uuid.NewString()[:8], adapter.BuiltInRoles...)
			_, err := rbac.Enforcer().AddPolicy(holder.ID.String(), r.legacy.Object, r.legacy.Action, "allow", "", "legacy")
			Expect(err).ToNot(HaveOccurred())
			DeferCleanup(func() {
				_, err := rbac.Enforcer().DeletePermissionsForUser(holder.ID.String())
				Expect(err).ToNot(HaveOccurred())
			})

			Expect(rbac.Check(DefaultContext, holder.ID.String(), r.legacy.Object, r.legacy.Action)).To(BeTrue())
			Expect(passes(holder, r)).To(BeFalse())
		})
	}

	It("grants each action on a type on its own", func() {
		creator := person("silence-creator")
		bind(creator, "create-silences", v1.ResourceNotificationSilence, policy.ActionCreate)
		Expect(rbac.ReloadPolicy()).To(Succeed())

		Expect(authorized(serveAs(e, creator, http.MethodPost, "/notification/silence"))).To(BeTrue())
		Expect(serveAs(e, creator, http.MethodPatch, "/db/notification_silences")).To(Equal(http.StatusForbidden))
		Expect(serveAs(e, creator, http.MethodDelete, "/db/notification_silences")).To(Equal(http.StatusForbidden))
		Expect(serveAs(e, creator, http.MethodGet, "/notification/silence_preview")).To(Equal(http.StatusForbidden))
	})

	It("limits a token by its Roles on each type's own object", func() {
		owner := writers[v1.ResourceNotificationSilence]
		bind(owner, "write-notifications-too", v1.ResourceNotification, policy.ActionCreate, policy.ActionUpdate, policy.ActionDelete)
		Expect(rbac.ReloadPolicy()).To(Succeed())
		Expect(authorized(serveAs(e, owner, http.MethodPost, "/notification/silence"))).To(BeTrue())

		body, err := json.Marshal(auth.CreateTokenRequest{Name: "notifications-only", Roles: models.AccessTokenRoles{{Namespace: namespace, Name: "write-notifications-too"}}})
		Expect(err).ToNot(HaveOccurred())
		req := httptest.NewRequest(http.MethodPost, "/auth/create_token", bytes.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		Expect(auth.CreateToken(echo.New().NewContext(req.WithContext(DefaultContext.WithUser(owner)), rec))).To(Succeed())
		Expect(rec.Code).To(Equal(http.StatusOK), rec.Body.String())

		var token models.AccessToken
		Expect(DefaultContext.DB().Where("name = ? AND created_by = ?", "notifications-only", owner.ID).First(&token).Error).To(Succeed())
		DeferCleanup(func() { Expect(DefaultContext.DB().Delete(&token).Error).To(Succeed()) })

		tokenCtx := mcRBAC.WithAccessToken(DefaultContext.WithUser(owner), &token)
		Expect(authorized(serveWith(e, tokenCtx, http.MethodPatch, "/db/notifications"))).To(BeTrue())
		Expect(serveWith(e, tokenCtx, http.MethodPost, "/notification/silence")).To(Equal(http.StatusForbidden),
			"a Role granting notifications doesn't grant their silences")
	})

	It("refuses writes to a Scope's targets through /db", func() {
		Expect(authorized(serveAs(e, writers[v1.ResourceScope], http.MethodPost, "/rbac/scopes"))).To(BeTrue())
		for _, method := range []string{http.MethodPost, http.MethodPatch, http.MethodDelete} {
			Expect(serveAs(e, writers[v1.ResourceScope], method, "/db/scope_targets")).To(Equal(http.StatusMethodNotAllowed), method)
		}
	})
})

var _ = Describe("Records and lookup views of a type through /db", Ordered, func() {
	const namespace = "records-of-types"

	var (
		e                           *echo.Echo
		admin, peopleReader, nobody *models.Person
	)

	BeforeAll(func() {
		Expect(mcRBAC.Init(DefaultContext)).To(Succeed())
		admin = newAdmin("Records Admin", "records-admin@test.com")
		peopleReader = setup.CreateUserWithRole(DefaultContext, "Records People Reader", "records-people-reader@test.com")
		nobody = setup.CreateUserWithRole(DefaultContext, "Records Nobody", "records-nobody@test.com")
		bindWholeType(namespace, "read-people", v1.ResourcePerson, boundPeople(peopleReader), policy.ActionRead)
		Expect(rbac.ReloadPolicy()).To(Succeed())
		e = wholeTypeRoutes()
	})

	AfterAll(func() {
		for _, p := range []*models.Person{admin, peopleReader, nobody} {
			Expect(DefaultContext.DB().Delete(p).Error).To(Succeed())
		}
		deleteNamespace(namespace)
	})

	It("lets the shipped admin read every one, but those of views", func() {
		for table, types := range mcRBAC.TablesOfTypes() {
			expected := http.StatusOK
			if lo.Contains(types, policy.ResourceView) {
				expected = http.StatusForbidden
			}
			Expect(serveAs(e, admin, http.MethodGet, "/db/"+table)).To(Equal(expected), table)
		}
	})

	It("lets a subject read those of the types it may read entirely, and no others", func() {
		Expect(serveAs(e, peopleReader, http.MethodGet, "/db/identities")).To(Equal(http.StatusOK))
		Expect(serveAs(e, peopleReader, http.MethodGet, "/db/people_roles")).To(Equal(http.StatusOK))
		Expect(serveAs(e, peopleReader, http.MethodGet, "/db/team_components")).To(Equal(http.StatusForbidden), "team_components takes read on teams and components too")
		Expect(serveAs(e, peopleReader, http.MethodGet, "/db/notification_send_history")).To(Equal(http.StatusForbidden))
		Expect(serveAs(e, peopleReader, http.MethodPatch, "/db/identities")).To(Equal(http.StatusForbidden))
		Expect(serveAs(e, nobody, http.MethodGet, "/db/identities")).To(Equal(http.StatusForbidden))
	})
})
