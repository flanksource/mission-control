package permissions_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"

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
	"github.com/flanksource/incident-commander/auth"
	"github.com/flanksource/incident-commander/db"
	dbModels "github.com/flanksource/incident-commander/db/models"
	"github.com/flanksource/incident-commander/plugin"
	pluginAPI "github.com/flanksource/incident-commander/plugin/api"
	mcRBAC "github.com/flanksource/incident-commander/rbac"
	"github.com/flanksource/incident-commander/rbac/adapter"
	"github.com/flanksource/incident-commander/rbac/shipped"
)

var _ = Describe("Shipped admins team", Ordered, func() {
	const namespace = adapter.ShippedNamespace

	var (
		adminLocal, member, outsider, teamAdmin *models.Person
		otherTeam                               *models.Team
		e                                       *echo.Echo
	)

	storedTeam := func() *models.Team {
		GinkgoHelper()
		var teams []models.Team
		Expect(DefaultContext.DB().Where("name = ? AND deleted_at IS NULL", shipped.TeamAdmins).Find(&teams).Error).To(Succeed())
		if len(teams) == 0 {
			return nil
		}
		Expect(teams).To(HaveLen(1))
		return &teams[0]
	}

	storedBinding := func() *models.RoleBinding {
		GinkgoHelper()
		var bindings []models.RoleBinding
		Expect(DefaultContext.DB().Where("namespace = ? AND name = ? AND deleted_at IS NULL", namespace, shipped.BindingAdmins).
			Find(&bindings).Error).To(Succeed())
		if len(bindings) == 0 {
			return nil
		}
		Expect(bindings).To(HaveLen(1))
		return &bindings[0]
	}

	isMember := func(person *models.Person) bool {
		GinkgoHelper()
		team := storedTeam()
		Expect(team).ToNot(BeNil())
		var count int64
		Expect(DefaultContext.DB().Model(&dbModels.TeamMember{}).
			Where("team_id = ? AND person_id = ?", team.ID, person.ID).Count(&count).Error).To(Succeed())
		return count > 0
	}

	canManagePeople := func(person *models.Person) bool {
		return rbac.Check(DefaultContext, person.ID.String(), policy.ObjectPeople, adapter.ActionPersonManage)
	}

	requestAs := func(person *models.Person, method, path, contentType string, body any) *httptest.ResponseRecorder {
		GinkgoHelper()
		var payload []byte
		switch body := body.(type) {
		case nil:
		case string:
			payload = []byte(body)
		default:
			var err error
			payload, err = json.Marshal(body)
			Expect(err).ToNot(HaveOccurred())
		}

		req := httptest.NewRequest(method, path, bytes.NewReader(payload))
		req.Header.Set(echo.HeaderContentType, contentType)
		req = req.WithContext(DefaultContext.WithUser(person))
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		return rec
	}

	request := func(method, path string, body any) *httptest.ResponseRecorder {
		GinkgoHelper()
		return requestAs(teamAdmin, method, path, echo.MIMEApplicationJSON, body)
	}

	BeforeAll(func() {
		Expect(mcRBAC.Init(DefaultContext)).To(Succeed())

		var err error
		adminLocal, err = auth.GetOrCreateAdminUser(DefaultContext)
		Expect(err).ToNot(HaveOccurred())

		member = setup.CreateUserWithRole(DefaultContext, "Admins Member", "admins-member@test.com")
		outsider = setup.CreateUserWithRole(DefaultContext, "Admins Outsider", "admins-outsider@test.com")
		teamAdmin = setup.CreateUserWithRole(DefaultContext, "Admins Team Admin", "admins-team-admin@test.com")

		otherTeam = &models.Team{Name: "not-shipped", CreatedBy: teamAdmin.ID}
		Expect(DefaultContext.DB().Create(otherTeam).Error).To(Succeed())

		// As on the first start: admin@local joins the team before the first Sync
		Expect(shipped.AddToAdmins(DefaultContext, adminLocal.ID)).To(Succeed())
		Expect(shipped.Sync(DefaultContext)).To(Succeed())
		Expect(shipped.AddToAdmins(DefaultContext, teamAdmin.ID)).To(Succeed())

		e = echo.New()
		mcRBAC.RegisterRoutes(e)
		e.Any("/db/*", func(c echo.Context) error { return c.NoContent(http.StatusOK) }, mcRBAC.DbMiddleware())
	})

	AfterAll(func() {
		Expect(DefaultContext.DB().Exec(`DELETE FROM team_members WHERE team_id IN (SELECT id FROM teams WHERE source = ? OR id = ?)`,
			adapter.SourceShipped, otherTeam.ID).Error).To(Succeed())
		Expect(DefaultContext.DB().Where("namespace = ?", namespace).Delete(&models.RoleBinding{}).Error).To(Succeed())
		Expect(DefaultContext.DB().Where("namespace = ?", namespace).Delete(&models.Role{}).Error).To(Succeed())
		Expect(DefaultContext.DB().Where("namespace = ?", namespace).Delete(&models.Scope{}).Error).To(Succeed())
		Expect(DefaultContext.DB().Where("source = ? OR id = ?", adapter.SourceShipped, otherTeam.ID).Delete(&models.Team{}).Error).To(Succeed())
		for _, p := range []*models.Person{member, outsider, teamAdmin} {
			Expect(DefaultContext.DB().Delete(p).Error).To(Succeed())
		}
		Expect(rbac.ReloadPolicy()).To(Succeed())
	})

	It("stores the team and its binding, Ready", func() {
		team := storedTeam()
		Expect(team).ToNot(BeNil())
		Expect(team.Source).To(Equal(adapter.SourceShipped))

		binding := storedBinding()
		Expect(binding).ToNot(BeNil())
		Expect(binding.Source).To(Equal(adapter.SourceShipped))
		Expect(binding.Role).To(Equal(shipped.RoleAdmin))
		Expect(binding.Error).To(BeNil(), "%v", binding.Error)
	})

	It("makes admin@local an admin through the binding alone", func() {
		Expect(isMember(adminLocal)).To(BeTrue())

		roles, err := rbac.RolesForUser(adminLocal.ID.String())
		Expect(err).ToNot(HaveOccurred())
		Expect(roles).ToNot(ContainElement(policy.RoleAdmin))

		Expect(canManagePeople(adminLocal)).To(BeTrue())
		Expect(canManagePeople(outsider)).To(BeFalse())
	})

	It("lets members be added and removed as usual", func() {
		team := storedTeam()
		Expect(request(http.MethodPost, "/db/team_members", map[string]any{"team_id": team.ID, "person_id": member.ID}).Code).To(Equal(http.StatusOK))
		Expect(request(http.MethodDelete, "/db/team_members?person_id=eq."+member.ID.String(), nil).Code).To(Equal(http.StatusOK))

		// As the team_members notification handler does
		Expect(db.AddPersonToTeam(DefaultContext, member.ID, team.ID)).To(Succeed())
		Expect(rbac.AddRoleForUser(member.ID.String(), team.ID.String())).To(Succeed())
		Expect(canManagePeople(member)).To(BeTrue())

		Expect(DefaultContext.DB().Where("team_id = ? AND person_id = ?", team.ID, member.ID).Delete(&dbModels.TeamMember{}).Error).To(Succeed())
		Expect(rbac.DeleteRoleForUser(member.ID.String(), team.ID.String())).To(Succeed())
		Expect(rbac.ReloadPolicy()).To(Succeed())
		Expect(canManagePeople(member)).To(BeFalse())
	})

	It("lets someone who's an admin only through the team manage teams and their members through /db", func() {
		team := storedTeam()
		Expect(db.AddPersonToTeam(DefaultContext, member.ID, team.ID)).To(Succeed())
		Expect(rbac.AddRoleForUser(member.ID.String(), team.ID.String())).To(Succeed())
		DeferCleanup(func() {
			Expect(DefaultContext.DB().Where("team_id = ? AND person_id = ?", team.ID, member.ID).Delete(&dbModels.TeamMember{}).Error).To(Succeed())
			Expect(rbac.DeleteRoleForUser(member.ID.String(), team.ID.String())).To(Succeed())
			Expect(rbac.ReloadPolicy()).To(Succeed())
		})
		roles, err := rbac.RolesForUser(member.ID.String())
		Expect(err).ToNot(HaveOccurred())
		Expect(roles).ToNot(ContainElement(policy.RoleAdmin))

		asMember := func(method, path string, body any) int {
			return requestAs(member, method, path, echo.MIMEApplicationJSON, body).Code
		}
		Expect(asMember(http.MethodGet, "/db/teams", nil)).To(Equal(http.StatusOK))
		Expect(asMember(http.MethodGet, "/db/team_members?team_id=eq."+team.ID.String(), nil)).To(Equal(http.StatusOK))
		Expect(asMember(http.MethodPost, "/db/team_members", map[string]any{"team_id": team.ID, "person_id": outsider.ID})).To(Equal(http.StatusOK))
		Expect(asMember(http.MethodDelete, "/db/team_members?person_id=eq."+outsider.ID.String(), nil)).To(Equal(http.StatusOK))
		Expect(asMember(http.MethodPatch, "/db/teams?id=eq."+otherTeam.ID.String(), map[string]any{"icon": "x"})).To(Equal(http.StatusOK))
		Expect(asMember(http.MethodDelete, "/db/teams?id=eq."+team.ID.String(), nil)).To(Equal(http.StatusConflict))

		asOutsider := func(method, path string, body any) int {
			return requestAs(outsider, method, path, echo.MIMEApplicationJSON, body).Code
		}
		Expect(asOutsider(http.MethodPost, "/db/team_members", map[string]any{"team_id": team.ID, "person_id": outsider.ID})).To(Equal(http.StatusForbidden))
		Expect(asOutsider(http.MethodPatch, "/db/teams?id=eq."+otherTeam.ID.String(), map[string]any{"icon": "x"})).To(Equal(http.StatusForbidden))
	})

	It("reserves the plugin-admins binding before any plugin has an operation", func() {
		binding := &v1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: shipped.BindingPluginAdmins, Namespace: namespace, UID: k8sTypes.UID(uuid.NewString())},
			Spec: v1.RoleBindingSpec{Role: shipped.RoleViewer, Subjects: v1.RoleBindingSubjects{
				PermissionGroupSubjects: v1.PermissionGroupSubjects{People: []string{outsider.Email}},
			}},
		}
		var notReady *kopper.NotReadyError
		Expect(errors.As(db.PersistRoleBindingFromCRD(DefaultContext, binding), &notReady)).To(BeTrue())
		Expect(notReady.Reason).To(Equal(db.ReasonShipped))

		rec := request(http.MethodPost, "/rbac/role-bindings", map[string]any{
			"metadata": map[string]any{"name": shipped.BindingPluginAdmins, "namespace": namespace},
			"spec":     map[string]any{"role": shipped.RoleViewer, "subjects": map[string]any{"people": []string{outsider.Email}}},
		})
		Expect(rec.Code).To(Equal(http.StatusConflict), rec.Body.String())
	})

	It("binds the team to plugin-admin while some plugin has an operation", func() {
		pluginBinding := func() *models.RoleBinding {
			GinkgoHelper()
			var bindings []models.RoleBinding
			Expect(DefaultContext.DB().Where("namespace = ? AND name = ? AND deleted_at IS NULL", namespace, shipped.BindingPluginAdmins).
				Find(&bindings).Error).To(Succeed())
			if len(bindings) == 0 {
				return nil
			}
			return &bindings[0]
		}
		Expect(pluginBinding()).To(BeNil())

		pluginID := uuid.New()
		_, err := plugin.DefaultRegistry.Upsert(pluginID, "default", "admins-plugin", v1.PluginSpec{Source: "test"})
		Expect(err).ToNot(HaveOccurred())
		Expect(plugin.DefaultRegistry.SetManifest(pluginID, &pluginAPI.PluginManifest{
			Name: "admins-plugin", Operations: []*pluginAPI.OperationDef{{Name: "run"}},
		})).To(Succeed())
		Expect(shipped.Sync(DefaultContext)).To(Succeed())

		binding := pluginBinding()
		Expect(binding).ToNot(BeNil())
		Expect(binding.Role).To(Equal(shipped.RolePluginAdmin))
		Expect(binding.Error).To(BeNil())
		config := models.ConfigItem{ID: uuid.New(), Name: lo.ToPtr("admins-config"), Type: lo.ToPtr("Admins::Test"), ConfigClass: "Test"}
		Expect(DefaultContext.DB().Create(&config).Error).To(Succeed())
		DeferCleanup(func() { Expect(DefaultContext.DB().Delete(&config).Error).To(Succeed()) })
		Expect(rbac.HasPermission(DefaultContext, adminLocal.ID.String(), &models.ABACAttribute{Config: config},
			policy.NewPluginInvokeAction("admins-plugin", "run"))).To(BeTrue())

		plugin.DefaultRegistry.Remove(pluginID)
		Expect(shipped.Sync(DefaultContext)).To(Succeed())
		Expect(pluginBinding()).To(BeNil())
	})

	It("refuses API changes and deletes of the team and the binding", func() {
		team := storedTeam()
		teamID := team.ID.String()

		for _, rec := range []*httptest.ResponseRecorder{
			request(http.MethodPut, "/rbac/role-bindings/mission-control-builtin/admins", map[string]any{
				"spec": map[string]any{"role": "viewer", "subjects": map[string]any{"teams": []string{"admins"}}},
			}),
			request(http.MethodDelete, "/rbac/role-bindings/mission-control-builtin/admins", nil),
			request(http.MethodDelete, "/db/teams?id=eq."+teamID, nil),
			request(http.MethodPatch, "/db/teams?id=eq."+teamID, map[string]any{"name": "renamed"}),
			request(http.MethodPatch, "/db/teams?id=in.("+otherTeam.ID.String()+","+teamID+")", map[string]any{"deleted_at": "now()"}),
			request(http.MethodPost, "/db/teams", []map[string]any{{"id": teamID, "name": "renamed"}}),
			request(http.MethodPost, "/db/teams", map[string]any{"name": shipped.TeamAdmins}),
		} {
			Expect(rec.Code).To(Equal(http.StatusConflict), rec.Body.String())
			Expect(rec.Body.String()).To(ContainSubstring("shipped with Mission Control"))
		}

		// Ids are compared as UUIDs, whatever their spelling, and inserts must be JSON
		for _, rec := range []*httptest.ResponseRecorder{
			request(http.MethodDelete, "/db/teams?id=eq."+strings.ToUpper(teamID), nil),
			request(http.MethodPatch, "/db/teams?id=in.(\""+strings.ToUpper(teamID)+"\")", map[string]any{"name": "renamed"}),
		} {
			Expect(rec.Code).To(Equal(http.StatusConflict), rec.Body.String())
		}
		csv := requestAs(teamAdmin, http.MethodPost, "/db/teams", "text/csv", "id,name\n"+teamID+",renamed\n")
		Expect(csv.Code).To(Equal(http.StatusBadRequest), csv.Body.String())
		Expect(request(http.MethodDelete, "/db/teams?id=eq.not-a-uuid", nil).Code).To(Equal(http.StatusBadRequest))

		Expect(request(http.MethodDelete, "/db/teams?name=eq.admins", nil).Code).To(Equal(http.StatusBadRequest))
		Expect(request(http.MethodPatch, "/db/teams?id=eq."+otherTeam.ID.String(), map[string]any{"icon": "x"}).Code).To(Equal(http.StatusOK))

		Expect(storedTeam().ID).To(Equal(team.ID))
		Expect(storedBinding().Role).To(Equal(shipped.RoleAdmin))
	})

	It("isn't overwritten by a Team or RoleBinding applied through Kubernetes", func() {
		var notReady *kopper.NotReadyError
		team := &v1.Team{ObjectMeta: metav1.ObjectMeta{Name: shipped.TeamAdmins, Namespace: "default", UID: k8sTypes.UID(uuid.NewString())}}
		Expect(errors.As(db.PersistTeamFromCRD(DefaultContext, team), &notReady)).To(BeTrue())
		Expect(notReady.Reason).To(Equal(db.ReasonShipped))
		Expect(db.DeleteStaleTeam(DefaultContext, &v1.Team{
			ObjectMeta: team.ObjectMeta, Spec: v1.TeamSpec{DisplayName: shipped.TeamAdmins},
		})).To(Succeed())

		binding := &v1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: shipped.BindingAdmins, Namespace: namespace, UID: k8sTypes.UID(uuid.NewString())},
			Spec: v1.RoleBindingSpec{Role: shipped.RoleViewer, Subjects: v1.RoleBindingSubjects{
				PermissionGroupSubjects: v1.PermissionGroupSubjects{Teams: []string{shipped.TeamAdmins}},
			}},
		}
		Expect(errors.As(db.PersistRoleBindingFromCRD(DefaultContext, binding), &notReady)).To(BeTrue())
		Expect(db.DeleteStaleRoleBinding(DefaultContext, binding)).ToNot(Succeed())

		Expect(storedTeam().Source).To(Equal(adapter.SourceShipped))
		Expect(storedBinding().Role).To(Equal(shipped.RoleAdmin))
	})

	It("restores the team, the binding and admin@local's membership when they're deleted", func() {
		team := storedTeam()
		Expect(DefaultContext.DB().Where("team_id = ?", team.ID).Delete(&dbModels.TeamMember{}).Error).To(Succeed())
		Expect(DefaultContext.DB().Delete(team).Error).To(Succeed())
		Expect(DefaultContext.DB().Where("namespace = ? AND name = ?", namespace, shipped.BindingAdmins).Delete(&models.RoleBinding{}).Error).To(Succeed())
		Expect(rbac.ReloadPolicy()).To(Succeed())
		Expect(storedTeam()).To(BeNil())

		// Without the binding, admin@local isn't an admin, even assigned the built-in admin role as earlier versions did
		Expect(rbac.AddRoleForUser(adminLocal.ID.String(), policy.RoleAdmin)).To(Succeed())
		DeferCleanup(func() { Expect(rbac.DeleteRoleForUser(adminLocal.ID.String(), policy.RoleAdmin)).To(Succeed()) })
		Expect(canManagePeople(adminLocal)).To(BeFalse())
		Expect(mcRBAC.CheckContext(DefaultContext.WithUser(adminLocal), policy.ObjectCatalog, policy.ActionRead)).To(BeFalse())
		Expect(mcRBAC.CheckContext(DefaultContext.WithUser(adminLocal), "*", "*")).To(BeFalse())

		// A restart: the auth middleware adds admin@local, and Sync runs
		Expect(shipped.AddToAdmins(DefaultContext, adminLocal.ID)).To(Succeed())
		Expect(shipped.Sync(DefaultContext)).To(Succeed())

		Expect(storedTeam().ID).To(Equal(team.ID))
		Expect(storedBinding()).ToNot(BeNil())
		Expect(storedBinding().Error).To(BeNil())
		Expect(isMember(adminLocal)).To(BeTrue())
		Expect(canManagePeople(adminLocal)).To(BeTrue())
	})

	It("restores a soft-deleted team and binding on Sync, keeping the team's members", func() {
		team := storedTeam()
		Expect(DefaultContext.DB().Model(&models.Team{}).Where("id = ?", team.ID).Update("deleted_at", "now()").Error).To(Succeed())
		Expect(DefaultContext.DB().Model(&models.RoleBinding{}).Where("namespace = ? AND name = ?", namespace, shipped.BindingAdmins).
			Update("deleted_at", "now()").Error).To(Succeed())

		Expect(shipped.Sync(DefaultContext)).To(Succeed())

		Expect(storedTeam().ID).To(Equal(team.ID))
		Expect(storedBinding()).ToNot(BeNil())
		Expect(isMember(adminLocal)).To(BeTrue())
		Expect(canManagePeople(adminLocal)).To(BeTrue())
	})
})
