package permissions_test

import (
	"database/sql"
	"os"

	icrbac "github.com/flanksource/incident-commander/rbac"

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
	"gorm.io/gorm"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sTypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/yaml"

	v1 "github.com/flanksource/incident-commander/api/v1"
	"github.com/flanksource/incident-commander/auth"
	"github.com/flanksource/incident-commander/db"
)

var _ = Describe("Permissions", Ordered, ContinueOnFailure, func() {
	var (
		guestUser             *models.Person
		guestUserNoPerms      *models.Person
		guestUserMultiTarget  *models.Person
		homelabManager        *models.Person
		wildcardManager       *models.Person
		adminUser             *models.Person
		multiScopeUser        *models.Person
		homelabDefaultManager *models.Person
	)

	var bindings []*v1.RoleBinding

	BeforeAll(func() {
		err := icrbac.Init(DefaultContext)
		Expect(err).ToNot(HaveOccurred())

		guestUser = setup.CreateUserWithRole(DefaultContext, "Guest User", "guest@test.com")
		guestUserNoPerms = setup.CreateUserWithRole(DefaultContext, "Guest User No Permissions", "guest-noperms@test.com")
		guestUserMultiTarget = setup.CreateUserWithRole(DefaultContext, "Guest User Multi Target", "guest-multi@test.com")
		homelabManager = setup.CreateUserWithRole(DefaultContext, "Homelab Manager", "manager@homelab.com")
		homelabDefaultManager = setup.CreateUserWithRole(DefaultContext, "Homelab Default Manager", "homelab-default@manager.com")
		multiScopeUser = setup.CreateUserWithRole(DefaultContext, "Multi Scope User", "multi-scope@test.com")
		wildcardManager = setup.CreateUserWithRole(DefaultContext, "Wildcard Manager", "wildcard@manager.com")
		adminUser = newAdmin("Admin User", "admin@test.com")

		loadScopes()
		for _, reader := range []struct {
			person *models.Person
			scopes []string
		}{
			{guestUser, []string{"missioncontrol-configs", "monitoring-configs", "restart-pod-playbook"}},
			{guestUserMultiTarget, []string{"multi-target-scope"}},
			{homelabManager, []string{"homelab-configs"}},
			{homelabDefaultManager, []string{"homelab-default-configs"}},
			{multiScopeUser, []string{"missioncontrol-configs", "monitoring-configs", "homelab-configs"}},
			{wildcardManager, []string{"wildcard-all-configs"}},
		} {
			bindings = append(bindings, bindReader(reader.person, reader.scopes...))
		}

		Expect(rbac.ReloadPolicy()).ToNot(HaveOccurred())
	})

	AfterAll(func() {
		for _, binding := range bindings {
			Expect(DefaultContext.DB().Where("id = ?", binding.UID).Delete(&models.RoleBinding{}).Error).To(Succeed())
			Expect(DefaultContext.DB().Where("namespace = ? AND name = ?", binding.Namespace, binding.Spec.Role).Delete(&models.Role{}).Error).To(Succeed())
		}

		// Clean up users
		users := []*models.Person{guestUser, guestUserNoPerms, guestUserMultiTarget, homelabManager, homelabDefaultManager, multiScopeUser, wildcardManager, adminUser}
		for _, user := range users {
			if user != nil {
				err := DefaultContext.DB().Delete(user).Error
				Expect(err).ToNot(HaveOccurred())
			}
		}

		Expect(rbac.ReloadPolicy()).ToNot(HaveOccurred())
	})

	Context("RLS translation", func() {
		const (
			missioncontrolScope  = "db80e7f5-b6af-4896-8431-8d6e5430c6f2"
			monitoringScope      = "eb5a2647-f377-4fd9-83fd-07020e761740"
			homelabScope         = "3c4e5f6a-7b8c-4d9e-0f1a-2b3c4d5e6f7a"
			homelabDefaultScope  = "7a8b9c0d-1e2f-4a3b-5c6d-7e8f9a0b1c2d"
			multiTargetScope     = "a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d"
			restartPodScope      = "f1e2d3c4-b5a6-4978-8abc-def012345678"
			wildcardConfigsScope = "f1e2d3c4-b5a6-4c7d-8e9f-0a1b2c3d4e5f"
		)

		grants := func(scopeIDs ...string) *rls.Grants {
			g := rls.NoRows()
			for _, id := range scopeIDs {
				g.Add(rls.Grant{Scope: id})
			}
			return g
		}

		It("should grant rows only through the Scopes a guest's Role reads", func() {
			payload, err := auth.GetRLSPayload(DefaultContext.WithUser(guestUser))
			Expect(err).ToNot(HaveOccurred())

			Expect(payload.Disable).To(BeFalse(), "RLS should be enabled for guest users with scopes")
			Expect(payload.Config).To(Equal(grants(missioncontrolScope, monitoringScope)))
			Expect(payload.Playbook).To(Equal(grants(restartPodScope)))
			Expect(payload.View).To(BeNil())
			Expect(payload.Component).To(BeNil())
			Expect(payload.Canary).To(BeNil())
		})

		It("should grant each type a multi-target Scope selects", func() {
			payload, err := auth.GetRLSPayload(DefaultContext.WithUser(guestUserMultiTarget))
			Expect(err).ToNot(HaveOccurred())

			Expect(payload).To(Equal(&rls.Payload{
				Config:   grants(multiTargetScope),
				Playbook: grants(multiTargetScope),
				View:     grants(multiTargetScope),
			}))
		})

		for _, tc := range []struct {
			name  string
			user  func() *models.Person
			scope string
		}{
			{"an agent-based Scope", func() *models.Person { return homelabManager }, homelabScope},
			{"a whole-type Scope", func() *models.Person { return wildcardManager }, wildcardConfigsScope},
			{"a combined agent and tag Scope", func() *models.Person { return homelabDefaultManager }, homelabDefaultScope},
		} {
			It("should grant the rows of "+tc.name, func() {
				payload, err := auth.GetRLSPayload(DefaultContext.WithUser(tc.user()))
				Expect(err).ToNot(HaveOccurred())
				Expect(payload).To(Equal(&rls.Payload{Config: grants(tc.scope)}))
			})
		}

		It("should grant the rows of any of several Scopes", func() {
			payload, err := auth.GetRLSPayload(DefaultContext.WithUser(multiScopeUser))
			Expect(err).ToNot(HaveOccurred())

			Expect(payload.Config).To(Equal(grants(missioncontrolScope, monitoringScope, homelabScope)))
			Expect(payload.Playbook).To(BeNil())
			Expect(payload.Component).To(BeNil())
			Expect(payload.Canary).To(BeNil())
			Expect(payload.View).To(BeNil())
		})

		It("should filter an admin's rows by their grants, like any subject's", func() {
			payload, err := auth.GetRLSPayload(DefaultContext.WithUser(adminUser))
			Expect(err).ToNot(HaveOccurred())
			Expect(payload.Disable).To(BeFalse())
			Expect(payload.Config).ToNot(BeNil())
			Expect(payload.Config.Any).ToNot(BeEmpty())
		})

		It("should grant no rows to a guest user with no permissions", func() {
			payload, err := auth.GetRLSPayload(DefaultContext.WithUser(guestUserNoPerms))
			Expect(err).ToNot(HaveOccurred())

			Expect(payload.Disable).To(BeFalse(), "RLS should be enabled for guest users even without permissions")
			Expect(payload).To(Equal(&rls.Payload{}))
		})
	})

	Context("Casbin policy translation", func() {
		DescribeTable("guest user with some permissions",
			func(attr models.ABACAttribute, action string, expectedAllowed bool, description string) {
				storeConfig(attr)
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
			// Config read access - denied (not in scopes)
			Entry("should deny read access to kube-system namespace config",
				models.ABACAttribute{Config: models.ConfigItem{ID: uuid.New(), Tags: map[string]string{"namespace": "kube-system"}}},
				policy.ActionRead, false,
				"guest user should NOT have read access to kube-system namespace configs"),
			Entry("should deny read access to configs with no namespace tag",
				models.ABACAttribute{Config: models.ConfigItem{ID: uuid.New(), Name: lo.ToPtr("notag")}},
				policy.ActionRead, false,
				"guest user should NOT have read access to configs with no namespace tag"),

			// Playbook read access - allowed via scope for restart-pod
			Entry("should allow read access to restart-pod playbook via scope",
				models.ABACAttribute{Playbook: models.Playbook{ID: dummy.RestartPod.ID, Name: "restart-pod"}},
				policy.ActionRead, true,
				"guest user should have read access to restart-pod playbook via scope"),

			// Playbook read access - denied for other playbooks
			Entry("should deny read access to other playbooks",
				models.ABACAttribute{Playbook: models.Playbook{ID: uuid.New(), Name: "other-playbook"}},
				policy.ActionRead, false,
				"guest user should NOT have read access to other playbooks"),

			// Playbook run access - denied (only read granted)
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
				storeConfig(attr)
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
				storeConfig(attr)
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
				storeConfig(attr)
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
				storeConfig(attr)
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
				storeConfig(attr)
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
				storeConfig(attr)
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
				storeConfig(attr)
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

	Context("RLS E2E", func() {
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
			// Guest user: namespace in [missioncontrol, monitoring], through Scopes.
			DefaultContext.DB().
				Where("tags->>'namespace' IN ?", []string{"missioncontrol", "monitoring"}).
				Model(&models.ConfigItem{}).
				Count(&guestUserConfigCount)

			// Guest user: playbook restart-pod, through a Scope.
			DefaultContext.DB().
				Where("name IN ?", []string{"restart-pod"}).
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
		It("should allow guest user to see only configs of the Scopes it's granted (missioncontrol, monitoring)", func() {
			ctx := DefaultContext.WithUser(guestUser)

			payload, err := auth.GetRLSPayload(ctx)
			Expect(err).ToNot(HaveOccurred())

			Expect(payload.SetPostgresSessionRLS(tx)).To(BeNil())

			var count int64
			Expect(tx.Model(&models.ConfigItem{}).Count(&count).Error).To(BeNil())
			Expect(count).To(Equal(guestUserConfigCount), "guest user should see configs in missioncontrol and monitoring namespaces")
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
		It("should allow guest user to see the playbooks of the Scopes it's granted (restart-pod)", func() {
			ctx := DefaultContext.WithUser(guestUser)

			payload, err := auth.GetRLSPayload(ctx)
			Expect(err).ToNot(HaveOccurred())

			Expect(payload.SetPostgresSessionRLS(tx)).To(BeNil())

			var count int64
			Expect(tx.Model(&models.Playbook{}).Count(&count).Error).To(BeNil())
			Expect(count).To(Equal(guestUserPlaybookCount), "guest user should see the restart-pod playbook")
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
			// Guest user with no permissions should see no views
			ctx := DefaultContext.WithUser(guestUserNoPerms)

			payload, err := auth.GetRLSPayload(ctx)
			Expect(err).ToNot(HaveOccurred())

			Expect(payload.SetPostgresSessionRLS(tx)).To(BeNil())

			var guestNoPermsViewCount int64
			Expect(tx.Model(&models.View{}).Where("deleted_at IS NULL").Count(&guestNoPermsViewCount).Error).To(BeNil())
			Expect(guestNoPermsViewCount).To(Equal(int64(0)), "guest user with no permissions should see no views")
		})
	})

	Describe("View ABAC Permissions", func() {
		DescribeTable("admin user",
			func(view models.View, description string) {
				attr := &models.ABACAttribute{
					View: view,
				}
				allowed := rbac.HasPermission(DefaultContext, adminUser.ID.String(), attr, policy.ActionRead)
				Expect(allowed).To(BeTrue(), description)
			},
			Entry("should have access to pods view",
				dummy.PodView,
				"the admin Role reads every view"),
			Entry("should have access to dev dashboard view",
				dummy.ViewDev,
				"the admin Role reads every view"),
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

// bindReader creates a Role with a read rule on each of the Scopes, and binds the person to it.
func bindReader(person *models.Person, scopes ...string) *v1.RoleBinding {
	GinkgoHelper()
	name := "permissions-reader-" + person.ID.String()
	role := &v1.Role{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default", UID: k8sTypes.UID(uuid.NewString())},
		Spec: v1.RoleSpec{Rules: lo.Map(scopes, func(scope string, _ int) v1.RoleRule {
			return v1.RoleRule{Name: scope, Action: policy.ActionRead, Resource: &v1.ScopeReference{ScopeRef: scope}}
		})},
	}
	Expect(db.PersistRoleFromCRD(DefaultContext, role)).To(Succeed())

	binding := &v1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default", UID: k8sTypes.UID(uuid.NewString())},
		Spec:       v1.RoleBindingSpec{Role: name, Subjects: boundPeople(person)},
	}
	Expect(db.PersistRoleBindingFromCRD(DefaultContext, binding)).To(Succeed())
	return binding
}

// storeConfig stores the request's config, if any, which matches it to its Scopes:
// checks read stored membership, so a config that isn't stored is in no Scope.
func storeConfig(attr models.ABACAttribute) {
	GinkgoHelper()
	if attr.Config.ID == uuid.Nil {
		return
	}

	config := attr.Config
	config.Name = lo.CoalesceOrEmpty(config.Name, lo.ToPtr("permissions-test"))
	config.Type = lo.ToPtr("Permissions::Test")
	config.ConfigClass = "Test"
	Expect(DefaultContext.DB().Create(&config).Error).To(Succeed())

	DeferCleanup(func() {
		Expect(DefaultContext.DB().Delete(&models.ConfigItem{}, "id = ?", config.ID).Error).To(Succeed())
	})
}
