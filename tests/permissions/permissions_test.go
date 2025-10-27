package permissions_test

import (
	"database/sql"
	"os"
	"time"

	"github.com/flanksource/duty/models"
	"github.com/flanksource/duty/rbac"
	"github.com/flanksource/duty/rbac/policy"
	"github.com/flanksource/duty/rls"
	"github.com/flanksource/duty/tests/fixtures/dummy"
	"github.com/flanksource/duty/tests/setup"
	"github.com/flanksource/duty/types"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/patrickmn/go-cache"
	"github.com/samber/lo"
	"gorm.io/gorm"
	"sigs.k8s.io/yaml"

	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/auth"
	"github.com/flanksource/incident-commander/db"
	"github.com/flanksource/incident-commander/rbac/adapter"
)

var _ = Describe("Permissions", Ordered, ContinueOnFailure, func() {
	var (
		guestUser             *models.Person
		guestUserNoPerms      *models.Person
		guestUserDirectPerms  *models.Person
		guestUserMultiTarget  *models.Person
		homelabManager        *models.Person
		wildcardManager       *models.Person
		adminUser             *models.Person
		multiScopeUser        *models.Person
		homelabDefaultManager *models.Person
		userMetrics           *models.Person
	)

	var directPermissions []*models.Permission

	BeforeAll(func() {
		err := rbac.Init(DefaultContext, []string{"admin"}, adapter.NewPermissionAdapter)
		Expect(err).ToNot(HaveOccurred())

		guestUser = setup.CreateUserWithRole(DefaultContext, "Guest User", "guest@test.com", policy.RoleGuest)
		guestUserNoPerms = setup.CreateUserWithRole(DefaultContext, "Guest User No Permissions", "guest-noperms@test.com", policy.RoleGuest)
		guestUserDirectPerms = setup.CreateUserWithRole(DefaultContext, "Guest User Direct Permissions", "guest-direct@test.com", policy.RoleGuest)
		guestUserMultiTarget = setup.CreateUserWithRole(DefaultContext, "Guest User Multi Target", "guest-multi@test.com", policy.RoleGuest)
		homelabManager = setup.CreateUserWithRole(DefaultContext, "Homelab Manager", "manager@homelab.com", policy.RoleGuest)
		homelabDefaultManager = setup.CreateUserWithRole(DefaultContext, "Homelab Default Manager", "homelab-default@manager.com", policy.RoleGuest)
		multiScopeUser = setup.CreateUserWithRole(DefaultContext, "Multi Scope User", "multi-scope@test.com", policy.RoleGuest)
		wildcardManager = setup.CreateUserWithRole(DefaultContext, "Wildcard Manager", "wildcard@manager.com", policy.RoleGuest)
		userMetrics = setup.CreateUserWithRole(DefaultContext, "User Metrics", "user-metrics@test.com", policy.RoleGuest)
		adminUser = setup.CreateUserWithRole(DefaultContext, "Admin User", "admin@test.com", policy.RoleAdmin)

		// Load fixtures
		loadScopes()
		loadPermissions()

		// These are permissions that cannot be created via the CRD as they refer to the resource directly via the ID.
		// That's why they are created here in the test setup.
		directPermissions = createDirectPermissions(guestUserDirectPerms.ID.String())

		var permissions []models.Permission
		err = DefaultContext.DB().Where("deleted_at IS NULL").Find(&permissions).Error
		Expect(err).ToNot(HaveOccurred())

		// Initialize RBAC only after saving all the permissions.
		Expect(rbac.ReloadPolicy()).ToNot(HaveOccurred())
	})

	AfterAll(func() {
		// Clean up direct permissions
		for _, perm := range directPermissions {
			if perm != nil {
				err := DefaultContext.DB().Delete(perm).Error
				Expect(err).ToNot(HaveOccurred())
			}
		}

		// Clean up users
		users := []*models.Person{guestUser, guestUserNoPerms, guestUserDirectPerms, guestUserMultiTarget, homelabManager, homelabDefaultManager, multiScopeUser, wildcardManager, userMetrics, adminUser}
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
			Expect(payload.Config).To(HaveLen(3), "should have three config scopes")
			Expect(payload.Config).To(ContainElements([]rls.Scope{
				{Tags: map[string]string{"namespace": "missioncontrol"}},
				{Tags: map[string]string{"namespace": "monitoring"}},
				{Tags: map[string]string{"namespace": "media"}},
			}))

			// Playbook scopes should include echo-config and restart-pod
			Expect(payload.Playbook).To(HaveLen(2), "should have two playbook scopes")
			Expect(payload.Playbook).To(ContainElements([]rls.Scope{
				{Names: []string{"echo-config"}},
				{Names: []string{"restart-pod"}},
			}))

			// View scopes should be included if user has view permissions
			Expect(payload.View).To(HaveLen(1), "should have one view scope for pods view")
			Expect(payload.View).To(ContainElement(rls.Scope{
				Names: []string{"pods"},
			}))

			// Other resource types should be empty
			Expect(payload.Component).To(BeEmpty(), "component scope should be empty")
			Expect(payload.Canary).To(BeEmpty(), "canary scope should be empty")
		})

		It("should return RLS payload for guest user with multi-target scope", func() {
			ctx := DefaultContext.WithUser(guestUserMultiTarget)

			payload, err := auth.GetRLSPayload(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(payload).ToNot(BeNil())

			// Verify exact match of entire payload - user should have access to ONLY these resources
			expectedPayload := &rls.Payload{
				Disable: false,
				Config: []rls.Scope{
					{Tags: map[string]string{"namespace": "database"}},
				},
				Playbook: []rls.Scope{
					{Names: []string{"echo-config"}},
				},
				View: []rls.Scope{
					{Names: []string{"metrics"}},
				},
				Component: nil,
				Canary:    nil,
			}

			Expect(payload).To(Equal(expectedPayload), "RLS payload should match exactly - user should only see database configs, echo-config playbook, and metrics view")
		})

		It("should return RLS payload for guest user with agent-based scope", func() {
			ctx := DefaultContext.WithUser(homelabManager)

			payload, err := auth.GetRLSPayload(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(payload).ToNot(BeNil())

			// Verify exact match of entire payload - user should have access to ONLY homelab agent configs
			expectedPayload := &rls.Payload{
				Disable: false,
				Config: []rls.Scope{
					{Agents: []string{dummy.HomelabAgent.ID.String()}},
				},
				Playbook:  nil,
				Component: nil,
				Canary:    nil,
			}

			Expect(payload).To(Equal(expectedPayload), "RLS payload should match exactly - user should only see homelab agent configs")
		})

		It("should return RLS payload for wildcard manager with full wildcard scope", func() {
			ctx := DefaultContext.WithUser(wildcardManager)

			payload, err := auth.GetRLSPayload(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(payload).ToNot(BeNil())

			// Verify exact match of entire payload - user should have access to ALL configs via "*"
			expectedPayload := &rls.Payload{
				Disable: false,
				Config: []rls.Scope{
					{Names: []string{"*"}},
				},
				Playbook:  nil,
				Component: nil,
				Canary:    nil,
			}

			Expect(payload).To(Equal(expectedPayload), "RLS payload should match exactly - user should see all configs via wildcard '*'")
		})

		It("should return RLS payload for homelab default manager with combined agent+tag scope", func() {
			ctx := DefaultContext.WithUser(homelabDefaultManager)

			payload, err := auth.GetRLSPayload(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(payload).ToNot(BeNil())

			// Verify exact match of entire payload - user should have access to homelab agent configs in default namespace
			expectedPayload := &rls.Payload{
				Disable: false,
				Config: []rls.Scope{
					{
						Agents: []string{dummy.HomelabAgent.ID.String()},
						Tags:   map[string]string{"namespace": "default"},
					},
				},
				Playbook:  nil,
				Component: nil,
				Canary:    nil,
			}

			Expect(payload).To(Equal(expectedPayload), "RLS payload should match exactly - user should see homelab agent configs in default namespace")
		})

		It("should return RLS payload for multi-scope user with multiple scopes (OR behavior)", func() {
			ctx := DefaultContext.WithUser(multiScopeUser)

			payload, err := auth.GetRLSPayload(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(payload).ToNot(BeNil())

			// Verify the payload contains all three scopes (not merged/AND'ed)
			Expect(payload.Disable).To(BeFalse(), "RLS should be enabled for guest users")
			Expect(payload.Config).To(HaveLen(3), "should have three separate config scopes")

			// Verify all three scopes are present
			Expect(payload.Config).To(ContainElements([]rls.Scope{
				{Tags: map[string]string{"namespace": "missioncontrol"}},
				{Tags: map[string]string{"namespace": "monitoring"}},
				{Agents: []string{dummy.HomelabAgent.ID.String()}},
			}))

			// Other resource types should be empty
			Expect(payload.Playbook).To(BeEmpty(), "playbook scope should be empty")
			Expect(payload.Component).To(BeEmpty(), "component scope should be empty")
			Expect(payload.Canary).To(BeEmpty(), "canary scope should be empty")
			Expect(payload.View).To(BeEmpty(), "view scope should be empty")
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
			Expect(payload.View).To(BeEmpty(), "view scope should be empty for guest user with no permissions")
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

		It("should return RLS payload for user with metrics view permission", func() {
			ctx := DefaultContext.WithUser(userMetrics)

			payload, err := auth.GetRLSPayload(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(payload).ToNot(BeNil())

			// Verify exact match of entire payload - user should have access to ONLY metrics view
			expectedPayload := &rls.Payload{
				Disable:   false,
				Config:    nil,
				Playbook:  nil,
				Component: nil,
				Canary:    nil,
				View: []rls.Scope{
					{Names: []string{"metrics"}},
				},
			}

			Expect(payload).To(Equal(expectedPayload), "RLS payload should match exactly - user should only see metrics view")
		})
	})

	Context("Scope expansion", func() {
		It("should expand multi-target scope into permission object_selector", func() {
			var permission models.Permission
			err := DefaultContext.DB().
				Where("subject = ? AND deleted_at IS NULL", guestUserMultiTarget.ID.String()).
				Where("object_selector IS NOT NULL").
				First(&permission).Error
			Expect(err).ToNot(HaveOccurred(), "should find the multi-target scope permission")

			// Expand the permission
			expandedPerm, err := adapter.ExpandPermissionScopes(DefaultContext, cache.New(time.Minute, time.Minute), permission)
			Expect(err).ToNot(HaveOccurred(), "scope expansion should succeed")

			Expect(expandedPerm).To(HaveLen(3))
			Expect(expandedPerm).To(ContainElements(
				v1.PermissionObject{
					Selectors: rbac.Selectors{
						Configs: []types.ResourceSelector{{TagSelector: "namespace=database"}},
					},
				},
				v1.PermissionObject{
					Selectors: rbac.Selectors{
						Playbooks: []types.ResourceSelector{{Name: "echo-config"}},
					},
				},
				v1.PermissionObject{
					Selectors: rbac.Selectors{
						Views: []rbac.ViewRef{{Name: "metrics", Namespace: "mc"}},
					},
				},
			))
		})

		It("should expand combined agent+tag scope into permission object_selector", func() {
			var permission models.Permission
			err := DefaultContext.DB().
				Where("subject = ? AND deleted_at IS NULL", homelabDefaultManager.ID.String()).
				Where("object_selector IS NOT NULL").
				First(&permission).Error
			Expect(err).ToNot(HaveOccurred(), "should find the combined agent+tag scope permission")

			// Expand the permission
			expandedPerm, err := adapter.ExpandPermissionScopes(DefaultContext, cache.New(time.Minute, time.Minute), permission)
			Expect(err).ToNot(HaveOccurred(), "scope expansion should succeed")

			Expect(expandedPerm).To(HaveLen(1))
			Expect(expandedPerm).To(Equal([]v1.PermissionObject{
				{
					Selectors: rbac.Selectors{
						Configs: []types.ResourceSelector{{
							Agent:       dummy.HomelabAgent.ID.String(),
							TagSelector: "namespace=default",
						}},
					},
				},
			}))
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

		DescribeTable("guest user with multi-target scope",
			func(attr models.ABACAttribute, action string, expectedAllowed bool, description string) {
				allowed := rbac.HasPermission(DefaultContext, guestUserMultiTarget.ID.String(), &attr, action)
				Expect(allowed).To(Equal(expectedAllowed), description)
			},
			// Config read access - allowed only for database namespace
			Entry("should allow read access to database namespace config via multi-target scope",
				models.ABACAttribute{Config: models.ConfigItem{ID: uuid.New(), Tags: map[string]string{"namespace": "database"}}},
				policy.ActionRead, true,
				"guest user with multi-target scope should have read access to database namespace configs"),
			Entry("should deny read access to monitoring namespace config",
				models.ABACAttribute{Config: models.ConfigItem{ID: uuid.New(), Tags: map[string]string{"namespace": "monitoring"}}},
				policy.ActionRead, false,
				"guest user with multi-target scope should NOT have read access to monitoring namespace configs"),
			Entry("should deny read access to missioncontrol namespace config",
				models.ABACAttribute{Config: models.ConfigItem{ID: uuid.New(), Tags: map[string]string{"namespace": "missioncontrol"}}},
				policy.ActionRead, false,
				"guest user with multi-target scope should NOT have read access to missioncontrol namespace configs"),
			Entry("should deny read access to other namespace configs",
				models.ABACAttribute{Config: models.ConfigItem{ID: uuid.New(), Tags: map[string]string{"namespace": "kube-system"}}},
				policy.ActionRead, false,
				"guest user with multi-target scope should NOT have read access to kube-system namespace configs"),

			// Playbook read access - allowed only for echo-config
			Entry("should allow read access to echo-config playbook via multi-target scope",
				models.ABACAttribute{Playbook: models.Playbook{ID: dummy.EchoConfig.ID, Name: "echo-config"}},
				policy.ActionRead, true,
				"guest user with multi-target scope should have read access to echo-config playbook"),
			Entry("should deny read access to restart-pod playbook",
				models.ABACAttribute{Playbook: models.Playbook{ID: dummy.RestartPod.ID, Name: "restart-pod"}},
				policy.ActionRead, false,
				"guest user with multi-target scope should NOT have read access to restart-pod playbook"),
			Entry("should deny read access to other playbooks",
				models.ABACAttribute{Playbook: models.Playbook{ID: uuid.New(), Name: "other-playbook"}},
				policy.ActionRead, false,
				"guest user with multi-target scope should NOT have read access to other playbooks"),

			// Playbook run access - should be denied (only read permission granted)
			Entry("should deny playbook:run on echo-config",
				models.ABACAttribute{Playbook: models.Playbook{ID: dummy.EchoConfig.ID, Name: "echo-config"}},
				policy.ActionPlaybookRun, false,
				"guest user with multi-target scope should NOT have playbook:run access (only read)"),
		)

		DescribeTable("guest user with agent-based scope",
			func(attr models.ABACAttribute, action string, expectedAllowed bool, description string) {
				allowed := rbac.HasPermission(DefaultContext, homelabManager.ID.String(), &attr, action)
				Expect(allowed).To(Equal(expectedAllowed), description)
			},
			// Config read access - allowed only for homelab agent configs
			Entry("should allow read access to config with homelab agent",
				models.ABACAttribute{Config: models.ConfigItem{ID: uuid.New(), AgentID: dummy.HomelabAgent.ID}},
				policy.ActionRead, true,
				"guest user with agent scope should have read access to homelab agent configs"),
			Entry("should deny read access to config with GCP agent",
				models.ABACAttribute{Config: models.ConfigItem{ID: uuid.New(), AgentID: dummy.GCPAgent.ID}},
				policy.ActionRead, false,
				"guest user with agent scope should NOT have read access to GCP agent configs"),
			Entry("should deny read access to local agent's config",
				models.ABACAttribute{Config: models.ConfigItem{ID: uuid.New(), AgentID: uuid.Nil}},
				policy.ActionRead, false,
				"guest user with agent scope should NOT have read access to local agent's config"),
			Entry("should deny read access to config with no agent",
				models.ABACAttribute{Config: models.ConfigItem{ID: uuid.New(), Tags: map[string]string{"namespace": "default"}}},
				policy.ActionRead, false,
				"guest user with agent scope should NOT have read access to configs without an agent"),
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

		DescribeTable("wildcard manager with full wildcard scope",
			func(attr models.ABACAttribute, action string, expectedAllowed bool, description string) {
				allowed := rbac.HasPermission(DefaultContext, wildcardManager.ID.String(), &attr, action)
				Expect(allowed).To(Equal(expectedAllowed), description)
			},
			// Config read access - allowed for ALL configs via "*" wildcard
			Entry("should allow read access to nginx-ingress config via wildcard scope",
				models.ABACAttribute{Config: models.ConfigItem{ID: uuid.New(), Name: lo.ToPtr("nginx-ingress")}},
				policy.ActionRead, true,
				"wildcard manager should have read access to nginx-ingress config via wildcard scope"),
			Entry("should allow read access to nginx-ingress-controller config via wildcard scope",
				models.ABACAttribute{Config: models.ConfigItem{ID: uuid.New(), Name: lo.ToPtr("nginx-ingress-controller-7d9b8f6c4-xplmn")}},
				policy.ActionRead, true,
				"wildcard manager should have read access to nginx-ingress-controller config via wildcard scope"),
			Entry("should allow read access to redis config via wildcard scope",
				models.ABACAttribute{Config: models.ConfigItem{ID: uuid.New(), Name: lo.ToPtr("redis")}},
				policy.ActionRead, true,
				"wildcard manager should have read access to redis config via wildcard scope"),
			Entry("should allow read access to any config via wildcard scope",
				models.ABACAttribute{Config: models.ConfigItem{ID: uuid.New(), Name: lo.ToPtr("other-config")}},
				policy.ActionRead, true,
				"wildcard manager should have read access to any config via wildcard scope"),
			Entry("should allow read access to configs in any namespace via wildcard scope",
				models.ABACAttribute{Config: models.ConfigItem{ID: uuid.New(), Tags: map[string]string{"namespace": "monitoring"}}},
				policy.ActionRead, true,
				"wildcard manager should have read access to configs in any namespace via wildcard scope"),
		)

		DescribeTable("homelab default manager with combined agent+tag scope",
			func(attr models.ABACAttribute, action string, expectedAllowed bool, description string) {
				allowed := rbac.HasPermission(DefaultContext, homelabDefaultManager.ID.String(), &attr, action)
				Expect(allowed).To(Equal(expectedAllowed), description)
			},
			// Config read access - allowed ONLY for homelab agent configs in default namespace (AND condition)
			Entry("should allow read access to config with homelab agent AND default namespace",
				models.ABACAttribute{Config: models.ConfigItem{ID: uuid.New(), AgentID: dummy.HomelabAgent.ID, Tags: map[string]string{"namespace": "default"}}},
				policy.ActionRead, true,
				"homelab default manager should have read access to homelab agent configs in default namespace"),
			Entry("should deny read access to config with homelab agent but production namespace",
				models.ABACAttribute{Config: models.ConfigItem{ID: uuid.New(), AgentID: dummy.HomelabAgent.ID, Tags: map[string]string{"namespace": "production"}}},
				policy.ActionRead, false,
				"homelab default manager should NOT have read access to homelab agent configs in production namespace"),
			Entry("should deny read access to config with GCP agent but default namespace",
				models.ABACAttribute{Config: models.ConfigItem{ID: uuid.New(), AgentID: dummy.GCPAgent.ID, Tags: map[string]string{"namespace": "default"}}},
				policy.ActionRead, false,
				"homelab default manager should NOT have read access to GCP agent configs even in default namespace"),
			Entry("should deny read access to config with no agent",
				models.ABACAttribute{Config: models.ConfigItem{ID: uuid.New(), Tags: map[string]string{"namespace": "default"}}},
				policy.ActionRead, false,
				"homelab default manager should NOT have read access to configs without agent"),
		)

		DescribeTable("multi-scope user with multiple scopes (OR behavior)",
			func(attr models.ABACAttribute, action string, expectedAllowed bool, description string) {
				allowed := rbac.HasPermission(DefaultContext, multiScopeUser.ID.String(), &attr, action)
				Expect(allowed).To(Equal(expectedAllowed), description)
			},
			// Config read access - allowed for ANY of the three scopes (OR condition)
			Entry("should allow read access to missioncontrol namespace config",
				models.ABACAttribute{Config: models.ConfigItem{ID: uuid.New(), Tags: map[string]string{"namespace": "missioncontrol"}}},
				policy.ActionRead, true,
				"multi-scope user should have read access to missioncontrol namespace configs"),
			Entry("should allow read access to monitoring namespace config",
				models.ABACAttribute{Config: models.ConfigItem{ID: uuid.New(), Tags: map[string]string{"namespace": "monitoring"}}},
				policy.ActionRead, true,
				"multi-scope user should have read access to monitoring namespace configs"),
			Entry("should allow read access to homelab agent config (any namespace)",
				models.ABACAttribute{Config: models.ConfigItem{ID: uuid.New(), AgentID: dummy.HomelabAgent.ID}},
				policy.ActionRead, true,
				"multi-scope user should have read access to homelab agent configs"),
			Entry("should allow read access to homelab agent config in production namespace",
				models.ABACAttribute{Config: models.ConfigItem{ID: uuid.New(), AgentID: dummy.HomelabAgent.ID, Tags: map[string]string{"namespace": "production"}}},
				policy.ActionRead, true,
				"multi-scope user should have read access to homelab agent configs in any namespace"),
			Entry("should deny read access to database namespace config without homelab agent",
				models.ABACAttribute{Config: models.ConfigItem{ID: uuid.New(), Tags: map[string]string{"namespace": "database"}}},
				policy.ActionRead, false,
				"multi-scope user should NOT have read access to database namespace configs"),
			Entry("should deny read access to GCP agent config",
				models.ABACAttribute{Config: models.ConfigItem{ID: uuid.New(), AgentID: dummy.GCPAgent.ID}},
				policy.ActionRead, false,
				"multi-scope user should NOT have read access to GCP agent configs"),
		)
	})

	Context("Permission to RLS E2E", func() {
		var (
			tx *gorm.DB

			// Expected counts calculated from dummy data
			guestUserConfigCount              int64
			guestUserPlaybookCount            int64
			guestUserNoPermsConfigCount       int64
			guestUserNoPermsPlaybookCount     int64
			guestUserMultiTargetConfigCount   int64
			guestUserMultiTargetPlaybookCount int64
			homelabManagerConfigCount         int64
			homelabDefaultManagerConfigCount  int64
			multiScopeUserConfigCount         int64
			totalConfigs                      int64
			totalPlaybooks                    int64
		)

		BeforeAll(func() {
			// Calculate expected counts from dummy data (without RLS)
			// Guest user: namespace in [missioncontrol, monitoring, media]
			DefaultContext.DB().
				Where("tags->>'namespace' IN ?", []string{"missioncontrol", "monitoring", "media"}).
				Model(&models.ConfigItem{}).
				Count(&guestUserConfigCount)

			// Guest user: playbooks [echo-config, restart-pod]
			DefaultContext.DB().
				Where("name IN ?", []string{"echo-config", "restart-pod"}).
				Model(&models.Playbook{}).
				Count(&guestUserPlaybookCount)

			// Multi-target user: namespace=database
			DefaultContext.DB().
				Where("tags->>'namespace' = ?", "database").
				Model(&models.ConfigItem{}).
				Count(&guestUserMultiTargetConfigCount)

			// Multi-target user: playbook=echo-config only
			DefaultContext.DB().
				Where("name = ?", "echo-config").
				Model(&models.Playbook{}).
				Count(&guestUserMultiTargetPlaybookCount)

			// Homelab manager: agent_id=HomelabAgent.ID
			DefaultContext.DB().
				Where("agent_id = ?", dummy.HomelabAgent.ID).
				Model(&models.ConfigItem{}).
				Count(&homelabManagerConfigCount)

			// Homelab default manager: agent_id=HomelabAgent.ID AND namespace=default (should be 3)
			DefaultContext.DB().
				Where("agent_id = ? AND tags->>'namespace' = ?", dummy.HomelabAgent.ID, "default").
				Model(&models.ConfigItem{}).
				Count(&homelabDefaultManagerConfigCount)

			// Multi-scope user: namespace IN (missioncontrol, monitoring) OR agent_id=HomelabAgent.ID
			var missionControlCount, monitoringCount int64
			DefaultContext.DB().
				Where("tags->>'namespace' = ?", "missioncontrol").
				Model(&models.ConfigItem{}).
				Count(&missionControlCount)
			DefaultContext.DB().
				Where("tags->>'namespace' = ?", "monitoring").
				Model(&models.ConfigItem{}).
				Count(&monitoringCount)
			multiScopeUserConfigCount = missionControlCount + monitoringCount + homelabManagerConfigCount

			// Wildcard manager: has wildcard "*" which matches all configs
			// Expected count is same as totalConfigs

			// Total counts for admin user verification
			DefaultContext.DB().Model(&models.ConfigItem{}).Count(&totalConfigs)
			DefaultContext.DB().Model(&models.Playbook{}).Count(&totalPlaybooks)

			// No-perms user should see 0 rows
			guestUserNoPermsConfigCount = 0
			guestUserNoPermsPlaybookCount = 0
		})

		BeforeEach(func() {
			// Create a fresh transaction for each test
			tx = DefaultContext.DB().Session(&gorm.Session{NewDB: true}).Begin(&sql.TxOptions{ReadOnly: true})

			// Set role to postgrest_api
			Expect(tx.Exec("SET LOCAL ROLE 'postgrest_api'").Error).To(BeNil())

			// Verify role is set correctly
			var currentRole string
			Expect(tx.Raw("SELECT CURRENT_USER").Scan(&currentRole).Error).To(BeNil())
			Expect(currentRole).To(Equal("postgrest_api"))
		})

		AfterEach(func() {
			Expect(tx.Rollback().Error).To(BeNil())
		})

		// Config items tests
		It("should allow guest user to see only configs in permitted namespaces (missioncontrol, monitoring, media)", func() {
			ctx := DefaultContext.WithUser(guestUser)

			payload, err := auth.GetRLSPayload(ctx)
			Expect(err).ToNot(HaveOccurred())

			Expect(payload.SetPostgresSessionRLS(tx)).To(BeNil())

			var count int64
			Expect(tx.Model(&models.ConfigItem{}).Count(&count).Error).To(BeNil())
			Expect(count).To(Equal(guestUserConfigCount), "guest user should see configs in missioncontrol, monitoring, and media namespaces")
		})

		It("should deny access to all configs for guest user with no permissions", func() {
			ctx := DefaultContext.WithUser(guestUserNoPerms)

			payload, err := auth.GetRLSPayload(ctx)
			Expect(err).ToNot(HaveOccurred())

			Expect(payload.SetPostgresSessionRLS(tx)).To(BeNil())

			var count int64
			Expect(tx.Model(&models.ConfigItem{}).Count(&count).Error).To(BeNil())
			Expect(count).To(Equal(guestUserNoPermsConfigCount), "guest user with no permissions should see no configs")
		})

		It("should allow multi-target guest user to see only database namespace configs", func() {
			ctx := DefaultContext.WithUser(guestUserMultiTarget)

			payload, err := auth.GetRLSPayload(ctx)
			Expect(err).ToNot(HaveOccurred())

			Expect(payload.SetPostgresSessionRLS(tx)).To(BeNil())

			var count int64
			Expect(tx.Model(&models.ConfigItem{}).Count(&count).Error).To(BeNil())
			Expect(count).To(Equal(guestUserMultiTargetConfigCount), "multi-target guest user should see only database namespace configs")
		})

		It("should allow homelab manager to see only configs with homelab agent", func() {
			ctx := DefaultContext.WithUser(homelabManager)

			payload, err := auth.GetRLSPayload(ctx)
			Expect(err).ToNot(HaveOccurred())

			Expect(payload.SetPostgresSessionRLS(tx)).To(BeNil())

			var count int64
			Expect(tx.Model(&models.ConfigItem{}).Count(&count).Error).To(BeNil())
			Expect(count).To(Equal(homelabManagerConfigCount), "homelab manager should see only configs with homelab agent")
		})

		It("should allow wildcard manager to see all configs (wildcard name '*')", func() {
			ctx := DefaultContext.WithUser(wildcardManager)

			payload, err := auth.GetRLSPayload(ctx)
			Expect(err).ToNot(HaveOccurred())

			Expect(payload.SetPostgresSessionRLS(tx)).To(BeNil())

			var count int64
			Expect(tx.Model(&models.ConfigItem{}).Count(&count).Error).To(BeNil())
			Expect(count).To(Equal(totalConfigs), "wildcard manager with '*' should see all configs")
		})

		It("should allow admin user to see all configs (RLS disabled)", func() {
			ctx := DefaultContext.WithUser(adminUser)

			payload, err := auth.GetRLSPayload(ctx)
			Expect(err).ToNot(HaveOccurred())

			Expect(payload.SetPostgresSessionRLS(tx)).To(BeNil())

			var count int64
			Expect(tx.Model(&models.ConfigItem{}).Count(&count).Error).To(BeNil())
			Expect(count).To(Equal(totalConfigs), "admin user should see all configs")
		})

		// Playbook tests
		It("should allow guest user to see permitted playbooks (echo-config, restart-pod)", func() {
			ctx := DefaultContext.WithUser(guestUser)

			payload, err := auth.GetRLSPayload(ctx)
			Expect(err).ToNot(HaveOccurred())

			Expect(payload.SetPostgresSessionRLS(tx)).To(BeNil())

			var count int64
			Expect(tx.Model(&models.Playbook{}).Count(&count).Error).To(BeNil())
			Expect(count).To(Equal(guestUserPlaybookCount), "guest user should see echo-config and restart-pod playbooks")
		})

		It("should deny access to all playbooks for guest user with no permissions", func() {
			ctx := DefaultContext.WithUser(guestUserNoPerms)

			payload, err := auth.GetRLSPayload(ctx)
			Expect(err).ToNot(HaveOccurred())

			Expect(payload.SetPostgresSessionRLS(tx)).To(BeNil())

			var count int64
			Expect(tx.Model(&models.Playbook{}).Count(&count).Error).To(BeNil())
			Expect(count).To(Equal(guestUserNoPermsPlaybookCount), "guest user with no permissions should see no playbooks")
		})

		It("should allow multi-target guest user to see only echo-config playbook", func() {
			ctx := DefaultContext.WithUser(guestUserMultiTarget)

			payload, err := auth.GetRLSPayload(ctx)
			Expect(err).ToNot(HaveOccurred())

			Expect(payload.SetPostgresSessionRLS(tx)).To(BeNil())

			var count int64
			Expect(tx.Model(&models.Playbook{}).Count(&count).Error).To(BeNil())
			Expect(count).To(Equal(guestUserMultiTargetPlaybookCount), "multi-target guest user should see only echo-config playbook")
		})

		It("should allow admin user to see all playbooks (RLS disabled)", func() {
			ctx := DefaultContext.WithUser(adminUser)

			payload, err := auth.GetRLSPayload(ctx)
			Expect(err).ToNot(HaveOccurred())

			Expect(payload.SetPostgresSessionRLS(tx)).To(BeNil())

			var count int64
			Expect(tx.Model(&models.Playbook{}).Count(&count).Error).To(BeNil())
			Expect(count).To(Equal(totalPlaybooks), "admin user should see all playbooks")
		})

		// Direct ID permissions tests
		It("should include direct ID-based playbook permission in RLS filtering", func() {
			ctx := DefaultContext.WithUser(guestUserDirectPerms)

			payload, err := auth.GetRLSPayload(ctx)
			Expect(err).ToNot(HaveOccurred())

			Expect(payload.SetPostgresSessionRLS(tx)).To(BeNil())

			// guestUserDirectPerms has direct permission to echo-config playbook by ID
			// They should see at least the echo-config playbook
			var count int64
			Expect(tx.Model(&models.Playbook{}).Where("id = ?", dummy.EchoConfig.ID).Count(&count).Error).To(BeNil())
			Expect(count).To(BeNumerically(">=", 1), "guest user with direct permissions should see echo-config playbook by ID")
		})

		// Specific resource verification tests
		It("should allow guest user to see specific config in permitted namespace (LogisticsDBRDS)", func() {
			ctx := DefaultContext.WithUser(guestUser)

			payload, err := auth.GetRLSPayload(ctx)
			Expect(err).ToNot(HaveOccurred())

			Expect(payload.SetPostgresSessionRLS(tx)).To(BeNil())

			var count int64
			Expect(tx.Model(&models.ConfigItem{}).Where("id = ?", dummy.LogisticsDBRDS.ID).Count(&count).Error).To(BeNil())
			Expect(count).To(Equal(int64(1)), "guest user should see LogisticsDBRDS (namespace=missioncontrol)")
		})

		It("should deny guest user access to specific config in unpermitted namespace (RedisHelmRelease)", func() {
			ctx := DefaultContext.WithUser(guestUser)

			payload, err := auth.GetRLSPayload(ctx)
			Expect(err).ToNot(HaveOccurred())

			Expect(payload.SetPostgresSessionRLS(tx)).To(BeNil())

			var count int64
			Expect(tx.Model(&models.ConfigItem{}).Where("id = ?", dummy.RedisHelmRelease.ID).Count(&count).Error).To(BeNil())
			Expect(count).To(Equal(int64(0)), "guest user should NOT see RedisHelmRelease (namespace=database)")
		})

		It("should allow multi-target user to see specific config in permitted namespace (RedisHelmRelease)", func() {
			ctx := DefaultContext.WithUser(guestUserMultiTarget)

			payload, err := auth.GetRLSPayload(ctx)
			Expect(err).ToNot(HaveOccurred())

			Expect(payload.SetPostgresSessionRLS(tx)).To(BeNil())

			var count int64
			Expect(tx.Model(&models.ConfigItem{}).Where("id = ?", dummy.RedisHelmRelease.ID).Count(&count).Error).To(BeNil())
			Expect(count).To(Equal(int64(1)), "multi-target user should see RedisHelmRelease (namespace=database)")
		})

		It("should deny multi-target user access to specific config in unpermitted namespace (LogisticsDBRDS)", func() {
			ctx := DefaultContext.WithUser(guestUserMultiTarget)

			payload, err := auth.GetRLSPayload(ctx)
			Expect(err).ToNot(HaveOccurred())

			Expect(payload.SetPostgresSessionRLS(tx)).To(BeNil())

			var count int64
			Expect(tx.Model(&models.ConfigItem{}).Where("id = ?", dummy.LogisticsDBRDS.ID).Count(&count).Error).To(BeNil())
			Expect(count).To(Equal(int64(0)), "multi-target user should NOT see LogisticsDBRDS (namespace=missioncontrol)")
		})

		It("should allow homelab manager to see config with homelab agent", func() {
			ctx := DefaultContext.WithUser(homelabManager)

			payload, err := auth.GetRLSPayload(ctx)
			Expect(err).ToNot(HaveOccurred())

			Expect(payload.SetPostgresSessionRLS(tx)).To(BeNil())

			// Find a config with homelab agent
			var config models.ConfigItem
			err = DefaultContext.DB().Where("agent_id = ?", dummy.HomelabAgent.ID).First(&config).Error
			Expect(err).ToNot(HaveOccurred(), "should find at least one config with homelab agent")

			var count int64
			Expect(tx.Model(&models.ConfigItem{}).Where("id = ?", config.ID).Count(&count).Error).To(BeNil())
			Expect(count).To(Equal(int64(1)), "homelab manager should see config with homelab agent")
		})

		It("should deny homelab manager access to config without homelab agent", func() {
			ctx := DefaultContext.WithUser(homelabManager)

			payload, err := auth.GetRLSPayload(ctx)
			Expect(err).ToNot(HaveOccurred())

			Expect(payload.SetPostgresSessionRLS(tx)).To(BeNil())

			// LogisticsDBRDS doesn't have homelab agent
			var count int64
			Expect(tx.Model(&models.ConfigItem{}).Where("id = ?", dummy.LogisticsDBRDS.ID).Count(&count).Error).To(BeNil())
			Expect(count).To(Equal(int64(0)), "homelab manager should NOT see LogisticsDBRDS (no homelab agent)")
		})

		It("should allow guestUserDirectPerms to see NginxIngressPod via direct ID permission", func() {
			ctx := DefaultContext.WithUser(guestUserDirectPerms)

			payload, err := auth.GetRLSPayload(ctx)
			Expect(err).ToNot(HaveOccurred())

			Expect(payload.SetPostgresSessionRLS(tx)).To(BeNil())

			// guestUserDirectPerms has direct permission to NginxIngressPod config by ID
			var count int64
			Expect(tx.Model(&models.ConfigItem{}).Where("id = ?", dummy.NginxIngressPod.ID).Count(&count).Error).To(BeNil())
			Expect(count).To(Equal(int64(1)), "guest user with direct permissions should see NginxIngressPod config by ID")
		})

		It("should allow homelab default manager to see only configs with homelab agent AND default namespace (combined scope)", func() {
			ctx := DefaultContext.WithUser(homelabDefaultManager)

			payload, err := auth.GetRLSPayload(ctx)
			Expect(err).ToNot(HaveOccurred())

			Expect(payload.SetPostgresSessionRLS(tx)).To(BeNil())

			var count int64
			Expect(tx.Model(&models.ConfigItem{}).Count(&count).Error).To(BeNil())
			Expect(count).To(Equal(homelabDefaultManagerConfigCount), "homelab default manager should see exactly 3 configs (homelab agent + default namespace)")
		})

		It("should allow multi-scope user to see configs from ALL three scopes (OR behavior)", func() {
			ctx := DefaultContext.WithUser(multiScopeUser)

			payload, err := auth.GetRLSPayload(ctx)
			Expect(err).ToNot(HaveOccurred())

			Expect(payload.SetPostgresSessionRLS(tx)).To(BeNil())

			var count int64
			Expect(tx.Model(&models.ConfigItem{}).Count(&count).Error).To(BeNil())
			Expect(count).To(Equal(multiScopeUserConfigCount), "multi-scope user should see all configs from missioncontrol, monitoring, and homelab agent (OR behavior)")
		})

		// View RLS tests
		It("should filter views based on RLS for users with view permissions", func() {
			// Admin should see all views
			ctx := DefaultContext.WithUser(adminUser)

			payload, err := auth.GetRLSPayload(ctx)
			Expect(err).ToNot(HaveOccurred())

			Expect(payload.SetPostgresSessionRLS(tx)).To(BeNil())

			var adminViewCount int64
			Expect(tx.Model(&models.View{}).Where("deleted_at IS NULL").Count(&adminViewCount).Error).To(BeNil())
			Expect(adminViewCount).To(BeNumerically(">", 0), "admin user should see all views")

			// Guest user with no permissions should see no views
			ctx = DefaultContext.WithUser(guestUserNoPerms)

			payload, err = auth.GetRLSPayload(ctx)
			Expect(err).ToNot(HaveOccurred())

			Expect(payload.SetPostgresSessionRLS(tx)).To(BeNil())

			var guestNoPermsViewCount int64
			Expect(tx.Model(&models.View{}).Where("deleted_at IS NULL").Count(&guestNoPermsViewCount).Error).To(BeNil())
			Expect(guestNoPermsViewCount).To(Equal(int64(0)), "guest user with no permissions should see no views")
		})
	})

	Describe("View ABAC Permissions", func() {
		DescribeTable("admin user with view permissions",
			func(view models.View, description string) {
				attr := &models.ABACAttribute{
					View: view,
				}
				allowed := rbac.HasPermission(DefaultContext, adminUser.ID.String(), attr, policy.ActionRead)
				Expect(allowed).To(BeTrue(), description)
			},
			Entry("should have access to pods view",
				dummy.PodView,
				"admin user should have read access to pods view"),
			Entry("should have access to dev dashboard view",
				dummy.ViewDev,
				"admin user should have read access to dev dashboard view"),
		)

		DescribeTable("guest user with no permissions",
			func(view models.View, description string) {
				attr := &models.ABACAttribute{
					View: view,
				}
				allowed := rbac.HasPermission(DefaultContext, guestUserNoPerms.ID.String(), attr, policy.ActionRead)
				Expect(allowed).To(BeFalse(), description)
			},
			Entry("should NOT have access to pods view",
				dummy.PodView,
				"guest user with no permissions should NOT have read access to pods view"),
			Entry("should NOT have access to dev dashboard view",
				dummy.ViewDev,
				"guest user with no permissions should NOT have read access to dev dashboard view"),
		)

		DescribeTable("user with metrics view permission",
			func(view models.View, action string, expectedAllowed bool, description string) {
				attr := &models.ABACAttribute{
					View: view,
				}
				allowed := rbac.HasPermission(DefaultContext, userMetrics.ID.String(), attr, action)
				Expect(allowed).To(Equal(expectedAllowed), description)
			},
			Entry("should have read access to metrics view",
				dummy.ImportedDummyViews["mc/metrics"],
				policy.ActionRead, true,
				"user with metrics permission should have read access to metrics view"),
			Entry("should NOT have read access to pods view",
				dummy.PodView,
				policy.ActionRead, false,
				"user with metrics permission should NOT have read access to pods view"),
			Entry("should NOT have read access to dev dashboard view",
				dummy.ViewDev,
				policy.ActionRead, false,
				"user with metrics permission should NOT have read access to dev dashboard view"),
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

func createDirectPermissions(userID string) []*models.Permission {
	// Create direct ID-based permissions for guest user with direct permissions
	// These test permissions that use specific resource IDs instead of object_selector.

	directPlaybookPermission := &models.Permission{
		ID:          uuid.New(),
		Name:        "direct-playbook-permission",
		Namespace:   "default",
		Action:      policy.ActionRead,
		Subject:     userID,
		SubjectType: models.PermissionSubjectTypePerson,
		PlaybookID:  &dummy.EchoConfig.ID,
	}
	err := DefaultContext.DB().Create(directPlaybookPermission).Error
	Expect(err).ToNot(HaveOccurred())

	directCanaryPermission := &models.Permission{
		ID:          uuid.New(),
		Name:        "direct-canary-permission",
		Namespace:   "default",
		Action:      policy.ActionRead,
		Subject:     userID,
		SubjectType: models.PermissionSubjectTypePerson,
		CanaryID:    &dummy.LogisticsAPICanary.ID,
	}
	err = DefaultContext.DB().Create(directCanaryPermission).Error
	Expect(err).ToNot(HaveOccurred())

	directComponentPermission := &models.Permission{
		ID:          uuid.New(),
		Name:        "direct-component-permission",
		Namespace:   "default",
		Action:      policy.ActionRead,
		Subject:     userID,
		SubjectType: models.PermissionSubjectTypePerson,
		ComponentID: &dummy.Logistics.ID,
	}
	err = DefaultContext.DB().Create(directComponentPermission).Error
	Expect(err).ToNot(HaveOccurred())

	directConfigPermission := &models.Permission{
		ID:          uuid.New(),
		Name:        "direct-config-permission",
		Namespace:   "default",
		Action:      policy.ActionRead,
		Subject:     userID,
		SubjectType: models.PermissionSubjectTypePerson,
		ConfigID:    &dummy.NginxIngressPod.ID,
	}
	err = DefaultContext.DB().Create(directConfigPermission).Error
	Expect(err).ToNot(HaveOccurred())

	return []*models.Permission{directPlaybookPermission, directCanaryPermission, directComponentPermission, directConfigPermission}
}
