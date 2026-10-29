package permissions_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/flanksource/duty/tests/setup"
	"github.com/flanksource/kopper"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sTypes "k8s.io/apimachinery/pkg/types"

	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/plugin"
	pluginAPI "github.com/flanksource/incident-commander/plugin/api"
	mcRBAC "github.com/flanksource/incident-commander/rbac"
	"github.com/flanksource/incident-commander/rbac/adapter"
	"github.com/flanksource/incident-commander/rbac/rbactest"
	"github.com/flanksource/incident-commander/rbac/shipped"
)

var _ = Describe("Shipped roles", Ordered, func() {
	const (
		namespace  = adapter.ShippedNamespace
		bindings   = "shipped-roles-test"
		pluginName = "shipped-roles-plugin"
		operation  = "tail-logs"
	)

	var (
		viewerPerson, adminPerson, teamAdmin, outsider *models.Person
		config                                         models.ConfigItem
		connection                                     models.Connection
		pluginID                                       = uuid.New()
		preexistingScope                               *v1.Scope
	)

	invokeAction := policy.NewPluginInvokeAction(pluginName, operation)

	uid := func() k8sTypes.UID { return k8sTypes.UID(uuid.NewString()) }

	stored := func() ([]models.Scope, []models.Role) {
		GinkgoHelper()
		var scopes []models.Scope
		var roles []models.Role
		Expect(DefaultContext.DB().Where("namespace = ? AND deleted_at IS NULL", namespace).Order("name").Find(&scopes).Error).To(Succeed())
		Expect(DefaultContext.DB().Where("namespace = ? AND deleted_at IS NULL", namespace).Order("name").Find(&roles).Error).To(Succeed())
		return scopes, roles
	}

	storedRole := func(name string) models.Role {
		GinkgoHelper()
		var role models.Role
		Expect(DefaultContext.DB().Where("namespace = ? AND name = ? AND deleted_at IS NULL", namespace, name).First(&role).Error).To(Succeed())
		return role
	}

	// pluginAdminActions returns the actions of plugin-admin, none when it isn't shipped.
	pluginAdminActions := func() []string {
		GinkgoHelper()
		var roles []models.Role
		Expect(DefaultContext.DB().Where("namespace = ? AND name = ? AND deleted_at IS NULL", namespace, shipped.RolePluginAdmin).
			Find(&roles).Error).To(Succeed())
		var actions []string
		for _, role := range roles {
			Expect(role.Error).To(BeNil())
			var rules []v1.RoleRule
			Expect(json.Unmarshal(role.Rules, &rules)).To(Succeed())
			actions = append(actions, lo.Map(rules, func(r v1.RoleRule, _ int) string { return r.Action })...)
		}
		return actions
	}

	ruleActions := func(role models.Role) []string {
		GinkgoHelper()
		var rules []v1.RoleRule
		Expect(json.Unmarshal(role.Rules, &rules)).To(Succeed())
		return lo.Map(rules, func(r v1.RoleRule, _ int) string { return r.Action })
	}

	sync := func() {
		GinkgoHelper()
		Expect(shipped.Sync(DefaultContext)).To(Succeed())
	}

	bind := func(role string, person *models.Person) {
		GinkgoHelper()
		Expect(db.PersistRoleBindingFromCRD(DefaultContext, &v1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: role + "-binding", Namespace: bindings, UID: uid()},
			Spec: v1.RoleBindingSpec{Role: namespace + "/" + role, Subjects: v1.RoleBindingSubjects{
				PermissionGroupSubjects: v1.PermissionGroupSubjects{People: []string{person.Email}},
			}},
		})).To(Succeed())
	}

	canRead := func(person *models.Person, attr *models.ABACAttribute) bool {
		return rbac.HasPermission(DefaultContext, person.ID.String(), attr, policy.ActionRead)
	}

	canInvoke := func(person *models.Person) bool {
		return rbac.HasPermission(DefaultContext, person.ID.String(), &models.ABACAttribute{Config: config}, invokeAction)
	}

	BeforeAll(func() {
		Expect(mcRBAC.Init(DefaultContext)).To(Succeed())

		viewerPerson = setup.CreateUserWithRole(DefaultContext, "Shipped Viewer", "shipped-viewer@test.com")
		adminPerson = setup.CreateUserWithRole(DefaultContext, "Shipped Admin", "shipped-admin@test.com")
		teamAdmin = setup.CreateUserWithRole(DefaultContext, "Shipped Team Admin", "shipped-builtin-admin@test.com")
		outsider = setup.CreateUserWithRole(DefaultContext, "Shipped Outsider", "shipped-outsider@test.com")

		config = models.ConfigItem{ID: uuid.New(), Name: lo.ToPtr("shipped-config"), Type: lo.ToPtr("Shipped::Test"), ConfigClass: "Test"}
		Expect(DefaultContext.DB().Create(&config).Error).To(Succeed())
		connection = models.Connection{ID: uuid.New(), Name: "shipped-connection", Namespace: "default", Type: "http", URL: "http://example.com", Source: models.SourceUI}
		Expect(DefaultContext.DB().Create(&connection).Error).To(Succeed())

		// Other specs make admins, which writes the shipped objects
		Expect(rbactest.DeleteNamespace(DefaultContext, namespace)).To(Succeed())

		// A Scope stored in the shipped namespace before Mission Control owned it
		preexistingScope = &v1.Scope{
			ObjectMeta: metav1.ObjectMeta{Name: "all-configs", Namespace: namespace, UID: uid()},
			Spec:       v1.ScopeSpec{Targets: []v1.ScopeTarget{{Config: &v1.ScopeConfigSelector{TagSelector: "env=preexisting"}}}},
		}
		Expect(db.PersistScope(DefaultContext, preexistingScope, models.SourceCRD, nil)).To(Succeed())

		_, err := plugin.DefaultRegistry.Upsert(pluginID, "default", pluginName, v1.PluginSpec{Source: "test"})
		Expect(err).ToNot(HaveOccurred())
		Expect(plugin.DefaultRegistry.SetManifest(pluginID, &pluginAPI.PluginManifest{
			Name:       pluginName,
			Operations: []*pluginAPI.OperationDef{{Name: operation}},
		})).To(Succeed())

		sync()
		Expect(shipped.AddToAdmins(DefaultContext, teamAdmin.ID)).To(Succeed())
		Expect(rbac.ReloadPolicy()).To(Succeed())

		bind(shipped.RoleViewer, viewerPerson)
		bind(shipped.RoleAdmin, adminPerson)
		bind(shipped.RolePluginAdmin, adminPerson)
		Expect(rbac.ReloadPolicy()).To(Succeed())
	})

	AfterAll(func() {
		plugin.DefaultRegistry.Remove(pluginID)
		Expect(DefaultContext.DB().Where("namespace IN ?", []string{namespace, bindings}).Delete(&models.RoleBinding{}).Error).To(Succeed())
		Expect(DefaultContext.DB().Where("namespace = ?", namespace).Delete(&models.Role{}).Error).To(Succeed())
		Expect(DefaultContext.DB().Where("namespace = ?", namespace).Delete(&models.Scope{}).Error).To(Succeed())
		Expect(DefaultContext.DB().Exec(`DELETE FROM team_members WHERE team_id IN (SELECT id FROM teams WHERE source = ?)`, adapter.SourceShipped).Error).To(Succeed())
		Expect(DefaultContext.DB().Where("source = ?", adapter.SourceShipped).Delete(&models.Team{}).Error).To(Succeed())
		Expect(DefaultContext.DB().Delete(&connection).Error).To(Succeed())
		Expect(DefaultContext.DB().Delete(&config).Error).To(Succeed())
		for _, p := range []*models.Person{viewerPerson, adminPerson, teamAdmin, outsider} {
			Expect(DefaultContext.DB().Delete(p).Error).To(Succeed())
		}
		Expect(rbac.ReloadPolicy()).To(Succeed())
	})

	It("stores the shipped Roles and their Scopes, all Ready", func() {
		scopes, roles := stored()

		Expect(lo.Map(roles, func(r models.Role, _ int) string { return r.Name })).To(ConsistOf(shipped.RoleAdmin, shipped.RoleViewer, shipped.RoleAccessManage, shipped.RoleMCPUser, shipped.RolePluginAdmin))
		for _, role := range roles {
			Expect(role.Source).To(Equal(adapter.SourceShipped))
			Expect(role.Error).To(BeNil(), "role %s: %s", role.Name, lo.FromPtr(role.Error))
		}

		Expect(scopes).ToNot(BeEmpty())
		for _, scope := range scopes {
			Expect(scope.Source).To(Equal(adapter.SourceShipped))
			Expect(scope.Error).To(BeNil(), "scope %s: %s", scope.Name, lo.FromPtr(scope.Error))
		}
	})

	It("replaces a Scope of the same name applied before it was shipped", func() {
		scopes, _ := stored()
		allConfigs, ok := lo.Find(scopes, func(s models.Scope) bool { return s.Name == "all-configs" })
		Expect(ok).To(BeTrue())
		Expect(allConfigs.ID.String()).ToNot(Equal(string(preexistingScope.UID)))
	})

	It("stays Ready when the policy is reloaded", func() {
		Expect(rbac.ReloadPolicy()).To(Succeed())
		for _, name := range []string{shipped.RoleViewer, shipped.RoleAdmin} {
			Expect(storedRole(name).Error).To(BeNil())
		}
	})

	It("lets a viewer read configs, but not connections", func() {
		Expect(canRead(viewerPerson, &models.ABACAttribute{Config: config})).To(BeTrue())
		Expect(canRead(viewerPerson, &models.ABACAttribute{Connection: connection})).To(BeFalse())
		Expect(rbac.Check(DefaultContext, viewerPerson.ID.String(), policy.ObjectConnection, policy.ActionRead)).To(BeFalse())
		Expect(canInvoke(viewerPerson)).To(BeFalse())
	})

	It("lets a viewer read applications, people and teams, but not what configures access", func() {
		check := func(person *models.Person, object, action string) bool {
			return rbac.Check(DefaultContext, person.ID.String(), object, action)
		}

		for _, object := range []string{policy.ObjectApplication, policy.ObjectPeople, adapter.ObjectTeams} {
			Expect(check(viewerPerson, object, policy.ActionRead)).To(BeTrue(), object)
		}
		for _, object := range []string{policy.ObjectNotification, adapter.ObjectNotificationSilences, policy.ObjectAgent,
			adapter.ObjectScopes, adapter.ObjectRoles, adapter.ObjectRoleBindings} {
			Expect(check(viewerPerson, object, policy.ActionRead)).To(BeFalse(), object)
			Expect(check(adminPerson, object, policy.ActionRead)).To(BeTrue(), object)
			Expect(check(adminPerson, object, policy.ActionUpdate)).To(BeTrue(), object)
		}
		Expect(check(adminPerson, adapter.ObjectTeams, policy.ActionCreate)).To(BeTrue())
		Expect(check(viewerPerson, adapter.ObjectTeams, policy.ActionCreate)).To(BeFalse())
	})

	It("lets an admin read connections", func() {
		Expect(canRead(adminPerson, &models.ABACAttribute{Connection: connection})).To(BeTrue())
	})

	It("lets plugin-admin invoke an installed plugin's operation, until the plugin is removed", func() {
		Expect(pluginAdminActions()).To(ContainElement(invokeAction))
		Expect(canInvoke(adminPerson)).To(BeTrue())

		plugin.DefaultRegistry.Remove(pluginID)
		sync()

		Expect(pluginAdminActions()).To(BeEmpty(), "no plugin-admin without a plugin operation")
		Expect(canInvoke(adminPerson)).To(BeFalse())
	})

	It("keeps admin as written, whatever plugins are installed", func() {
		admin := storedRole(shipped.RoleAdmin)
		_, err := plugin.DefaultRegistry.Upsert(pluginID, "default", pluginName, v1.PluginSpec{Source: "test"})
		Expect(err).ToNot(HaveOccurred())
		Expect(plugin.DefaultRegistry.SetManifest(pluginID, &pluginAPI.PluginManifest{
			Name: pluginName, Operations: []*pluginAPI.OperationDef{{Name: operation}},
		})).To(Succeed())
		sync()
		DeferCleanup(func() {
			plugin.DefaultRegistry.Remove(pluginID)
			sync()
		})

		Expect(storedRole(shipped.RoleAdmin).Rules).To(MatchJSON(admin.Rules))
		Expect(ruleActions(storedRole(shipped.RoleAdmin))).ToNot(ContainElement(HavePrefix(policy.ActionPluginInvokePrefix)))
		Expect(canInvoke(viewerPerson)).To(BeFalse())
	})

	It("keeps an installed plugin's rules until it registers again, e.g. after a restart", func() {
		restarted := uuid.New()
		_, err := plugin.DefaultRegistry.Upsert(pluginID, "default", pluginName, v1.PluginSpec{Source: "test"})
		Expect(err).ToNot(HaveOccurred())
		Expect(plugin.DefaultRegistry.SetManifest(pluginID, &pluginAPI.PluginManifest{
			Name: pluginName, Operations: []*pluginAPI.OperationDef{{Name: operation}},
		})).To(Succeed())
		sync()
		Expect(pluginAdminActions()).To(ContainElement(invokeAction))

		// The process restarts: the plugin is in the registry again, without its manifest yet
		plugin.DefaultRegistry.Remove(pluginID)
		_, err = plugin.DefaultRegistry.Upsert(restarted, "default", pluginName, v1.PluginSpec{Source: "test"})
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() { plugin.DefaultRegistry.Remove(restarted) })
		sync()
		Expect(pluginAdminActions()).To(ContainElement(invokeAction))
		Expect(canInvoke(adminPerson)).To(BeTrue())

		// It registers with a different operation
		upgraded := policy.NewPluginInvokeAction(pluginName, "upgraded")
		Expect(plugin.DefaultRegistry.SetManifest(restarted, &pluginAPI.PluginManifest{
			Name: pluginName, Operations: []*pluginAPI.OperationDef{{Name: "upgraded"}},
		})).To(Succeed())
		sync()
		Expect(pluginAdminActions()).To(ContainElement(upgraded))
		Expect(pluginAdminActions()).ToNot(ContainElement(invokeAction))

		// It registers again without any operation: nothing of it is kept
		Expect(plugin.DefaultRegistry.SetManifest(restarted, &pluginAPI.PluginManifest{Name: pluginName})).To(Succeed())
		sync()
		Expect(pluginAdminActions()).To(BeEmpty())

		plugin.DefaultRegistry.Remove(restarted)
		sync()
		Expect(pluginAdminActions()).To(BeEmpty())
	})

	It("is idempotent", func() {
		scopesBefore, rolesBefore := stored()
		sync()
		sync()
		scopesAfter, rolesAfter := stored()

		ids := func(scopes []models.Scope, roles []models.Role) []uuid.UUID {
			return append(lo.Map(scopes, func(s models.Scope, _ int) uuid.UUID { return s.ID }),
				lo.Map(roles, func(r models.Role, _ int) uuid.UUID { return r.ID })...)
		}
		Expect(ids(scopesAfter, rolesAfter)).To(Equal(ids(scopesBefore, rolesBefore)))
		for i := range rolesBefore {
			Expect(rolesAfter[i].Rules).To(MatchJSON(rolesBefore[i].Rules))
		}
		for i := range scopesBefore {
			Expect(scopesAfter[i].Targets).To(MatchJSON(scopesBefore[i].Targets))
		}

		Expect(canRead(viewerPerson, &models.ABACAttribute{Config: config})).To(BeTrue())
	})

	It("reserves plugin-admin before any plugin has an operation", func() {
		Expect(pluginAdminActions()).To(BeEmpty(), "no plugin is installed")

		// Otherwise bindings to this Role would grant every plugin operation once the shipped one replaces it
		role := &v1.Role{
			ObjectMeta: metav1.ObjectMeta{Name: shipped.RolePluginAdmin, Namespace: namespace, UID: uid()},
			Spec: v1.RoleSpec{Rules: []v1.RoleRule{
				{Name: "read-configs", Action: policy.ActionRead, Resource: &v1.ScopeReference{ScopeRef: "all-configs"}},
			}},
		}
		var notReady *kopper.NotReadyError
		Expect(errors.As(db.PersistRoleFromCRD(DefaultContext, role), &notReady)).To(BeTrue())
		Expect(notReady.Reason).To(Equal(db.ReasonShipped))

		var count int64
		Expect(DefaultContext.DB().Model(&models.Role{}).Where("id = ?", role.UID).Count(&count).Error).To(Succeed())
		Expect(count).To(BeZero())
	})

	It("isn't overwritten by a Role or Scope applied through Kubernetes", func() {
		role := &v1.Role{
			ObjectMeta: metav1.ObjectMeta{Name: shipped.RoleViewer, Namespace: namespace, UID: uid()},
			Spec: v1.RoleSpec{Rules: []v1.RoleRule{
				{Name: "read-connections", Action: policy.ActionRead, Resource: &v1.ScopeReference{ScopeRef: "all-connections"}},
			}},
		}
		var notReady *kopper.NotReadyError
		Expect(errors.As(db.PersistRoleFromCRD(DefaultContext, role), &notReady)).To(BeTrue())
		Expect(notReady.Reason).To(Equal(db.ReasonShipped))
		Expect(db.DeleteStaleRole(DefaultContext, role)).ToNot(Succeed())

		scope := &v1.Scope{
			ObjectMeta: metav1.ObjectMeta{Name: "all-configs", Namespace: namespace, UID: uid()},
			Spec:       v1.ScopeSpec{Targets: []v1.ScopeTarget{{Config: &v1.ScopeConfigSelector{TagSelector: "env=none"}}}},
		}
		Expect(errors.As(db.PersistScopeFromCRD(DefaultContext, scope), &notReady)).To(BeTrue())
		Expect(db.DeleteStaleScope(DefaultContext, scope)).ToNot(Succeed())

		viewer := storedRole(shipped.RoleViewer)
		Expect(viewer.Source).To(Equal(adapter.SourceShipped))
		Expect(ruleActions(viewer)).To(HaveEach(policy.ActionRead))
		var count int64
		Expect(DefaultContext.DB().Model(&models.Role{}).Where("id = ?", role.UID).Count(&count).Error).To(Succeed())
		Expect(count).To(BeZero())

		scopes, _ := stored()
		allConfigs, _ := lo.Find(scopes, func(s models.Scope) bool { return s.Name == "all-configs" })
		Expect(allConfigs.Source).To(Equal(adapter.SourceShipped))
	})

	Context("through the API", func() {
		var e *echo.Echo

		requestAs := func(person *models.Person, method, path string, body any) *httptest.ResponseRecorder {
			GinkgoHelper()
			var payload []byte
			if body != nil {
				var err error
				payload, err = json.Marshal(body)
				Expect(err).ToNot(HaveOccurred())
			}

			req := httptest.NewRequest(method, path, bytes.NewReader(payload))
			req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
			req = req.WithContext(DefaultContext.WithUser(person))
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)
			return rec
		}

		request := func(method, path string, body any) *httptest.ResponseRecorder {
			GinkgoHelper()
			return requestAs(teamAdmin, method, path, body)
		}

		readRole := map[string]any{"rules": []any{map[string]any{
			"name": "read", "action": "read", "resource": map[string]any{"scopeRef": "all-connections"},
		}}}
		configTargets := map[string]any{"targets": []any{map[string]any{"config": map[string]any{"tagSelector": "env=none"}}}}

		BeforeAll(func() {
			e = echo.New()
			mcRBAC.RegisterRoutes(e)
		})

		It("lets a subject bound to admin manage RoleBindings, and not one bound to viewer", func() {
			binding := map[string]any{
				"metadata": map[string]any{"name": "api-viewers", "namespace": bindings},
				"spec":     map[string]any{"role": namespace + "/" + shipped.RoleViewer, "subjects": map[string]any{"people": []string{outsider.Email}}},
			}
			Expect(requestAs(viewerPerson, http.MethodPost, "/rbac/role-bindings", binding).Code).To(Equal(http.StatusForbidden))

			rec := requestAs(adminPerson, http.MethodPost, "/rbac/role-bindings", binding)
			Expect(rec.Code).To(Equal(http.StatusCreated), rec.Body.String())
			Expect(requestAs(adminPerson, http.MethodGet, "/rbac/role-bindings/shipped-roles-test/api-viewers", nil).Code).To(Equal(http.StatusOK))
			Expect(requestAs(adminPerson, http.MethodDelete, "/rbac/role-bindings/shipped-roles-test/api-viewers", nil).Code).To(Equal(http.StatusOK))
		})

		It("refuses to create plugin-admin, shipped or not", func() {
			rec := request(http.MethodPost, "/rbac/roles", map[string]any{
				"metadata": map[string]any{"name": shipped.RolePluginAdmin, "namespace": namespace}, "spec": readRole,
			})
			Expect(rec.Code).To(Equal(http.StatusConflict), rec.Body.String())
			Expect(rec.Body.String()).To(ContainSubstring("reserved"))
		})

		It("refuses every write in the shipped namespace", func() {
			before, _ := stored()
			viewerBefore := storedRole(shipped.RoleViewer)

			conflicts := []*httptest.ResponseRecorder{
				request(http.MethodPut, "/rbac/roles/mission-control-builtin/viewer", map[string]any{"spec": readRole}),
				request(http.MethodDelete, "/rbac/roles/mission-control-builtin/admin", nil),
				request(http.MethodPut, "/rbac/scopes/mission-control-builtin/all-configs", map[string]any{"spec": configTargets}),
				request(http.MethodDelete, "/rbac/scopes/mission-control-builtin/all-configs", nil),
				request(http.MethodPost, "/rbac/scopes", map[string]any{
					"metadata": map[string]any{"name": "all-configs", "namespace": namespace}, "spec": configTargets,
				}),
				request(http.MethodPost, "/rbac/roles", map[string]any{
					"metadata": map[string]any{"name": "viewer", "namespace": namespace}, "spec": readRole,
				}),
				request(http.MethodPost, "/rbac/roles", map[string]any{
					"metadata": map[string]any{"name": "auditor", "namespace": namespace}, "spec": readRole,
				}),
			}
			for _, rec := range conflicts {
				Expect(rec.Code).To(Equal(http.StatusConflict), rec.Body.String())
				Expect(rec.Body.String()).To(ContainSubstring("shipped with Mission Control"))
			}

			after, _ := stored()
			Expect(after).To(HaveLen(len(before)))
			viewerAfter := storedRole(shipped.RoleViewer)
			Expect(viewerAfter.ID).To(Equal(viewerBefore.ID))
			Expect(viewerAfter.Rules).To(MatchJSON(viewerBefore.Rules))
		})
	})
})
