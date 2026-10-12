package permissions_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/flanksource/duty/tests/setup"
	"github.com/flanksource/duty/types"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sTypes "k8s.io/apimachinery/pkg/types"

	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/auth"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/playbook"
	mcRBAC "github.com/flanksource/incident-commander/rbac"
	"github.com/flanksource/incident-commander/rbac/adapter"
)

// Access tokens limited by Roles (specs/authorization/roles.md, Section 4.5).
var _ = ginkgo.Describe("Access tokens limited by Roles", ginkgo.Ordered, func() {
	const namespace = "token-roles"

	var (
		e                    *echo.Echo
		alice, bob           *models.Person
		inA, inB, outside    models.ConfigItem
		runbook              models.Playbook
		roleIDs, bindingIDs  = map[string]string{}, map[string]string{}
		readonly, unscoped   *models.Person
		tenantToken, missing *models.Person
	)

	newScope := func(name string, targets ...v1.ScopeTarget) {
		ginkgo.GinkgoHelper()
		scope := &v1.Scope{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, UID: k8sTypes.UID(uuid.NewString())},
			Spec:       v1.ScopeSpec{Targets: targets},
		}
		Expect(db.PersistScopeFromCRD(DefaultContext, scope)).To(Succeed())
	}

	allow := func(name, action, resource string) v1.RoleRule {
		return v1.RoleRule{Name: name, Action: action, Resource: &v1.ScopeReference{ScopeRef: resource}}
	}

	newRole := func(name string, rules ...v1.RoleRule) {
		ginkgo.GinkgoHelper()
		role := &v1.Role{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, UID: k8sTypes.UID(uuid.NewString())},
			Spec:       v1.RoleSpec{Rules: rules},
		}
		Expect(db.PersistRoleFromCRD(DefaultContext, role)).To(Succeed())
		roleIDs[name] = string(role.UID)
	}

	bind := func(name, role string, person *models.Person) {
		ginkgo.GinkgoHelper()
		binding := &v1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, UID: k8sTypes.UID(uuid.NewString())},
			Spec: v1.RoleBindingSpec{Role: role, Subjects: v1.RoleBindingSubjects{
				PermissionGroupSubjects: v1.PermissionGroupSubjects{People: []string{person.Email}},
			}},
		}
		Expect(db.PersistRoleBindingFromCRD(DefaultContext, binding)).To(Succeed())
		bindingIDs[name] = string(binding.UID)
	}

	reload := func() {
		ginkgo.GinkgoHelper()
		Expect(rbac.ReloadPolicy()).To(Succeed())
		auth.FlushTokenCache()
	}

	do := func(user *models.Person, method, path string, body any) *httptest.ResponseRecorder {
		ginkgo.GinkgoHelper()
		raw, err := json.Marshal(body)
		Expect(err).ToNot(HaveOccurred())

		req := httptest.NewRequest(method, path, bytes.NewReader(raw))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		req = req.WithContext(DefaultContext.WithUser(user))
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		return rec
	}

	// createToken creates a token of the owner through the API and returns the person it authenticates as.
	createToken := func(owner *models.Person, name string, roles ...string) *models.Person {
		ginkgo.GinkgoHelper()
		tokenRoles := lo.Map(roles, func(role string, _ int) models.AccessTokenRole {
			return models.AccessTokenRole{Namespace: namespace, Name: role}
		})
		rec := do(owner, http.MethodPost, "/auth/create_token", auth.CreateTokenRequest{Name: name, Roles: tokenRoles})
		Expect(rec.Code).To(Equal(http.StatusOK), rec.Body.String())

		var token models.AccessToken
		Expect(DefaultContext.DB().Where("name = ? AND created_by = ?", name, owner.ID).First(&token).Error).To(Succeed())
		var person models.Person
		Expect(DefaultContext.DB().Where("id = ?", token.PersonID).First(&person).Error).To(Succeed())
		return &person
	}

	canRead := func(subject *models.Person, config models.ConfigItem) bool {
		return mcRBAC.HasPermission(DefaultContext, subject.ID.String(), &models.ABACAttribute{Config: config}, policy.ActionRead)
	}

	canRun := func(subject *models.Person, config models.ConfigItem) bool {
		return mcRBAC.HasPermission(DefaultContext, subject.ID.String(), &models.ABACAttribute{Playbook: runbook, Config: config}, policy.ActionPlaybookRun)
	}

	// listed returns which of the test's configs a listing as the person returns.
	listed := func(person *models.Person) []uuid.UUID {
		ginkgo.GinkgoHelper()
		payload, err := auth.GetRLSPayload(DefaultContext.WithUser(person))
		Expect(err).ToNot(HaveOccurred())

		tx := DefaultContext.DB().Begin()
		defer tx.Rollback()
		Expect(payload.SetPostgresSessionRLS(tx)).To(Succeed())

		var ids []uuid.UUID
		Expect(tx.Model(&models.ConfigItem{}).Where("id IN ?", []uuid.UUID{inA.ID, inB.ID, outside.ID}).Pluck("id", &ids).Error).To(Succeed())
		return ids
	}

	ginkgo.BeforeAll(func() {
		Expect(rbac.Init(DefaultContext, []string{"admin"}, adapter.NewPermissionAdapter)).To(Succeed())

		alice = setup.CreateUserWithRole(DefaultContext, "Token Alice", "token-alice@test.com", policy.RoleGuest)
		bob = setup.CreateUserWithRole(DefaultContext, "Token Bob", "token-bob@test.com", policy.RoleGuest)

		config := func(name string, tags types.JSONStringMap) models.ConfigItem {
			return models.ConfigItem{ID: uuid.New(), Name: lo.ToPtr(name), ConfigClass: "Pod", Type: lo.ToPtr("Kubernetes::Pod"), Tags: tags}
		}
		inA = config("token-roles-in-a", types.JSONStringMap{"token-roles": "yes", "token-tenant": "a"})
		inB = config("token-roles-in-b", types.JSONStringMap{"token-roles": "yes", "token-tenant": "b"})
		outside = config("token-roles-outside", types.JSONStringMap{"token-roles": "no", "token-tenant": "a"})
		for _, cfg := range []*models.ConfigItem{&inA, &inB, &outside} {
			Expect(DefaultContext.DB().Create(cfg).Error).To(Succeed())
		}

		runbook = models.Playbook{
			ID:        uuid.New(),
			Name:      "token-roles-run",
			Namespace: "default",
			Source:    models.SourceUI,
			Spec:      []byte(`{"configs":[{"name":"*"}],"actions":[{"name":"echo","exec":{"script":"echo"}}]}`),
		}
		Expect(DefaultContext.DB().Create(&runbook).Error).To(Succeed())

		newScope("all-configs", v1.ScopeTarget{Config: &v1.ScopeConfigSelector{ScopeResourceRef: v1.ScopeResourceRef{Name: "*"}}})
		newScope("token-configs", v1.ScopeTarget{Config: &v1.ScopeConfigSelector{TagSelector: "token-roles=yes"}})
		newScope("tenant-a", v1.ScopeTarget{Config: &v1.ScopeConfigSelector{TagSelector: "token-tenant=a"}})
		newScope("playbooks", v1.ScopeTarget{Playbook: &v1.ScopePlaybookRef{Name: "token-roles-*"}})

		runOnConfigs := allow("run", policy.ActionPlaybookRun, "playbooks")
		runOnConfigs.Target = &v1.ScopeReference{ScopeRef: "all-configs"}
		newRole("operator",
			allow("read-configs", policy.ActionRead, "all-configs"),
			allow("delete-configs", policy.ActionDelete, "all-configs"),
			allow("read-playbooks", policy.ActionRead, "playbooks"),
			runOnConfigs)
		// Stands for the shipped viewer Role, whose name a Role can't take yet
		newRole("readonly",
			allow("read-configs", policy.ActionRead, "all-configs"),
			allow("read-playbooks", policy.ActionRead, "playbooks"))
		newRole("token-configs-reader", allow("read", policy.ActionRead, "token-configs"))
		newRole("tenant-a-reader", allow("read", policy.ActionRead, "tenant-a"))

		bind("alice-operator", "operator", alice)
		bind("bob-token-configs", "token-configs-reader", bob)
		reload()

		e = echo.New()
		auth.RegisterRoutes(e)
		playbook.RegisterRoutes(e)
		e.GET("/test/catalog", func(c echo.Context) error { return c.NoContent(http.StatusOK) }, mcRBAC.Authorization(policy.ObjectCatalog, policy.ActionRead))
		e.DELETE("/test/catalog", func(c echo.Context) error { return c.NoContent(http.StatusOK) }, mcRBAC.Authorization(policy.ObjectCatalog, policy.ActionDelete))

		readonly = createToken(alice, "readonly", "readonly")
		unscoped = createToken(alice, "unscoped")
		tenantToken = createToken(bob, "tenant-a", "tenant-a-reader")
		missing = createToken(alice, "missing-role", "does-not-exist")
	})

	ginkgo.AfterAll(func() {
		DefaultContext.DB().Where("playbook_id = ?", runbook.ID).Delete(&models.PlaybookRun{})
		DefaultContext.DB().Delete(&runbook)
		for _, cfg := range []models.ConfigItem{inA, inB, outside} {
			DefaultContext.DB().Delete(&cfg)
		}
		DefaultContext.DB().Where("namespace = ?", namespace).Delete(&models.RoleBinding{})
		DefaultContext.DB().Where("namespace = ?", namespace).Delete(&models.Role{})
		DefaultContext.DB().Where("namespace = ?", namespace).Delete(&models.Scope{})
		for _, token := range []*models.Person{readonly, unscoped, tenantToken, missing} {
			DefaultContext.DB().Where("person_id = ?", token.ID).Delete(&models.AccessToken{})
		}
		Expect(rbac.ReloadPolicy()).To(Succeed())
	})

	ginkgo.It("lets a token with a read-only Role read configs but not run a playbook its owner can run", func() {
		Expect(do(alice, http.MethodGet, "/test/catalog", nil).Code).To(Equal(http.StatusOK))
		Expect(do(readonly, http.MethodGet, "/test/catalog", nil).Code).To(Equal(http.StatusOK))

		Expect(do(alice, http.MethodDelete, "/test/catalog", nil).Code).To(Equal(http.StatusOK))
		Expect(do(readonly, http.MethodDelete, "/test/catalog", nil).Code).To(Equal(http.StatusForbidden))

		Expect(canRead(alice, inA)).To(BeTrue())
		Expect(canRead(readonly, inA)).To(BeTrue())
		Expect(canRun(alice, inA)).To(BeTrue())
		Expect(canRun(readonly, inA)).To(BeFalse())

		ownerSummary := mcRBAC.AccessSummary(DefaultContext.WithUser(alice))[policy.ResourceConfig]
		Expect(ownerSummary[policy.ActionRead]).To(Equal(mcRBAC.AccessAll))
		Expect(ownerSummary[policy.ActionDelete]).To(Equal(mcRBAC.AccessAll))
		tokenSummary := mcRBAC.AccessSummary(DefaultContext.WithUser(readonly))[policy.ResourceConfig]
		Expect(tokenSummary[policy.ActionRead]).To(Equal(mcRBAC.AccessAll))
		Expect(tokenSummary[policy.ActionDelete]).To(Equal(mcRBAC.AccessNone))

		run := map[string]any{"id": runbook.ID, "config_id": inA.ID}
		Expect(do(readonly, http.MethodPost, "/playbook/run", run).Code).To(Equal(http.StatusForbidden))
		rec := do(alice, http.MethodPost, "/playbook/run", run)
		Expect(rec.Code).To(Equal(http.StatusCreated), rec.Body.String())
	})

	ginkgo.It("leaves tokens without Roles unaffected", func() {
		Expect(do(unscoped, http.MethodDelete, "/test/catalog", nil).Code).To(Equal(http.StatusOK))
		Expect(canRun(unscoped, inA)).To(BeTrue())

		rec := do(unscoped, http.MethodPost, "/playbook/run", map[string]any{"id": runbook.ID, "config_id": inA.ID})
		Expect(rec.Code).To(Equal(http.StatusCreated), rec.Body.String())

		Expect(listed(unscoped)).To(ConsistOf(listed(alice)))
	})

	ginkgo.It("lists the rows both the owner and the token's Roles allow", func() {
		Expect(listed(bob)).To(ConsistOf(inA.ID, inB.ID))
		Expect(listed(tenantToken)).To(Equal([]uuid.UUID{inA.ID}), "tenant-a-reader allows inA and outside, bob reads inA and inB")

		Expect(canRead(tenantToken, inA)).To(BeTrue())
		Expect(canRead(tenantToken, inB)).To(BeFalse())
		Expect(canRead(tenantToken, outside)).To(BeFalse())
	})

	ginkgo.It("matches nothing through a Role that doesn't exist", func() {
		Expect(canRead(missing, inA)).To(BeFalse())
		Expect(do(missing, http.MethodGet, "/test/catalog", nil).Code).To(Equal(http.StatusForbidden))
		Expect(listed(missing)).To(BeEmpty())
	})

	ginkgo.It("matches nothing through a Role removed from under the token", func() {
		Expect(db.DeleteRole(DefaultContext, roleIDs["tenant-a-reader"])).To(Succeed())
		reload()

		Expect(canRead(tenantToken, inA)).To(BeFalse())
		Expect(listed(tenantToken)).To(BeEmpty())
		Expect(listed(bob)).To(ConsistOf(inA.ID, inB.ID))
	})

	ginkgo.It("loses what its owner loses", func() {
		Expect(db.DeleteRoleBinding(DefaultContext, bindingIDs["alice-operator"])).To(Succeed())
		reload()

		Expect(canRead(readonly, inA)).To(BeFalse())
		Expect(do(readonly, http.MethodGet, "/test/catalog", nil).Code).To(Equal(http.StatusForbidden))
		Expect(listed(readonly)).To(BeEmpty())
	})
})
