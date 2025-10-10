package permissions_test

import (
	"os"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/flanksource/duty/rls"
	"github.com/flanksource/duty/tests/fixtures/dummy"
	"github.com/flanksource/duty/tests/setup"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/samber/lo"
	"sigs.k8s.io/yaml"

	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/auth"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/rbac/adapter"
)

var _ = Describe("Permissions", Ordered, ContinueOnFailure, func() {
	var (
		guestUser            *models.Person
		guestUserNoPerms     *models.Person
		guestUserDirectPerms *models.Person
		adminUser            *models.Person

		directPlaybookPermission  *models.Permission
		directCanaryPermission    *models.Permission
		directComponentPermission *models.Permission
		directConfigPermission    *models.Permission
	)

	BeforeAll(func() {
		err := rbac.Init(DefaultContext, []string{"admin"}, adapter.NewPermissionAdapter)
		Expect(err).ToNot(HaveOccurred())

		guestUser = setup.CreateUserWithRole(DefaultContext, "Guest User", "guest@test.com", policy.RoleGuest)
		guestUserNoPerms = setup.CreateUserWithRole(DefaultContext, "Guest User No Permissions", "guest-noperms@test.com", policy.RoleGuest)
		guestUserDirectPerms = setup.CreateUserWithRole(DefaultContext, "Guest User Direct Permissions", "guest-direct@test.com", policy.RoleGuest)
		adminUser = setup.CreateUserWithRole(DefaultContext, "Admin User", "admin@test.com", policy.RoleAdmin)

		// Load fixtures
		loadScopes()
		loadPermissions()

		// Create direct ID-based permissions for guest user with direct permissions
		// These test permissions that use specific resource IDs instead of object_selector
		directPlaybookPermission = &models.Permission{
			ID:          uuid.New(),
			Name:        "direct-playbook-permission",
			Namespace:   "default",
			Action:      policy.ActionRead,
			Subject:     guestUserDirectPerms.ID.String(),
			SubjectType: models.PermissionSubjectTypePerson,
			PlaybookID:  &dummy.EchoConfig.ID,
		}
		err = DefaultContext.DB().Create(directPlaybookPermission).Error
		Expect(err).ToNot(HaveOccurred())

		directCanaryPermission = &models.Permission{
			ID:          uuid.New(),
			Name:        "direct-canary-permission",
			Namespace:   "default",
			Action:      policy.ActionRead,
			Subject:     guestUserDirectPerms.ID.String(),
			SubjectType: models.PermissionSubjectTypePerson,
			CanaryID:    &dummy.LogisticsAPICanary.ID,
		}
		err = DefaultContext.DB().Create(directCanaryPermission).Error
		Expect(err).ToNot(HaveOccurred())

		directComponentPermission = &models.Permission{
			ID:          uuid.New(),
			Name:        "direct-component-permission",
			Namespace:   "default",
			Action:      policy.ActionRead,
			Subject:     guestUserDirectPerms.ID.String(),
			SubjectType: models.PermissionSubjectTypePerson,
			ComponentID: &dummy.Logistics.ID,
		}
		err = DefaultContext.DB().Create(directComponentPermission).Error
		Expect(err).ToNot(HaveOccurred())

		directConfigPermission = &models.Permission{
			ID:          uuid.New(),
			Name:        "direct-config-permission",
			Namespace:   "default",
			Action:      policy.ActionRead,
			Subject:     guestUser.ID.String(),
			SubjectType: models.PermissionSubjectTypePerson,
			ConfigID:    &dummy.NginxIngressPod.ID,
		}
		err = DefaultContext.DB().Create(directConfigPermission).Error
		Expect(err).ToNot(HaveOccurred())

		var permissions []models.Permission
		err = DefaultContext.DB().Where("deleted_at IS NULL").Find(&permissions).Error
		Expect(err).ToNot(HaveOccurred())

		// Initialize RBAC only after saving all the permissions.
		Expect(rbac.ReloadPolicy()).ToNot(HaveOccurred())
	})

	AfterAll(func() {
		// Clean up direct permissions
		permissions := []*models.Permission{directPlaybookPermission, directCanaryPermission, directComponentPermission, directConfigPermission}
		for _, perm := range permissions {
			if perm != nil {
				err := DefaultContext.DB().Delete(perm).Error
				Expect(err).ToNot(HaveOccurred())
			}
		}

		// Clean up users
		users := []*models.Person{guestUser, guestUserNoPerms, guestUserDirectPerms, adminUser}
		for _, user := range users {
			if user != nil {
				err := DefaultContext.DB().Delete(user).Error
				Expect(err).ToNot(HaveOccurred())
			}
		}

		Expect(rbac.ReloadPolicy()).ToNot(HaveOccurred())
	})

	Context("Permission to RLS translation", func() {
		It("should return RLS payload with config scope for guest user", func() {
			ctx := DefaultContext.WithUser(guestUser)

			payload, err := auth.GetRLSPayload(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(payload).ToNot(BeNil())

			Expect(payload.Disable).To(BeFalse(), "RLS should be enabled for guest users with scopes")
			Expect(payload.Config).To(HaveLen(4), "should have four config scopes including ID-based permission")
			Expect(payload.Config).To(ContainElements([]rls.Scope{
				{Tags: map[string]string{"namespace": "missioncontrol"}},
				{Tags: map[string]string{"namespace": "monitoring"}},
				{Tags: map[string]string{"namespace": "media"}},
				{ID: dummy.NginxIngressPod.ID.String()},
			}))

			// Playbook scopes should include echo-config and restart-pod
			Expect(payload.Playbook).To(HaveLen(2), "should have two playbook scopes")
			Expect(payload.Playbook).To(ContainElements([]rls.Scope{
				{Names: []string{"echo-config"}},
				{Names: []string{"restart-pod"}},
			}))

			// Other resource types should be empty
			Expect(payload.Component).To(BeEmpty(), "component scope should be empty")
			Expect(payload.Canary).To(BeEmpty(), "canary scope should be empty")
		})

		It("should disable RLS for non-guest users", func() {
			ctx := DefaultContext.WithUser(adminUser)

			payload, err := auth.GetRLSPayload(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(payload).ToNot(BeNil())

			// Admin users should have RLS disabled
			Expect(payload.Disable).To(BeTrue(), "RLS should be disabled for admin users")
		})

		It("should return empty RLS payload for guest user with no permissions", func() {
			ctx := DefaultContext.WithUser(guestUserNoPerms)

			payload, err := auth.GetRLSPayload(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(payload).ToNot(BeNil())

			// RLS should be enabled (not disabled) for guest users even without permissions
			Expect(payload.Disable).To(BeFalse(), "RLS should be enabled for guest users even without permissions")

			// All resource scopes should be empty since user has no permissions
			Expect(payload.Config).To(BeEmpty(), "config scope should be empty for guest user with no permissions")
			Expect(payload.Component).To(BeEmpty(), "component scope should be empty for guest user with no permissions")
			Expect(payload.Playbook).To(BeEmpty(), "playbook scope should be empty for guest user with no permissions")
			Expect(payload.Canary).To(BeEmpty(), "canary scope should be empty for guest user with no permissions")
		})

		It("should include direct ID-based permissions in RLS payload", func() {
			ctx := DefaultContext.WithUser(guestUserDirectPerms)

			payload, err := auth.GetRLSPayload(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(payload).ToNot(BeNil())

			Expect(payload.Disable).To(BeFalse(), "RLS should be enabled for guest users")

			// Verify playbook scope includes the direct playbook ID
			var hasPlaybookID bool
			for _, scope := range payload.Playbook {
				if scope.ID == dummy.EchoConfig.ID.String() {
					hasPlaybookID = true
					break
				}
			}
			Expect(hasPlaybookID).To(BeTrue(), "playbook scope should include direct playbook ID")

			// Verify canary scope includes the direct canary ID
			var hasCanaryID bool
			for _, scope := range payload.Canary {
				if scope.ID == dummy.LogisticsAPICanary.ID.String() {
					hasCanaryID = true
					break
				}
			}
			Expect(hasCanaryID).To(BeTrue(), "canary scope should include direct canary ID")

			// Verify component scope includes the direct component ID
			var hasComponentID bool
			for _, scope := range payload.Component {
				if scope.ID == dummy.Logistics.ID.String() {
					hasComponentID = true
					break
				}
			}
			Expect(hasComponentID).To(BeTrue(), "component scope should include direct component ID")
		})
	})

	Context("Database constraints", func() {
		It("should prevent creating permission with both object_selector and resource ID", func() {
			// This test verifies the database CHECK constraint that prevents the AND'ing bug
			// A permission cannot have BOTH object_selector AND a resource ID field set
			configID := dummy.NginxIngressPod.ID
			buggyPermission := &models.Permission{
				ID:             uuid.New(),
				Name:           "buggy-permission",
				Namespace:      "default",
				Action:         policy.ActionRead,
				Subject:        guestUser.ID.String(),
				SubjectType:    models.PermissionSubjectTypePerson,
				ObjectSelector: []byte(`{"configs":[{"tagSelector":"namespace=monitoring"}]}`),
				ConfigID:       &configID, // BOTH object_selector AND config_id - should fail!
			}

			err := DefaultContext.DB().Create(buggyPermission).Error
			Expect(err).To(HaveOccurred(), "should reject permission with both object_selector and config_id")
			Expect(err.Error()).To(ContainSubstring("permissions_selector_or_id_check"), "error should mention the check constraint")
		})
	})

	Context("Permission to Casbin policy translation", func() {
		DescribeTable("guest user with some permissions",
			func(attr models.ABACAttribute, action string, expectedAllowed bool, description string) {
				allowed := rbac.HasPermission(DefaultContext, guestUser.ID.String(), &attr, action)
				Expect(allowed).To(Equal(expectedAllowed), description)
			},
			// Config read access - allowed via scopes
			Entry("should allow read access to missioncontrol namespace config via scope",
				models.ABACAttribute{Config: models.ConfigItem{ID: uuid.New(), Tags: map[string]string{"namespace": "missioncontrol"}}},
				policy.ActionRead, true,
				"guest user should have read access to missioncontrol namespace configs via scope"),
			Entry("should allow read access to monitoring namespace config via scope",
				models.ABACAttribute{Config: models.ConfigItem{ID: uuid.New(), Tags: map[string]string{"namespace": "monitoring"}}},
				policy.ActionRead, true,
				"guest user should have read access to monitoring namespace configs via scope"),
			Entry("should allow read access to media namespace config via direct permission",
				models.ABACAttribute{Config: models.ConfigItem{ID: uuid.New(), Tags: map[string]string{"namespace": "media"}}},
				policy.ActionRead, true,
				"guest user should have read access to media namespace configs via direct permission"),

			// Config read access by ID - tests for potential AND'ing bug with scope-based permissions
			Entry("should allow read access to specific config by ID",
				models.ABACAttribute{Config: models.ConfigItem{ID: dummy.NginxIngressPod.ID, Tags: map[string]string{"namespace": "production"}}},
				policy.ActionRead, true,
				"guest user should have read access to specific config by ID"),
			Entry("should still allow read access to monitoring configs after adding ID-based permission",
				models.ABACAttribute{Config: models.ConfigItem{ID: uuid.New(), Tags: map[string]string{"namespace": "monitoring"}}},
				policy.ActionRead, true,
				"guest user should still have read access to monitoring configs (verifies permissions aren't AND'ed)"),

			// Config read access - denied (not in scopes or permissions)
			Entry("should deny read access to kube-system namespace config",
				models.ABACAttribute{Config: models.ConfigItem{ID: uuid.New(), Tags: map[string]string{"namespace": "kube-system"}}},
				policy.ActionRead, false,
				"guest user should NOT have read access to kube-system namespace configs"),
			Entry("should deny read access to configs with no namespace tag",
				models.ABACAttribute{Config: models.ConfigItem{ID: uuid.New(), Name: lo.ToPtr("notag")}},
				policy.ActionRead, false,
				"guest user should NOT have read access to configs with no namespace tag"),

			// Playbook read access - allowed via direct permission for echo-config
			Entry("should allow read access to echo-config playbook via direct permission",
				models.ABACAttribute{Playbook: models.Playbook{ID: dummy.EchoConfig.ID, Name: "echo-config"}},
				policy.ActionRead, true,
				"guest user should have read access to echo-config playbook via direct permission"),

			// Playbook read access - allowed via scope permission for restart-pod
			Entry("should allow read access to restart-pod playbook via scope",
				models.ABACAttribute{Playbook: models.Playbook{ID: dummy.RestartPod.ID, Name: "restart-pod"}},
				policy.ActionRead, true,
				"guest user should have read access to restart-pod playbook via scope"),

			// Playbook read access - denied for other playbooks
			Entry("should deny read access to other playbooks",
				models.ABACAttribute{Playbook: models.Playbook{ID: uuid.New(), Name: "other-playbook"}},
				policy.ActionRead, false,
				"guest user should NOT have read access to other playbooks"),

			// Playbook run access - allowed for echo-config on missioncontrol configs via direct permission
			Entry("should allow playbook:run on echo-config for missioncontrol configs",
				models.ABACAttribute{
					Playbook: models.Playbook{ID: dummy.EchoConfig.ID, Name: "echo-config"},
					Config:   models.ConfigItem{ID: uuid.New(), Tags: map[string]string{"namespace": "missioncontrol"}},
				},
				policy.ActionPlaybookRun, true,
				"guest user should have playbook:run access to echo-config on missioncontrol configs"),

			// Playbook run access - denied for other playbooks
			Entry("should deny playbook:run on echo-config for monitoring configs",
				models.ABACAttribute{
					Playbook: models.Playbook{ID: dummy.EchoConfig.ID, Name: "echo-config"},
					Config:   models.ConfigItem{ID: uuid.New(), Tags: map[string]string{"namespace": "monitoring"}},
				},
				policy.ActionPlaybookRun, false,
				"guest user should have playbook:run access to echo-config on missioncontrol configs"),
			Entry("should deny playbook:run on restart-pod (no run permission)",
				models.ABACAttribute{Playbook: models.Playbook{ID: dummy.RestartPod.ID, Name: "restart-pod"}},
				policy.ActionPlaybookRun, false,
				"guest user should NOT have playbook:run access to restart-pod (only read)"),
			Entry("should deny playbook:run on restart-pod (no run permission)",
				models.ABACAttribute{
					Playbook: models.Playbook{ID: dummy.RestartPod.ID, Name: "restart-pod"},
					Config:   models.ConfigItem{ID: uuid.New(), Tags: map[string]string{"namespace": "monitoring"}},
				},
				policy.ActionPlaybookRun, false,
				"guest user should NOT have playbook:run access to restart-pod (only read)"),
			Entry("should deny playbook:run on other playbooks",
				models.ABACAttribute{Playbook: models.Playbook{ID: uuid.New(), Name: "other-playbook"}},
				policy.ActionPlaybookRun, false,
				"guest user should NOT have playbook:run access to other playbooks"),
		)

		DescribeTable("guest user with no permissions at all",
			func(attr models.ABACAttribute, action string, description string) {
				allowed := rbac.HasPermission(DefaultContext, guestUserNoPerms.ID.String(), &attr, action)
				Expect(allowed).To(BeFalse(), description)
			},
			Entry("should deny read access to missioncontrol namespace config",
				models.ABACAttribute{Config: models.ConfigItem{ID: uuid.New(), Tags: map[string]string{"namespace": "missioncontrol"}}},
				policy.ActionRead,
				"guest user with no permissions should NOT have read access to any configs"),
			Entry("should deny read access to monitoring namespace config",
				models.ABACAttribute{Config: models.ConfigItem{ID: uuid.New(), Tags: map[string]string{"namespace": "monitoring"}}},
				policy.ActionRead,
				"guest user with no permissions should NOT have read access to any configs"),
			Entry("should deny read access to media namespace config",
				models.ABACAttribute{Config: models.ConfigItem{ID: uuid.New(), Tags: map[string]string{"namespace": "media"}}},
				policy.ActionRead,
				"guest user with no permissions should NOT have read access to any configs"),
			Entry("should deny read access to playbooks",
				models.ABACAttribute{Playbook: models.Playbook{ID: dummy.EchoConfig.ID, Name: "echo-config"}},
				policy.ActionRead,
				"guest user with no permissions should NOT have read access to playbooks"),
			Entry("should deny playbook:run access",
				models.ABACAttribute{Playbook: models.Playbook{ID: dummy.RestartPod.ID, Name: "restart-pod"}},
				policy.ActionPlaybookRun,
				"guest user with no permissions should NOT have playbook:run access"),
			Entry("should deny read access to other playbooks",
				models.ABACAttribute{Playbook: models.Playbook{ID: uuid.New(), Name: "other-playbook"}},
				policy.ActionRead,
				"guest user should NOT have read access to other playbooks"),
		)

		DescribeTable("admin user must have access to everything",
			func(attr models.ABACAttribute, action string, description string) {
				allowed := rbac.HasPermission(DefaultContext, adminUser.ID.String(), &attr, action)
				Expect(allowed).To(BeTrue(), description)
			},
			// Admin should have access to everything
			Entry("should allow read access to missioncontrol namespace config",
				models.ABACAttribute{Config: models.ConfigItem{ID: uuid.New(), Tags: map[string]string{"namespace": "missioncontrol"}}},
				policy.ActionRead,
				"admin user should have read access to all configs"),
			Entry("should allow read access to monitoring namespace config",
				models.ABACAttribute{Config: models.ConfigItem{ID: uuid.New(), Tags: map[string]string{"namespace": "monitoring"}}},
				policy.ActionRead,
				"admin user should have read access to all configs"),
			Entry("should allow read access to kube-system namespace config",
				models.ABACAttribute{Config: models.ConfigItem{ID: uuid.New(), Tags: map[string]string{"namespace": "kube-system"}}},
				policy.ActionRead,
				"admin user should have read access to all configs"),
			Entry("should allow read access to configs with no namespace tag",
				models.ABACAttribute{Config: models.ConfigItem{ID: uuid.New(), Name: lo.ToPtr("notag")}},
				policy.ActionRead,
				"admin user should have read access to all configs"),
			Entry("should allow read access to playbooks",
				models.ABACAttribute{Playbook: models.Playbook{ID: uuid.New()}},
				policy.ActionRead,
				"admin user should have read access to playbooks"),
			Entry("should allow playbook:run access",
				models.ABACAttribute{Playbook: models.Playbook{ID: uuid.New()}},
				policy.ActionPlaybookRun,
				"admin user should have playbook:run access"),
		)
	})
})

func loadScopes() {
	scopeFiles, err := os.ReadDir("testdata/scopes")
	Expect(err).ToNot(HaveOccurred())

	for _, file := range scopeFiles {
		if file.IsDir() {
			continue
		}

		scopeBytes, err := os.ReadFile("testdata/scopes/" + file.Name())
		Expect(err).ToNot(HaveOccurred())

		var scope v1.Scope
		err = yaml.Unmarshal(scopeBytes, &scope)
		Expect(err).ToNot(HaveOccurred())

		err = db.PersistScopeFromCRD(DefaultContext, &scope)
		Expect(err).ToNot(HaveOccurred())
	}
}

func loadPermissions() {
	permFiles, err := os.ReadDir("testdata/permissions")
	Expect(err).ToNot(HaveOccurred())

	for _, file := range permFiles {
		if file.IsDir() {
			continue
		}

		permBytes, err := os.ReadFile("testdata/permissions/" + file.Name())
		Expect(err).ToNot(HaveOccurred())

		var permission v1.Permission
		err = yaml.Unmarshal(permBytes, &permission)
		Expect(err).ToNot(HaveOccurred())

		err = db.PersistPermissionFromCRD(DefaultContext, &permission)
		Expect(err).ToNot(HaveOccurred())
	}
}
